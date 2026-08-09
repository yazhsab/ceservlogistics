// Package portal implements the three audience-specific surfaces of Release 4:
// the customer portal (M27), the franchise portal (M28) and the hub/branch
// console (M29).
//
// # Why one package for three portals
//
// They are not three feature sets. They are three *answers to the same
// question*: given who is asking, what may they see and how much of it should
// travel? Each one is a scope rule plus a projection, over services that
// already exist. Splitting them would triplicate the scope resolution, which is
// the part that must not drift.
//
// # The scope rule is the feature
//
//   - A **customer** sees only accounts they are linked to through
//     customer_users. Never another customer's, never the tenant's.
//   - A **franchise** sees only its own operating units, its own commission,
//     its own COD custody and its own settlements.
//   - A **console** sees one facility at a time, and only what the operator is
//     assigned to.
//
// In every case the scope is resolved from the authenticated principal, never
// from a parameter the client supplies (§9). A request that names an account or
// a facility gets it checked; a request that names nothing gets its own.
//
// # Payloads are deliberately small
//
// The console in particular returns ten columns where the internal API returns
// forty. A handheld scanner on a branch's mobile connection is the worst
// network in the platform, and it runs these queries more than anything else
// runs anything.
package portal

import (
	"context"
	"log/slog"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Permissions.
const (
	PermCustomer  = "portal.customer"
	PermFranchise = "portal.franchise"
	PermConsole   = "portal.console"
)

// Service answers portal questions.
type Service struct {
	db  *database.DB
	q   *dbgen.Queries
	log *slog.Logger
}

func NewService(db *database.DB, q *dbgen.Queries, log *slog.Logger) *Service {
	return &Service{db: db, q: q, log: log}
}

// ---------------------------------------------------------------------------
// M27 Customer portal
// ---------------------------------------------------------------------------

// CustomerScope returns the customer ids a portal principal may act for.
//
// It refuses a staff principal outright rather than treating "no restriction"
// as "everything". That distinction is the whole security boundary here:
// Principal.CustomerScope returns nil for a staff user meaning *unrestricted*,
// and passing nil into a customer-portal query would hand one customer the
// tenant's entire book.
func CustomerScope(p *tenant.Principal) ([]int64, error) {
	if !p.IsPortalUser {
		return nil, apierr.Forbidden(
			"The customer portal is for customer accounts. Staff use the operations API.")
	}
	ids := p.CustomerScope()
	if len(ids) == 0 {
		return nil, apierr.Forbidden(
			"This login is not linked to a customer account. Contact your account manager.")
	}
	return ids, nil
}

// CustomerSummary is the portal landing payload.
type CustomerSummary struct {
	WindowDays     int    `json:"windowDays"`
	Total          int64  `json:"total"`
	Delivered      int64  `json:"delivered"`
	InProgress     int64  `json:"inProgress"`
	Exceptions     int64  `json:"exceptions"`
	Returning      int64  `json:"returning"`
	SpendMinor     int64  `json:"spendMinor"`
	CODBookedMinor int64  `json:"codBookedMinor"`
	Currency       string `json:"currency"`
	// DeliveryRateBP is delivered over total, in basis points.
	DeliveryRateBP int64 `json:"deliveryRateBasisPoints"`
}

// Summary returns the customer's own tiles.
func (s *Service) Summary(
	ctx context.Context, p *tenant.Principal, days int,
) (*CustomerSummary, error) {
	ids, err := CustomerScope(p)
	if err != nil {
		return nil, err
	}
	if days <= 0 || days > 400 {
		days = 90
	}
	row, err := s.q.PortalCustomerSummary(ctx, dbgen.PortalCustomerSummaryParams{
		OrganizationID: p.OrganizationID, CustomerIds: ids,
		Since: time.Now().AddDate(0, 0, -days),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return &CustomerSummary{
		WindowDays: days, Total: row.Total, Delivered: row.Delivered,
		InProgress: row.InProgress, Exceptions: row.Exceptions,
		Returning: row.Returning, SpendMinor: row.SpendMinor,
		CODBookedMinor: row.CodBookedMinor, Currency: p.OrganizationCurrency,
		DeliveryRateBP: rateBP(row.Delivered, row.Total),
	}, nil
}

// Account is one customer the portal user may act for.
type Account struct {
	ID               string `json:"id"`
	Code             string `json:"code"`
	Name             string `json:"name"`
	CustomerType     string `json:"customerType"`
	Status           string `json:"status"`
	CreditLimitMinor *int64 `json:"creditLimitMinor,omitempty"`
	CreditUsedMinor  *int64 `json:"creditUsedMinor,omitempty"`
	// CreditAvailableMinor is computed rather than stored, so it cannot drift
	// from the two numbers it is derived from.
	CreditAvailableMinor *int64  `json:"creditAvailableMinor,omitempty"`
	PaymentTermsDays     *int32  `json:"paymentTermsDays,omitempty"`
	CreditStatus         *string `json:"creditStatus,omitempty"`
	Currency             *string `json:"currency,omitempty"`
}

// Accounts lists the accounts behind this login.
func (s *Service) Accounts(ctx context.Context, p *tenant.Principal) ([]Account, error) {
	ids, err := CustomerScope(p)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.PortalCustomerAccounts(ctx, dbgen.PortalCustomerAccountsParams{
		OrganizationID: p.OrganizationID, CustomerIds: ids,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]Account, 0, len(rows))
	for _, r := range rows {
		a := Account{
			ID: r.PublicID, Code: r.Code, Name: r.Name,
			CustomerType: r.CustomerType, Status: r.Status,
			CreditLimitMinor: r.CreditLimitMinor, CreditUsedMinor: r.CreditUsedMinor,
			PaymentTermsDays: r.PaymentTermsDays, CreditStatus: r.CreditStatus,
			Currency: r.Currency,
		}
		if r.CreditLimitMinor != nil && r.CreditUsedMinor != nil {
			available := *r.CreditLimitMinor - *r.CreditUsedMinor
			a.CreditAvailableMinor = &available
		}
		out = append(out, a)
	}
	return out, nil
}

// InvoiceSummary is one invoice as a customer sees it.
type InvoiceSummary struct {
	ID               string  `json:"id"`
	Number           string  `json:"invoiceNumber"`
	Status           string  `json:"status"`
	IssueDate        string  `json:"issueDate"`
	DueDate          string  `json:"dueDate"`
	TotalMinor       int64   `json:"totalMinor"`
	PaidMinor        int64   `json:"paidMinor"`
	OutstandingMinor int64   `json:"outstandingMinor"`
	Currency         string  `json:"currency"`
	PeriodStart      *string `json:"periodStart,omitempty"`
	PeriodEnd        *string `json:"periodEnd,omitempty"`
	Customer         string  `json:"customer"`
}

// Invoices lists the customer's own invoices.
func (s *Service) Invoices(
	ctx context.Context, p *tenant.Principal, status string, cursor *int64, limit int32,
) ([]InvoiceSummary, error) {
	ids, err := CustomerScope(p)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.PortalCustomerInvoices(ctx, dbgen.PortalCustomerInvoicesParams{
		OrganizationID: p.OrganizationID, CustomerIds: ids,
		Status: ops.Optional(status), CursorID: cursor, RowLimit: clampLimit(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]InvoiceSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, InvoiceSummary{
			ID: r.PublicID, Number: r.InvoiceNumber, Status: r.Status,
			IssueDate: r.IssueDate.Format(dateLayout), DueDate: r.DueDate.Format(dateLayout),
			TotalMinor: r.TotalMinor, PaidMinor: r.PaidMinor,
			OutstandingMinor: r.TotalMinor - r.PaidMinor,
			Currency:         r.Currency,
			PeriodStart:      dateOrNil(r.PeriodStart), PeriodEnd: dateOrNil(r.PeriodEnd),
			Customer: r.CustomerName,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// M28 Franchise portal
// ---------------------------------------------------------------------------

// Franchise is the resolved identity of a franchise principal.
type Franchise struct {
	ID       int64
	PublicID string
	Code     string
	Name     string
	UnitID   int64
	UnitCode string
}

// ResolveFranchise finds the franchise behind the authenticated principal.
//
// Derived from the principal's operating-unit grants, never from a parameter.
// A franchise operator is assigned to their unit; the franchise is whatever
// owns that unit. An unscoped principal is refused rather than shown the first
// franchise in the list — "which franchise am I?" has no answer for a head
// office user, and guessing would be worse than saying so.
func (s *Service) ResolveFranchise(
	ctx context.Context, p *tenant.Principal, requested string,
) (*Franchise, error) {
	// A head-office user may name one explicitly, if their role lets them see
	// every unit. Anyone else gets their own and only their own.
	if requested != "" {
		f, err := s.q.GetFranchiseByPublicID(ctx, dbgen.GetFranchiseByPublicIDParams{
			PublicID: requested, OrganizationID: p.OrganizationID,
		})
		if err != nil {
			return nil, ops.NotFoundOr(err, "Franchise")
		}
		if err := p.RequireUnitInScope(f.OperatingUnitID); err != nil {
			return nil, apierr.NotFound("Franchise")
		}
		return s.franchiseFrom(ctx, p, dbgen.Franchise{
			ID: f.ID, PublicID: f.PublicID, Code: f.Code, Name: f.Name,
			OperatingUnitID: f.OperatingUnitID,
		})
	}

	scope := p.UnitScope()
	if scope == nil {
		return nil, apierr.Validation(
			"Your access covers the whole network. Name a franchise with franchiseId.", nil)
	}
	if len(scope) == 0 {
		return nil, apierr.Forbidden("You are not assigned to any operating unit.")
	}

	// The first unit that belongs to a franchise wins. A principal scoped to
	// several units of the same franchise is the normal case; one scoped across
	// two franchises is a configuration error, and is reported rather than
	// silently resolved to whichever came first.
	var found *dbgen.Franchise
	for _, unitID := range scope {
		f, err := s.q.GetFranchiseByOperatingUnit(ctx, dbgen.GetFranchiseByOperatingUnitParams{
			OperatingUnitID: unitID, OrganizationID: p.OrganizationID,
		})
		if err != nil {
			continue
		}
		if found != nil && found.ID != f.ID {
			return nil, apierr.Validation(
				"Your access covers more than one franchise. Name one with franchiseId.", nil)
		}
		copied := f
		found = &copied
	}
	if found == nil {
		return nil, apierr.Forbidden("Your operating units do not belong to a franchise.")
	}
	return s.franchiseFrom(ctx, p, *found)
}

func (s *Service) franchiseFrom(
	ctx context.Context, p *tenant.Principal, f dbgen.Franchise,
) (*Franchise, error) {
	out := &Franchise{
		ID: f.ID, PublicID: f.PublicID, Code: f.Code, Name: f.Name,
		UnitID: f.OperatingUnitID,
	}
	if unit, err := s.q.GetOperatingUnitByID(ctx, dbgen.GetOperatingUnitByIDParams{
		ID: f.OperatingUnitID, OrganizationID: p.OrganizationID,
	}); err == nil {
		out.UnitCode = unit.Code
	}
	return out, nil
}

// FranchiseSummary is the franchise landing payload.
type FranchiseSummary struct {
	Franchise    Ref    `json:"franchise"`
	From         string `json:"from"`
	To           string `json:"to"`
	Booked       int64  `json:"booked"`
	Delivered    int64  `json:"delivered"`
	NDR          int64  `json:"ndr"`
	RTO          int64  `json:"rto"`
	RevenueMinor int64  `json:"revenueMinor"`

	CommissionEarnedMinor  int64 `json:"commissionEarnedMinor"`
	CommissionSettledMinor int64 `json:"commissionSettledMinor"`
	// CommissionOutstandingMinor is earned minus settled: what the franchise is
	// owed but has not yet been paid.
	CommissionOutstandingMinor int64 `json:"commissionOutstandingMinor"`

	CODInCustodyMinor int64 `json:"codInCustodyMinor"`
	CODObligations    int64 `json:"codObligations"`
	CODAgedOver48h    int64 `json:"codAgedOver48h"`

	Currency       string `json:"currency"`
	DeliveryRateBP int64  `json:"deliveryRateBasisPoints"`
}

// Ref is a compact reference.
type Ref struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// FranchiseOverview assembles the franchise's own dashboard.
func (s *Service) FranchiseOverview(
	ctx context.Context, p *tenant.Principal, f *Franchise, from, to time.Time,
) (*FranchiseSummary, error) {
	stats, err := s.q.PortalFranchiseSummary(ctx, dbgen.PortalFranchiseSummaryParams{
		OrganizationID: p.OrganizationID, UnitIds: []int64{f.UnitID},
		FromDate: from, ToDate: to,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	earnings, err := s.q.PortalFranchiseEarnings(ctx, dbgen.PortalFranchiseEarningsParams{
		OrganizationID: p.OrganizationID, FranchiseID: &f.ID,
		FromDate: from, ToDate: to,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	cod, err := s.q.PortalFranchiseCODPosition(ctx, dbgen.PortalFranchiseCODPositionParams{
		OrganizationID: p.OrganizationID, FranchiseID: &f.ID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	return &FranchiseSummary{
		Franchise: Ref{ID: f.PublicID, Code: f.Code, Name: f.Name},
		From:      from.Format(dateLayout), To: to.Format(dateLayout),
		Booked: stats.Booked, Delivered: stats.Delivered,
		NDR: stats.Ndr, RTO: stats.Rto, RevenueMinor: stats.RevenueMinor,

		CommissionEarnedMinor:      earnings.EarnedMinor,
		CommissionSettledMinor:     earnings.SettledMinor,
		CommissionOutstandingMinor: earnings.EarnedMinor - earnings.SettledMinor,

		CODInCustodyMinor: cod.InCustodyMinor,
		CODObligations:    cod.ObligationCount,
		CODAgedOver48h:    cod.AgedOver48h,

		Currency:       p.OrganizationCurrency,
		DeliveryRateBP: rateBP(stats.Delivered, stats.Booked),
	}, nil
}

// SettlementSummary is one statement as a franchise sees it.
type SettlementSummary struct {
	ID               string  `json:"id"`
	Number           string  `json:"settlementNumber"`
	Status           string  `json:"status"`
	PeriodStart      string  `json:"periodStart"`
	PeriodEnd        string  `json:"periodEnd"`
	NetAmountMinor   int64   `json:"netAmountMinor"`
	PaidMinor        int64   `json:"paidMinor"`
	OutstandingMinor int64   `json:"outstandingMinor"`
	Currency         string  `json:"currency"`
	ApprovedAt       *string `json:"approvedAt,omitempty"`
	ClosedAt         *string `json:"closedAt,omitempty"`
}

// FranchiseSettlements lists the franchise's own statements.
func (s *Service) FranchiseSettlements(
	ctx context.Context, p *tenant.Principal, f *Franchise,
	status string, cursor *int64, limit int32,
) ([]SettlementSummary, error) {
	rows, err := s.q.PortalFranchiseSettlements(ctx, dbgen.PortalFranchiseSettlementsParams{
		OrganizationID: p.OrganizationID, FranchiseID: f.ID,
		Status: ops.Optional(status), CursorID: cursor, RowLimit: clampLimit(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]SettlementSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, SettlementSummary{
			ID: r.PublicID, Number: r.SettlementNumber, Status: r.Status,
			PeriodStart:    r.PeriodStart.Format(dateLayout),
			PeriodEnd:      r.PeriodEnd.Format(dateLayout),
			NetAmountMinor: r.NetAmountMinor, PaidMinor: r.PaidMinor,
			OutstandingMinor: r.NetAmountMinor - r.PaidMinor,
			Currency:         r.Currency,
			ApprovedAt:       timeOrNil(r.ApprovedAt), ClosedAt: timeOrNil(r.ClosedAt),
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

const dateLayout = "2006-01-02"

func rateBP(part, whole int64) int64 {
	if whole <= 0 {
		return 0
	}
	return part * 10_000 / whole
}

func clampLimit(n int32) int32 {
	switch {
	case n <= 0:
		return 50
	case n > 200:
		return 200
	default:
		return n
	}
}

func dateOrNil(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(dateLayout)
	return &s
}

func timeOrNil(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// FranchiseShipment is one parcel as a franchise sees it.
type FranchiseShipment struct {
	ID               string `json:"id"`
	AWB              string `json:"awb"`
	Status           string `json:"status"`
	Customer         string `json:"customer"`
	Service          string `json:"service"`
	Pieces           int32  `json:"pieces"`
	DestinationPin   string `json:"destinationPincode"`
	PaymentMode      string `json:"paymentMode"`
	TotalAmountMinor int64  `json:"totalAmountMinor"`
	CODAmountMinor   int64  `json:"codAmountMinor"`
	Currency         string `json:"currency"`
	BookedAt         string `json:"bookedAt"`
	// Role says why this shipment is on the franchise's list: ORIGIN means they
	// booked it, DESTINATION means they must deliver it. Stated rather than
	// implied, because the two carry different money and different work.
	Role string `json:"role"`
}

// Franchise shipment relationships.
const (
	RoleOrigin      = "ORIGIN"
	RoleDestination = "DESTINATION"
	RoleAny         = "ANY"
)

// FranchiseShipments lists a franchise's parcels for one relationship.
//
// The default is ORIGIN, which is what the summary beside it counts. Anything
// else would put two numbers on one screen that disagree.
func (s *Service) FranchiseShipments(
	ctx context.Context, p *tenant.Principal, f *Franchise,
	role, status string, cursor *int64, limit int32,
) ([]FranchiseShipment, error) {
	switch role {
	case RoleOrigin, RoleDestination, RoleAny:
	case "":
		role = RoleOrigin
	default:
		return nil, apierr.Validation("Unknown relationship.",
			map[string]any{"role": role, "allowed": []string{RoleOrigin, RoleDestination, RoleAny}})
	}

	rows, err := s.q.PortalFranchiseShipments(ctx, dbgen.PortalFranchiseShipmentsParams{
		OrganizationID: p.OrganizationID, UnitID: &f.UnitID, Role: role,
		Status: ops.Optional(status), CursorID: cursor, RowLimit: clampLimit(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]FranchiseShipment, 0, len(rows))
	for _, r := range rows {
		out = append(out, FranchiseShipment{
			ID: r.PublicID, AWB: r.Awb, Status: r.CurrentStatus,
			Customer: r.CustomerName, Service: r.ServiceCode,
			Pieces: r.PieceCount, DestinationPin: r.DestinationPincode,
			PaymentMode: r.PaymentMode, TotalAmountMinor: r.TotalAmountMinor,
			CODAmountMinor: r.CodAmountMinor, Currency: r.Currency,
			BookedAt: r.BookedAt.UTC().Format(time.RFC3339),
			Role:     r.FranchiseRole,
		})
	}
	return out, nil
}
