// Package financeapi is the HTTP transport for the Release 3 finance modules.
//
// It follows ADR 0011 for the same reason opsapi does: the five finance modules
// are not independent resources. A COD collection posts a journal; approving a
// settlement reads commission and COD and posts a journal; issuing an invoice
// posts a journal. Giving each module its own handler package would duplicate
// the request plumbing five times or introduce cycles between transport layers.
//
// The modules keep their business logic free of net/http, and this package owns
// parsing, validation, permission gating and response shaping for all of them.
//
// # Permissions and maker/checker
//
// The `require` gate here is coarse: it answers "may this role perform this
// class of action at all". The maker/checker rule — that the *same person* may
// not both raise and approve — is not a permission question and is not enforced
// here. It lives in the services and in CHECK constraints, because it depends
// on who raised the specific row being acted on. A finance manager legitimately
// holds both halves of the pair; they simply cannot use both on one document.
package financeapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/billing"
	"github.com/ceserve/courier-os/internal/cod"
	"github.com/ceserve/courier-os/internal/commission"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/settlement"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Handler serves every Release 3 finance endpoint.
type Handler struct {
	ledger     *ledger.Service
	commission *commission.Service
	cod        *cod.Service
	settlement *settlement.Service
	billing    *billing.Service
}

// Services groups the module services the transport needs.
type Services struct {
	Ledger     *ledger.Service
	Commission *commission.Service
	COD        *cod.Service
	Settlement *settlement.Service
	Billing    *billing.Service
}

// New builds the finance transport.
func New(s Services) *Handler {
	return &Handler{
		ledger: s.Ledger, commission: s.Commission, cod: s.COD,
		settlement: s.Settlement, billing: s.Billing,
	}
}

// Routes mounts the authenticated finance surface.
func (h *Handler) Routes(r chi.Router) {
	r.Route("/ledger", h.ledgerRoutes)
	r.Route("/commission", h.commissionRoutes)
	r.Route("/cod", h.codRoutes)
	r.Route("/settlements", h.settlementRoutes)
	r.Route("/settlement-adjustments", h.adjustmentRoutes)
	r.Route("/invoices", h.invoiceRoutes)
	r.Route("/credit-notes", h.creditNoteRoutes)
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

func principal(r *http.Request) (*tenant.Principal, error) { return tenant.Require(r) }

// require wraps a handler in a permission check.
func require(permission string, h httpx.Handler) http.HandlerFunc {
	return httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		p, err := principal(r)
		if err != nil {
			return err
		}
		if err := p.Require(permission); err != nil {
			return err
		}
		return h(w, r)
	})
}

// body decodes JSON with unknown-field rejection, which is the mass-assignment
// control (§35): a client cannot smuggle a field the handler did not intend.
func body[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var dst T
	if err := httpx.DecodeJSON(w, r, &dst); err != nil {
		return dst, err
	}
	return dst, nil
}

func pathID(r *http.Request, param, prefix, resource string) (string, error) {
	return httpx.PathPublicID(r, param, prefix, resource)
}

// limit reads a bounded page size. Every list endpoint paginates (§27).
func limit(r *http.Request) int32 {
	n, err := httpx.QueryInt(r, "limit", 50, 1, 200)
	if err != nil {
		return 50
	}
	return int32(n)
}

func offset(r *http.Request) int32 {
	n, err := httpx.QueryInt(r, "offset", 0, 0, 100000)
	if err != nil {
		return 0
	}
	return int32(n)
}

// optDate parses an optional YYYY-MM-DD query parameter.
func optDate(r *http.Request, name string) (*time.Time, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, apierr.Validation("Invalid date.",
			map[string]any{name: "expected YYYY-MM-DD"})
	}
	return &t, nil
}

// reqDate parses a required YYYY-MM-DD field from a request body value.
func reqDate(value, field string) (time.Time, error) {
	if value == "" {
		return time.Time{}, apierr.Validation("This date is required.",
			map[string]any{field: "expected YYYY-MM-DD"})
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, apierr.Validation("Invalid date.",
			map[string]any{field: "expected YYYY-MM-DD"})
	}
	return t, nil
}

// optInt64 turns an optional numeric query parameter into a pointer.
func optInt64(r *http.Request, name string) *int64 {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil
	}
	n, err := httpx.QueryInt(r, name, 0, 0, 1<<62)
	if err != nil || n == 0 {
		return nil
	}
	v := int64(n)
	return &v
}

// txn runs a function inside a database transaction.
//
// Most finance operations open their own transaction inside the service. The
// exceptions are the ledger's Post and Reverse, which deliberately take a
// pgx.Tx so a caller can compose them with other writes; when the transport
// invokes them directly it supplies the transaction here.
func (h *Handler) txn(r *http.Request, fn func(tx pgx.Tx) error) error {
	return h.ledger.DB().InTx(r.Context(), fn)
}

// pathInt64 reads a numeric path parameter.
//
// Used only where the identifier is a party — an agent, a branch, a franchise —
// referenced by its internal id in a custody query. Business objects are always
// addressed by prefixed public id (§11); a party in a custody lookup is a
// coordinate, not an object the API hands out.
func pathInt64(r *http.Request, param string) (int64, error) {
	raw := chi.URLParam(r, param)
	if raw == "" {
		return 0, apierr.Validation("Missing path parameter.", map[string]any{param: "required"})
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return 0, apierr.Validation("Invalid identifier.",
			map[string]any{param: "expected a positive integer"})
	}
	return v, nil
}
