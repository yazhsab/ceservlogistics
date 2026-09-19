package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/httpx"
)

func TestOperationalBrowserPreflight(t *testing.T) {
	wantedHeaders := []string{"Authorization", "Content-Type", ops.HeaderDeviceID,
		ops.HeaderDeviceEvent, ops.HeaderDeviceModel, ops.HeaderSource, "X-Operating-Unit"}
	handler := httpx.CORS(config.HTTPConfig{CORSOrigins: []string{"https://app.example.test"}})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("preflight must not execute the operation")
		}))
	for _, origin := range []string{"https://app.example.test", "https://untrusted.example.test"} {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, "/api/v1/pickups/pkr_test/complete", nil)
			req.Header.Set("Origin", origin)
			req.Header.Set("Access-Control-Request-Method", "POST")
			req.Header.Set("Access-Control-Request-Headers", strings.ToLower(strings.Join(wantedHeaders, ",")))
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != http.StatusNoContent {
				t.Fatalf("preflight status = %d", res.Code)
			}
			if origin != "https://app.example.test" {
				if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
					t.Fatalf("untrusted origin authorized: %q", got)
				}
				return
			}
			if res.Header().Get("Access-Control-Allow-Origin") != origin {
				t.Fatal("configured frontend origin not authorized")
			}
			allowed := map[string]bool{}
			for _, h := range strings.Split(res.Header().Get("Access-Control-Allow-Headers"), ",") {
				allowed[strings.ToLower(strings.TrimSpace(h))] = true
			}
			for _, h := range wantedHeaders {
				if !allowed[strings.ToLower(h)] {
					t.Errorf("browser would block required operational header %s", h)
				}
			}
		})
	}
}
