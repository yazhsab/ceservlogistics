package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/ceserve/courier-os/tests/harness"
)

// urlQuery escapes a search term so the test exercises the handler rather than
// the query parser — a term with a space or a percent sign is exactly the input
// worth testing, and it must survive the wire intact.
func urlQuery(s string) string { return url.QueryEscape(s) }

// Finding a postcode from what a sender knows.
//
// These tests exist because of a market fact rather than a code fact: Nigerian
// postal codes validate fine but are poorly adopted, so a booking form that can
// only be filled by someone who already knows their six digits cannot be
// filled. The assertion that matters is not "the endpoint returns 200" — it is
// that typing an area name a real sender would type yields the postcode that
// booking then accepts.

func TestPlaceSearchResolvesAnAreaNameToABookablePostcode(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "PLACE"})

	// What a Lagos sender types. Nobody types 100001.
	resp := env.Do(t, "GET", "/api/v1/geography/places?q=Ikeja+GRA", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("place search failed: %d %s", resp.Status, resp.Raw)
	}
	data, _ := resp.Body["data"].([]any)
	if len(data) == 0 {
		t.Fatalf("no results for an area name that exists: %s", resp.Raw)
	}
	first, _ := data[0].(map[string]any)
	if got := first["code"]; got != "100001" {
		t.Fatalf("top result code = %v, want 100001 (%s)", got, resp.Raw)
	}
	if got := first["matchedOn"]; got != "AREA" {
		t.Errorf("matchedOn = %v, want AREA", got)
	}
	if got := first["district"]; got != "Ikeja" {
		t.Errorf("district = %v, want Ikeja", got)
	}
	// The label is what a picker renders. It reads in address order and drops
	// the repeat: Lagos is both the city and the state here.
	if got, _ := first["label"].(string); got != "Ikeja GRA, Ikeja, Lagos" {
		t.Errorf("label = %q, want %q", got, "Ikeja GRA, Ikeja, Lagos")
	}

	// The whole point: the code that came back is bookable.
	code, _ := first["code"].(string)
	booking := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": code, "destinationPincode": tn.DestPincode,
		"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	})
	if booking.Status != http.StatusOK {
		t.Fatalf("the postcode found by search was not quotable: %d %s", booking.Status, booking.Raw)
	}
}

func TestPlaceSearchMatchesEveryFieldASenderMightName(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "PLCM"})

	for _, tc := range []struct {
		name, query, wantCode, wantKind string
	}{
		{"area", "Maitama", "900001", "AREA"},
		{"area with a digit", "Wuse II", "900001", "AREA"},
		{"landmark people give as an address", "Computer Village", "100001", "AREA"},
		{"city", "Jalingo", "660001", "AREA"},
		{"LGA", "Eti-Osa", "101001", "DISTRICT"},
		{"code prefix", "9000", "900001", "CODE"},
		{"full code", "100001", "100001", "CODE"},
		{"lowercase", "maitama", "900001", "AREA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := env.Do(t, "GET", "/api/v1/geography/places?q="+urlQuery(tc.query), tn.AdminAccessTok, nil)
			if resp.Status != http.StatusOK {
				t.Fatalf("search %q: %d %s", tc.query, resp.Status, resp.Raw)
			}
			data, _ := resp.Body["data"].([]any)
			if len(data) == 0 {
				t.Fatalf("search %q returned nothing", tc.query)
			}
			first, _ := data[0].(map[string]any)
			if got := first["code"]; got != tc.wantCode {
				t.Errorf("search %q: code = %v, want %v", tc.query, got, tc.wantCode)
			}
			if got := first["matchedOn"]; got != tc.wantKind {
				t.Errorf("search %q: matchedOn = %v, want %v", tc.query, got, tc.wantKind)
			}
		})
	}
}

// A postcode matched by several branches must appear once, at its strongest
// classification. "Ikeja" is the area, the office and the LGA all at once, and
// three identical-looking rows in a picker is a bug the user sees.
func TestPlaceSearchCollapsesAPostcodeMatchedSeveralWays(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "PLCD"})

	resp := env.Do(t, "GET", "/api/v1/geography/places?q=Ikeja", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("search failed: %d %s", resp.Status, resp.Raw)
	}
	data, _ := resp.Body["data"].([]any)
	seen := map[string]int{}
	for _, row := range data {
		m, _ := row.(map[string]any)
		code, _ := m["code"].(string)
		seen[code]++
	}
	for code, n := range seen {
		if n > 1 {
			t.Errorf("postcode %s appeared %d times; expected once", code, n)
		}
	}
	if seen["100001"] != 1 {
		t.Errorf("expected 100001 exactly once, got %d", seen["100001"])
	}
}

func TestPlaceSearchRejectsUnusableInput(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "PLCV"})

	for _, tc := range []struct {
		name, query string
	}{
		{"empty", ""},
		{"one character matches most of the dataset", "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := env.Do(t, "GET", "/api/v1/geography/places?q="+urlQuery(tc.query), tn.AdminAccessTok, nil)
			if resp.Status != http.StatusUnprocessableEntity && resp.Status != http.StatusBadRequest {
				t.Errorf("q=%q returned %d, want a validation error", tc.query, resp.Status)
			}
		})
	}

	// A LIKE wildcard is a search term, not an instruction. Typing "%" must
	// find postcodes containing a percent sign — that is, none — rather than
	// matching every row in the dataset.
	resp := env.Do(t, "GET", "/api/v1/geography/places?q="+urlQuery("%%"), tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("wildcard search failed: %d %s", resp.Status, resp.Raw)
	}
	if data, _ := resp.Body["data"].([]any); len(data) != 0 {
		t.Errorf("a wildcard matched %d rows; it must be treated as literal text", len(data))
	}
}

// Reference geography is shared by every tenant by design, but it is still
// behind authentication: an unauthenticated caller must not be able to
// enumerate it.
func TestPlaceSearchRequiresAuthentication(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	env.Geography(t)

	resp := env.Do(t, "GET", "/api/v1/geography/places?q=Ikeja", "", nil)
	if resp.Status != http.StatusUnauthorized {
		t.Errorf("anonymous place search returned %d, want 401", resp.Status)
	}
}

func TestDistrictsAreListedWithTheMarketsOwnLabel(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "PLCL"})

	resp := env.Do(t, "GET", "/api/v1/geography/districts?state=LA", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("district list failed: %d %s", resp.Status, resp.Raw)
	}
	// A Nigerian operator asked for a "district" is being asked about
	// something they do not have a word for.
	if got := resp.Body["label"]; got != "LGA" {
		t.Errorf("label = %v, want LGA", got)
	}
	data, _ := resp.Body["data"].([]any)
	names := map[string]bool{}
	for _, row := range data {
		m, _ := row.(map[string]any)
		name, _ := m["name"].(string)
		names[name] = true
	}
	for _, want := range []string{"Ikeja", "Eti-Osa"} {
		if !names[want] {
			t.Errorf("LGA %q missing from the Lagos list: %s", want, resp.Raw)
		}
	}

	// India still gets its own word.
	indian := env.Do(t, "GET", "/api/v1/geography/districts?state=LA&country=IN", tn.AdminAccessTok, nil)
	if got := indian.Body["label"]; got != "District" {
		t.Errorf("country=IN label = %v, want District", got)
	}
}

func TestDistrictListRequiresAState(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "PLCS"})

	resp := env.Do(t, "GET", "/api/v1/geography/districts", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusUnprocessableEntity && resp.Status != http.StatusBadRequest {
		t.Errorf("missing state returned %d, want a validation error", resp.Status)
	}
}
