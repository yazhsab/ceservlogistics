// Package contract validates docs/openapi.yaml, which is the published
// contract the frontend is built against.
//
// A broken or drifted specification is a release blocker: Codex builds screens
// from it, so it has to parse, resolve every reference, and agree with the
// enums the Go code actually enforces.
package contract

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ceserve/courier-os/internal/bagging"
	"github.com/ceserve/courier-os/internal/billing"
	"github.com/ceserve/courier-os/internal/cod"
	"github.com/ceserve/courier-os/internal/commission"
	"github.com/ceserve/courier-os/internal/delivery"
	"github.com/ceserve/courier-os/internal/hubops"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/linehaul"
	"github.com/ceserve/courier-os/internal/manifest"
	"github.com/ceserve/courier-os/internal/ndr"
	"github.com/ceserve/courier-os/internal/notification"
	"github.com/ceserve/courier-os/internal/partner"
	"github.com/ceserve/courier-os/internal/reporting"
	"github.com/ceserve/courier-os/internal/scanning"
	"github.com/ceserve/courier-os/internal/settlement"
	"github.com/ceserve/courier-os/internal/shipment"
	"sort"
)

const specPath = "../../docs/openapi.yaml"

type spec struct {
	OpenAPI    string                    `yaml:"openapi"`
	Info       map[string]any            `yaml:"info"`
	Servers    []map[string]any          `yaml:"servers"`
	Security   []map[string][]string     `yaml:"security"`
	Tags       []map[string]any          `yaml:"tags"`
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		SecuritySchemes map[string]any `yaml:"securitySchemes"`
		Parameters      map[string]any `yaml:"parameters"`
		Responses       map[string]any `yaml:"responses"`
		Schemas         map[string]any `yaml:"schemas"`
	} `yaml:"components"`
}

func load(t *testing.T) (*spec, []byte) {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	var s spec
	if err := yaml.Unmarshal(raw, &s); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}
	return &s, raw
}

func TestSpecIsWellFormed(t *testing.T) {
	s, _ := load(t)

	if !strings.HasPrefix(s.OpenAPI, "3.1") {
		t.Errorf("expected OpenAPI 3.1, got %q", s.OpenAPI)
	}
	if s.Info["title"] == nil || s.Info["version"] == nil {
		t.Error("info.title and info.version are required")
	}
	if len(s.Servers) == 0 {
		t.Error("at least one server must be declared")
	}
	if len(s.Paths) == 0 {
		t.Fatal("the specification declares no paths")
	}
	if _, ok := s.Components.SecuritySchemes["bearerAuth"]; !ok {
		t.Error("the bearerAuth security scheme is missing")
	}
	if len(s.Security) == 0 {
		t.Error("a global security requirement must be declared")
	}
}

// TestEveryReferenceResolves catches the most common way a hand-written spec
// breaks: a $ref to a component that was renamed or never added.
func TestEveryReferenceResolves(t *testing.T) {
	s, raw := load(t)

	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}

	var refs []string
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, value := range v {
				if key == "$ref" {
					if ref, ok := value.(string); ok {
						refs = append(refs, ref)
					}
					continue
				}
				walk(value)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(doc)

	if len(refs) == 0 {
		t.Fatal("expected the specification to use component references")
	}
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "#/components/") {
			t.Errorf("external reference %q is not allowed; the spec must be self-contained", ref)
			continue
		}
		parts := strings.Split(strings.TrimPrefix(ref, "#/components/"), "/")
		if len(parts) != 2 {
			t.Errorf("malformed reference %q", ref)
			continue
		}
		var registry map[string]any
		switch parts[0] {
		case "schemas":
			registry = s.Components.Schemas
		case "parameters":
			registry = s.Components.Parameters
		case "responses":
			registry = s.Components.Responses
		case "securitySchemes":
			registry = s.Components.SecuritySchemes
		default:
			t.Errorf("reference %q points at an unknown component section", ref)
			continue
		}
		if _, ok := registry[parts[1]]; !ok {
			t.Errorf("reference %q does not resolve", ref)
		}
	}
}

// TestShipmentStatusEnumMatchesCode is the drift guard that matters most: the
// frontend renders and filters on these values, so the published enum must be
// exactly what the state machine and the database CHECK constraint allow.
func TestShipmentStatusEnumMatchesCode(t *testing.T) {
	s, _ := load(t)

	schema, ok := s.Components.Schemas["ShipmentStatus"].(map[string]any)
	if !ok {
		t.Fatal("the ShipmentStatus schema is missing")
	}
	rawEnum, ok := schema["enum"].([]any)
	if !ok {
		t.Fatal("ShipmentStatus has no enum")
	}
	published := map[string]bool{}
	for _, v := range rawEnum {
		published[v.(string)] = true
	}

	implemented := shipment.StatusStrings()
	if len(published) != len(implemented) {
		t.Errorf("the published enum has %d values, the implementation has %d",
			len(published), len(implemented))
	}
	for _, status := range implemented {
		if !published[status] {
			t.Errorf("status %q is implemented but not published in the contract", status)
		}
		delete(published, status)
	}
	for status := range published {
		t.Errorf("status %q is published in the contract but not implemented", status)
	}
}

// TestErrorCodeEnumIsComplete keeps the documented failure codes aligned with
// what handlers actually return, since clients switch on them.
func TestErrorCodeEnumIsComplete(t *testing.T) {
	s, _ := load(t)
	schema, ok := s.Components.Schemas["ErrorCode"].(map[string]any)
	if !ok {
		t.Fatal("the ErrorCode schema is missing")
	}
	rawEnum, _ := schema["enum"].([]any)
	published := map[string]bool{}
	for _, v := range rawEnum {
		published[v.(string)] = true
	}

	// Codes the frontend must be able to handle for Release 1 flows.
	for _, required := range []string{
		"VALIDATION_FAILED", "UNAUTHORIZED", "INVALID_CREDENTIALS",
		"TOKEN_EXPIRED", "TOKEN_REVOKED", "FORBIDDEN", "NOT_FOUND",
		"ACCOUNT_LOCKED", "ACCOUNT_INACTIVE", "CONFLICT", "DUPLICATE_RESOURCE",
		"IMMUTABLE_RESOURCE", "CONCURRENT_MODIFICATION",
		"IDEMPOTENCY_KEY_REUSED", "IDEMPOTENCY_IN_PROGRESS",
		"SHIPMENT_INVALID_STATE", "SHIPMENT_NOT_SERVICEABLE",
		"RATE_LIMITED", "INTERNAL_ERROR",
	} {
		if !published[required] {
			t.Errorf("error code %q is returned by the API but not published", required)
		}
	}
}

// TestEveryOperationIsDocumented enforces the contract-quality bar: an
// endpoint without a summary, a description or a success response is not
// usable by someone who has never seen the Go code.
func TestEveryOperationIsDocumented(t *testing.T) {
	s, _ := load(t)
	methods := map[string]bool{
		"get": true, "post": true, "put": true, "patch": true, "delete": true,
	}

	operations := 0
	for path, item := range s.Paths {
		for method, raw := range item {
			if !methods[method] {
				continue
			}
			where := method + " " + path
			op, ok := raw.(map[string]any)
			if !ok {
				t.Errorf("%s is not an operation object", where)
				continue
			}
			// An empty operation used to be skipped here, which let
			// `post: {}` sit in the document describing a route the server
			// does not serve. A generated client would emit that method and
			// get a 405. Publishing nothing is worse than publishing badly.
			if len(op) == 0 {
				t.Errorf("%s is declared but empty; remove it or document it", where)
				continue
			}
			operations++

			if op["summary"] == nil || op["summary"] == "" {
				t.Errorf("%s has no summary", where)
			}
			if op["tags"] == nil {
				t.Errorf("%s has no tag, so it will not appear under any section", where)
			}
			responses, ok := op["responses"].(map[string]any)
			if !ok || len(responses) == 0 {
				t.Errorf("%s declares no responses", where)
				continue
			}
			hasSuccess := false
			for code := range responses {
				if strings.HasPrefix(code, "2") {
					hasSuccess = true
				}
			}
			if !hasSuccess {
				t.Errorf("%s declares no success response", where)
			}
			// Mutations must document how they can fail, or a client cannot
			// build sensible error handling.
			if method != "get" {
				documentsFailure := false
				for code := range responses {
					if strings.HasPrefix(code, "4") {
						documentsFailure = true
					}
				}
				if !documentsFailure {
					t.Errorf("%s is a mutation but documents no failure response", where)
				}
			}
		}
	}
	if operations < 40 {
		t.Errorf("expected the Release 1 surface to document at least 40 operations, found %d", operations)
	}
	t.Logf("validated %d documented operations across %d paths", operations, len(s.Paths))
}

// TestNoVagueObjectSchemas enforces Constitution §33: a structured type must be
// described, not published as a bare `object`.
func TestNoVagueObjectSchemas(t *testing.T) {
	s, _ := load(t)

	// These carry genuinely open-ended, operator-defined content.
	allowed := map[string]bool{
		"AuditEvent": true, "Organization": true, "OperatingUnit": true,
		"RateCardVersion": true, "Shipment": true, "ImportJob": true,
		"RoutingExplanation": true, "ServiceabilityResult": true,
		"CreateCourierServiceRequest": true, "BookingRequest": true,
		"CreateOperatingUnitRequest": true, "UpdateOperatingUnitRequest": true,
		"UpdateCustomerRequest": true, "ReadinessResponse": true,
		"CursorPage": true, "OffsetPage": true, "ErrorEnvelope": true,
	}
	for name, raw := range s.Components.Schemas {
		schema, ok := raw.(map[string]any)
		if !ok || allowed[name] {
			continue
		}
		if schema["type"] == "object" && schema["properties"] == nil &&
			schema["additionalProperties"] == nil && schema["allOf"] == nil {
			t.Errorf("schema %q is an untyped object; describe its fields", name)
		}
	}
}

// TestOperationalEnumsMatchCode keeps the Release 2 enums in the contract
// aligned with the Go constants that enforce them.
//
// A frontend that renders a scan-type picker from the spec must offer exactly
// the values the API accepts; a spec that drifts is worse than no spec, because
// it is trusted.
func TestOperationalEnumsMatchCode(t *testing.T) {
	s, _ := load(t)

	cases := []struct {
		schema string
		want   []string
	}{
		{"ScanType", scanTypeStrings()},
		{"NDRAction", ndr.Actions},
		{"ExceptionType", hubops.ExceptionTypes},
	}
	for _, c := range cases {
		schema, ok := s.Components.Schemas[c.schema].(map[string]any)
		if !ok {
			t.Errorf("schema %s is missing", c.schema)
			continue
		}
		raw, ok := schema["enum"].([]any)
		if !ok {
			t.Errorf("schema %s declares no enum", c.schema)
			continue
		}
		got := make([]string, 0, len(raw))
		for _, v := range raw {
			got = append(got, v.(string))
		}
		sort.Strings(got)
		want := append([]string(nil), c.want...)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s enum has drifted from the code:\n spec: %v\n code: %v",
				c.schema, got, want)
		}
	}
}

// TestBagAndManifestLifecyclesAreDocumented checks that the state machines a
// frontend has to render are published.
func TestBagAndManifestLifecyclesAreDocumented(t *testing.T) {
	s, _ := load(t)
	for schema, want := range map[string][]string{
		"Bag":         bagging.AllStatuses,
		"Manifest":    manifest.AllStatuses,
		"Trip":        linehaul.AllStatuses,
		"DeliveryRun": delivery.AllStatuses,
	} {
		obj, ok := s.Components.Schemas[schema].(map[string]any)
		if !ok {
			t.Errorf("schema %s is missing", schema)
			continue
		}
		props, _ := obj["properties"].(map[string]any)
		status, _ := props["status"].(map[string]any)
		raw, ok := status["enum"].([]any)
		if !ok {
			t.Errorf("%s.status declares no enum", schema)
			continue
		}
		got := map[string]bool{}
		for _, v := range raw {
			got[v.(string)] = true
		}
		for _, w := range want {
			if !got[w] {
				t.Errorf("%s.status is missing %q, which the code can produce", schema, w)
			}
		}
	}
}

func scanTypeStrings() []string {
	out := make([]string, 0, len(scanning.AllScanTypes))
	for _, t := range scanning.AllScanTypes {
		out = append(out, string(t))
	}
	return out
}

// TestFinanceEnumsMatchCode pins the Release 3 enums the frontend switches on
// to the Go constants that back them.
//
// The failure this prevents is quiet: a status added in Go but not in the
// document leaves Codex rendering an unknown value with no branch to handle it,
// and nothing else in the build complains.
func TestFinanceEnumsMatchCode(t *testing.T) {
	s, _ := load(t)

	cases := []struct {
		schema string
		want   []string
	}{
		{"CommissionType", []string{
			commission.TypeBooking, commission.TypePickup, commission.TypeOriginHandling,
			commission.TypeDestinationHandling, commission.TypeDelivery, commission.TypeCOD,
			commission.TypeVolumeIncentive, commission.TypeCustom,
		}},
		{"CODStatus", []string{
			cod.StatusExpected, cod.StatusAgentCollected, cod.StatusBranchReceived,
			cod.StatusFranchiseConfirmed, cod.StatusReconciled, cod.StatusRemitted,
			cod.StatusClosed, cod.StatusCancelled, cod.StatusWrittenOff,
		}},
		{"SettlementStatus", []string{
			settlement.StatusDraft, settlement.StatusCalculated, settlement.StatusUnderReview,
			settlement.StatusApproved, settlement.StatusPartiallyPaid, settlement.StatusPaid,
			settlement.StatusClosed, settlement.StatusCancelled,
		}},
		{"InvoiceStatus", []string{
			billing.StatusDraft, billing.StatusIssued, billing.StatusPartiallyPaid,
			billing.StatusPaid, billing.StatusOverdue, billing.StatusCancelled,
			billing.StatusWrittenOff,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.schema, func(t *testing.T) {
			schema, ok := s.Components.Schemas[tc.schema].(map[string]any)
			if !ok {
				t.Fatalf("schema %s is missing from the specification", tc.schema)
			}
			raw, ok := schema["enum"].([]any)
			if !ok {
				t.Fatalf("schema %s has no enum", tc.schema)
			}
			documented := make(map[string]bool, len(raw))
			for _, v := range raw {
				documented[fmt.Sprint(v)] = true
			}
			for _, want := range tc.want {
				if !documented[want] {
					t.Errorf("%s is enforced in Go but missing from the %s enum", want, tc.schema)
				}
			}
			if len(documented) != len(tc.want) {
				t.Errorf("%s documents %d values, code defines %d",
					tc.schema, len(documented), len(tc.want))
			}
		})
	}
}

// TestLedgerAccountCodesAreDocumented checks that the account codes Go posts to
// appear in the contract, so a frontend building a chart-of-accounts screen
// knows what to expect.
func TestLedgerAccountCodesAreDocumented(t *testing.T) {
	_, raw := load(t)
	text := string(raw)

	for _, code := range []string{
		ledger.AcctCash, ledger.AcctTradeReceivable, ledger.AcctFranchisePayable,
		ledger.AcctCommissionPayable, ledger.AcctCODPayableConsignor,
		ledger.AcctCODReceivableAgent, ledger.AcctTaxPayable,
	} {
		if !strings.Contains(text, code) {
			t.Errorf("account code %s is posted to from Go but never appears in the contract", code)
		}
	}
}

// TestPartnerScopesMatchCode keeps the published scope catalogue identical to
// the one the key service enforces.
//
// The scope list is also mirrored by a CHECK constraint on api_keys, so three
// copies of it exist. A partner reading the spec and asking for a scope the
// server rejects is the worst of the three failure modes, because the spec is
// what they trust.
func TestPartnerScopesMatchCode(t *testing.T) {
	s, _ := load(t)

	schema, ok := s.Components.Schemas["PartnerScope"].(map[string]any)
	if !ok {
		t.Fatal("PartnerScope is missing from the specification")
	}
	raw, ok := schema["enum"].([]any)
	if !ok {
		t.Fatal("PartnerScope has no enum")
	}
	documented := make(map[string]bool, len(raw))
	for _, v := range raw {
		documented[fmt.Sprint(v)] = true
	}
	for _, scope := range partner.AllScopes {
		if !documented[scope] {
			t.Errorf("scope %s is enforced in Go but not documented", scope)
		}
	}
	if len(documented) != len(partner.AllScopes) {
		t.Errorf("the spec documents %d scopes, code defines %d",
			len(documented), len(partner.AllScopes))
	}
}

// TestWebhookEventTypesMatchCode does the same for the event catalogue.
//
// Renaming an event is not a cosmetic change: it silently stops firing for
// every consumer subscribed to the old name, and they find out from a customer
// rather than from us.
func TestWebhookEventTypesMatchCode(t *testing.T) {
	s, _ := load(t)

	schema, ok := s.Components.Schemas["WebhookEventType"].(map[string]any)
	if !ok {
		t.Fatal("WebhookEventType is missing from the specification")
	}
	raw, ok := schema["enum"].([]any)
	if !ok {
		t.Fatal("WebhookEventType has no enum")
	}
	documented := make(map[string]bool, len(raw))
	for _, v := range raw {
		documented[fmt.Sprint(v)] = true
	}
	for _, event := range partner.AllEvents {
		if !documented[event] {
			t.Errorf("event %s is emitted by the platform but not documented", event)
		}
	}
	if len(documented) != len(partner.AllEvents) {
		t.Errorf("the spec documents %d event types, code defines %d",
			len(documented), len(partner.AllEvents))
	}
}

// TestPartnerSurfaceUsesTheKeyScheme checks that every partner path declares
// the API-key security scheme rather than inheriting the session default.
//
// Inheriting would publish a lie: a generated client would attach a session
// token to a surface that refuses it, and the integrator would be debugging a
// 401 with a credential the documentation told them to use.
func TestPartnerSurfaceUsesTheKeyScheme(t *testing.T) {
	s, _ := load(t)

	checked := 0
	for path, item := range s.Paths {
		if !strings.HasPrefix(path, "/api/v1/partner") {
			continue
		}
		for method, rawOp := range item {
			op, ok := rawOp.(map[string]any)
			if !ok || len(op) == 0 {
				continue
			}
			security, ok := op["security"].([]any)
			if !ok || len(security) == 0 {
				t.Errorf("%s %s does not declare the partner security scheme", method, path)
				continue
			}
			names := map[string]bool{}
			for _, entry := range security {
				for name := range entry.(map[string]any) {
					names[name] = true
				}
			}
			if !names["partnerKey"] {
				t.Errorf("%s %s does not use partnerKey", method, path)
			}
			if names["bearerAuth"] {
				t.Errorf("%s %s accepts a session token, which the server refuses", method, path)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no partner operations found; the surface is not documented")
	}
	t.Logf("verified the key scheme on %d partner operations", checked)
}

// TestReportTypesMatchCode keeps the published catalogue identical to the one
// the engine accepts and the CHECK on report_runs.report_type permits.
//
// Three copies again, and the spec is the one a client trusts: asking for a
// report type the spec lists and the server rejects is the worst of the three
// failure modes.
func TestReportTypesMatchCode(t *testing.T) {
	s, _ := load(t)

	schema, ok := s.Components.Schemas["ReportType"].(map[string]any)
	if !ok {
		t.Fatal("ReportType is missing from the specification")
	}
	raw, ok := schema["enum"].([]any)
	if !ok {
		t.Fatal("ReportType has no enum")
	}
	documented := make(map[string]bool, len(raw))
	for _, v := range raw {
		documented[fmt.Sprint(v)] = true
	}
	for _, reportType := range reporting.AllTypes {
		if !documented[reportType] {
			t.Errorf("report type %s is accepted by the engine but not documented", reportType)
		}
	}
	if len(documented) != len(reporting.AllTypes) {
		t.Errorf("the spec documents %d report types, code defines %d",
			len(documented), len(reporting.AllTypes))
	}
}

// TestNotificationSuppressionReasonsAreDocumented checks that every reason the
// service can record appears in the contract.
//
// A suppressed notification is the module's *only* way of reporting failure —
// it never fails the business operation that caused it — so a reason a client
// cannot interpret is a silent failure by another route.
func TestNotificationSuppressionReasonsAreDocumented(t *testing.T) {
	s, _ := load(t)

	schema, ok := s.Components.Schemas["NotificationSuppressionReason"].(map[string]any)
	if !ok {
		t.Fatal("NotificationSuppressionReason is missing from the specification")
	}
	raw, ok := schema["enum"].([]any)
	if !ok {
		t.Fatal("NotificationSuppressionReason has no enum")
	}
	documented := make(map[string]bool, len(raw))
	for _, v := range raw {
		documented[fmt.Sprint(v)] = true
	}
	for _, reason := range []string{
		notification.SuppressOptedOut, notification.SuppressNoTemplate,
		notification.SuppressNoAddress, notification.SuppressNoSender,
		notification.SuppressQuietHours, notification.SuppressDuplicate,
	} {
		if !documented[reason] {
			t.Errorf("suppression reason %s is recorded in Go but not documented", reason)
		}
	}
}
