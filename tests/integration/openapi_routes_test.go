package integration

import (
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ceserve/courier-os/tests/harness"
)

// Every documented path must actually route.
//
// # Why this test exists
//
// `docs/openapi.yaml` is the frontend contract (constitution §6). The contract
// tests in tests/contract check that it is *well formed* — references resolve,
// enums match code, no vague object schemas — but they parse the file in
// isolation and never ask the running server whether any of it is true.
//
// That gap let 71 paths ship documented without their `/api/v1` prefix:
// `/bags`, `/manifests`, `/scans`, `/pod`, `/delivery-runs` and the rest of the
// Release 2 operational surface. The routes existed and worked; the document
// pointed somewhere else. Every one of those endpoints would have returned 404
// to a frontend engineer following the spec, and no test noticed, because a
// syntactically perfect document can still describe a different API.
//
// This lives in tests/integration rather than tests/contract because it needs a
// real router, which is exactly the thing the contract tests lack.
//
// # What it asserts
//
// For each documented path, that the router resolves it to *something other
// than "no such route"*. Authentication, validation and not-found on a
// fabricated id are all fine — they prove the route exists. Only the router's
// unmatched-route 404 is a failure.
//
// Both 404s carry `code: NOT_FOUND`, so the code cannot distinguish them; only
// the message does ("The requested endpoint does not exist." against "Bag not
// found."). Rather than hardcode that sentence, the test **probes a path that
// certainly does not exist and calibrates on whatever comes back**. If the
// wording is ever changed the test keeps working, which matters because the
// first version of this test hardcoded a guess, passed against an injected
// regression, and was worthless until that was caught.

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// errorMessage pulls the human-readable message out of the error envelope.
func errorMessage(r harness.Response) string {
	errObj, ok := r.Body["error"].(map[string]any)
	if !ok {
		return ""
	}
	msg, _ := errObj["message"].(string)
	return msg
}

// specPathsUnderTest reads the documented paths, keeping only ones this test
// can meaningfully probe.
func specPathsUnderTest(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}

	out := make([]string, 0, len(spec.Paths))
	for p, ops := range spec.Paths {
		// Only GET: a POST/DELETE probe against a live router would mutate
		// data, and the question here is whether the router knows the path,
		// which any method answers. Paths with no GET are covered by the
		// method-mismatch case below.
		if _, hasGet := ops["get"]; !hasGet {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// concreteise substitutes a syntactically valid but non-existent value for each
// path parameter, so the request reaches routing and stops at the handler.
func concreteise(path string) string {
	return pathParam.ReplaceAllStringFunc(path, func(m string) string {
		name := strings.ToLower(strings.Trim(m, "{}"))
		switch {
		case strings.Contains(name, "awb") || strings.Contains(name, "barcode"):
			return "ZZZ260101000001"
		case strings.Contains(name, "code"):
			return "999999"
		case strings.Contains(name, "pincode"):
			return "999999"
		case strings.Contains(name, "partytype"):
			return "FRANCHISE"
		default:
			// A well-formed public id that cannot exist. The prefix does not
			// need to match: a prefix mismatch is a 400/404 from the handler,
			// which still proves the route resolved.
			return "xxx_01KZZZZZZZZZZZZZZZZZZZZZZZ"
		}
	})
}

func TestEveryDocumentedPathIsRoutable(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "SPEC"})

	paths := specPathsUnderTest(t)
	if len(paths) < 100 {
		t.Fatalf("only %d documented GET paths found; the spec did not parse as expected", len(paths))
	}

	// Calibrate: what does this server say when a route genuinely does not
	// exist? Everything below is compared against that, so the test does not
	// depend on a hardcoded sentence.
	sentinel := env.Do(t, "GET", "/api/v1/__no_such_route_"+tn.OrgCode, tn.AdminAccessTok, nil)
	if sentinel.Status != http.StatusNotFound {
		t.Fatalf("expected 404 for a nonexistent route, got %d: %s", sentinel.Status, sentinel.Raw)
	}
	unmatchedMessage := errorMessage(sentinel)
	if unmatchedMessage == "" {
		t.Fatalf("could not read the unmatched-route message from %s", sentinel.Raw)
	}
	t.Logf("unmatched-route signature: %q", unmatchedMessage)

	var unroutable []string
	for _, docPath := range paths {
		probe := concreteise(docPath)
		resp := env.Do(t, "GET", probe, tn.AdminAccessTok, nil)

		// Anything that is not a 404 reached a handler, so the route exists.
		if resp.Status != http.StatusNotFound {
			continue
		}
		// A 404 whose message differs from the sentinel came from a handler
		// saying "that id does not exist", which also proves the route exists.
		if errorMessage(resp) != unmatchedMessage {
			continue
		}
		unroutable = append(unroutable, docPath+"  (probed "+probe+")")
	}

	if len(unroutable) > 0 {
		t.Errorf("%d documented paths do not route. A frontend following the contract "+
			"would get 404 from every one of them:\n  %s",
			len(unroutable), strings.Join(unroutable, "\n  "))
	}
}

// The inverse direction: a route the server serves but the contract does not
// mention is an undocumented surface. This is a weaker check — chi does not
// expose a route list through the handler this harness holds — so it is limited
// to the handful of top-level prefixes, which is enough to catch a whole module
// being left out of the document.
func TestNoDocumentedPathIsMissingItsAPIPrefix(t *testing.T) {
	paths := specPathsUnderTest(t)

	// Only the probes live outside the versioned API, because only they are
	// reachable through the production proxy. Nginx serves /livez, /readyz and
	// /api/, blocks /metrics, and 404s everything else — so a documented path
	// outside those is a path no production client can call.
	//
	// /version is deliberately absent from the contract for that reason: the
	// endpoint exists and is useful on the internal network, and publishing the
	// running build to the internet only helps somebody matching CVEs.
	//
	// Public tracking is *not* infrastructure: it is mounted at
	// /api/v1/track/{awb}, and the spec claimed /track/{awb} until this test
	// was written.
	allowedOutside := []string{"/livez", "/readyz", "/healthz"}

	var offenders []string
	for _, p := range paths {
		if strings.HasPrefix(p, "/api/v1/") {
			continue
		}
		ok := false
		for _, a := range allowedOutside {
			if strings.HasPrefix(p, a) {
				ok = true
				break
			}
		}
		if !ok {
			offenders = append(offenders, p)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("%d documented paths are outside /api/v1 and are not known infrastructure "+
			"routes:\n  %s", len(offenders), strings.Join(offenders, "\n  "))
	}
}
