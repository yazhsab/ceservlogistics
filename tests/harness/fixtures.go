package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/security"
)

// Tenant is a fully configured organization ready to book shipments.
//
// It mirrors what a real onboarding produces: geography, a hub with two
// branches, service areas, a route, a product, zones, an active rate card with
// tax, and an admin user. Tests that need less still get the whole thing,
// because a booking touches all of it and building it once keeps each test
// focused on the behaviour under examination.
type Tenant struct {
	OrgID       int64
	OrgPublicID string
	OrgCode     string
	AWBPrefix   string

	AdminUserID    int64
	AdminEmail     string
	AdminPassword  string
	AdminAccessTok string

	HubID             int64
	HubCode           string
	HubPublicID       string
	DestHubID         int64
	DestHubCode       string
	DestHubPubID      string
	OriginBranchID    int64
	OriginBranch      string
	OriginBranchPubID string
	DestBranchID      int64
	DestBranch        string
	DestBranchPubID   string

	ServiceCode string
	ServiceID   int64

	OriginPincode string
	DestPincode   string
	FarPincode    string

	ZoneLocalID    int64
	ZoneNationalID int64

	RateCardID        int64
	RateCardVersionID int64

	CustomerID       int64
	CustomerPublicID string
}

// Geography seeds the shared reference data. It is global, so it is created
// once and reused by every tenant in a test.
func (e *Env) Geography(t *testing.T) map[string]int64 {
	t.Helper()
	ctx := context.Background()

	// The home market. Tests exercise the market the platform actually serves:
	// a fixture in a different country than DefaultCountry means every booking
	// resolves a postal code that is not there.
	country, err := e.Queries.UpsertCountry(ctx, dbgen.UpsertCountryParams{
		PublicID: publicid.New(publicid.PrefixCountry), Iso2: "NG", Iso3: "NGA",
		Name: "Nigeria", PhoneCode: "+234", Currency: "NGN",
	})
	if err != nil {
		t.Fatalf("seed country: %v", err)
	}

	states := map[string]string{
		"Lagos":                     "LA",
		"Federal Capital Territory": "FC",
		"Taraba":                    "TA",
	}
	stateIDs := map[string]int64{}
	for name, code := range states {
		st, sErr := e.Queries.UpsertState(ctx, dbgen.UpsertStateParams{
			PublicID: publicid.New(publicid.PrefixState), CountryID: country.ID,
			Code: code, Name: name,
		})
		if sErr != nil {
			t.Fatalf("seed state %s: %v", name, sErr)
		}
		stateIDs[name] = st.ID
	}

	// Lagos and Abuja are the busiest real corridor; Taraba stands in for the
	// remote-area surcharge path.
	//
	// Each postcode carries its LGA and the area names a sender would actually
	// type, because that is what a real onboarding loads and because place
	// search has nothing to find without them.
	pincodes := []struct {
		code, state, city, tier, lga string
		areas                        []string
		remote                       bool
	}{
		{"100001", "Lagos", "Lagos", "METRO", "Ikeja",
			[]string{"Ikeja GRA", "Computer Village", "Allen Avenue"}, false},
		{"101001", "Lagos", "Lagos", "METRO", "Eti-Osa",
			[]string{"Victoria Island", "Lekki Phase 1"}, false},
		{"900001", "Federal Capital Territory", "Abuja", "METRO", "Abuja Municipal",
			[]string{"Wuse II", "Maitama", "Garki"}, false},
		{"660001", "Taraba", "Jalingo", "TIER_3", "Jalingo",
			[]string{"Jalingo"}, true},
	}
	out := map[string]int64{"country": country.ID}
	for name, id := range stateIDs {
		out["state:"+name] = id
	}
	for _, p := range pincodes {
		city, cErr := e.Queries.UpsertCity(ctx, dbgen.UpsertCityParams{
			PublicID: publicid.New(publicid.PrefixCity), StateID: stateIDs[p.state],
			Name: p.city, Tier: p.tier,
		})
		if cErr != nil {
			t.Fatalf("seed city %s: %v", p.city, cErr)
		}
		district, dErr := e.Queries.UpsertDistrict(ctx, dbgen.UpsertDistrictParams{
			PublicID: publicid.New(publicid.PrefixDistrict), StateID: stateIDs[p.state],
			Code: strings.ToUpper(strings.NewReplacer(" ", "_", "-", "_").Replace(p.lga)),
			Name: p.lga,
		})
		if dErr != nil {
			t.Fatalf("seed LGA %s: %v", p.lga, dErr)
		}
		row, pErr := e.Queries.UpsertPincode(ctx, dbgen.UpsertPincodeParams{
			PublicID: publicid.New(publicid.PrefixPincode), CountryID: country.ID,
			Code: p.code, StateID: stateIDs[p.state], CityID: &city.ID,
			DistrictID: &district.ID, IsRemote: p.remote,
		})
		if pErr != nil {
			t.Fatalf("seed pincode %s: %v", p.code, pErr)
		}
		out["pincode:"+p.code] = row.ID
		out["lga:"+p.lga] = district.ID
		for _, area := range p.areas {
			if _, lErr := e.Queries.UpsertLocality(ctx, dbgen.UpsertLocalityParams{
				PublicID: publicid.New(publicid.PrefixLocality), PincodeID: row.ID, Name: area,
			}); lErr != nil {
				t.Fatalf("seed area %s: %v", area, lErr)
			}
		}
	}
	return out
}

// TenantOptions tunes fixture creation.
type TenantOptions struct {
	Code      string
	AWBPrefix string
	// BaseFreightMinor is the price for the first 500 g on the national lane.
	BaseFreightMinor int64
	// SkipRateCard leaves the tenant without pricing, for tests that assert the
	// missing-rate-card failure path.
	SkipRateCard bool
}

// NewTenant builds a complete, bookable tenant.
func (e *Env) NewTenant(t *testing.T, geo map[string]int64, opts TenantOptions) *Tenant {
	t.Helper()
	ctx := context.Background()
	if opts.Code == "" {
		// The random tail of the ULID, not its head: the first ten characters
		// are the timestamp, so two tenants created in the same millisecond
		// would collide on the organization code unique index.
		id := publicid.New("x")[2:]
		opts.Code = "T" + id[10:16]
	}
	if opts.AWBPrefix == "" {
		opts.AWBPrefix = randomPrefix()
	}
	if opts.BaseFreightMinor == 0 {
		opts.BaseFreightMinor = 5000 // INR 50.00
	}

	org, err := e.Queries.CreateOrganization(ctx, dbgen.CreateOrganizationParams{
		PublicID: publicid.New(publicid.PrefixOrganization), Code: opts.Code,
		Name: opts.Code + " Couriers", Timezone: "Africa/Lagos", Currency: "NGN",
		AwbPrefix: opts.AWBPrefix, Settings: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	// A new organization gets the default NDR catalogue, exactly as
	// provisioning does in production.
	if err := e.Queries.SeedDefaultNDRReasons(ctx, org.ID); err != nil {
		t.Fatalf("seed ndr reasons: %v", err)
	}

	// The same finance setup provisioning performs, so a tenant created by a
	// test can post commission and issue invoices without extra fixtures.
	if err := e.Queries.SeedChartOfAccounts(ctx, org.ID); err != nil {
		t.Fatalf("seed chart of accounts: %v", err)
	}
	if err := e.Queries.SeedCurrentAccountingPeriod(ctx, dbgen.SeedCurrentAccountingPeriodParams{
		OrganizationID: org.ID, AsOf: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed accounting period: %v", err)
	}
	if err := e.Queries.SeedDefaultCommissionScheme(ctx, org.ID); err != nil {
		t.Fatalf("seed commission scheme: %v", err)
	}
	if err := e.Queries.SeedInvoiceSequences(ctx, org.ID); err != nil {
		t.Fatalf("seed invoice sequences: %v", err)
	}
	if err := e.Queries.SeedDefaultNotificationTemplates(ctx, org.ID); err != nil {
		t.Fatalf("seed notification templates: %v", err)
	}
	tn := &Tenant{
		OrgID: org.ID, OrgPublicID: org.PublicID, OrgCode: org.Code, AWBPrefix: org.AwbPrefix,
		OriginPincode: "100001", DestPincode: "900001", FarPincode: "660001",
	}

	// Admin user with ORG_ADMIN.
	hasher := security.NewHasher(security.Argon2Params{Time: 1, MemoryKiB: 16384, Parallelism: 1})
	password := "TestPassw0rd!" + opts.Code
	hash, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	admin, err := e.Queries.CreateUser(ctx, dbgen.CreateUserParams{
		PublicID: publicid.New(publicid.PrefixUser), OrganizationID: org.ID,
		Email: "admin@" + lower(opts.Code) + ".test", PasswordHash: hash,
		FullName: "Admin " + opts.Code, Status: "ACTIVE",
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	tn.AdminUserID, tn.AdminEmail, tn.AdminPassword = admin.ID, admin.Email, password
	e.GrantRole(t, org.ID, admin.ID, "ORG_ADMIN", nil)

	// Network: one hub with two branches under it.
	tn.HubCode = opts.Code + "-HUB"
	hub := e.createUnit(t, org.ID, tn.HubCode, "Hub", "DELIVERY_HUB", nil, "100001", geo)
	tn.HubID = hub

	tn.OriginBranch = opts.Code + "-BLR"
	tn.OriginBranchID = e.createUnit(t, org.ID, tn.OriginBranch, "Lagos Branch", "COMPANY_BRANCH", &hub, "100001", geo)

	// The destination branch sits under its own hub so the lane needs a route.
	tn.DestHubCode = opts.Code + "-DHUB"
	destHub := e.createUnit(t, org.ID, tn.DestHubCode, "Abuja Hub", "DELIVERY_HUB", nil, "900001", geo)
	tn.DestHubID = destHub
	tn.DestBranch = opts.Code + "-DEL"
	tn.DestBranchID = e.createUnit(t, org.ID, tn.DestBranch, "Abuja Branch", "COMPANY_BRANCH", &destHub, "900001", geo)

	// Public identifiers, which is what the operational API accepts.
	tn.HubPublicID = e.unitPublicID(t, tn.HubID)
	tn.DestHubPubID = e.unitPublicID(t, tn.DestHubID)
	tn.OriginBranchPubID = e.unitPublicID(t, tn.OriginBranchID)
	tn.DestBranchPubID = e.unitPublicID(t, tn.DestBranchID)

	// Service areas: origin branch picks up in 100001, destination delivers in
	// 900001, and the origin branch also covers the remote postcode for surcharge
	// tests.
	e.createServiceArea(t, org.ID, tn.OriginBranchID, geo["pincode:100001"], "BOTH", 100)
	e.createServiceArea(t, org.ID, tn.DestBranchID, geo["pincode:900001"], "BOTH", 100)
	e.createServiceArea(t, org.ID, tn.DestBranchID, geo["pincode:660001"], "DELIVERY", 100)

	// Product.
	tn.ServiceCode = "EXPRESS"
	svc, err := e.Queries.CreateCourierService(ctx, dbgen.CreateCourierServiceParams{
		PublicID: publicid.New(publicid.PrefixCourierService), OrganizationID: org.ID,
		Code: tn.ServiceCode, Name: "Express Air", Description: "Next-day air", Mode: "AIR",
		MinWeightGrams: 1, MaxWeightGrams: 50_000, VolumetricDivisor: 5000,
		WeightRoundingGrams: 500, CodAllowed: true, InsuranceAllowed: true,
		SlaTransitHours: 48, SlaRules: []byte(`{"remoteAreaExtraHours":24}`),
		EffectiveFrom: time.Now().AddDate(0, 0, -1), SortOrder: 1,
	})
	if err != nil {
		t.Fatalf("create courier service: %v", err)
	}
	tn.ServiceID = svc.ID

	// Route between the two hubs.
	route, err := e.Queries.CreateRouteDefinition(ctx, dbgen.CreateRouteDefinitionParams{
		PublicID: publicid.New(publicid.PrefixRoute), OrganizationID: org.ID,
		Code: opts.Code + "-LOS-ABV", Name: "Lagos to Abuja",
		OriginUnitID: hub, DestinationUnitID: destHub, Priority: 100, TransitHours: 24,
		EffectiveFrom: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("create route: %v", err)
	}
	if _, err := e.Queries.CreateRouteLeg(ctx, dbgen.CreateRouteLegParams{
		PublicID: publicid.New(publicid.PrefixRouteLeg), OrganizationID: org.ID,
		RouteDefinitionID: route.ID, Sequence: 1, FromUnitID: hub, ToUnitID: destHub,
		Mode: "AIR", TransitHours: 24,
	}); err != nil {
		t.Fatalf("create route leg: %v", err)
	}

	// Zones and mappings.
	tn.ZoneLocalID = e.createZone(t, org.ID, "LOCAL", "Local", "LOCAL", 1)
	tn.ZoneNationalID = e.createZone(t, org.ID, "NATIONAL", "National", "NATIONAL", 2)
	e.mapZone(t, org.ID, geo["pincode:100001"], tn.ZoneLocalID, nil)
	e.mapZone(t, org.ID, geo["pincode:101001"], tn.ZoneLocalID, nil)
	e.mapZone(t, org.ID, geo["pincode:900001"], tn.ZoneNationalID, nil)
	remote := true
	e.mapZone(t, org.ID, geo["pincode:660001"], tn.ZoneNationalID, &remote)

	if !opts.SkipRateCard {
		e.seedRateCard(t, tn, opts.BaseFreightMinor)
	}

	// A retail customer to book against.
	cust, err := e.Queries.CreateCustomer(ctx, dbgen.CreateCustomerParams{
		PublicID: publicid.New(publicid.PrefixCustomer), OrganizationID: org.ID,
		Code: "WALKIN", CustomerType: "RETAIL", Name: "Walk-in Customer",
		Phone: "+919800000000", OwningUnitID: &tn.OriginBranchID, Metadata: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	tn.CustomerID, tn.CustomerPublicID = cust.ID, cust.PublicID

	tn.AdminAccessTok = e.Login(t, tn.AdminEmail, tn.AdminPassword)
	return tn
}

func (e *Env) seedRateCard(t *testing.T, tn *Tenant, baseMinor int64) {
	t.Helper()
	ctx := context.Background()

	card, err := e.Queries.CreateRateCard(ctx, dbgen.CreateRateCardParams{
		PublicID: publicid.New(publicid.PrefixRateCard), OrganizationID: tn.OrgID,
		Code: "RETAIL", Name: "Retail Tariff", Description: "Default retail rates",
		Scope: "RETAIL", Currency: "NGN", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create rate card: %v", err)
	}
	version, err := e.Queries.CreateRateCardVersion(ctx, dbgen.CreateRateCardVersionParams{
		PublicID: publicid.New(publicid.PrefixRateCardVersion), OrganizationID: tn.OrgID,
		RateCardID: card.ID, EffectiveFrom: time.Now().Add(-time.Hour), Notes: "seed",
	})
	if err != nil {
		t.Fatalf("create rate card version: %v", err)
	}
	tn.RateCardID, tn.RateCardVersionID = card.ID, version.ID

	lanes := []struct{ origin, dest int64 }{
		{tn.ZoneLocalID, tn.ZoneNationalID},
		{tn.ZoneLocalID, tn.ZoneLocalID},
		{tn.ZoneNationalID, tn.ZoneLocalID},
		{tn.ZoneNationalID, tn.ZoneNationalID},
	}
	for _, lane := range lanes {
		if _, err := e.Queries.UpsertZoneRate(ctx, dbgen.UpsertZoneRateParams{
			PublicID: publicid.New(publicid.PrefixZoneRate), OrganizationID: tn.OrgID,
			RateCardVersionID: version.ID, CourierServiceID: tn.ServiceID,
			OriginZoneID: lane.origin, DestinationZoneID: lane.dest,
			BaseWeightGrams: 500, BasePriceMinor: baseMinor,
			AdditionalStepGrams: 500, AdditionalPriceMinor: baseMinor / 2,
			MinChargeableWeightGrams: 0,
		}); err != nil {
			t.Fatalf("create zone rate: %v", err)
		}
	}

	// Fuel surcharge: 18.5% of freight, taxable.
	fuelBP := int32(1850)
	if _, err := e.Queries.CreateSurchargeRule(ctx, dbgen.CreateSurchargeRuleParams{
		PublicID: publicid.New(publicid.PrefixSurchargeRule), OrganizationID: tn.OrgID,
		RateCardVersionID: version.ID, Code: "FUEL", Name: "Fuel surcharge",
		SurchargeType: "FUEL", CalcType: "PERCENTAGE", PercentageBp: &fuelBP,
		AppliesTo: "FREIGHT", Conditions: []byte("{}"), Priority: 10, IsTaxable: true,
	}); err != nil {
		t.Fatalf("create fuel surcharge: %v", err)
	}
	// Remote-area surcharge: flat INR 75, only when either end is remote.
	remoteValue := int64(7500)
	if _, err := e.Queries.CreateSurchargeRule(ctx, dbgen.CreateSurchargeRuleParams{
		PublicID: publicid.New(publicid.PrefixSurchargeRule), OrganizationID: tn.OrgID,
		RateCardVersionID: version.ID, Code: "ODA", Name: "Remote area surcharge",
		SurchargeType: "REMOTE_AREA", CalcType: "FIXED", ValueMinor: &remoteValue,
		AppliesTo: "FREIGHT", Conditions: []byte(`{"remoteEither":true}`),
		Priority: 20, IsTaxable: true,
	}); err != nil {
		t.Fatalf("create remote surcharge: %v", err)
	}
	// COD fee: 2% of the COD amount, minimum INR 30.
	codBP := int32(200)
	codMin := int64(3000)
	if _, err := e.Queries.CreateSurchargeRule(ctx, dbgen.CreateSurchargeRuleParams{
		PublicID: publicid.New(publicid.PrefixSurchargeRule), OrganizationID: tn.OrgID,
		RateCardVersionID: version.ID, Code: "COD", Name: "COD collection fee",
		SurchargeType: "COD", CalcType: "PERCENTAGE", PercentageBp: &codBP,
		AppliesTo: "COD_AMOUNT", MinAmountMinor: &codMin,
		Conditions: []byte(`{"paymentModes":["COD"]}`), Priority: 30, IsTaxable: true,
	}); err != nil {
		t.Fatalf("create cod surcharge: %v", err)
	}

	if _, err := e.Queries.ActivateRateCardVersion(ctx, dbgen.ActivateRateCardVersionParams{
		PublicID: version.PublicID, OrganizationID: tn.OrgID,
	}); err != nil {
		t.Fatalf("activate rate card version: %v", err)
	}

	// The home market: Nigerian VAT at a single rate, applied to every lane.
	//
	// intra_state_only is nil rather than false, which is the difference
	// between "applies either way" and "applies only to inter-state" — a
	// single-rate jurisdiction has no split to express, and encoding one would
	// misrepresent the tax.
	taxes := []struct {
		code, name, taxType string
		bp                  int32
		intraOnly           *bool
	}{
		{"VAT75", "VAT 7.5%", "VAT", 750, nil},
	}
	for i, tx := range taxes {
		if _, err := e.Queries.CreateTaxRule(ctx, dbgen.CreateTaxRuleParams{
			PublicID: publicid.New(publicid.PrefixTaxRule), OrganizationID: tn.OrgID,
			Code: tx.code, Name: tx.name, TaxType: tx.taxType, PercentageBp: tx.bp,
			IntraStateOnly: tx.intraOnly, Priority: int32(10 + i),
			EffectiveFrom: time.Now().Add(-time.Hour),
		}); err != nil {
			t.Fatalf("create tax rule %s: %v", tx.code, err)
		}
	}
}

// GrantRole assigns a system role, optionally scoped to an operating unit.
func (e *Env) GrantRole(t *testing.T, orgID, userID int64, roleCode string, unitID *int64) {
	t.Helper()
	ctx := context.Background()
	role, err := e.Queries.GetSystemRoleByCode(ctx, roleCode)
	if err != nil {
		t.Fatalf("lookup role %s: %v", roleCode, err)
	}
	if _, err := e.Queries.AssignUserRole(ctx, dbgen.AssignUserRoleParams{
		OrganizationID: orgID, UserID: userID, RoleID: role.ID, OperatingUnitID: unitID,
	}); err != nil {
		t.Fatalf("assign role %s: %v", roleCode, err)
	}
}

// NewUser creates an additional user with the given roles.
func (e *Env) NewUser(t *testing.T, orgID int64, email, roleCode string, unitID *int64) (int64, string, string) {
	t.Helper()
	ctx := context.Background()
	hasher := security.NewHasher(security.Argon2Params{Time: 1, MemoryKiB: 16384, Parallelism: 1})
	password := "UserPassw0rd!x"
	hash, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user, err := e.Queries.CreateUser(ctx, dbgen.CreateUserParams{
		PublicID: publicid.New(publicid.PrefixUser), OrganizationID: orgID,
		Email: email, PasswordHash: hash, FullName: email, Status: "ACTIVE",
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	if roleCode != "" {
		e.GrantRole(t, orgID, user.ID, roleCode, unitID)
	}
	token := e.Login(t, email, password)
	return user.ID, user.PublicID, token
}

// Login authenticates and returns an access token.
func (e *Env) Login(t *testing.T, email, password string) string {
	t.Helper()
	resp := e.Do(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"email": email, "password": password,
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("login %s failed: %d %s", email, resp.Status, resp.Raw)
	}
	tokens, ok := resp.Body["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("login response missing tokens: %s", resp.Raw)
	}
	token, _ := tokens["accessToken"].(string)
	if token == "" {
		t.Fatalf("login response missing accessToken: %s", resp.Raw)
	}
	return token
}

// Response is a decoded API response.
type Response struct {
	Status  int
	Body    map[string]any
	Raw     string
	Headers http.Header
}

// ErrorCode returns the error code from a failure envelope, or "".
func (r Response) ErrorCode() string {
	errObj, ok := r.Body["error"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := errObj["code"].(string)
	return code
}

// Do issues an API request with an optional bearer token.
func (e *Env) Do(t *testing.T, method, path, token string, body any, headers ...[2]string) Response {
	t.Helper()
	return e.DoRaw(t, method, path, token, body, headers...)
}

// DoRaw is Do without the helper marker, for use inside goroutines where
// t.Helper() is not meaningful.
func (e *Env) DoRaw(t *testing.T, method, path, token string, body any, headers ...[2]string) Response {
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, e.URL(path), reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}

	resp, err := e.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	out := Response{Status: resp.StatusCode, Headers: resp.Header}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read response: %v", err)
	}
	out.Raw = buf.String()
	if out.Raw != "" {
		_ = json.Unmarshal(buf.Bytes(), &out.Body)
	}
	return out
}

// ---- small helpers ---------------------------------------------------------

func (e *Env) createUnit(
	t *testing.T, orgID int64, code, name, unitType string, parent *int64, pincode string, geo map[string]int64,
) int64 {
	t.Helper()
	pincodeID := geo["pincode:"+pincode]
	unit, err := e.Queries.CreateOperatingUnit(context.Background(), dbgen.CreateOperatingUnitParams{
		PublicID: publicid.New(publicid.PrefixOperatingUnit), OrganizationID: orgID,
		Code: code, Name: name, UnitType: unitType, ParentUnitID: parent,
		AddressLine1: "1 Test Road", Pincode: pincode, PincodeID: &pincodeID,
		OperatingHours: []byte("{}"), EffectiveFrom: time.Now().AddDate(0, 0, -1),
	})
	if err != nil {
		t.Fatalf("create operating unit %s: %v", code, err)
	}
	return unit.ID
}

func (e *Env) createServiceArea(t *testing.T, orgID, unitID, pincodeID int64, areaType string, priority int32) {
	t.Helper()
	if _, err := e.Queries.CreateServiceArea(context.Background(), dbgen.CreateServiceAreaParams{
		PublicID: publicid.New(publicid.PrefixServiceArea), OrganizationID: orgID,
		OperatingUnitID: unitID, PincodeID: pincodeID, AreaType: areaType,
		Priority: priority, EffectiveFrom: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("create service area: %v", err)
	}
}

func (e *Env) createZone(t *testing.T, orgID int64, code, name, zoneType string, order int32) int64 {
	t.Helper()
	zone, err := e.Queries.CreateZone(context.Background(), dbgen.CreateZoneParams{
		PublicID: publicid.New(publicid.PrefixZone), OrganizationID: orgID,
		Code: code, Name: name, ZoneType: zoneType, Description: "", SortOrder: order,
	})
	if err != nil {
		t.Fatalf("create zone %s: %v", code, err)
	}
	return zone.ID
}

func (e *Env) mapZone(t *testing.T, orgID, pincodeID, zoneID int64, remote *bool) {
	t.Helper()
	if _, err := e.Queries.UpsertPincodeZoneMapping(context.Background(), dbgen.UpsertPincodeZoneMappingParams{
		PublicID: publicid.New(publicid.PrefixZoneMapping), OrganizationID: orgID,
		PincodeID: pincodeID, ZoneID: zoneID, IsRemoteOverride: remote,
		EffectiveFrom: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("map zone: %v", err)
	}
}

// BookingBody builds a valid booking payload for a tenant.
func (tn *Tenant) BookingBody(overrides map[string]any) map[string]any {
	body := map[string]any{
		"customerId":  tn.CustomerPublicID,
		"serviceCode": tn.ServiceCode,
		"paymentMode": "PREPAID",
		"sender": map[string]any{
			"contactName": "Sender Name", "phone": "+919800000001",
			"line1": "12 Origin Street", "city": "Lagos", "state": "Lagos",
			"pincode": tn.OriginPincode,
		},
		"recipient": map[string]any{
			"contactName": "Recipient Name", "phone": "+919800000002",
			"line1": "34 Destination Road", "city": "Abuja", "state": "Federal Capital Territory",
			"pincode": tn.DestPincode,
		},
		"packages": []map[string]any{
			{"actualWeightGrams": 500, "lengthMm": 200, "widthMm": 150, "heightMm": 100},
		},
		"contentDescription": "Documents",
	}
	for k, v := range overrides {
		body[k] = v
	}
	return body
}

func randomPrefix() string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	// The random tail, not the head. A ULID is 10 characters of timestamp
	// followed by 16 of randomness, so two tenants created in the same
	// millisecond window used to derive the same prefix — and their AWBs then
	// collided across tenants, which looked like a booking bug.
	raw := publicid.New("x")[2+10:]
	out := make([]byte, 3)
	for i := 0; i < 3; i++ {
		out[i] = letters[int(raw[i])%len(letters)]
	}
	return string(out)
}

func lower(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 32
		}
	}
	return string(out)
}

func boolPtr(b bool) *bool { return &b }

var _ = fmt.Sprintf

// unitPublicID reads back an operating unit's external identifier.
func (e *Env) unitPublicID(t *testing.T, id int64) string {
	t.Helper()
	var publicID string
	if err := e.DB.Pool.QueryRow(context.Background(),
		`SELECT public_id FROM operating_units WHERE id = $1`, id).Scan(&publicID); err != nil {
		t.Fatalf("read operating unit public id: %v", err)
	}
	return publicID
}

// RandomKey returns a unique string for idempotency keys, codes and test
// emails.
//
// It returns the ULID's random tail rather than the whole value: the first ten
// characters are a millisecond timestamp, so two calls in the same millisecond
// share them, and a caller slicing the front would get colliding "unique"
// values. Asking for the tail makes any prefix a caller takes actually random.
func RandomKey() string {
	id := publicid.New("x")
	return id[len(id)-16:]
}

// UserPublicID looks up a user's external identifier by email.
func (e *Env) UserPublicID(t *testing.T, email string) string {
	t.Helper()
	var publicID string
	if err := e.DB.Pool.QueryRow(context.Background(),
		`SELECT public_id FROM users WHERE email = $1`, email).Scan(&publicID); err != nil {
		t.Fatalf("read user public id: %v", err)
	}
	return publicID
}

// UploadPOD submits a proof of delivery with a real one-pixel PNG.
//
// A genuine image matters: the service decides the content type by sniffing the
// bytes, so a test that posted "not-a-png" would exercise the rejection path
// rather than the acceptance path.
func (e *Env) UploadPOD(t *testing.T, token, awb, unitPublicID string) Response {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range map[string]string{
		"barcode": awb, "recipientName": "R Sharma", "recipientRelationship": "SELF",
	} {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	part, err := w.CreateFormFile("signature", "signature.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(onePixelPNG); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", e.URL("/api/v1/pod"), &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Operating-Unit", unitPublicID)

	resp, err := e.Server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := Response{Status: resp.StatusCode, Raw: string(raw), Headers: resp.Header}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}

// onePixelPNG is the smallest valid PNG: enough for http.DetectContentType to
// identify it as image/png.
var onePixelPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

// OnePixelPNG returns a copy of the smallest valid PNG, for tests that need a
// file the content sniffer will genuinely accept.
func OnePixelPNG() []byte {
	out := make([]byte, len(onePixelPNG))
	copy(out, onePixelPNG)
	return out
}
