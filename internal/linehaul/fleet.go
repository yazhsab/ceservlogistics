package linehaul

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Carrier, vehicle and driver are network configuration rather than operational
// state, so they are ordinary records: created, listed, deactivated. The one
// rule that matters is that a trip cannot use an inactive one, which is checked
// at trip creation rather than here.

// CarrierInput creates a carrier.
type CarrierInput struct {
	Code         string
	Name         string
	CarrierType  string
	Modes        []string
	ContactName  string
	ContactPhone string
	ContactEmail string
	GSTNumber    string
	Metadata     map[string]any
}

// CarrierView is a carrier in a response.
type CarrierView struct {
	ID           string   `json:"id"`
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	CarrierType  string   `json:"carrierType"`
	Modes        []string `json:"modes"`
	ContactName  string   `json:"contactName,omitempty"`
	ContactPhone string   `json:"contactPhone,omitempty"`
	ContactEmail string   `json:"contactEmail,omitempty"`
	GSTNumber    string   `json:"gstNumber,omitempty"`
	IsActive     bool     `json:"isActive"`
}

// CreateCarrier registers a carrier.
func (s *Service) CreateCarrier(ctx context.Context, p *tenant.Principal, in CarrierInput) (*CarrierView, error) {
	var out *CarrierView
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.CreateCarrier(ctx, dbgen.CreateCarrierParams{
			PublicID: publicid.New(publicid.PrefixCarrier), OrganizationID: p.OrganizationID,
			Code: in.Code, Name: in.Name, CarrierType: in.CarrierType, Modes: in.Modes,
			ContactName: ops.Optional(in.ContactName), ContactPhone: ops.Optional(in.ContactPhone),
			ContactEmail: ops.Optional(in.ContactEmail), GstNumber: ops.Optional(in.GSTNumber),
			Metadata: encodeJSON(in.Metadata),
		})
		if err != nil {
			if ops.IsUnique(err, "carriers_code_unique") {
				return apierr.Conflict(apierr.CodeDuplicate,
					"A carrier with this code already exists.").WithDetail("code", in.Code)
			}
			return apierr.Internal(fmt.Errorf("create carrier: %w", err))
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionCarrierChanged, ResourceType: "carrier",
			ResourceID: &row.ID, ResourcePublicID: row.PublicID,
			After: map[string]any{"code": in.Code, "name": in.Name, "type": in.CarrierType},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		v := carrierView(row)
		out = &v
		return nil
	})
	return out, err
}

// ListCarriers returns the tenant's carriers.
func (s *Service) ListCarriers(
	ctx context.Context, p *tenant.Principal, carrierType string, limit, offset int,
) ([]CarrierView, error) {
	active := true
	params := dbgen.ListCarriersParams{
		OrganizationID: p.OrganizationID, IsActive: &active,
		PageSize: int32(limit), RowOffset: int32(offset),
	}
	if carrierType != "" {
		params.CarrierType = &carrierType
	}
	rows, err := s.q.ListCarriers(ctx, params)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]CarrierView, 0, len(rows))
	for _, r := range rows {
		out = append(out, carrierView(r))
	}
	return out, nil
}

func carrierView(r dbgen.Carrier) CarrierView {
	return CarrierView{
		ID: r.PublicID, Code: r.Code, Name: r.Name, CarrierType: r.CarrierType,
		Modes: r.Modes, ContactName: ops.Deref(r.ContactName),
		ContactPhone: ops.Deref(r.ContactPhone), ContactEmail: ops.Deref(r.ContactEmail),
		GSTNumber: ops.Deref(r.GstNumber), IsActive: r.IsActive,
	}
}

// VehicleInput creates a vehicle.
type VehicleInput struct {
	CarrierID      string
	Registration   string
	VehicleType    string
	CapacityGrams  *int64
	CapacityVolume *int64
	BaseUnitID     string
	Metadata       map[string]any
}

// VehicleView is a vehicle in a response.
type VehicleView struct {
	ID            string `json:"id"`
	Registration  string `json:"registrationNumber"`
	VehicleType   string `json:"vehicleType"`
	CarrierCode   string `json:"carrierCode,omitempty"`
	CarrierName   string `json:"carrierName,omitempty"`
	CapacityGrams *int64 `json:"capacityWeightGrams,omitempty"`
	IsActive      bool   `json:"isActive"`
}

// CreateVehicle registers a vehicle.
func (s *Service) CreateVehicle(ctx context.Context, p *tenant.Principal, in VehicleInput) (*VehicleView, error) {
	var out *VehicleView
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		params := dbgen.CreateVehicleParams{
			PublicID: publicid.New(publicid.PrefixVehicle), OrganizationID: p.OrganizationID,
			RegistrationNumber: in.Registration, VehicleType: in.VehicleType,
			CapacityWeightGrams: in.CapacityGrams, CapacityVolumeCc: in.CapacityVolume,
			Metadata: encodeJSON(in.Metadata),
		}
		if in.CarrierID != "" {
			c, cErr := q.GetCarrierByPublicID(ctx, dbgen.GetCarrierByPublicIDParams{
				PublicID: in.CarrierID, OrganizationID: p.OrganizationID,
			})
			if cErr != nil {
				return ops.NotFoundOr(cErr, "Carrier")
			}
			params.CarrierID = &c.ID
		}
		if in.BaseUnitID != "" {
			u, uErr := s.lookupUnit(ctx, p, in.BaseUnitID, "baseUnitId")
			if uErr != nil {
				return uErr
			}
			params.BaseUnitID = &u.ID
		}
		row, err := q.CreateVehicle(ctx, params)
		if err != nil {
			if ops.IsUnique(err, "vehicles_registration_unique") {
				return apierr.Conflict(apierr.CodeDuplicate,
					"A vehicle with this registration already exists.").
					WithDetail("registrationNumber", in.Registration)
			}
			return apierr.Internal(fmt.Errorf("create vehicle: %w", err))
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionVehicleChanged, ResourceType: "vehicle",
			ResourceID: &row.ID, ResourcePublicID: row.PublicID,
			After: map[string]any{"registration": in.Registration, "type": in.VehicleType},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		out = &VehicleView{
			ID: row.PublicID, Registration: row.RegistrationNumber,
			VehicleType: row.VehicleType, CapacityGrams: row.CapacityWeightGrams,
			IsActive: row.IsActive,
		}
		return nil
	})
	return out, err
}

// ListVehicles returns the tenant's vehicles.
func (s *Service) ListVehicles(
	ctx context.Context, p *tenant.Principal, vehicleType string, limit, offset int,
) ([]VehicleView, error) {
	active := true
	params := dbgen.ListVehiclesParams{
		OrganizationID: p.OrganizationID, IsActive: &active,
		PageSize: int32(limit), RowOffset: int32(offset),
	}
	if vehicleType != "" {
		params.VehicleType = &vehicleType
	}
	rows, err := s.q.ListVehicles(ctx, params)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]VehicleView, 0, len(rows))
	for _, r := range rows {
		out = append(out, VehicleView{
			ID: r.PublicID, Registration: r.RegistrationNumber, VehicleType: r.VehicleType,
			CarrierCode: ops.Deref(r.CarrierCode), CarrierName: ops.Deref(r.CarrierName),
			CapacityGrams: r.CapacityWeightGrams, IsActive: r.IsActive,
		})
	}
	return out, nil
}

// DriverInput creates a driver.
type DriverInput struct {
	CarrierID     string
	UserID        string
	Code          string
	FullName      string
	Phone         string
	LicenceNumber string
	BaseUnitID    string
	Metadata      map[string]any
}

// DriverView is a driver in a response.
type DriverView struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	FullName    string `json:"fullName"`
	Phone       string `json:"phone"`
	CarrierCode string `json:"carrierCode,omitempty"`
	IsActive    bool   `json:"isActive"`
}

// CreateDriver registers a driver.
func (s *Service) CreateDriver(ctx context.Context, p *tenant.Principal, in DriverInput) (*DriverView, error) {
	var out *DriverView
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		params := dbgen.CreateDriverParams{
			PublicID: publicid.New(publicid.PrefixDriver), OrganizationID: p.OrganizationID,
			Code: in.Code, FullName: in.FullName, Phone: in.Phone,
			LicenceNumber: ops.Optional(in.LicenceNumber), Metadata: encodeJSON(in.Metadata),
		}
		if in.CarrierID != "" {
			c, cErr := q.GetCarrierByPublicID(ctx, dbgen.GetCarrierByPublicIDParams{
				PublicID: in.CarrierID, OrganizationID: p.OrganizationID,
			})
			if cErr != nil {
				return ops.NotFoundOr(cErr, "Carrier")
			}
			params.CarrierID = &c.ID
		}
		if in.UserID != "" {
			u, uErr := q.GetUserByPublicID(ctx, dbgen.GetUserByPublicIDParams{
				PublicID: in.UserID, OrganizationID: p.OrganizationID,
			})
			if uErr != nil {
				return ops.NotFoundOr(uErr, "User")
			}
			params.UserID = &u.ID
		}
		if in.BaseUnitID != "" {
			u, uErr := s.lookupUnit(ctx, p, in.BaseUnitID, "baseUnitId")
			if uErr != nil {
				return uErr
			}
			params.BaseUnitID = &u.ID
		}
		row, err := q.CreateDriver(ctx, params)
		if err != nil {
			if ops.IsUnique(err, "drivers_code_unique") {
				return apierr.Conflict(apierr.CodeDuplicate,
					"A driver with this code already exists.").WithDetail("code", in.Code)
			}
			return apierr.Internal(fmt.Errorf("create driver: %w", err))
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionDriverChanged, ResourceType: "driver",
			ResourceID: &row.ID, ResourcePublicID: row.PublicID,
			After: map[string]any{"code": in.Code, "fullName": in.FullName},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		out = &DriverView{
			ID: row.PublicID, Code: row.Code, FullName: row.FullName,
			Phone: row.Phone, IsActive: row.IsActive,
		}
		return nil
	})
	return out, err
}

// ListDrivers returns the tenant's drivers.
func (s *Service) ListDrivers(
	ctx context.Context, p *tenant.Principal, limit, offset int,
) ([]DriverView, error) {
	active := true
	rows, err := s.q.ListDrivers(ctx, dbgen.ListDriversParams{
		OrganizationID: p.OrganizationID, IsActive: &active,
		PageSize: int32(limit), RowOffset: int32(offset),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]DriverView, 0, len(rows))
	for _, r := range rows {
		out = append(out, DriverView{
			ID: r.PublicID, Code: r.Code, FullName: r.FullName, Phone: r.Phone,
			CarrierCode: ops.Deref(r.CarrierCode), IsActive: r.IsActive,
		})
	}
	return out, nil
}
