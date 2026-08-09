package customer

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/geography"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

type addressRequest struct {
	Label        string   `json:"label"`
	AddressType  string   `json:"addressType"`
	ContactName  string   `json:"contactName"`
	ContactPhone string   `json:"contactPhone"`
	AltPhone     string   `json:"altPhone,omitempty"`
	Line1        string   `json:"line1"`
	Line2        string   `json:"line2,omitempty"`
	Landmark     string   `json:"landmark,omitempty"`
	Pincode      string   `json:"pincode"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	IsDefault    bool     `json:"isDefault,omitempty"`
}

type updateAddressRequest struct {
	addressRequest
	Status string `json:"status,omitempty"`
}

func (h *Handler) listAddresses(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customer, err := h.loadCustomer(r, p)
	if err != nil {
		return err
	}
	params := dbgen.ListCustomerAddressesParams{CustomerID: customer.ID}
	if st, sErr := httpx.QueryEnum(r, "status", []string{"ACTIVE", "INACTIVE"}); sErr != nil {
		return sErr
	} else if st != "" {
		params.Status = &st
	} else {
		active := "ACTIVE"
		params.Status = &active
	}
	if at, aErr := httpx.QueryEnum(r, "addressType", AddressTypes); aErr != nil {
		return aErr
	} else if at != "" {
		params.AddressType = &at
	}
	rows, err := h.svc.q.ListCustomerAddresses(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		items = append(items, addressView(a))
	}
	return httpx.OK(w, map[string]any{"data": items})
}

func (h *Handler) createAddress(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customer, err := h.loadCustomer(r, p)
	if err != nil {
		return err
	}
	var req addressRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	fields, err := h.validateAddress(r, req, true)
	if err != nil {
		return err
	}

	params := dbgen.CreateCustomerAddressParams{
		PublicID: publicid.New(publicid.PrefixCustomerAddress), OrganizationID: p.OrganizationID,
		CustomerID: customer.ID, Label: fields.label, AddressType: fields.addressType,
		ContactName: fields.contactName, ContactPhone: fields.contactPhone,
		AltPhone: optional(fields.altPhone), Line1: fields.line1,
		Line2: optional(req.Line2), Landmark: optional(req.Landmark),
		Pincode: fields.pincode, PincodeID: &fields.pincodeID,
		CityID: fields.cityID, StateID: &fields.stateID,
		CityName: fields.cityName, StateName: fields.stateName, CountryCode: "IN",
		Latitude: req.Latitude, Longitude: req.Longitude, IsDefault: req.IsDefault,
	}

	var created dbgen.CustomerAddress
	err = h.svc.db.InTx(r.Context(), func(tx pgx.Tx) error {
		qtx := h.svc.q.WithTx(tx)
		// Only one default per (customer, address type); clearing first inside
		// the transaction avoids tripping the partial unique index.
		if req.IsDefault {
			if _, cErr := qtx.ClearDefaultAddress(r.Context(), dbgen.ClearDefaultAddressParams{
				CustomerID: customer.ID, AddressType: fields.addressType,
			}); cErr != nil {
				return apierr.Internal(cErr)
			}
		}
		var aErr error
		created, aErr = qtx.CreateCustomerAddress(r.Context(), params)
		if aErr != nil {
			return apierr.Internal(aErr)
		}
		return nil
	})
	if err != nil {
		return err
	}

	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionAddressCreated, ResourceType: "customer_address",
		ResourceID: &created.ID, ResourcePublicID: created.PublicID,
		After: map[string]any{
			"customerId": customer.PublicID, "label": created.Label,
			"pincode": created.Pincode, "addressType": created.AddressType,
		},
	}))
	return httpx.Created(w, "", addressViewFromRow(created))
}

func (h *Handler) updateAddress(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customer, err := h.loadCustomer(r, p)
	if err != nil {
		return err
	}
	addressID, err := httpx.PathPublicID(r, "addressId", publicid.PrefixCustomerAddress, "Address")
	if err != nil {
		return err
	}
	existing, err := h.svc.q.GetCustomerAddressByPublicID(r.Context(), dbgen.GetCustomerAddressByPublicIDParams{
		PublicID: addressID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Address")
		}
		return apierr.Internal(err)
	}
	// An address id from another customer must not be editable through this
	// customer's URL.
	if existing.CustomerID != customer.ID {
		return apierr.NotFound("Address")
	}

	var req updateAddressRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	params := dbgen.UpdateCustomerAddressParams{
		PublicID: addressID, OrganizationID: p.OrganizationID,
	}
	v := validate.New()
	if req.Label != "" {
		l := v.Text("label", req.Label, 1, 80, true)
		params.Label = &l
	}
	if req.AddressType != "" {
		at := v.Enum("addressType", req.AddressType, AddressTypes, true)
		params.AddressType = &at
	}
	if req.ContactName != "" {
		cn := v.Text("contactName", req.ContactName, 2, 160, true)
		params.ContactName = &cn
	}
	if req.ContactPhone != "" {
		cp := v.Phone("contactPhone", req.ContactPhone, true)
		params.ContactPhone = &cp
	}
	if req.Line1 != "" {
		l1 := v.Text("line1", req.Line1, 3, 200, true)
		params.Line1 = &l1
	}
	params.Line2 = optional(req.Line2)
	params.Landmark = optional(req.Landmark)
	params.AltPhone = optional(req.AltPhone)
	if req.Pincode != "" {
		pin := v.Pincode("pincode", req.Pincode)
		if pin != "" {
			pincode, pErr := h.geo.LookupPincode(r.Context(), pin, geography.DefaultCountry)
			if pErr != nil {
				v.Add("pincode", "This PIN code is not recognised.")
			} else {
				params.Pincode = &pin
				params.PincodeID = &pincode.ID
				params.StateID = &pincode.StateID
				params.CityID = pincode.CityID
				params.StateName = &pincode.StateName
				city := pincode.CityName
				if city == "" {
					city = pincode.DistrictName
				}
				params.CityName = &city
			}
		}
	}
	v.Latitude("latitude", req.Latitude)
	v.Longitude("longitude", req.Longitude)
	params.Latitude = req.Latitude
	params.Longitude = req.Longitude
	if req.Status != "" {
		st := v.Enum("status", req.Status, []string{"ACTIVE", "INACTIVE"}, true)
		params.Status = &st
	}
	if err := v.Err(); err != nil {
		return err
	}

	var updated dbgen.CustomerAddress
	err = h.svc.db.InTx(r.Context(), func(tx pgx.Tx) error {
		qtx := h.svc.q.WithTx(tx)
		if req.IsDefault {
			addressType := existing.AddressType
			if params.AddressType != nil {
				addressType = *params.AddressType
			}
			if _, cErr := qtx.ClearDefaultAddress(r.Context(), dbgen.ClearDefaultAddressParams{
				CustomerID: customer.ID, AddressType: addressType,
			}); cErr != nil {
				return apierr.Internal(cErr)
			}
			isDefault := true
			params.IsDefault = &isDefault
		}
		var uErr error
		updated, uErr = qtx.UpdateCustomerAddress(r.Context(), params)
		if uErr != nil {
			if database.IsNoRows(uErr) {
				return apierr.NotFound("Address")
			}
			return apierr.Internal(uErr)
		}
		return nil
	})
	if err != nil {
		return err
	}

	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionAddressUpdated, ResourceType: "customer_address",
		ResourceID: &updated.ID, ResourcePublicID: updated.PublicID,
		Before: map[string]any{"line1": existing.Line1, "pincode": existing.Pincode, "status": existing.Status},
		After:  map[string]any{"line1": updated.Line1, "pincode": updated.Pincode, "status": updated.Status},
	}))
	return httpx.OK(w, addressViewFromRow(updated))
}

// ---- contacts --------------------------------------------------------------

type contactRequest struct {
	Name        string `json:"name"`
	Designation string `json:"designation,omitempty"`
	Email       string `json:"email,omitempty"`
	Phone       string `json:"phone"`
	ContactType string `json:"contactType,omitempty"`
	IsPrimary   bool   `json:"isPrimary,omitempty"`
}

func (h *Handler) listContacts(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customer, err := h.loadCustomer(r, p)
	if err != nil {
		return err
	}
	rows, err := h.svc.q.ListCustomerContacts(r.Context(), customer.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, c := range rows {
		items = append(items, map[string]any{
			"id": c.PublicID, "name": c.Name, "designation": c.Designation,
			"email": c.Email, "phone": c.Phone, "contactType": c.ContactType,
			"isPrimary": c.IsPrimary, "status": c.Status,
		})
	}
	return httpx.OK(w, map[string]any{"data": items})
}

func (h *Handler) createContact(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customer, err := h.loadCustomer(r, p)
	if err != nil {
		return err
	}
	var req contactRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	name := v.Text("name", req.Name, 2, 160, true)
	phone := v.Phone("phone", req.Phone, true)
	var email string
	if req.Email != "" {
		email = v.Email("email", req.Email)
	}
	contactType := "PRIMARY"
	if req.ContactType != "" {
		contactType = v.Enum("contactType", req.ContactType, ContactTypes, true)
	}
	if err := v.Err(); err != nil {
		return err
	}

	var created dbgen.CustomerContact
	err = h.svc.db.InTx(r.Context(), func(tx pgx.Tx) error {
		qtx := h.svc.q.WithTx(tx)
		if req.IsPrimary {
			if _, cErr := qtx.ClearPrimaryContact(r.Context(), customer.ID); cErr != nil {
				return apierr.Internal(cErr)
			}
		}
		var aErr error
		created, aErr = qtx.CreateCustomerContact(r.Context(), dbgen.CreateCustomerContactParams{
			PublicID: publicid.New(publicid.PrefixCustomerContact), OrganizationID: p.OrganizationID,
			CustomerID: customer.ID, Name: name, Designation: optional(req.Designation),
			Email: optional(email), Phone: phone, ContactType: contactType, IsPrimary: req.IsPrimary,
		})
		if aErr != nil {
			return apierr.Internal(aErr)
		}
		return nil
	})
	if err != nil {
		return err
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionCustomerUpdated, ResourceType: "customer_contact",
		ResourceID: &created.ID, ResourcePublicID: created.PublicID,
		After: map[string]any{"customerId": customer.PublicID, "name": created.Name},
	}))
	return httpx.Created(w, "", map[string]any{
		"id": created.PublicID, "name": created.Name, "phone": created.Phone,
		"contactType": created.ContactType, "isPrimary": created.IsPrimary,
	})
}

// ---- credit and billing ----------------------------------------------------

type creditRequest struct {
	CreditLimitMinor int64  `json:"creditLimitMinor"`
	PaymentTermsDays int    `json:"paymentTermsDays"`
	CreditStatus     string `json:"creditStatus,omitempty"`
	BlockedReason    string `json:"blockedReason,omitempty"`
}

func (h *Handler) getCredit(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customer, err := h.loadCustomer(r, p)
	if err != nil {
		return err
	}
	cp, err := h.svc.q.GetCreditProfileByCustomer(r.Context(), customer.ID)
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Credit profile")
		}
		return apierr.Internal(err)
	}
	limit, page, err := window(r)
	if err != nil {
		return err
	}
	entries, err := h.svc.q.ListCreditEntries(r.Context(), dbgen.ListCreditEntriesParams{
		CustomerID: customer.ID, OrganizationID: p.OrganizationID,
		RowLimit: int32(limit), RowOffset: int32((page - 1) * limit),
	})
	if err != nil {
		return apierr.Internal(err)
	}
	history := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		history = append(history, map[string]any{
			"entryType": e.EntryType, "amountMinor": e.AmountMinor,
			"balanceAfterMinor": e.BalanceAfterMinor, "awb": e.Awb,
			"reason": e.Reason, "createdAt": e.CreatedAt,
		})
	}
	body := creditView(cp)
	body["history"] = history
	return httpx.OK(w, body)
}

func (h *Handler) setCredit(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customer, err := h.loadCustomer(r, p)
	if err != nil {
		return err
	}
	var req creditRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	v.NonNegativeMinor("creditLimitMinor", req.CreditLimitMinor)
	v.IntRange("paymentTermsDays", req.PaymentTermsDays, 0, 180)
	status := "GOOD"
	if req.CreditStatus != "" {
		status = v.Enum("creditStatus", req.CreditStatus, CreditStatuses, true)
	}
	if status == "BLOCKED" {
		v.Text("blockedReason", req.BlockedReason, 5, 500, true)
	}
	if err := v.Err(); err != nil {
		return err
	}

	before, _ := h.svc.q.GetCreditProfileByCustomer(r.Context(), customer.ID)
	params := dbgen.UpsertCreditProfileParams{
		PublicID: publicid.New(publicid.PrefixCreditProfile), OrganizationID: p.OrganizationID,
		CustomerID: customer.ID, Currency: p.OrganizationCurrency,
		CreditLimitMinor: req.CreditLimitMinor, PaymentTermsDays: int32(req.PaymentTermsDays),
		CreditStatus: status,
	}
	if req.BlockedReason != "" {
		params.BlockedReason = &req.BlockedReason
	}
	cp, err := h.svc.q.UpsertCreditProfile(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionCreditChanged, ResourceType: "credit_profile",
		ResourceID: &cp.ID, ResourcePublicID: cp.PublicID, Reason: req.BlockedReason,
		Before: map[string]any{
			"creditLimitMinor": before.CreditLimitMinor, "creditStatus": before.CreditStatus,
		},
		After: map[string]any{
			"creditLimitMinor": cp.CreditLimitMinor, "creditStatus": cp.CreditStatus,
		},
	}))
	return httpx.OK(w, creditView(cp))
}

type billingProfileRequest struct {
	LegalName          string `json:"legalName"`
	GSTNumber          string `json:"gstNumber,omitempty"`
	PANNumber          string `json:"panNumber,omitempty"`
	BillingAddressID   string `json:"billingAddressId,omitempty"`
	BillingEmail       string `json:"billingEmail,omitempty"`
	InvoiceDelivery    string `json:"invoiceDelivery,omitempty"`
	PlaceOfSupplyState string `json:"placeOfSupplyStateCode,omitempty"`
}

func (h *Handler) setBillingProfile(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	customer, err := h.loadCustomer(r, p)
	if err != nil {
		return err
	}
	var req billingProfileRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	legalName := v.Text("legalName", req.LegalName, 2, 200, true)
	gst := v.GSTIN("gstNumber", req.GSTNumber, false)
	pan := v.PAN("panNumber", req.PANNumber, false)
	var billingEmail string
	if req.BillingEmail != "" {
		billingEmail = v.Email("billingEmail", req.BillingEmail)
	}
	delivery := "EMAIL"
	if req.InvoiceDelivery != "" {
		delivery = v.Enum("invoiceDelivery", req.InvoiceDelivery, []string{"EMAIL", "PORTAL", "BOTH"}, true)
	}
	if err := v.Err(); err != nil {
		return err
	}

	params := dbgen.UpsertBillingProfileParams{
		PublicID: publicid.New(publicid.PrefixBillingProfile), OrganizationID: p.OrganizationID,
		CustomerID: customer.ID, LegalName: legalName,
		GstNumber: optional(gst), PanNumber: optional(pan),
		BillingEmail: optional(billingEmail), InvoiceDelivery: delivery,
		Currency: p.OrganizationCurrency,
	}
	if req.BillingAddressID != "" {
		addr, aErr := h.svc.q.GetCustomerAddressByPublicID(r.Context(), dbgen.GetCustomerAddressByPublicIDParams{
			PublicID: req.BillingAddressID, OrganizationID: p.OrganizationID,
		})
		if aErr != nil || addr.CustomerID != customer.ID {
			return apierr.Validation("The billing address does not belong to this customer.", nil)
		}
		params.BillingAddressID = &addr.ID
		params.PlaceOfSupplyStateID = addr.StateID
	}

	profile, err := h.svc.q.UpsertBillingProfile(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionCustomerUpdated, ResourceType: "billing_profile",
		ResourceID: &profile.ID, ResourcePublicID: profile.PublicID,
		After: map[string]any{"legalName": profile.LegalName, "gstNumber": profile.GstNumber},
	}))
	return httpx.OK(w, map[string]any{
		"id": profile.PublicID, "legalName": profile.LegalName,
		"gstNumber": profile.GstNumber, "invoiceDelivery": profile.InvoiceDelivery,
	})
}

// ---- helpers ---------------------------------------------------------------

type addressFields struct {
	label        string
	addressType  string
	contactName  string
	contactPhone string
	altPhone     string
	line1        string
	pincode      string
	pincodeID    int64
	stateID      int64
	cityID       *int64
	cityName     string
	stateName    string
}

func (h *Handler) validateAddress(r *http.Request, req addressRequest, required bool) (*addressFields, error) {
	v := validate.New()
	f := &addressFields{
		label:        v.Text("label", req.Label, 1, 80, required),
		addressType:  v.Enum("addressType", req.AddressType, AddressTypes, required),
		contactName:  v.Text("contactName", req.ContactName, 2, 160, required),
		contactPhone: v.Phone("contactPhone", req.ContactPhone, required),
		altPhone:     v.Phone("altPhone", req.AltPhone, false),
		line1:        v.Text("line1", req.Line1, 3, 200, required),
		pincode:      v.Pincode("pincode", req.Pincode),
	}
	v.Latitude("latitude", req.Latitude)
	v.Longitude("longitude", req.Longitude)
	if err := v.Err(); err != nil {
		return nil, err
	}
	// The address must resolve against the shared PIN code dataset so booking
	// can route it without a second lookup path.
	pincode, err := h.geo.RequireActivePincode(r.Context(), f.pincode, geography.DefaultCountry)
	if err != nil {
		return nil, err
	}
	f.pincodeID = pincode.ID
	f.stateID = pincode.StateID
	f.cityID = pincode.CityID
	f.stateName = pincode.StateName
	f.cityName = pincode.CityName
	if f.cityName == "" {
		f.cityName = pincode.DistrictName
	}
	if f.cityName == "" {
		f.cityName = pincode.OfficeName
	}
	return f, nil
}

func (h *Handler) loadCustomer(r *http.Request, p *tenant.Principal) (*dbgen.GetCustomerByPublicIDRow, error) {
	customerID, err := httpx.PathPublicID(r, "customerId", publicid.PrefixCustomer, "Customer")
	if err != nil {
		return nil, err
	}
	row, err := h.svc.q.GetCustomerByPublicID(r.Context(), dbgen.GetCustomerByPublicIDParams{
		PublicID: customerID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.NotFound("Customer")
		}
		return nil, apierr.Internal(err)
	}
	if !p.IsCustomerInScope(row.ID) {
		return nil, apierr.NotFound("Customer")
	}
	return &row, nil
}

func addressViewFromRow(a dbgen.CustomerAddress) map[string]any {
	return map[string]any{
		"id": a.PublicID, "label": a.Label, "addressType": a.AddressType,
		"contactName": a.ContactName, "contactPhone": a.ContactPhone, "altPhone": a.AltPhone,
		"line1": a.Line1, "line2": a.Line2, "landmark": a.Landmark,
		"city": a.CityName, "state": a.StateName, "pincode": a.Pincode,
		"latitude": a.Latitude, "longitude": a.Longitude,
		"isDefault": a.IsDefault, "status": a.Status,
	}
}

func addressView(a dbgen.CustomerAddress) map[string]any { return addressViewFromRow(a) }
