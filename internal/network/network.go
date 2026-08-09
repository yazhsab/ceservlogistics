// Package network owns the courier network master: regions, operating units
// (hubs and branches), capabilities, franchises and franchise agreements (M02).
//
// Hub and Branch are not separate tables. They are operating_units discriminated
// by unit_type, because they share every structural attribute and because
// shipments, bags, manifests and trips must all reference "a facility"
// uniformly. See docs/adr/0002-operating-units-model-hubs-and-branches.md.
package network

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
)

// Unit types.
const (
	UnitHeadOffice      = "HEAD_OFFICE"
	UnitRegionalHub     = "REGIONAL_HUB"
	UnitTransitHub      = "TRANSIT_HUB"
	UnitDeliveryHub     = "DELIVERY_HUB"
	UnitCompanyBranch   = "COMPANY_BRANCH"
	UnitFranchiseBranch = "FRANCHISE_BRANCH"
)

// UnitTypes is the full enum, in hierarchy order.
var UnitTypes = []string{
	UnitHeadOffice, UnitRegionalHub, UnitTransitHub, UnitDeliveryHub,
	UnitCompanyBranch, UnitFranchiseBranch,
}

// HubTypes are the unit types that can terminate a line-haul route.
var HubTypes = []string{UnitRegionalHub, UnitTransitHub, UnitDeliveryHub}

// BranchTypes are the unit types that book, pick up and deliver.
var BranchTypes = []string{UnitCompanyBranch, UnitFranchiseBranch}

// Capabilities is the full capability enum.
var Capabilities = []string{
	"BOOKING", "PICKUP", "DELIVERY", "BAGGING", "MANIFEST", "LINEHAUL_ORIGIN",
	"LINEHAUL_DESTINATION", "TRANSIT", "COD_COLLECTION", "RTO_PROCESSING",
	"CUSTOMER_WALKIN", "WAREHOUSING",
}

// IsHub reports whether a unit type terminates line haul.
func IsHub(unitType string) bool {
	for _, t := range HubTypes {
		if t == unitType {
			return true
		}
	}
	return false
}

// Service exposes network reads used by other modules.
type Service struct {
	db  *database.DB
	q   *dbgen.Queries
	log *slog.Logger
}

// NewService builds the network service.
func NewService(db *database.DB, q *dbgen.Queries, log *slog.Logger) *Service {
	return &Service{db: db, q: q, log: log}
}

// Unit is the resolved view of an operating unit.
type Unit struct {
	ID           int64
	PublicID     string
	Code         string
	Name         string
	UnitType     string
	Status       string
	ParentUnitID *int64
	Pincode      string
	CutoffTime   string
}

// GetUnitByPublicID resolves a unit inside a tenant.
func (s *Service) GetUnitByPublicID(ctx context.Context, orgID int64, publicID string) (*Unit, error) {
	row, err := s.q.GetOperatingUnitByPublicID(ctx, dbgen.GetOperatingUnitByPublicIDParams{
		PublicID: publicID, OrganizationID: orgID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.NotFound("Operating unit")
		}
		return nil, apierr.Internal(fmt.Errorf("load operating unit: %w", err))
	}
	return &Unit{
		ID: row.ID, PublicID: row.PublicID, Code: row.Code, Name: row.Name,
		UnitType: row.UnitType, Status: row.Status, ParentUnitID: row.ParentUnitID,
		Pincode: row.Pincode,
	}, nil
}

// ResolveHub walks up the hierarchy to the nearest hub above a branch.
//
// Routing is defined hub to hub, so a branch that has no hub ancestor cannot be
// routed and the caller is told exactly that rather than silently falling
// through to "not serviceable".
func (s *Service) ResolveHub(ctx context.Context, orgID, unitID int64) (*Unit, error) {
	row, err := s.q.ResolveAncestorHub(ctx, dbgen.ResolveAncestorHubParams{
		UnitID: unitID, OrganizationID: orgID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}
		return nil, apierr.Internal(fmt.Errorf("resolve hub: %w", err))
	}
	return &Unit{
		ID: row.ID, PublicID: row.PublicID, Code: row.Code,
		Name: row.Name, UnitType: row.UnitType, Status: row.Status,
	}, nil
}

// HasCapability reports whether a facility is permitted to perform an action.
func (s *Service) HasCapability(ctx context.Context, unitID int64, capability string) (bool, error) {
	ok, err := s.q.HasOperatingUnitCapability(ctx, dbgen.HasOperatingUnitCapabilityParams{
		OperatingUnitID: unitID, Capability: capability,
	})
	if err != nil {
		return false, apierr.Internal(err)
	}
	return ok, nil
}

// GetFranchiseForUnit returns the franchise operating a unit, if any.
//
// Takes the organization explicitly rather than deriving it from the unit: the
// caller already knows whose request this is, and passing it makes the tenant
// boundary visible at the call site instead of implied by the unit id.
func (s *Service) GetFranchiseForUnit(ctx context.Context, orgID, unitID int64) (*dbgen.Franchise, error) {
	f, err := s.q.GetFranchiseByOperatingUnit(ctx, dbgen.GetFranchiseByOperatingUnitParams{
		OperatingUnitID: unitID, OrganizationID: orgID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}
		return nil, apierr.Internal(err)
	}
	return &f, nil
}

// unitEffectiveOn reports whether a unit is usable at a point in time.
func unitEffectiveOn(effectiveFrom time.Time, effectiveTo *time.Time, at time.Time) bool {
	day := at.UTC().Truncate(24 * time.Hour)
	if effectiveFrom.After(day) {
		return false
	}
	if effectiveTo != nil && effectiveTo.Before(day) {
		return false
	}
	return true
}
