// Package customer owns retail and business customers, their contacts,
// addresses, billing profile and credit state (M07).
package customer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/auth"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/geography"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Enums.
var (
	CustomerTypes  = []string{"RETAIL", "BUSINESS"}
	CustomerStatus = []string{"ACTIVE", "SUSPENDED", "CLOSED"}
	AddressTypes   = []string{"PICKUP", "DELIVERY", "BILLING", "BOTH"}
	ContactTypes   = []string{"PRIMARY", "BILLING", "OPERATIONS", "ESCALATION"}
	CreditStatuses = []string{"GOOD", "WARNING", "ON_HOLD", "BLOCKED"}
	BillingCycles  = []string{"WEEKLY", "FORTNIGHTLY", "MONTHLY"}
)

// Service exposes customer reads used by booking.
type Service struct {
	db  *database.DB
	q   *dbgen.Queries
	log *slog.Logger
}

// NewService builds the customer service.
func NewService(db *database.DB, q *dbgen.Queries, log *slog.Logger) *Service {
	return &Service{db: db, q: q, log: log}
}

// Resolved bundles the customer with the commercial context booking needs.
type Resolved struct {
	Customer       dbgen.Customer
	CreditProfile  *dbgen.CreditProfile
	FranchiseID    *int64
	BillingStateID *int64
}

// ResolveForBooking loads a customer and everything booking must check.
func (s *Service) ResolveForBooking(ctx context.Context, orgID int64, publicID string) (*Resolved, error) {
	row, err := s.q.GetCustomerByPublicID(ctx, dbgen.GetCustomerByPublicIDParams{
		PublicID: publicID, OrganizationID: orgID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.NotFound("Customer")
		}
		return nil, apierr.Internal(fmt.Errorf("load customer: %w", err))
	}
	out := &Resolved{Customer: dbgen.Customer{
		ID: row.ID, PublicID: row.PublicID, OrganizationID: row.OrganizationID,
		Code: row.Code, CustomerType: row.CustomerType, Name: row.Name,
		Email: row.Email, Phone: row.Phone, OwningUnitID: row.OwningUnitID,
		Status: row.Status, SuspensionReason: row.SuspensionReason,
		GstNumber: row.GstNumber, PanNumber: row.PanNumber, Version: row.Version,
	}}
	if cp, cErr := s.q.GetCreditProfileByCustomer(ctx, row.ID); cErr == nil {
		out.CreditProfile = &cp
	} else if !database.IsNoRows(cErr) {
		return nil, apierr.Internal(cErr)
	}
	if row.OwningUnitID != nil {
		if f, fErr := s.q.GetFranchiseByOperatingUnit(ctx, dbgen.GetFranchiseByOperatingUnitParams{
			OperatingUnitID: *row.OwningUnitID, OrganizationID: row.OrganizationID}); fErr == nil {
			out.FranchiseID = &f.ID
		}
	}
	if bp, bErr := s.q.GetBillingProfileByCustomer(ctx, row.ID); bErr == nil {
		out.BillingStateID = bp.PlaceOfSupplyStateID
	}
	return out, nil
}

// ReserveCredit checks and consumes credit inside a booking transaction.
//
// The profile row is locked FOR UPDATE first, so two concurrent bookings on the
// same account cannot both see headroom and both pass (Constitution §22). Every
// change writes an append-only ledger entry alongside the counter, so the
// balance is always reproducible from history.
func (s *Service) ReserveCredit(
	ctx context.Context, tx pgx.Tx, orgID, customerID int64, amountMinor int64,
	shipmentID *int64, actorUserID *int64, requestID string,
) error {
	if amountMinor <= 0 {
		return nil
	}
	q := s.q.WithTx(tx)
	profile, err := q.LockCreditProfile(ctx, dbgen.LockCreditProfileParams{
		CustomerID: customerID, OrganizationID: orgID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Conflict(apierr.CodeConflict,
				"This customer has no credit profile. Set a credit limit before booking on credit terms.")
		}
		return apierr.Internal(fmt.Errorf("lock credit profile: %w", err))
	}
	switch profile.CreditStatus {
	case "BLOCKED", "ON_HOLD":
		return apierr.Conflict(apierr.CodeConflict,
			"This customer's credit account is on hold. Settle the outstanding balance or book as prepaid.").
			WithDetail("creditStatus", profile.CreditStatus)
	}
	if profile.CreditUsedMinor+amountMinor > profile.CreditLimitMinor {
		return apierr.Conflict(apierr.CodeConflict,
			"This booking exceeds the customer's available credit.").
			WithDetail("creditLimitMinor", profile.CreditLimitMinor).
			WithDetail("creditUsedMinor", profile.CreditUsedMinor).
			WithDetail("requiredMinor", amountMinor)
	}

	updated, err := q.AdjustCreditUsage(ctx, dbgen.AdjustCreditUsageParams{
		CustomerID: customerID, OrganizationID: orgID, DeltaMinor: amountMinor,
	})
	if err != nil {
		return apierr.Internal(fmt.Errorf("adjust credit usage: %w", err))
	}
	entry := dbgen.InsertCreditEntryParams{
		OrganizationID: orgID, CustomerID: customerID, EntryType: "RESERVE",
		Currency: profile.Currency, AmountMinor: amountMinor,
		BalanceAfterMinor: updated.CreditUsedMinor, ShipmentID: shipmentID,
		CreatedBy: actorUserID,
	}
	if requestID != "" {
		entry.RequestID = &requestID
	}
	if _, err := q.InsertCreditEntry(ctx, entry); err != nil {
		return apierr.Internal(fmt.Errorf("record credit entry: %w", err))
	}
	return nil
}

// ReleaseCredit returns reserved credit, for example on cancellation.
func (s *Service) ReleaseCredit(
	ctx context.Context, tx pgx.Tx, orgID, customerID int64, amountMinor int64,
	shipmentID *int64, actorUserID *int64, reason string,
) error {
	if amountMinor <= 0 {
		return nil
	}
	q := s.q.WithTx(tx)
	profile, err := q.LockCreditProfile(ctx, dbgen.LockCreditProfileParams{
		CustomerID: customerID, OrganizationID: orgID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil // nothing was reserved
		}
		return apierr.Internal(err)
	}
	// Never release more than is outstanding; the counter has a >= 0 CHECK and
	// a negative balance would be meaningless.
	if amountMinor > profile.CreditUsedMinor {
		amountMinor = profile.CreditUsedMinor
	}
	if amountMinor == 0 {
		return nil
	}
	updated, err := q.AdjustCreditUsage(ctx, dbgen.AdjustCreditUsageParams{
		CustomerID: customerID, OrganizationID: orgID, DeltaMinor: -amountMinor,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	_, err = q.InsertCreditEntry(ctx, dbgen.InsertCreditEntryParams{
		OrganizationID: orgID, CustomerID: customerID, EntryType: "RELEASE",
		Currency: profile.Currency, AmountMinor: -amountMinor,
		BalanceAfterMinor: updated.CreditUsedMinor, ShipmentID: shipmentID,
		Reason: optional(reason), CreatedBy: actorUserID,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	return nil
}

// ---- handlers --------------------------------------------------------------

// Handler exposes customer administration.
type Handler struct {
	svc   *Service
	geo   *geography.Service
	audit *audit.Recorder
}

// NewHandler builds the customer handler.
func NewHandler(svc *Service, geo *geography.Service, rec *audit.Recorder) *Handler {
	return &Handler{svc: svc, geo: geo, audit: rec}
}

// Routes mounts customer endpoints under /customers.
func (h *Handler) Routes(r chi.Router) {
	r.With(auth.RequirePermission("customer.read")).Get("/", httpx.Wrap(h.list))
	r.With(auth.RequirePermission("customer.create")).Post("/", httpx.Wrap(h.create))
	r.With(auth.RequirePermission("customer.read")).Get("/{customerId}", httpx.Wrap(h.get))
	r.With(auth.RequirePermission("customer.update")).Patch("/{customerId}", httpx.Wrap(h.update))
	r.With(auth.RequirePermission("customer.suspend")).Post("/{customerId}/status", httpx.Wrap(h.setStatus))

	r.With(auth.RequirePermission("customer.read")).Get("/{customerId}/addresses", httpx.Wrap(h.listAddresses))
	r.With(auth.RequirePermission("customer.update")).Post("/{customerId}/addresses", httpx.Wrap(h.createAddress))
	r.With(auth.RequirePermission("customer.update")).Patch("/{customerId}/addresses/{addressId}", httpx.Wrap(h.updateAddress))

	r.With(auth.RequirePermission("customer.read")).Get("/{customerId}/contacts", httpx.Wrap(h.listContacts))
	r.With(auth.RequirePermission("customer.update")).Post("/{customerId}/contacts", httpx.Wrap(h.createContact))

	r.With(auth.RequirePermission("customer.read")).Get("/{customerId}/credit", httpx.Wrap(h.getCredit))
	r.With(auth.RequirePermission("customer_credit.manage")).Put("/{customerId}/credit", httpx.Wrap(h.setCredit))
	r.With(auth.RequirePermission("customer.update")).Put("/{customerId}/billing-profile", httpx.Wrap(h.setBillingProfile))
}

type createCustomerRequest struct {
	Code            string                  `json:"code,omitempty"`
	CustomerType    string                  `json:"customerType"`
	Name            string                  `json:"name"`
	Email           string                  `json:"email,omitempty"`
	Phone           string                  `json:"phone"`
	OwningUnitID    string                  `json:"owningUnitId,omitempty"`
	GSTNumber       string                  `json:"gstNumber,omitempty"`
	PANNumber       string                  `json:"panNumber,omitempty"`
	Notes           string                  `json:"notes,omitempty"`
	Metadata        map[string]any          `json:"metadata,omitempty"`
	BusinessAccount *businessAccountRequest `json:"businessAccount,omitempty"`
}

type businessAccountRequest struct {
	AccountCode      string `json:"accountCode"`
	LegalName        string `json:"legalName"`
	Industry         string `json:"industry,omitempty"`
	CreditLimitMinor int64  `json:"creditLimitMinor"`
	PaymentTermsDays int    `json:"paymentTermsDays"`
	BillingCycle     string `json:"billingCycle,omitempty"`
	RateCardID       string `json:"rateCardId,omitempty"`
}

type updateCustomerRequest struct {
	Name            *string        `json:"name,omitempty"`
	Email           *string        `json:"email,omitempty"`
	Phone           *string        `json:"phone,omitempty"`
	OwningUnitID    *string        `json:"owningUnitId,omitempty"`
	GSTNumber       *string        `json:"gstNumber,omitempty"`
	PANNumber       *string        `json:"panNumber,omitempty"`
	Notes           *string        `json:"notes,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	ExpectedVersion int32          `json:"expectedVersion"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := window(r)
	if err != nil {
		return err
	}
	params := dbgen.ListCustomersParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if ct, cErr := httpx.QueryEnum(r, "customerType", CustomerTypes); cErr != nil {
		return cErr
	} else if ct != "" {
		params.CustomerType = &ct
	}
	if st, sErr := httpx.QueryEnum(r, "status", CustomerStatus); sErr != nil {
		return sErr
	} else if st != "" {
		params.Status = &st
	}
	if s := httpx.Query(r, "search"); s != "" {
		params.Search = &s
	}
	// Branch staff see the customers owned by their branch; a portal user sees
	// only the customer records they are linked to.
	if scope := p.UnitScope("customer.suspend"); scope != nil {
		params.ScopedUnitIds = scope
	}
	if ids := p.CustomerScope(); ids != nil {
		params.CustomerIds = ids
	}

	rows, err := h.svc.q.ListCustomers(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, c := range rows {
		total = c.TotalCount
		item := map[string]any{
			"id": c.PublicID, "code": c.Code, "customerType": c.CustomerType,
			"name": c.Name, "email": c.Email, "phone": c.Phone, "status": c.Status,
			"owningUnitCode": c.OwningUnitCode, "createdAt": c.CreatedAt, "version": c.Version,
		}
		if c.AccountCode != nil {
			item["businessAccount"] = map[string]any{
				"accountCode": *c.AccountCode, "creditLimitMinor": c.AccountCreditLimitMinor,
			}
		}
		if c.CreditStatus != nil {
			item["credit"] = map[string]any{
				"status": *c.CreditStatus, "usedMinor": c.CreditUsedMinor,
			}
		}
		items = append(items, item)
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "name", "asc"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createCustomerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	customerType := v.Enum("customerType", req.CustomerType, CustomerTypes, true)
	name := v.Text("name", req.Name, 2, 160, true)
	phone := v.Phone("phone", req.Phone, true)
	var email string
	if req.Email != "" {
		email = v.Email("email", req.Email)
	}
	gst := v.GSTIN("gstNumber", req.GSTNumber, false)
	pan := v.PAN("panNumber", req.PANNumber, false)
	code := req.Code
	if code == "" {
		// Auto-generated codes stay unique without a sequence table; the
		// per-tenant unique index remains the authority.
		code = "CUS-" + publicid.New("x")[2:12]
	}
	code = v.Code("code", code)
	if customerType == "BUSINESS" && req.BusinessAccount == nil {
		v.Add("businessAccount", "A BUSINESS customer requires a business account block.")
	}
	if customerType == "RETAIL" && req.BusinessAccount != nil {
		v.Add("businessAccount", "A RETAIL customer must not carry a business account block.")
	}
	if err := v.Err(); err != nil {
		return err
	}

	params := dbgen.CreateCustomerParams{
		PublicID: publicid.New(publicid.PrefixCustomer), OrganizationID: p.OrganizationID,
		Code: code, CustomerType: customerType, Name: name,
		Email: optional(email), Phone: phone,
		GstNumber: optional(gst), PanNumber: optional(pan), Notes: optional(req.Notes),
		Metadata: encodeJSON(req.Metadata), CreatedBy: &p.UserID,
	}
	if req.OwningUnitID != "" {
		unit, uErr := h.svc.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
			PublicID: req.OwningUnitID, OrganizationID: p.OrganizationID,
		})
		if uErr != nil {
			return apierr.Validation("The owning operating unit does not exist.", nil)
		}
		if err := p.RequireUnitInScope(unit.ID); err != nil {
			return err
		}
		params.OwningUnitID = &unit.ID
	} else if len(p.ScopedUnitIDs) == 1 && !p.HasUnscopedRole {
		// Branch staff implicitly own the customers they create.
		params.OwningUnitID = &p.ScopedUnitIDs[0]
	}

	var created dbgen.Customer
	err = h.svc.db.InTx(r.Context(), func(tx pgx.Tx) error {
		qtx := h.svc.q.WithTx(tx)
		var cErr error
		created, cErr = qtx.CreateCustomer(r.Context(), params)
		if cErr != nil {
			if database.IsUniqueViolation(cErr, "customers_code_unique") {
				return apierr.Duplicate("A customer with this code already exists.")
			}
			return apierr.Internal(fmt.Errorf("create customer: %w", cErr))
		}
		if req.BusinessAccount == nil {
			return nil
		}
		ba := req.BusinessAccount
		bv := validate.New()
		accountCode := bv.Code("businessAccount.accountCode", ba.AccountCode)
		legalName := bv.Text("businessAccount.legalName", ba.LegalName, 2, 200, true)
		bv.NonNegativeMinor("businessAccount.creditLimitMinor", ba.CreditLimitMinor)
		bv.IntRange("businessAccount.paymentTermsDays", ba.PaymentTermsDays, 0, 180)
		cycle := "MONTHLY"
		if ba.BillingCycle != "" {
			cycle = bv.Enum("businessAccount.billingCycle", ba.BillingCycle, BillingCycles, true)
		}
		if bErr := bv.Err(); bErr != nil {
			return bErr
		}
		accountParams := dbgen.CreateBusinessAccountParams{
			PublicID: publicid.New(publicid.PrefixBusinessAccount), OrganizationID: p.OrganizationID,
			CustomerID: created.ID, AccountCode: accountCode, LegalName: legalName,
			Industry: optional(ba.Industry), CreditLimitMinor: ba.CreditLimitMinor,
			Currency: p.OrganizationCurrency, PaymentTermsDays: int32(ba.PaymentTermsDays),
			BillingCycle: cycle, TaxMetadata: []byte("{}"),
		}
		if ba.RateCardID != "" {
			card, rcErr := qtx.GetRateCardByPublicID(r.Context(), dbgen.GetRateCardByPublicIDParams{
				PublicID: ba.RateCardID, OrganizationID: p.OrganizationID,
			})
			if rcErr != nil {
				return apierr.Validation("The rate card does not exist.", nil)
			}
			accountParams.RateCardID = &card.ID
		}
		if _, aErr := qtx.CreateBusinessAccount(r.Context(), accountParams); aErr != nil {
			if database.IsUniqueViolation(aErr) {
				return apierr.Duplicate("A business account with this code already exists.")
			}
			return apierr.Internal(aErr)
		}
		// A business account implies a credit profile; creating them together
		// means booking on credit terms never fails on missing configuration.
		if _, cpErr := qtx.UpsertCreditProfile(r.Context(), dbgen.UpsertCreditProfileParams{
			PublicID: publicid.New(publicid.PrefixCreditProfile), OrganizationID: p.OrganizationID,
			CustomerID: created.ID, Currency: p.OrganizationCurrency,
			CreditLimitMinor: ba.CreditLimitMinor, PaymentTermsDays: int32(ba.PaymentTermsDays),
			CreditStatus: "GOOD",
		}); cpErr != nil {
			return apierr.Internal(cpErr)
		}
		return nil
	})
	if err != nil {
		return err
	}

	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionCustomerCreated, ResourceType: "customer",
		ResourceID: &created.ID, ResourcePublicID: created.PublicID,
		OperatingUnitID: created.OwningUnitID,
		After: map[string]any{
			"code": created.Code, "name": created.Name, "customerType": created.CustomerType,
		},
	}))
	return httpx.Created(w, "/api/v1/customers/"+created.PublicID, map[string]any{
		"id": created.PublicID, "code": created.Code, "name": created.Name,
		"customerType": created.CustomerType, "status": created.Status, "version": created.Version,
	})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customerID, err := httpx.PathPublicID(r, "customerId", publicid.PrefixCustomer, "Customer")
	if err != nil {
		return err
	}
	row, err := h.svc.q.GetCustomerByPublicID(r.Context(), dbgen.GetCustomerByPublicIDParams{
		PublicID: customerID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Customer")
		}
		return apierr.Internal(err)
	}
	if !p.IsCustomerInScope(row.ID) {
		return apierr.NotFound("Customer")
	}

	out := map[string]any{
		"id": row.PublicID, "code": row.Code, "customerType": row.CustomerType,
		"name": row.Name, "email": row.Email, "phone": row.Phone, "status": row.Status,
		"gstNumber": row.GstNumber, "panNumber": row.PanNumber, "notes": row.Notes,
		"owningUnitCode": row.OwningUnitCode, "owningUnitName": row.OwningUnitName,
		"metadata": decodeJSON(row.Metadata), "version": row.Version,
		"createdAt": row.CreatedAt, "updatedAt": row.UpdatedAt,
	}
	if ba, bErr := h.svc.q.GetBusinessAccountByCustomer(r.Context(), row.ID); bErr == nil {
		out["businessAccount"] = map[string]any{
			"id": ba.PublicID, "accountCode": ba.AccountCode, "legalName": ba.LegalName,
			"creditLimitMinor": ba.CreditLimitMinor, "paymentTermsDays": ba.PaymentTermsDays,
			"billingCycle": ba.BillingCycle, "status": ba.Status,
			"rateCardId": ba.RateCardPublicID, "rateCardCode": ba.RateCardCode,
		}
	}
	if cp, cErr := h.svc.q.GetCreditProfileByCustomer(r.Context(), row.ID); cErr == nil {
		out["credit"] = creditView(cp)
	}
	return httpx.OK(w, out)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customerID, err := httpx.PathPublicID(r, "customerId", publicid.PrefixCustomer, "Customer")
	if err != nil {
		return err
	}
	var req updateCustomerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.ExpectedVersion < 1 {
		return apierr.Validation("expectedVersion is required and must be the version you last read.", nil)
	}
	existing, err := h.svc.q.GetCustomerByPublicID(r.Context(), dbgen.GetCustomerByPublicIDParams{
		PublicID: customerID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Customer")
		}
		return apierr.Internal(err)
	}
	if !p.IsCustomerInScope(existing.ID) {
		return apierr.NotFound("Customer")
	}

	v := validate.New()
	params := dbgen.UpdateCustomerParams{
		PublicID: customerID, OrganizationID: p.OrganizationID, ExpectedVersion: req.ExpectedVersion,
	}
	if req.Name != nil {
		n := v.Text("name", *req.Name, 2, 160, true)
		params.Name = &n
	}
	if req.Email != nil {
		e := v.Email("email", *req.Email)
		params.Email = &e
	}
	if req.Phone != nil {
		ph := v.Phone("phone", *req.Phone, true)
		params.Phone = &ph
	}
	if req.GSTNumber != nil {
		g := v.GSTIN("gstNumber", *req.GSTNumber, false)
		params.GstNumber = &g
	}
	if req.PANNumber != nil {
		pn := v.PAN("panNumber", *req.PANNumber, false)
		params.PanNumber = &pn
	}
	params.Notes = req.Notes
	if req.Metadata != nil {
		params.Metadata = encodeJSON(req.Metadata)
	}
	if req.OwningUnitID != nil && *req.OwningUnitID != "" {
		unit, uErr := h.svc.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
			PublicID: *req.OwningUnitID, OrganizationID: p.OrganizationID,
		})
		if uErr != nil {
			v.Add("owningUnitId", "The operating unit does not exist.")
		} else {
			params.OwningUnitID = &unit.ID
		}
	}
	if err := v.Err(); err != nil {
		return err
	}

	updated, err := h.svc.q.UpdateCustomer(r.Context(), params)
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Conflict(apierr.CodeConcurrentModification,
				"This customer was modified by someone else. Reload it and try again.").
				WithDetail("expectedVersion", req.ExpectedVersion).
				WithDetail("currentVersion", existing.Version)
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionCustomerUpdated, ResourceType: "customer",
		ResourceID: &updated.ID, ResourcePublicID: updated.PublicID,
		Before: map[string]any{"name": existing.Name, "email": existing.Email, "phone": existing.Phone},
		After:  map[string]any{"name": updated.Name, "email": updated.Email, "phone": updated.Phone},
	}))
	return httpx.OK(w, map[string]any{
		"id": updated.PublicID, "code": updated.Code, "name": updated.Name,
		"status": updated.Status, "version": updated.Version,
	})
}

type statusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customerID, err := httpx.PathPublicID(r, "customerId", publicid.PrefixCustomer, "Customer")
	if err != nil {
		return err
	}
	var req statusRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	status := v.Enum("status", req.Status, CustomerStatus, true)
	if status == "SUSPENDED" {
		v.Text("reason", req.Reason, 5, 500, true)
	}
	if err := v.Err(); err != nil {
		return err
	}
	existing, err := h.svc.q.GetCustomerByPublicID(r.Context(), dbgen.GetCustomerByPublicIDParams{
		PublicID: customerID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Customer")
		}
		return apierr.Internal(err)
	}
	params := dbgen.SetCustomerStatusParams{
		PublicID: customerID, OrganizationID: p.OrganizationID, Status: status,
	}
	if req.Reason != "" {
		params.SuspensionReason = &req.Reason
	}
	updated, err := h.svc.q.SetCustomerStatus(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	action := audit.ActionCustomerReinstated
	if status != "ACTIVE" {
		action = audit.ActionCustomerSuspended
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: action, ResourceType: "customer",
		ResourceID: &updated.ID, ResourcePublicID: updated.PublicID, Reason: req.Reason,
		Before: map[string]any{"status": existing.Status},
		After:  map[string]any{"status": updated.Status},
	}))
	return httpx.OK(w, map[string]any{"id": updated.PublicID, "status": updated.Status})
}

// ---- helpers ---------------------------------------------------------------

func creditView(cp dbgen.CreditProfile) map[string]any {
	return map[string]any{
		"creditLimitMinor": cp.CreditLimitMinor,
		"creditUsedMinor":  cp.CreditUsedMinor,
		"availableMinor":   cp.CreditLimitMinor - cp.CreditUsedMinor,
		"currency":         cp.Currency,
		"creditStatus":     cp.CreditStatus,
		"paymentTermsDays": cp.PaymentTermsDays,
		"blockedReason":    cp.BlockedReason,
		"lastReviewedAt":   cp.LastReviewedAt,
	}
}

func window(r *http.Request) (limit, page int, err error) {
	limit, err = pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return 0, 0, err
	}
	page, err = httpx.QueryInt(r, "page", 1, 1, 10_000)
	if err != nil {
		return 0, 0, err
	}
	return limit, page, nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func encodeJSON(m map[string]any) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func decodeJSON(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
