package integration

import (
	"github.com/ceserve/courier-os/tests/harness"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Assert public transport shape in addition to the service tests' arithmetic.
// Raw sqlc rows previously produced capitalized fields and numeric IDs, which
// left finance screens blank and sent follow-up actions to /undefined.
func financeHTTP(t *testing.T, env *harness.Env, token, method, path string, body any) map[string]any {
	t.Helper()
	res := env.Do(t, method, path, token, body)
	if res.Status < 200 || res.Status >= 300 {
		t.Fatalf("%s %s: %d %s", method, path, res.Status, res.Raw)
	}
	assertNoDatabaseFields(t, res.Body)
	return res.Body
}
func assertNoDatabaseFields(t *testing.T, value any) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key != "" && key[0] >= 'A' && key[0] <= 'Z' {
				t.Errorf("database field leaked: %s", key)
			}
			if key == "organizationId" || key == "requestId" {
				t.Errorf("internal field leaked: %s", key)
			}
			assertNoDatabaseFields(t, child)
		}
	case []any:
		for _, child := range v {
			assertNoDatabaseFields(t, child)
		}
	}
}
func publicID(t *testing.T, row map[string]any, prefix string) string {
	t.Helper()
	id, _ := row["id"].(string)
	if !strings.HasPrefix(id, prefix+"_") {
		t.Fatalf("missing public %s ID: %#v", prefix, row)
	}
	return id
}
func firstRow(t *testing.T, payload map[string]any, field string) map[string]any {
	t.Helper()
	rows, ok := payload[field].([]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("missing %s rows: %#v", field, payload)
	}
	return rows[0].(map[string]any)
}
func TestSettlementAndCODResponsesMatchPublicContract(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "FINCONTRACT"})
	_, franchiseID := franchiseFixtureAt(t, env, tn, tn.DestBranchID)
	j, runID := codJourney(t, env, tn, 90000)
	j.post(t, "/api/v1/deliveries/attempts", map[string]any{"barcode": j.awb, "outcome": "DELIVERED", "runId": runID, "recipientName": "QA Recipient", "recipientRelationship": "SELF", "codCollectedMinor": 90000, "codPaymentMode": "CASH"}, 200, [2]string{"Idempotency-Key", harness.RandomKey()})
	token := env.Login(t, tn.AdminEmail, tn.AdminPassword)
	do := func(method, path string, body any) map[string]any {
		return financeHTTP(t, env, token, method, path, body)
	}
	var shipmentPublicID string
	mustQueryRow(t, env, `SELECT public_id FROM shipments WHERE awb=$1`, []any{j.awb}, &shipmentPublicID)
	collected := financeHTTP(t, env, j.token, "POST", "/api/v1/cod/collections", map[string]any{"shipmentId": shipmentPublicID, "amountMinor": 90000, "paymentMode": "CASH", "reference": "QA_CONTRACT"})
	publicID(t, collected["collection"].(map[string]any), "cdc")
	ob := firstRow(t, do("GET", "/api/v1/cod/obligations", nil), "data")
	obID := publicID(t, ob, "cod")
	if ob["awb"] != j.awb || ob["collectedMinor"] != float64(90000) {
		t.Fatalf("COD amount/AWB lost: %#v", ob)
	}
	detail := do("GET", "/api/v1/cod/obligations/"+obID, nil)
	collection := firstRow(t, detail, "collections")
	publicID(t, collection, "cdc")
	if collection["amountMinor"] != float64(90000) {
		t.Fatal("collection amount lost")
	}
	today := time.Now().UTC().Format("2006-01-02")
	req := map[string]any{"franchiseId": franchiseID, "periodStart": today, "periodEnd": today}
	generated := do("POST", "/api/v1/settlements", req)
	stl := generated["settlement"].(map[string]any)
	id := publicID(t, stl, "stl")
	if stl["franchiseName"] != "Test Franchise" || stl["commissionMinor"] != float64(4250) || stl["periodStart"] != today {
		t.Fatalf("settlement fields: %#v", stl)
	}
	line := firstRow(t, generated, "lines")
	publicID(t, line, "sln")
	if line["sourcePublicId"] == nil || line["awb"] != j.awb {
		t.Fatalf("source references lost: %#v", line)
	}
	if do("POST", "/api/v1/settlements", req)["replayed"] != true {
		t.Fatal("generation replay lost")
	}
	if publicID(t, firstRow(t, do("GET", "/api/v1/settlements", nil), "data"), "stl") != id {
		t.Fatal("list ID differs")
	}
	detail = do("GET", "/api/v1/settlements/"+id, nil)
	if publicID(t, detail["settlement"].(map[string]any), "stl") != id {
		t.Fatal("detail ID differs")
	}
	do("POST", "/api/v1/settlements/"+id+"/submit", map[string]any{"comment": "QA review"})
	denied := env.Do(t, "POST", "/api/v1/settlements/"+id+"/approve", token, map[string]any{"comment": "same person"})
	if denied.Status != http.StatusForbidden {
		t.Fatalf("maker/checker bypass: %d", denied.Status)
	}
	_, _, checkerToken := env.NewUser(t, tn.OrgID, "finance-checker-contract@test.local", "FINANCE_MANAGER", nil)
	approved := financeHTTP(t, env, checkerToken, "POST", "/api/v1/settlements/"+id+"/approve", map[string]any{"comment": "QA independent review"})
	if approved["status"] != "APPROVED" {
		t.Fatalf("approve response: %#v", approved)
	}
	other := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "FINOTHER"})
	for _, path := range []string{"/api/v1/settlements/" + id, "/api/v1/cod/obligations/" + obID} {
		if r := env.Do(t, "GET", path, other.AdminAccessTok, nil); r.Status != 404 {
			t.Fatalf("cross-tenant read %s: %d", path, r.Status)
		}
	}
}
func TestInvoiceAndJournalResponsesMatchPublicContract(t *testing.T) {
	env, tn, _ := financeEnv(t)
	token := env.Login(t, tn.AdminEmail, tn.AdminPassword)
	do := func(method, path string, body any) map[string]any {
		return financeHTTP(t, env, token, method, path, body)
	}
	customerID, customerPubID := firstCustomer(t, env, tn)
	start, end := monthWindow()
	billableShipment(t, env, tn, customerID, 50000, 7500, start)
	draft := do("POST", "/api/v1/invoices", map[string]any{"customerId": customerPubID, "periodStart": start.Format("2006-01-02"), "periodEnd": end.Format("2006-01-02"), "taxes": []map[string]any{{"code": "QA_TAX", "name": "QA tax", "rateBp": 1800}}})
	invoice := draft["invoice"].(map[string]any)
	id := publicID(t, invoice, "inv")
	if invoice["customerName"] == "" || invoice["customerCode"] == "" || invoice["totalMinor"] != float64(67850) {
		t.Fatalf("invoice fields: %#v", invoice)
	}
	line := firstRow(t, draft, "lines")
	publicID(t, line, "ivl")
	if line["awb"] == nil || line["chargeSnapshotId"] == nil {
		t.Fatalf("invoice source references: %#v", line)
	}
	tax := firstRow(t, draft, "taxes")
	if tax["taxMinor"] != float64(10350) {
		t.Fatal("tax response wrong")
	}
	if publicID(t, firstRow(t, do("GET", "/api/v1/invoices", nil), "data"), "inv") != id {
		t.Fatal("invoice list ID differs")
	}
	do("GET", "/api/v1/invoices/"+id, nil)
	issued := do("POST", "/api/v1/invoices/"+id+"/issue", nil)
	if issued["status"] != "ISSUED" || !strings.HasPrefix(issued["invoiceNumber"].(string), "INV/") {
		t.Fatalf("issue response: %#v", issued)
	}
	journal := firstRow(t, do("GET", "/api/v1/ledger/journals", nil), "data")
	jid := publicID(t, journal, "jrn")
	if journal["sourceId"] != id {
		t.Fatalf("journal must link public source ID: %#v", journal)
	}
	detail := do("GET", "/api/v1/ledger/journals/"+jid, nil)
	rows := detail["entries"].([]any)
	var debit, credit float64
	for _, value := range rows {
		entry := value.(map[string]any)
		publicID(t, entry, "jen")
		if entry["accountCode"] == "" {
			t.Fatal("entry account missing")
		}
		debit += entry["debitMinor"].(float64)
		credit += entry["creditMinor"].(float64)
	}
	if debit != 67850 || debit != credit {
		t.Fatalf("journal amounts %v/%v", debit, credit)
	}
	period := firstRow(t, do("GET", "/api/v1/ledger/periods", nil), "data")
	publicID(t, period, "acp")
	if len(period["startsOn"].(string)) != 10 {
		t.Fatal("period date not date-only")
	}
	note := do("POST", "/api/v1/credit-notes", map[string]any{"invoiceId": id, "reasonCode": "OTHER", "reason": "QA contract correction", "amountMinor": 1000})
	noteID := publicID(t, note, "crn")
	if note["invoiceNumber"] != issued["invoiceNumber"] {
		t.Fatal("credit note invoice link lost")
	}
	second := do("POST", "/api/v1/credit-notes", map[string]any{"invoiceId": id, "reasonCode": "OTHER", "reason": "QA second draft", "amountMinor": 500})
	secondID := publicID(t, second, "crn")
	notePath := "/api/v1/invoices/" + id + "/credit-notes"
	page := do("GET", notePath+"?limit=1", nil)
	if publicID(t, firstRow(t, page, "data"), "crn") != secondID || page["pagination"].(map[string]any)["hasMore"] != true {
		t.Fatal("notes must paginate newest first")
	}
	next := page["pagination"].(map[string]any)["nextCursor"].(string)
	page = do("GET", notePath+"?limit=1&cursor="+next, nil)
	if publicID(t, firstRow(t, page, "data"), "crn") != noteID || page["pagination"].(map[string]any)["hasMore"] != false {
		t.Fatal("note cursor skipped or repeated a draft")
	}
	if res := env.Do(t, "POST", "/api/v1/credit-notes/"+noteID+"/issue", token, nil); res.Status != http.StatusForbidden {
		t.Fatalf("maker issued own note: %d", res.Status)
	}
	_, _, checker := env.NewUser(t, tn.OrgID, "note-checker-contract@test.local", "FINANCE_MANAGER", nil)
	financeHTTP(t, env, checker, "GET", notePath, nil)
	checked := financeHTTP(t, env, checker, "POST", "/api/v1/credit-notes/"+noteID+"/issue", nil)
	if checked["status"] != "ISSUED" {
		t.Fatal("checker could not issue retrieved note")
	}
	paid := do("POST", "/api/v1/invoices/"+id+"/payments", map[string]any{"amountMinor": 1000, "paymentMode": "CASH", "reference": "SYNTHETIC_TEST"})
	publicID(t, paid["payment"].(map[string]any), "ipm")
	// Tenant filtering remains at the joined header read, including mutation results.
	other := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "BILOTHER"})
	for _, path := range []string{"/api/v1/invoices/" + id, "/api/v1/ledger/journals/" + jid, notePath, notePath + "?cursor=" + secondID} {
		if r := env.Do(t, "GET", path, other.AdminAccessTok, nil); r.Status != 404 {
			t.Fatalf("cross-tenant read %s: %d", path, r.Status)
		}
	}
}
