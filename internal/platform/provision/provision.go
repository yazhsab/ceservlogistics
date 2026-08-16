// Package provision performs first-run and demo provisioning.
//
// A freshly migrated database has a permission catalogue and system roles but
// no organizations and no users, so nothing can authenticate. Bootstrap creates
// the platform operator tenant and its first SUPER_ADMIN; it is idempotent, so
// running it on every deploy is safe.
package provision

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/security"
)

// Bootstrap creates the platform organization and its first super administrator.
//
// It is idempotent: if the platform organization already exists, or the admin
// email is already taken, nothing changes and no error is returned. That makes
// it safe to run unconditionally from a deploy script.
func Bootstrap(ctx context.Context, db *database.DB, cfg *config.Config, log *slog.Logger) error {
	if cfg.Auth.BootstrapEmail == "" || cfg.Auth.BootstrapPassword == "" {
		return errors.New("BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD must both be set")
	}
	q := dbgen.New(db.Pool)
	email := strings.ToLower(strings.TrimSpace(cfg.Auth.BootstrapEmail))

	hasher := security.NewHasher(security.Argon2Params{
		Time: cfg.Auth.Argon2Time, MemoryKiB: cfg.Auth.Argon2MemoryKiB,
		Parallelism: cfg.Auth.Argon2Parallelism, KeyLength: cfg.Auth.Argon2KeyLength,
	})
	hash, err := hasher.Hash(cfg.Auth.BootstrapPassword)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}

	return db.InTx(ctx, func(tx pgx.Tx) error {
		qtx := q.WithTx(tx)

		org, err := qtx.GetPlatformOrganization(ctx)
		if err != nil {
			if !database.IsNoRows(err) {
				return fmt.Errorf("look up platform organization: %w", err)
			}
			org, err = qtx.CreateOrganization(ctx, dbgen.CreateOrganizationParams{
				PublicID: publicid.New(publicid.PrefixOrganization),
				Code:     strings.ToUpper(cfg.Auth.BootstrapOrgCode),
				Name:     cfg.Auth.BootstrapOrgName,
				Timezone: cfg.Market.Timezone, Currency: cfg.Market.Currency,
				Country:    &cfg.Market.Country,
				AwbPrefix:  cfg.Booking.AWBPrefixDefault,
				IsPlatform: true, Settings: []byte("{}"),
			})
			if err != nil {
				return fmt.Errorf("create platform organization: %w", err)
			}
			// Every organization starts with a working NDR catalogue: without
			// one, its first failed delivery would be refused for an unknown
			// reason code.
			if sErr := qtx.SeedDefaultNDRReasons(ctx, org.ID); sErr != nil {
				return fmt.Errorf("seed ndr reasons: %w", sErr)
			}
			// Finance cannot post without a chart of accounts, an open
			// period and a numbering series. Seeding them here means a new
			// tenant's first commission or invoice works rather than
			// failing on missing configuration.
			if sErr := seedFinance(ctx, qtx, org.ID); sErr != nil {
				return sErr
			}
			log.Info("created platform organization",
				slog.String("code", org.Code), slog.String("id", org.PublicID))
		}

		if _, err := qtx.GetUserForLogin(ctx, email); err == nil {
			log.Info("bootstrap administrator already exists; nothing to do",
				slog.String("email", email))
			return nil
		} else if !database.IsNoRows(err) {
			return fmt.Errorf("look up bootstrap user: %w", err)
		}

		user, err := qtx.CreateUser(ctx, dbgen.CreateUserParams{
			PublicID: publicid.New(publicid.PrefixUser), OrganizationID: org.ID,
			Email: email, PasswordHash: hash, FullName: "Platform Administrator",
			Status: "ACTIVE", IsSuperAdmin: true,
			// The bootstrap credential comes from an environment variable, which
			// is a deployment artefact rather than a secret the operator chose.
			MustChangePassword: true,
		})
		if err != nil {
			return fmt.Errorf("create bootstrap user: %w", err)
		}
		role, err := qtx.GetSystemRoleByCode(ctx, "SUPER_ADMIN")
		if err != nil {
			return fmt.Errorf("look up SUPER_ADMIN role (are migrations applied?): %w", err)
		}
		if _, err := qtx.AssignUserRole(ctx, dbgen.AssignUserRoleParams{
			OrganizationID: org.ID, UserID: user.ID, RoleID: role.ID,
		}); err != nil {
			return fmt.Errorf("assign SUPER_ADMIN: %w", err)
		}

		if _, err := qtx.InsertAuditEvent(ctx, dbgen.InsertAuditEventParams{
			PublicID: publicid.New(publicid.PrefixAuditEvent), OrganizationID: &org.ID,
			ActorType: "SYSTEM", Action: "user.created", ResourceType: "user",
			ResourceID: &user.ID, ResourcePublicID: &user.PublicID,
			Metadata: []byte(`{"source":"bootstrap"}`),
		}); err != nil {
			return fmt.Errorf("audit bootstrap: %w", err)
		}

		log.Info("created bootstrap administrator",
			slog.String("email", email),
			slog.String("organization", org.Code),
			slog.Bool("must_change_password", true))
		return nil
	})
}

// DemoResult describes what Demo created, so a load test can be pointed at it.
type DemoResult struct {
	OrganizationCode string
	AdminEmail       string
	AdminPassword    string
	CustomerPublicID string
	ServiceCode      string
	OriginPincode    string
	DestPincode      string
	Accounts         []DemoAccount
}

// DemoAccount is a ready-to-use role login created only by `migrate demo`.
type DemoAccount struct {
	Role     string
	Email    string
	Password string
}

// Demo builds a complete, bookable tenant for development and load testing.
//
// It refuses to run in production: it creates a known-password account and
// synthetic reference data, neither of which belongs in a live system.
func Demo(ctx context.Context, db *database.DB, cfg *config.Config, log *slog.Logger) (*DemoResult, error) {
	if cfg.IsProduction() {
		return nil, errors.New("the demo dataset must never be created in staging or production")
	}
	q := dbgen.New(db.Pool)
	const (
		orgCode    = "DEMO"
		adminEmail = "admin@demo.test"
		password   = "DemoPassw0rd!2026"
	)
	result := &DemoResult{
		OrganizationCode: orgCode, AdminEmail: adminEmail, AdminPassword: password,
		ServiceCode: "EXPRESS", OriginPincode: "100001", DestPincode: "900001",
	}
	result.Accounts = demoAccounts(password)

	hasher := security.NewHasher(security.Argon2Params{
		Time: cfg.Auth.Argon2Time, MemoryKiB: cfg.Auth.Argon2MemoryKiB,
		Parallelism: cfg.Auth.Argon2Parallelism,
	})
	hash, err := hasher.Hash(password)
	if err != nil {
		return nil, err
	}

	err = db.InTx(ctx, func(tx pgx.Tx) error {
		qtx := q.WithTx(tx)
		now := time.Now()

		if existing, gErr := qtx.GetOrganizationByCode(ctx, orgCode); gErr == nil {
			log.Info("demo organization already exists; ensuring role accounts",
				slog.String("id", existing.PublicID))
			if eErr := ensureDemoRoleAccounts(ctx, qtx, existing.ID, hash); eErr != nil {
				return eErr
			}
			if eErr := ensureDemoCommissionRules(ctx, qtx, existing.ID); eErr != nil {
				return eErr
			}
			cust, cErr := qtx.GetCustomerByCode(ctx, dbgen.GetCustomerByCodeParams{
				OrganizationID: existing.ID, Code: "WALKIN",
			})
			if cErr == nil {
				result.CustomerPublicID = cust.PublicID
			}
			return nil
		} else if !database.IsNoRows(gErr) {
			return gErr
		}

		// Geography: global reference data, shared with any other tenant.
		// A Lagos-to-Abuja lane, which is the busiest real corridor in the
		// market and therefore the most useful thing to demonstrate against.
		country, err := qtx.UpsertCountry(ctx, dbgen.UpsertCountryParams{
			PublicID: publicid.New(publicid.PrefixCountry), Iso2: "NG", Iso3: "NGA",
			Name: "Nigeria", PhoneCode: "+234", Currency: "NGN",
		})
		if err != nil {
			return fmt.Errorf("country: %w", err)
		}
		stateIDs := map[string]int64{}
		for _, st := range []struct{ code, name string }{
			{"LA", "Lagos"}, {"FC", "Federal Capital Territory"},
		} {
			row, sErr := qtx.UpsertState(ctx, dbgen.UpsertStateParams{
				PublicID: publicid.New(publicid.PrefixState), CountryID: country.ID,
				Code: st.code, Name: st.name,
			})
			if sErr != nil {
				return fmt.Errorf("state %s: %w", st.name, sErr)
			}
			stateIDs[st.name] = row.ID
		}
		pincodeIDs := map[string]int64{}
		for _, p := range []struct{ code, state, city string }{
			{"100001", "Lagos", "Lagos"},
			{"900001", "Federal Capital Territory", "Abuja"},
		} {
			city, cErr := qtx.UpsertCity(ctx, dbgen.UpsertCityParams{
				PublicID: publicid.New(publicid.PrefixCity), StateID: stateIDs[p.state],
				Name: p.city, Tier: "METRO",
			})
			if cErr != nil {
				return fmt.Errorf("city %s: %w", p.city, cErr)
			}
			row, pErr := qtx.UpsertPincode(ctx, dbgen.UpsertPincodeParams{
				PublicID: publicid.New(publicid.PrefixPincode), CountryID: country.ID,
				Code: p.code, StateID: stateIDs[p.state], CityID: &city.ID,
			})
			if pErr != nil {
				return fmt.Errorf("pincode %s: %w", p.code, pErr)
			}
			pincodeIDs[p.code] = row.ID
		}

		org, err := qtx.CreateOrganization(ctx, dbgen.CreateOrganizationParams{
			PublicID: publicid.New(publicid.PrefixOrganization), Code: orgCode,
			Name: "Demo Couriers", Timezone: "Africa/Lagos", Currency: "NGN",
			Country: ptrStr("NG"), AwbPrefix: "DMO", Settings: []byte("{}"),
		})
		if err != nil {
			return fmt.Errorf("organization: %w", err)
		}
		if sErr := qtx.SeedDefaultNDRReasons(ctx, org.ID); sErr != nil {
			return fmt.Errorf("seed ndr reasons: %w", sErr)
		}
		if sErr := seedFinance(ctx, qtx, org.ID); sErr != nil {
			return sErr
		}

		admin, err := qtx.CreateUser(ctx, dbgen.CreateUserParams{
			PublicID: publicid.New(publicid.PrefixUser), OrganizationID: org.ID,
			Email: adminEmail, PasswordHash: hash, FullName: "Demo Administrator",
			Status: "ACTIVE",
		})
		if err != nil {
			return fmt.Errorf("admin user: %w", err)
		}
		role, err := qtx.GetSystemRoleByCode(ctx, "ORG_ADMIN")
		if err != nil {
			return fmt.Errorf("ORG_ADMIN role: %w", err)
		}
		if _, err := qtx.AssignUserRole(ctx, dbgen.AssignUserRoleParams{
			OrganizationID: org.ID, UserID: admin.ID, RoleID: role.ID,
		}); err != nil {
			return fmt.Errorf("assign role: %w", err)
		}

		unit := func(code, name, unitType string, parent *int64, pincode string) (int64, error) {
			pid := pincodeIDs[pincode]
			row, uErr := qtx.CreateOperatingUnit(ctx, dbgen.CreateOperatingUnitParams{
				PublicID: publicid.New(publicid.PrefixOperatingUnit), OrganizationID: org.ID,
				Code: code, Name: name, UnitType: unitType, ParentUnitID: parent,
				AddressLine1: "1 Demo Road", Pincode: pincode, PincodeID: &pid,
				OperatingHours: []byte("{}"), EffectiveFrom: now.AddDate(0, 0, -1),
			})
			if uErr != nil {
				return 0, fmt.Errorf("unit %s: %w", code, uErr)
			}
			return row.ID, nil
		}
		losHub, err := unit("LOS-HUB", "Lagos Hub", "DELIVERY_HUB", nil, "100001")
		if err != nil {
			return err
		}
		abvHub, err := unit("ABV-HUB", "Abuja Hub", "DELIVERY_HUB", nil, "900001")
		if err != nil {
			return err
		}
		losBranch, err := unit("LOS-001", "Lagos Island", "COMPANY_BRANCH", &losHub, "100001")
		if err != nil {
			return err
		}
		abvBranch, err := unit("ABV-001", "Abuja Central", "COMPANY_BRANCH", &abvHub, "900001")
		if err != nil {
			return err
		}
		if eErr := ensureDemoRoleAccountsWithUnits(ctx, qtx, org.ID, hash,
			losHub, abvHub, losBranch, abvBranch); eErr != nil {
			return eErr
		}
		if eErr := ensureDemoCommissionRules(ctx, qtx, org.ID); eErr != nil {
			return eErr
		}

		for _, sa := range []struct {
			unit    int64
			pincode string
		}{{losBranch, "100001"}, {abvBranch, "900001"}} {
			if _, err := qtx.CreateServiceArea(ctx, dbgen.CreateServiceAreaParams{
				PublicID: publicid.New(publicid.PrefixServiceArea), OrganizationID: org.ID,
				OperatingUnitID: sa.unit, PincodeID: pincodeIDs[sa.pincode], AreaType: "BOTH",
				Priority: 100, EffectiveFrom: now.Add(-time.Hour),
			}); err != nil {
				return fmt.Errorf("service area: %w", err)
			}
		}

		svc, err := qtx.CreateCourierService(ctx, dbgen.CreateCourierServiceParams{
			PublicID: publicid.New(publicid.PrefixCourierService), OrganizationID: org.ID,
			Code: "EXPRESS", Name: "Express Air", Description: "Next-day air", Mode: "AIR",
			MinWeightGrams: 1, MaxWeightGrams: 50_000, VolumetricDivisor: 5000,
			WeightRoundingGrams: 500, CodAllowed: true, InsuranceAllowed: true,
			SlaTransitHours: 48, SlaRules: []byte(`{"remoteAreaExtraHours":24}`),
			EffectiveFrom: now.AddDate(0, 0, -1), SortOrder: 1,
		})
		if err != nil {
			return fmt.Errorf("courier service: %w", err)
		}

		route, err := qtx.CreateRouteDefinition(ctx, dbgen.CreateRouteDefinitionParams{
			PublicID: publicid.New(publicid.PrefixRoute), OrganizationID: org.ID,
			Code: "LOS-ABV-AIR", Name: "Lagos to Abuja (air)",
			OriginUnitID: losHub, DestinationUnitID: abvHub,
			Priority: 100, TransitHours: 24, EffectiveFrom: now.Add(-time.Hour),
		})
		if err != nil {
			return fmt.Errorf("route: %w", err)
		}
		if _, err := qtx.CreateRouteLeg(ctx, dbgen.CreateRouteLegParams{
			PublicID: publicid.New(publicid.PrefixRouteLeg), OrganizationID: org.ID,
			RouteDefinitionID: route.ID, Sequence: 1,
			FromUnitID: losHub, ToUnitID: abvHub, Mode: "AIR", TransitHours: 24,
		}); err != nil {
			return fmt.Errorf("route leg: %w", err)
		}

		zones := map[string]int64{}
		for _, z := range []struct{ code, name, kind string }{
			{"LOCAL", "Local", "LOCAL"}, {"NATIONAL", "National", "NATIONAL"},
		} {
			row, zErr := qtx.CreateZone(ctx, dbgen.CreateZoneParams{
				PublicID: publicid.New(publicid.PrefixZone), OrganizationID: org.ID,
				Code: z.code, Name: z.name, ZoneType: z.kind, Description: "",
			})
			if zErr != nil {
				return fmt.Errorf("zone %s: %w", z.code, zErr)
			}
			zones[z.code] = row.ID
		}
		for pincode, zone := range map[string]string{"100001": "LOCAL", "900001": "NATIONAL"} {
			if _, err := qtx.UpsertPincodeZoneMapping(ctx, dbgen.UpsertPincodeZoneMappingParams{
				PublicID: publicid.New(publicid.PrefixZoneMapping), OrganizationID: org.ID,
				PincodeID: pincodeIDs[pincode], ZoneID: zones[zone], EffectiveFrom: now.Add(-time.Hour),
			}); err != nil {
				return fmt.Errorf("zone mapping: %w", err)
			}
		}

		card, err := qtx.CreateRateCard(ctx, dbgen.CreateRateCardParams{
			PublicID: publicid.New(publicid.PrefixRateCard), OrganizationID: org.ID,
			Code: "RETAIL", Name: "Retail Tariff", Description: "Default retail rates",
			// The organization's currency, not a literal: a rate card in a
			// currency the tenant does not trade in is refused at booking, and
			// the demo tenant exists to prove the path works end to end.
			Scope: "RETAIL", Currency: org.Currency, IsDefault: true,
		})
		if err != nil {
			return fmt.Errorf("rate card: %w", err)
		}
		version, err := qtx.CreateRateCardVersion(ctx, dbgen.CreateRateCardVersionParams{
			PublicID: publicid.New(publicid.PrefixRateCardVersion), OrganizationID: org.ID,
			RateCardID: card.ID, EffectiveFrom: now.Add(-time.Hour), Notes: "demo",
		})
		if err != nil {
			return fmt.Errorf("rate card version: %w", err)
		}
		for _, lane := range [][2]string{
			{"LOCAL", "NATIONAL"}, {"NATIONAL", "LOCAL"},
			{"LOCAL", "LOCAL"}, {"NATIONAL", "NATIONAL"},
		} {
			if _, err := qtx.UpsertZoneRate(ctx, dbgen.UpsertZoneRateParams{
				PublicID: publicid.New(publicid.PrefixZoneRate), OrganizationID: org.ID,
				RateCardVersionID: version.ID, CourierServiceID: svc.ID,
				OriginZoneID: zones[lane[0]], DestinationZoneID: zones[lane[1]],
				BaseWeightGrams: 500, BasePriceMinor: 5000,
				AdditionalStepGrams: 500, AdditionalPriceMinor: 2500,
			}); err != nil {
				return fmt.Errorf("zone rate: %w", err)
			}
		}
		fuelBP := int32(1850)
		if _, err := qtx.CreateSurchargeRule(ctx, dbgen.CreateSurchargeRuleParams{
			PublicID: publicid.New(publicid.PrefixSurchargeRule), OrganizationID: org.ID,
			RateCardVersionID: version.ID, Code: "FUEL", Name: "Fuel surcharge",
			SurchargeType: "FUEL", CalcType: "PERCENTAGE", PercentageBp: &fuelBP,
			AppliesTo: "FREIGHT", Conditions: []byte("{}"), Priority: 10, IsTaxable: true,
		}); err != nil {
			return fmt.Errorf("surcharge: %w", err)
		}
		if _, err := qtx.ActivateRateCardVersion(ctx, dbgen.ActivateRateCardVersionParams{
			PublicID: version.PublicID, OrganizationID: org.ID,
		}); err != nil {
			return fmt.Errorf("activate version: %w", err)
		}
		interState := false
		if _, err := qtx.CreateTaxRule(ctx, dbgen.CreateTaxRuleParams{
			PublicID: publicid.New(publicid.PrefixTaxRule), OrganizationID: org.ID,
			Code: "VAT75", Name: "VAT 7.5%", TaxType: "VAT", PercentageBp: 750,
			IntraStateOnly: &interState, Priority: 10, EffectiveFrom: now.Add(-time.Hour),
		}); err != nil {
			return fmt.Errorf("tax rule: %w", err)
		}

		cust, err := qtx.CreateCustomer(ctx, dbgen.CreateCustomerParams{
			PublicID: publicid.New(publicid.PrefixCustomer), OrganizationID: org.ID,
			Code: "WALKIN", CustomerType: "RETAIL", Name: "Walk-in Customer",
			Phone: "+919800000000", OwningUnitID: &losBranch, Metadata: []byte("{}"),
		})
		if err != nil {
			return fmt.Errorf("customer: %w", err)
		}
		result.CustomerPublicID = cust.PublicID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func demoAccounts(password string) []DemoAccount {
	return []DemoAccount{
		{Role: "Organization administrator", Email: "admin@demo.test", Password: password},
		{Role: "Operations staff", Email: "operations@demo.test", Password: password},
		{Role: "Finance staff", Email: "finance@demo.test", Password: password},
		{Role: "Customer support", Email: "support@demo.test", Password: password},
		{Role: "Hub manager", Email: "hub@demo.test", Password: password},
		{Role: "Branch manager", Email: "branch@demo.test", Password: password},
		{Role: "Franchise owner", Email: "franchise.owner@demo.test", Password: password},
		{Role: "Franchise operator", Email: "franchise.staff@demo.test", Password: password},
		{Role: "Pickup agent", Email: "pickup@demo.test", Password: password},
		{Role: "Delivery driver", Email: "driver@demo.test", Password: password},
	}
}

func ensureDemoRoleAccounts(ctx context.Context, q *dbgen.Queries, orgID int64, hash string) error {
	losHub, err := q.GetOperatingUnitByCode(ctx, dbgen.GetOperatingUnitByCodeParams{OrganizationID: orgID, Code: "LOS-HUB"})
	if err != nil {
		return fmt.Errorf("demo Lagos hub: %w", err)
	}
	abvHub, err := q.GetOperatingUnitByCode(ctx, dbgen.GetOperatingUnitByCodeParams{OrganizationID: orgID, Code: "ABV-HUB"})
	if err != nil {
		return fmt.Errorf("demo Abuja hub: %w", err)
	}
	losBranch, err := q.GetOperatingUnitByCode(ctx, dbgen.GetOperatingUnitByCodeParams{OrganizationID: orgID, Code: "LOS-001"})
	if err != nil {
		return fmt.Errorf("demo Lagos branch: %w", err)
	}
	abvBranch, err := q.GetOperatingUnitByCode(ctx, dbgen.GetOperatingUnitByCodeParams{OrganizationID: orgID, Code: "ABV-001"})
	if err != nil {
		return fmt.Errorf("demo Abuja branch: %w", err)
	}
	return ensureDemoRoleAccountsWithUnits(ctx, q, orgID, hash, losHub.ID, abvHub.ID, losBranch.ID, abvBranch.ID)
}

func ensureDemoRoleAccountsWithUnits(
	ctx context.Context, q *dbgen.Queries, orgID int64, hash string,
	losHub, abvHub, losBranch, abvBranch int64,
) error {
	franchiseUnit, err := q.GetOperatingUnitByCode(ctx, dbgen.GetOperatingUnitByCodeParams{OrganizationID: orgID, Code: "LOS-FR1"})
	if database.IsNoRows(err) {
		parent, pErr := q.GetOperatingUnitByID(ctx, dbgen.GetOperatingUnitByIDParams{ID: losHub, OrganizationID: orgID})
		if pErr != nil {
			return fmt.Errorf("demo franchise parent: %w", pErr)
		}
		franchiseUnit, err = q.CreateOperatingUnit(ctx, dbgen.CreateOperatingUnitParams{
			PublicID: publicid.New(publicid.PrefixOperatingUnit), OrganizationID: orgID,
			Code: "LOS-FR1", Name: "Lagos Island Franchise", UnitType: "FRANCHISE_BRANCH",
			ParentUnitID: &losHub, AddressLine1: "24 Marina Road", Pincode: parent.Pincode,
			PincodeID: parent.PincodeID, CityID: parent.CityID, StateID: parent.StateID,
			OperatingHours: []byte(`{"mon-fri":"08:00-18:00","sat":"09:00-14:00"}`),
			EffectiveFrom:  time.Now().AddDate(0, 0, -1),
		})
	}
	if err != nil {
		return fmt.Errorf("demo franchise unit: %w", err)
	}

	type accountSeed struct {
		email, name, role, unitCode string
		unit                        *int64
	}
	seeds := []accountSeed{
		{"operations@demo.test", "Demo Operations Manager", "OPERATIONS_ADMIN", "", nil},
		{"finance@demo.test", "Demo Finance Manager", "FINANCE_MANAGER", "", nil},
		{"support@demo.test", "Demo Customer Support", "CUSTOMER_SUPPORT", "", nil},
		{"hub@demo.test", "Demo Hub Manager", "HUB_MANAGER", "ABV-HUB", &abvHub},
		{"branch@demo.test", "Demo Branch Manager", "BRANCH_MANAGER", "ABV-001", &abvBranch},
		{"franchise.owner@demo.test", "Demo Franchise Owner", "FRANCHISE_OWNER", "LOS-FR1", &franchiseUnit.ID},
		{"franchise.staff@demo.test", "Demo Franchise Counter Staff", "FRANCHISE_OPERATOR", "LOS-FR1", &franchiseUnit.ID},
		{"pickup@demo.test", "Demo Pickup Agent", "PICKUP_AGENT", "LOS-001", &losBranch},
		{"driver@demo.test", "Demo Delivery Driver", "DELIVERY_AGENT", "ABV-001", &abvBranch},
	}
	var franchiseOwnerID int64
	var franchiseOwnerName, franchiseOwnerEmail string
	for _, seed := range seeds {
		user, uErr := q.GetUserForLogin(ctx, seed.email)
		var userID int64
		var userOrgID int64
		var userName, userEmail string
		if database.IsNoRows(uErr) {
			created, cErr := q.CreateUser(ctx, dbgen.CreateUserParams{
				PublicID: publicid.New(publicid.PrefixUser), OrganizationID: orgID,
				Email: seed.email, PasswordHash: hash, FullName: seed.name,
				Phone: ptrStr("+2348000000000"), Status: "ACTIVE",
			})
			uErr = cErr
			userID, userOrgID, userName, userEmail = created.ID, created.OrganizationID, created.FullName, created.Email
		} else if uErr == nil {
			userID, userOrgID, userName, userEmail = user.ID, user.OrganizationID, user.FullName, user.Email
		}
		if uErr != nil {
			return fmt.Errorf("demo user %s: %w", seed.email, uErr)
		}
		if userOrgID != orgID {
			return fmt.Errorf("demo email %s belongs to another organization", seed.email)
		}
		role, rErr := q.GetSystemRoleByCode(ctx, seed.role)
		if rErr != nil {
			return fmt.Errorf("demo role %s: %w", seed.role, rErr)
		}
		assignments, aErr := q.ListUserRoleAssignments(ctx, userID)
		if aErr != nil {
			return fmt.Errorf("demo assignments %s: %w", seed.email, aErr)
		}
		assigned := false
		for _, a := range assignments {
			if a.RoleCode == seed.role && ((a.OperatingUnitCode == nil && seed.unit == nil) ||
				(a.OperatingUnitCode != nil && seed.unit != nil && *a.OperatingUnitCode == seed.unitCode)) {
				assigned = true
				break
			}
		}
		if !assigned {
			if _, aErr = q.AssignUserRole(ctx, dbgen.AssignUserRoleParams{
				OrganizationID: orgID, UserID: userID, RoleID: role.ID, OperatingUnitID: seed.unit,
			}); aErr != nil {
				return fmt.Errorf("assign %s to %s: %w", seed.role, seed.email, aErr)
			}
		}
		if seed.role == "FRANCHISE_OWNER" {
			franchiseOwnerID, franchiseOwnerName, franchiseOwnerEmail = userID, userName, userEmail
		}
	}
	if franchiseOwnerID != 0 {
		_, fErr := q.GetFranchiseByOperatingUnit(ctx, dbgen.GetFranchiseByOperatingUnitParams{OperatingUnitID: franchiseUnit.ID, OrganizationID: orgID})
		if database.IsNoRows(fErr) {
			now := time.Now()
			_, fErr = q.CreateFranchise(ctx, dbgen.CreateFranchiseParams{
				PublicID: publicid.New(publicid.PrefixFranchise), OrganizationID: orgID,
				Code: "LOS-FR1", Name: "Lagos Island Franchise", OperatingUnitID: franchiseUnit.ID,
				Category: "STANDARD", OwnerName: franchiseOwnerName, OwnerUserID: &franchiseOwnerID,
				OwnerPhone: "+2348000000000", OwnerEmail: &franchiseOwnerEmail,
				Status: "ACTIVE", OnboardedAt: &now, Metadata: []byte(`{"demo":true}`),
			})
		}
		if fErr != nil {
			return fmt.Errorf("demo franchise: %w", fErr)
		}
	}
	return nil
}

// ensureDemoCommissionRules makes the development tenant economically useful:
// every custody point can demonstrate the commission it earns. Production
// tenants still configure their own effective-dated rules through the API.
func ensureDemoCommissionRules(ctx context.Context, q *dbgen.Queries, orgID int64) error {
	scheme, err := q.GetDefaultScheme(ctx, orgID)
	if err != nil {
		return fmt.Errorf("demo commission scheme: %w", err)
	}
	type ruleSeed struct {
		code, name, commissionType, recipientRole, method string
		fixedMinor                                        *int64
		rateBP                                            *int32
		basis                                             *string
	}
	fixed := func(v int64) *int64 { return &v }
	rate := func(v int32) *int32 { return &v }
	basis := func(v string) *string { return &v }
	seeds := []ruleSeed{
		{"DEMO-BOOK", "Franchise booking commission", "BOOKING", "ORIGIN_FRANCHISE", "PERCENTAGE", nil, rate(1000), basis("FREIGHT")},
		{"DEMO-ORIGIN", "Origin handling commission", "ORIGIN_HANDLING", "ORIGIN_UNIT", "FIXED", fixed(15000), nil, nil},
		{"DEMO-TRANSIT", "Transit handling commission", "TRANSIT_HANDLING", "TRANSIT_UNIT", "FIXED", fixed(20000), nil, nil},
		{"DEMO-DEST", "Destination handling commission", "DESTINATION_HANDLING", "DESTINATION_UNIT", "FIXED", fixed(15000), nil, nil},
		{"DEMO-DELIVERY", "Destination franchise delivery commission", "DELIVERY", "DESTINATION_FRANCHISE", "PERCENTAGE", nil, rate(500), basis("FREIGHT")},
	}
	active := "ACTIVE"
	rules, err := q.ListCommissionRules(ctx, dbgen.ListCommissionRulesParams{
		OrganizationID: orgID, Limit: 500, Status: &active,
	})
	if err != nil {
		return fmt.Errorf("list demo commission rules: %w", err)
	}
	existing := make(map[string]bool, len(rules))
	for _, rule := range rules {
		existing[rule.Code] = true
	}
	for _, seed := range seeds {
		if existing[seed.code] {
			continue
		}
		description := "Development-only Nigeria commission example; replace with the approved commercial agreement."
		rule, cErr := q.CreateCommissionRule(ctx, dbgen.CreateCommissionRuleParams{
			PublicID: publicid.New("crl"), OrganizationID: orgID, SchemeID: scheme.ID,
			Code: seed.code, Name: seed.name, CommissionType: seed.commissionType,
			RecipientRole: seed.recipientRole, Priority: 10, Status: "ACTIVE",
			Description: &description,
		})
		if cErr != nil {
			return fmt.Errorf("create demo commission rule %s: %w", seed.code, cErr)
		}
		notes := "Development-only effective rate"
		if _, cErr = q.CreateRuleVersion(ctx, dbgen.CreateRuleVersionParams{
			PublicID: publicid.New("crv"), OrganizationID: orgID, RuleID: rule.ID,
			VersionNo: 1, CalculationMethod: seed.method, Currency: "NGN",
			FixedAmountMinor: seed.fixedMinor, RateBp: seed.rateBP, Basis: seed.basis,
			EffectiveFrom: time.Now().AddDate(0, 0, -1), Status: "ACTIVE", Notes: &notes,
		}); cErr != nil {
			return fmt.Errorf("create demo commission version %s: %w", seed.code, cErr)
		}
	}
	return nil
}

// seedFinance gives a new organization the finance configuration Release 3
// requires before anything can be posted: a chart of accounts, an open
// accounting period for the current month, a default commission scheme, and
// the statutory invoice numbering series.
//
// The definitions live in db/queries/ledger.sql alongside the ones migration
// 0020 applied to organizations that already existed, so there is a single
// description of "the default finance setup" rather than one per call site.
func seedFinance(ctx context.Context, q *dbgen.Queries, orgID int64) error {
	if err := q.SeedChartOfAccounts(ctx, orgID); err != nil {
		return fmt.Errorf("seed chart of accounts: %w", err)
	}
	if err := q.SeedCurrentAccountingPeriod(ctx, dbgen.SeedCurrentAccountingPeriodParams{
		OrganizationID: orgID, AsOf: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("seed accounting period: %w", err)
	}
	if err := q.SeedDefaultCommissionScheme(ctx, orgID); err != nil {
		return fmt.Errorf("seed commission scheme: %w", err)
	}
	if err := q.SeedInvoiceSequences(ctx, orgID); err != nil {
		return fmt.Errorf("seed invoice sequences: %w", err)
	}
	// A tenant with no notification templates suppresses every message, which
	// is indistinguishable from "notifications are broken".
	if err := q.SeedDefaultNotificationTemplates(ctx, orgID); err != nil {
		return fmt.Errorf("seed notification templates: %w", err)
	}
	return nil
}

func ptrStr(s string) *string { return &s }
