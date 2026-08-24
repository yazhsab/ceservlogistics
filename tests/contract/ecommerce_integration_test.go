package contract

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/mail"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/ceserve/courier-os/internal/partner"
	"github.com/ceserve/courier-os/internal/shipment"
)

const (
	ecommerceContractPath = "../../docs/contracts/ecommerce-integration-v1.md"
	ecommerceSchemaPath   = "../../docs/contracts/schemas/commerce-fulfillment-ready-v1.schema.json"
	ecommerceFixtureRoot  = "../../docs/contracts/fixtures/ecommerce"
)

var ecommerceScenarios = []string{"cod-ng", "prepaid-ng"}

// TestCommerceFulfillmentReadyFixturesMatchSchema makes the platform-neutral
// seam executable. An adapter cannot add a convenient platform field, omit a
// package unit, or change COD semantics without changing the versioned schema.
func TestCommerceFulfillmentReadyFixturesMatchSchema(t *testing.T) {
	schema, _ := readJSONMap(t, ecommerceSchemaPath)
	for _, scenario := range ecommerceScenarios {
		t.Run(scenario, func(t *testing.T) {
			fixture, _ := readJSONMap(t, fixturePath("canonical", scenario))
			assertSchemaValid(t, schema, schema, fixture)
			assertCanonicalInvariants(t, fixture)
		})
	}
}

// TestEcommerceBookingFixturesMatchOpenAPIAndRuntime checks three boundaries at
// once: canonical-to-BookingRequest mapping, the published OpenAPI schema, and
// the strict validator the partner handler actually calls.
func TestEcommerceBookingFixturesMatchOpenAPIAndRuntime(t *testing.T) {
	openAPI := loadOpenAPIRoot(t)
	bookingSchema := resolveJSONPointer(t, openAPI, "#/components/schemas/BookingRequest")

	for _, scenario := range ecommerceScenarios {
		t.Run(scenario, func(t *testing.T) {
			canonical, _ := readJSONMap(t, fixturePath("canonical", scenario))
			booking, raw := readJSONMap(t, fixturePath("booking-requests", scenario))

			assertSchemaValid(t, openAPI, bookingSchema, booking)
			mapped := mapCanonicalToBooking(t, canonical)
			if !jsonSemanticallyEqual(mapped, booking) {
				t.Fatalf("booking fixture has drifted from the deterministic mapping\n mapped: %s\nfixture: %s",
					mustJSON(t, mapped), mustJSON(t, booking))
			}

			var req shipment.BookingRequest
			if err := shipment.DecodeStrict(raw, &req); err != nil {
				t.Fatalf("strict BookingRequest decode failed: %v", err)
			}
			if err := shipment.ValidateBooking(&req, 50); err != nil {
				t.Fatalf("runtime BookingRequest validation failed: %v", err)
			}
			if req.Sender.CountryCode != "NG" || req.Recipient.CountryCode != "NG" {
				t.Fatalf("Nigerian fixture must send explicit NG countries: sender=%q recipient=%q",
					req.Sender.CountryCode, req.Recipient.CountryCode)
			}
			assertBookingMetadataAllowList(t, booking)
		})
	}
}

// TestEcommerceResponseAndWebhookFixturesMatchOpenAPI pins the minimum fields
// connectors persist and the exact shipment-status webhook envelope they later
// consume. It also proves identifiers remain correlated across all four files.
func TestEcommerceResponseAndWebhookFixturesMatchOpenAPI(t *testing.T) {
	openAPI := loadOpenAPIRoot(t)
	responseSchema := resolveJSONPointer(t, openAPI, "#/components/schemas/EcommerceBookingResponseV1")
	webhookSchema := resolveJSONPointer(t, openAPI, "#/components/schemas/PartnerShipmentStatusWebhookEnvelope")

	for _, scenario := range ecommerceScenarios {
		t.Run(scenario, func(t *testing.T) {
			canonical, _ := readJSONMap(t, fixturePath("canonical", scenario))
			booking, _ := readJSONMap(t, fixturePath("booking-requests", scenario))
			response, _ := readJSONMap(t, fixturePath("booking-responses", scenario))
			webhook, rawWebhook := readJSONMap(t, fixturePath("webhooks", scenario))

			assertSchemaValid(t, openAPI, responseSchema, response)
			assertSchemaValid(t, openAPI, webhookSchema, webhook)
			assertCrossFixtureConsistency(t, canonical, booking, response, webhook)

			createdAt := mustTime(t, stringAt(t, webhook, "createdAt"))
			timestamp := createdAt.Unix()
			secret := "fixture-only-signing-secret"
			mac := hmac.New(sha256.New, []byte(secret))
			_, _ = fmt.Fprintf(mac, "%d.", timestamp)
			_, _ = mac.Write(rawWebhook)
			want := "v1=" + hex.EncodeToString(mac.Sum(nil))
			if got := partner.Sign(secret, timestamp, rawWebhook); got != want {
				t.Fatalf("runtime signature %q does not match the published timestamp.rawBody formula %q", got, want)
			}
			if !partner.VerifySignature(secret, want, timestamp, rawWebhook) {
				t.Fatal("runtime verifier rejected the independently calculated fixture signature")
			}
		})
	}
}

func TestOperationalWebhookFixturesMatchOpenAPI(t *testing.T) {
	openAPI := loadOpenAPIRoot(t)
	fixtures := []struct {
		name   string
		schema string
	}{
		{"pickup-completed-ng", "PartnerPickupCompletedWebhookEnvelope"},
		{"pod-captured-ng", "PartnerPODCapturedWebhookEnvelope"},
		{"cod-collected-ng", "PartnerCODCollectedWebhookEnvelope"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			value, _ := readJSONMap(t, fixturePath("webhooks", fixture.name))
			schema := resolveJSONPointer(t, openAPI, "#/components/schemas/"+fixture.schema)
			assertSchemaValid(t, openAPI, schema, value)
		})
	}
}

func TestEcommercePolicyCoversEveryPublishedWebhookEvent(t *testing.T) {
	raw, err := os.ReadFile(ecommerceContractPath)
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	contract := string(raw)
	for _, event := range partner.AllEvents {
		if !strings.Contains(contract, "`"+event+"`") {
			t.Errorf("published event %q has no ecommerce projection policy", event)
		}
	}
	for _, required := range []string{
		"is **not payment settlement**",
		"Do not cancel ecommerce order automatically",
		"Restock only after warehouse reconciliation",
		"Dedupe on ID, not type/status",
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("contract is missing the safety rule %q", required)
		}
	}
	if got := partner.SignatureTolerance(); got != 5*time.Minute {
		t.Fatalf("published webhook tolerance is five minutes, runtime uses %s", got)
	}
}

func fixturePath(kind, scenario string) string {
	return ecommerceFixtureRoot + "/" + kind + "/" + scenario + ".json"
}

func readJSONMap(t *testing.T, path string) (map[string]any, []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value map[string]any
	if err := dec.Decode(&value); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("%s must contain exactly one JSON value: %v", path, err)
	}
	return value, raw
}

func loadOpenAPIRoot(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read OpenAPI: %v", err)
	}
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse OpenAPI: %v", err)
	}
	return root
}

func resolveJSONPointer(t *testing.T, root map[string]any, pointer string) any {
	t.Helper()
	value, err := resolvePointer(root, pointer)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func resolvePointer(root any, pointer string) (any, error) {
	if pointer == "#" {
		return root, nil
	}
	if !strings.HasPrefix(pointer, "#/") {
		return nil, fmt.Errorf("only local JSON pointers are supported: %s", pointer)
	}
	current := root
	for _, encoded := range strings.Split(strings.TrimPrefix(pointer, "#/"), "/") {
		key := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s crosses a non-object at %q", pointer, key)
		}
		next, ok := object[key]
		if !ok {
			return nil, fmt.Errorf("%s does not resolve at %q", pointer, key)
		}
		current = next
	}
	return current, nil
}

func assertSchemaValid(t *testing.T, root map[string]any, schema any, value any) {
	t.Helper()
	errs := schemaErrors(root, schema, value, "$")
	if len(errs) > 0 {
		t.Fatalf("schema validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
}

// schemaErrors implements the JSON Schema/OpenAPI subset used by the contract
// fixtures. Keeping it dependency-free makes `go test ./tests/contract/...`
// the one reproducible validation command rather than relying on a workstation
// CLI. Unsupported keywords remain documentation; every restrictive keyword
// used by CommerceFulfillmentReady/v1 is covered here.
func schemaErrors(root map[string]any, rawSchema any, value any, path string) []string {
	if boolean, ok := rawSchema.(bool); ok {
		if boolean {
			return nil
		}
		return []string{path + ": forbidden by schema"}
	}
	schema, ok := rawSchema.(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("%s: schema is %T, want object", path, rawSchema)}
	}
	var errs []string

	if ref, ok := schema["$ref"].(string); ok {
		resolved, err := resolvePointer(root, ref)
		if err != nil {
			errs = append(errs, path+": "+err.Error())
		} else {
			errs = append(errs, schemaErrors(root, resolved, value, path)...)
		}
	}
	if allOf, ok := schema["allOf"].([]any); ok {
		for _, branch := range allOf {
			errs = append(errs, schemaErrors(root, branch, value, path)...)
		}
	}
	if conditional, ok := schema["if"]; ok {
		if len(schemaErrors(root, conditional, value, path)) == 0 {
			if thenSchema, exists := schema["then"]; exists {
				errs = append(errs, schemaErrors(root, thenSchema, value, path)...)
			}
		} else if elseSchema, exists := schema["else"]; exists {
			errs = append(errs, schemaErrors(root, elseSchema, value, path)...)
		}
	}
	if notSchema, exists := schema["not"]; exists && len(schemaErrors(root, notSchema, value, path)) == 0 {
		errs = append(errs, path+": matches a forbidden schema")
	}

	if value == nil {
		if nullable, _ := schema["nullable"].(bool); nullable {
			return errs
		}
		if schema["type"] != nil {
			return append(errs, path+": null is not allowed")
		}
		return errs
	}

	if constant, exists := schema["const"]; exists && !jsonValueEqual(value, constant) {
		errs = append(errs, fmt.Sprintf("%s: got %v, want constant %v", path, value, constant))
	}
	if values, ok := schema["enum"].([]any); ok {
		matched := false
		for _, candidate := range values {
			matched = matched || jsonValueEqual(value, candidate)
		}
		if !matched {
			errs = append(errs, fmt.Sprintf("%s: %v is not in enum %v", path, value, values))
		}
	}

	types := schemaTypes(schema["type"])
	if len(types) > 0 && !matchesAnyType(value, types) {
		return append(errs, fmt.Sprintf("%s: got %T, want type %v", path, value, types))
	}

	switch typed := value.(type) {
	case map[string]any:
		required := stringSet(schema["required"])
		for field := range required {
			if _, exists := typed[field]; !exists {
				errs = append(errs, path+"."+field+": required property is missing")
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child, documented := properties[key]
			if documented {
				errs = append(errs, schemaErrors(root, child, typed[key], path+"."+key)...)
				continue
			}
			switch additional := schema["additionalProperties"].(type) {
			case bool:
				if !additional {
					errs = append(errs, path+"."+key+": additional property is forbidden")
				}
			case map[string]any:
				errs = append(errs, schemaErrors(root, additional, typed[key], path+"."+key)...)
			}
		}
	case []any:
		if minimum, ok := integerKeyword(schema["minItems"]); ok && int64(len(typed)) < minimum {
			errs = append(errs, fmt.Sprintf("%s: has %d items, minimum is %d", path, len(typed), minimum))
		}
		if maximum, ok := integerKeyword(schema["maxItems"]); ok && int64(len(typed)) > maximum {
			errs = append(errs, fmt.Sprintf("%s: has %d items, maximum is %d", path, len(typed), maximum))
		}
		if itemSchema, exists := schema["items"]; exists {
			for i, item := range typed {
				errs = append(errs, schemaErrors(root, itemSchema, item, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	case string:
		length := int64(utf8.RuneCountInString(typed))
		if minimum, ok := integerKeyword(schema["minLength"]); ok && length < minimum {
			errs = append(errs, fmt.Sprintf("%s: length %d is below %d", path, length, minimum))
		}
		if maximum, ok := integerKeyword(schema["maxLength"]); ok && length > maximum {
			errs = append(errs, fmt.Sprintf("%s: length %d exceeds %d", path, length, maximum))
		}
		if pattern, ok := schema["pattern"].(string); ok {
			re, err := regexp.Compile(pattern)
			if err != nil {
				errs = append(errs, path+": invalid schema pattern: "+err.Error())
			} else if !re.MatchString(typed) {
				errs = append(errs, fmt.Sprintf("%s: %q does not match %s", path, typed, pattern))
			}
		}
		switch schema["format"] {
		case "date-time":
			if _, err := time.Parse(time.RFC3339, typed); err != nil {
				errs = append(errs, path+": invalid RFC3339 date-time")
			}
		case "email":
			address, err := mail.ParseAddress(typed)
			if err != nil || address.Address != typed || !strings.Contains(typed, "@") {
				errs = append(errs, path+": invalid email")
			}
		}
	default:
		if number, ok := numberValue(value); ok {
			if minimum, exists := numberValue(schema["minimum"]); exists && number < minimum {
				errs = append(errs, fmt.Sprintf("%s: %v is below minimum %v", path, number, minimum))
			}
			if maximum, exists := numberValue(schema["maximum"]); exists && number > maximum {
				errs = append(errs, fmt.Sprintf("%s: %v exceeds maximum %v", path, number, maximum))
			}
		}
	}
	return errs
}

func schemaTypes(raw any) []string {
	switch value := raw.(type) {
	case string:
		return []string{value}
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return nil
	}
}

func matchesAnyType(value any, types []string) bool {
	for _, kind := range types {
		switch kind {
		case "object":
			_, ok := value.(map[string]any)
			if ok {
				return true
			}
		case "array":
			_, ok := value.([]any)
			if ok {
				return true
			}
		case "string":
			_, ok := value.(string)
			if ok {
				return true
			}
		case "integer":
			n, ok := numberValue(value)
			if ok && math.Trunc(n) == n {
				return true
			}
		case "number":
			if _, ok := numberValue(value); ok {
				return true
			}
		case "boolean":
			_, ok := value.(bool)
			if ok {
				return true
			}
		case "null":
			if value == nil {
				return true
			}
		}
	}
	return false
}

func stringSet(raw any) map[string]bool {
	out := map[string]bool{}
	if values, ok := raw.([]any); ok {
		for _, value := range values {
			out[fmt.Sprint(value)] = true
		}
	}
	return out
}

func integerKeyword(raw any) (int64, bool) {
	number, ok := numberValue(raw)
	return int64(number), ok && math.Trunc(number) == number
}

func numberValue(raw any) (float64, bool) {
	switch value := raw.(type) {
	case json.Number:
		number, err := value.Float64()
		return number, err == nil
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint64:
		return float64(value), true
	case float64:
		return value, true
	default:
		return 0, false
	}
}

func jsonValueEqual(left, right any) bool {
	if a, ok := numberValue(left); ok {
		b, ok := numberValue(right)
		return ok && a == b
	}
	return reflect.DeepEqual(left, right)
}

func assertCanonicalInvariants(t *testing.T, canonical map[string]any) {
	t.Helper()
	store := mapAt(t, canonical, "store")
	payment := mapAt(t, canonical, "payment")
	if stringAt(t, store, "countryCode") != "NG" || stringAt(t, store, "currency") != "NGN" {
		t.Fatal("Phase 1 Nigerian fixtures must pin NG and NGN")
	}
	if stringAt(t, payment, "currency") != stringAt(t, store, "currency") {
		t.Fatal("payment currency must equal store currency")
	}

	subtotal := intAt(t, payment, "merchandiseSubtotalMinor")
	discount := intAt(t, payment, "discountMinor")
	tax := intAt(t, payment, "taxMinor")
	shipping := intAt(t, payment, "shippingChargeMinor")
	prepaid := intAt(t, payment, "prepaidAmountMinor")
	credit := intAt(t, payment, "storeCreditAppliedMinor")
	total := subtotal - discount + tax + shipping
	if got := intAt(t, payment, "orderTotalMinor"); got != total {
		t.Fatalf("order total = %d, formula produces %d", got, total)
	}
	outstanding := total - prepaid - credit
	if outstanding < 0 {
		outstanding = 0
	}
	mode := stringAt(t, payment, "mode")
	if mode == "COD" {
		if got := intAt(t, payment, "codAmountMinor"); got <= 0 || got != outstanding {
			t.Fatalf("COD amount = %d, outstanding at door = %d", got, outstanding)
		}
	} else if outstanding != 0 {
		t.Fatalf("PREPAID fulfillment still has %d outstanding", outstanding)
	}

	var declared int64
	for _, rawPackage := range sliceAt(t, canonical, "packages") {
		pkg, ok := rawPackage.(map[string]any)
		if !ok {
			t.Fatalf("package is %T", rawPackage)
		}
		declared += intAt(t, pkg, "declaredValueMinor")
		for _, dimension := range []string{"actualWeightGrams", "lengthMm", "widthMm", "heightMm"} {
			if intAt(t, pkg, dimension) <= 0 {
				t.Fatalf("package %s must be positive", dimension)
			}
		}
	}
	if got := intAt(t, canonical, "declaredValueMinor"); got != declared {
		t.Fatalf("declaredValueMinor = %d, package sum = %d", got, declared)
	}
}

func mapCanonicalToBooking(t *testing.T, canonical map[string]any) map[string]any {
	t.Helper()
	merchant := mapAt(t, canonical, "merchant")
	payment := mapAt(t, canonical, "payment")
	store := mapAt(t, canonical, "store")
	order := mapAt(t, canonical, "order")
	fulfillment := mapAt(t, canonical, "fulfillment")
	metadata := map[string]any{
		"canonicalSchemaVersion":     canonical["schemaVersion"],
		"commerceEventId":            canonical["eventId"],
		"commercePlatform":           canonical["platform"],
		"commerceStoreId":            store["id"],
		"commerceOrderId":            order["id"],
		"commerceFulfillmentId":      fulfillment["id"],
		"commerceFulfillmentVersion": fulfillment["version"],
	}
	for key, value := range mapAt(t, canonical, "metadata") {
		metadata[key] = value
	}
	out := map[string]any{
		"customerId":         merchant["logisticsCustomerId"],
		"referenceNumber":    canonical["referenceNumber"],
		"serviceCode":        canonical["serviceCode"],
		"paymentMode":        payment["mode"],
		"sender":             addressForBooking(t, mapAt(t, canonical, "sender")),
		"recipient":          addressForBooking(t, mapAt(t, canonical, "recipient")),
		"packages":           canonical["packages"],
		"declaredValueMinor": canonical["declaredValueMinor"],
		"insuranceRequired":  canonical["insuranceRequired"],
		"contentDescription": canonical["contentDescription"],
		"isFragile":          canonical["isFragile"],
		"isDangerousGoods":   canonical["isDangerousGoods"],
		"metadata":           metadata,
	}
	for _, optional := range []string{"bookingUnitId", "specialInstructions"} {
		if value, exists := canonical[optional]; exists {
			out[optional] = value
		}
	}
	if value, exists := payment["codAmountMinor"]; exists {
		out["codAmountMinor"] = value
	}
	return out
}

func addressForBooking(t *testing.T, source map[string]any) map[string]any {
	t.Helper()
	out := make(map[string]any, len(source))
	for key, value := range source {
		if key != "externalCustomerId" {
			out[key] = value
		}
	}
	return out
}

func assertBookingMetadataAllowList(t *testing.T, booking map[string]any) {
	t.Helper()
	allowed := map[string]bool{
		"canonicalSchemaVersion":     true,
		"commerceEventId":            true,
		"commercePlatform":           true,
		"commerceStoreId":            true,
		"commerceOrderId":            true,
		"commerceFulfillmentId":      true,
		"commerceFulfillmentVersion": true,
		"sourceChannel":              true,
		"salesChannel":               true,
		"customerNoteReference":      true,
	}
	for key := range mapAt(t, booking, "metadata") {
		if !allowed[key] {
			t.Errorf("booking metadata key %q is not in the v1 allow-list", key)
		}
	}
}

func assertCrossFixtureConsistency(
	t *testing.T, canonical, booking, response, webhook map[string]any,
) {
	t.Helper()
	payment := mapAt(t, canonical, "payment")
	data := mapAt(t, webhook, "data")
	if stringAt(t, response, "referenceNumber") != stringAt(t, booking, "referenceNumber") ||
		stringAt(t, data, "reference") != stringAt(t, booking, "referenceNumber") {
		t.Fatal("referenceNumber drifted across canonical booking, response, or webhook")
	}
	if stringAt(t, response, "paymentMode") != stringAt(t, payment, "mode") {
		t.Fatal("response paymentMode differs from canonical payment mode")
	}
	if stringAt(t, response, "currency") != stringAt(t, payment, "currency") {
		t.Fatal("response currency differs from canonical payment currency")
	}
	if stringAt(t, data, "shipmentId") != stringAt(t, response, "id") ||
		stringAt(t, data, "awb") != stringAt(t, response, "awb") {
		t.Fatal("webhook shipment identity differs from booking response")
	}
	if intAt(t, response, "pieceCount") != int64(len(sliceAt(t, canonical, "packages"))) {
		t.Fatal("response pieceCount differs from canonical packages")
	}
	wantCOD := int64(0)
	if raw, exists := payment["codAmountMinor"]; exists {
		wantCOD = mustInt(t, raw)
	}
	if got := intAt(t, response, "codAmountMinor"); got != wantCOD {
		t.Fatalf("response COD amount = %d, canonical = %d", got, wantCOD)
	}
}

func mapAt(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want object", key, object[key])
	}
	return value
}

func sliceAt(t *testing.T, object map[string]any, key string) []any {
	t.Helper()
	value, ok := object[key].([]any)
	if !ok {
		t.Fatalf("%s is %T, want array", key, object[key])
	}
	return value
}

func stringAt(t *testing.T, object map[string]any, key string) string {
	t.Helper()
	value, ok := object[key].(string)
	if !ok {
		t.Fatalf("%s is %T, want string", key, object[key])
	}
	return value
}

func intAt(t *testing.T, object map[string]any, key string) int64 {
	t.Helper()
	value, exists := object[key]
	if !exists {
		t.Fatalf("%s is missing", key)
	}
	return mustInt(t, value)
}

func mustInt(t *testing.T, value any) int64 {
	t.Helper()
	number, ok := numberValue(value)
	if !ok || math.Trunc(number) != number {
		t.Fatalf("%v is not an integer", value)
	}
	return int64(number)
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}

func jsonSemanticallyEqual(left, right any) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(raw)
}
