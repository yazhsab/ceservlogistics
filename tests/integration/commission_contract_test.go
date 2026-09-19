package integration

import (
	"github.com/ceserve/courier-os/tests/harness"
	"net/http"
	"strings"
	"testing"
	"time"
)

func assertCommissionPublicFields(t *testing.T, body map[string]any, prefix string) string {
	t.Helper()
	id, _ := body["id"].(string)
	if !strings.HasPrefix(id, prefix+"_") {
		t.Fatalf("missing public id: %#v", body)
	}
	for _, key := range []string{"ID", "PublicID", "OrganizationID", "CreatedBy", "RuleID", "RecipientID"} {
		if _, ok := body[key]; ok {
			t.Errorf("database field %s leaked", key)
		}
	}
	return id
}

func TestCommissionRuleResponsesMatchPublicContract(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "CRCONTRACT"})
	created := env.Do(t, http.MethodPost, "/api/v1/commission/rules", tn.AdminAccessTok, map[string]any{
		"code": "QA_RULE", "name": "Test delivery rule", "commissionType": "DELIVERY", "recipientRole": "DELIVERY_AGENT", "serviceCode": tn.ServiceCode,
	})
	if created.Status != 201 {
		t.Fatalf("create: %d %s", created.Status, created.Raw)
	}
	id := assertCommissionPublicFields(t, created.Body, "crl")
	if created.Body["schemeCode"] != "STANDARD" || created.Body["commissionType"] != "DELIVERY" {
		t.Fatalf("rule fields: %#v", created.Body)
	}
	listing := env.Do(t, http.MethodGet, "/api/v1/commission/rules", tn.AdminAccessTok, nil)
	rows := listing.Body["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows: %#v", listing.Body)
	}
	if assertCommissionPublicFields(t, rows[0].(map[string]any), "crl") != id {
		t.Fatal("list ID differs")
	}
	date := time.Now().UTC().Format("2006-01-02")
	version := env.Do(t, http.MethodPost, "/api/v1/commission/rules/"+id+"/versions", tn.AdminAccessTok, map[string]any{
		"calculationMethod": "FIXED", "fixedAmountMinor": 1250, "effectiveFrom": date,
	})
	if version.Status != 201 {
		t.Fatalf("version: %d %s", version.Status, version.Raw)
	}
	vid := assertCommissionPublicFields(t, version.Body, "crv")
	if version.Body["effectiveFrom"] != date || version.Body["fixedAmountMinor"] != float64(1250) {
		t.Fatalf("version fields: %#v", version.Body)
	}
	detail := env.Do(t, http.MethodGet, "/api/v1/commission/rules/"+id, tn.AdminAccessTok, nil)
	if assertCommissionPublicFields(t, detail.Body["rule"].(map[string]any), "crl") != id {
		t.Fatal("detail ID differs")
	}
	versions := detail.Body["versions"].([]any)
	if len(versions) != 1 || assertCommissionPublicFields(t, versions[0].(map[string]any), "crv") != vid {
		t.Fatal("version detail differs")
	}
	other := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "CROTHER"})
	denied := env.Do(t, http.MethodGet, "/api/v1/commission/rules/"+id, other.AdminAccessTok, nil)
	if denied.Status != 404 {
		t.Fatalf("cross-tenant read: %d", denied.Status)
	}
}

func TestCommissionCalculationResponsesMatchPublicContract(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "CCCONTRACT"})
	franchiseFixtureAt(t, env, tn, tn.DestBranchID)
	j, runID := codJourney(t, env, tn, 90000)
	j.post(t, "/api/v1/deliveries/attempts", map[string]any{"barcode": j.awb, "outcome": "DELIVERED", "runId": runID, "recipientName": "Synthetic recipient", "recipientRelationship": "SELF", "codCollectedMinor": 90000, "codPaymentMode": "CASH"}, 200, [2]string{"Idempotency-Key", harness.RandomKey()})
	token := env.Login(t, tn.AdminEmail, tn.AdminPassword)
	list := env.Do(t, http.MethodGet, "/api/v1/commission/calculations", token, nil)
	rows := list.Body["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("calculations: %#v", list.Body)
	}
	row := rows[0].(map[string]any)
	id := assertCommissionPublicFields(t, row, "ccl")
	if row["awb"] != j.awb || row["status"] != "POSTED" {
		t.Fatalf("calculation fields: %#v", row)
	}
	if _, ok := row["calculationTrace"].([]any); !ok {
		t.Fatalf("trace is not JSON array: %#v", row)
	}
	detail := env.Do(t, http.MethodGet, "/api/v1/commission/calculations/"+id, token, nil)
	if assertCommissionPublicFields(t, detail.Body, "ccl") != id || detail.Body["amountMinor"] != row["amountMinor"] {
		t.Fatal("calculation detail differs")
	}
}
