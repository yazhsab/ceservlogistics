package geography

import (
	"net/http"
	"strings"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Finding a postcode from what a sender knows.
//
// Serviceability, routing, zones and pricing are keyed on postcode, and every
// other lookup in this package searches by code. That is backwards for a
// booking form in a market where postcodes are poorly adopted: a Nigerian
// sender knows "Ikeja GRA" or "Wuse II", not 100001, and had no way to get
// from one to the other. Booking failed at the first field.
//
// These two endpoints are the resolution step. They are deliberately read-only
// reference lookups and do not change the booking contract — an address is
// still submitted with a postcode. The frontend searches, the sender picks, the
// postcode goes on the request.

// searchMinLength is the shortest fragment accepted.
//
// Below three characters a trigram index cannot help — a trigram is three
// characters — and the query degrades to scanning the reference tables. Two is
// still allowed because Nigerian area names include short ones and the
// reference tables are small enough to scan; one is not, because a single
// letter matches most of the dataset and the result would be noise.
const searchMinLength = 2

// searchMaxLength bounds the pattern a caller can force into a LIKE.
const searchMaxLength = 64

type placeView struct {
	ID    string `json:"id"`
	Code  string `json:"code"`
	Label string `json:"label"`
	// MatchedOn names which field produced the hit, so a UI can show why a
	// result is in the list rather than leaving the sender to guess.
	MatchedOn string `json:"matchedOn"`
	Area      string `json:"area,omitempty"`
	City      string `json:"city,omitempty"`
	District  string `json:"district,omitempty"`
	State     string `json:"state"`
	StateCode string `json:"stateCode"`
	IsRemote  bool   `json:"isRemote"`
}

// searchPlaces resolves a free-text fragment to postcode candidates.
func (h *Handler) searchPlaces(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	term := strings.TrimSpace(httpx.Query(r, "q"))
	if len([]rune(term)) < searchMinLength {
		return apierr.Validation("A search term of at least two characters is required.", nil).
			WithDetail("field", "q")
	}
	if len(term) > searchMaxLength {
		return apierr.Validation("The search term is too long.", nil).
			WithDetail("field", "q")
	}
	limit, err := httpx.QueryInt(r, "limit", 20, 1, 50)
	if err != nil {
		return err
	}
	country := strings.ToUpper(httpx.QueryDefault(r, "country", orgCountryOr(p)))

	// Term drives the byte-ordered code range; Pattern drives the ILIKE
	// branches. They differ once the fragment contains a LIKE wildcard, which
	// is why the query takes both rather than escaping once and hoping.
	rows, err := h.svc.q.SearchPlaces(r.Context(), dbgen.SearchPlacesParams{
		Term: term, Pattern: escapeLike(term), Country: country, RowLimit: int32(limit),
	})
	if err != nil {
		return apierr.Internal(err)
	}

	out := make([]placeView, 0, len(rows))
	for _, row := range rows {
		v := placeView{
			ID: row.PublicID, Code: row.Code, MatchedOn: row.MatchedKind,
			State: row.StateName, StateCode: row.StateCode, IsRemote: row.IsRemote,
		}
		if row.MatchedKind == "AREA" {
			v.Area = row.MatchedName
		}
		if row.CityName != nil {
			v.City = *row.CityName
		}
		if row.DistrictName != nil {
			v.District = *row.DistrictName
		}
		v.Label = placeLabel(v, row.OfficeName)
		out = append(out, v)
	}
	return httpx.OK(w, map[string]any{
		"data": out,
		// The label a UI should put on the district field. A Nigerian operator
		// shown "District" instead of "LGA" is being asked a question in
		// somebody else's vocabulary.
		"districtLabel": DivisionLabel(country),
	})
}

// placeLabel builds the single line a picker shows.
//
// Ordered the way the address is written — area, LGA, city, state — rather than
// the order the columns happen to sit in. "Ikeja GRA, Ikeja, Lagos" is what a
// sender would put on a parcel; "Ikeja GRA, Lagos, Ikeja" is the same three
// facts in an order that reads as a mistake.
//
// Duplicates are dropped case-insensitively, because the levels genuinely
// coincide in places: Lagos is a city and a state, and Jalingo is an area, an
// LGA and a city. Repeating a name three times looks like a rendering bug.
func placeLabel(v placeView, office *string) string {
	parts := make([]string, 0, 4)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, existing := range parts {
			if strings.EqualFold(existing, s) {
				return
			}
		}
		parts = append(parts, s)
	}
	add(v.Area)
	// The post office name identifies the code when nothing more specific
	// matched, but it is a *different* place from the matched area — 900001's
	// office is Garki, so a search for Wuse rendered "Wuse, Garki, …" and named
	// two neighbourhoods as though one contained the other. It fills a gap; it
	// does not sit alongside.
	if v.Area == "" && office != nil {
		add(*office)
	}
	add(v.District)
	add(v.City)
	add(v.State)
	return strings.Join(parts, ", ")
}

type districtView struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	StateCode string `json:"stateCode"`
	StateName string `json:"stateName"`
}

// listDistricts lists a state's administrative divisions — Local Government
// Areas in Nigeria, districts in India.
func (h *Handler) listDistricts(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	state := strings.ToUpper(strings.TrimSpace(httpx.Query(r, "state")))
	if state == "" {
		return apierr.Validation("A state code is required.", nil).
			WithDetail("field", "state")
	}
	if len(state) > 16 {
		return apierr.Validation("The state code is too long.", nil).
			WithDetail("field", "state")
	}
	country := strings.ToUpper(httpx.QueryDefault(r, "country", orgCountryOr(p)))

	rows, err := h.svc.q.ListDistrictsByState(r.Context(), dbgen.ListDistrictsByStateParams{
		Country: country, StateCode: state,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	out := make([]districtView, 0, len(rows))
	for _, d := range rows {
		out = append(out, districtView{
			ID: d.PublicID, Code: d.Code, Name: d.Name,
			StateCode: d.StateCode, StateName: d.StateName,
		})
	}
	return httpx.OK(w, map[string]any{"data": out, "label": DivisionLabel(country)})
}

// DivisionLabel names the administrative level between state and city in the
// caller's market.
//
// The same idea has a different name in each: "LGA" is what every Nigerian
// address form says, and asking for a "district" instead reads as a question
// about something else.
func DivisionLabel(country string) string {
	switch strings.ToUpper(country) {
	case "NG":
		return "LGA"
	case "IN":
		return "District"
	default:
		return "District"
	}
}

// orgCountryOr falls back to the platform default when a principal predates the
// country column.
func orgCountryOr(p *tenant.Principal) string {
	if p != nil && p.OrganizationCountry != "" {
		return p.OrganizationCountry
	}
	return DefaultCountry
}

// escapeLike neutralises the LIKE wildcards so a sender typing a percent sign
// searches for a percent sign rather than matching every row.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
