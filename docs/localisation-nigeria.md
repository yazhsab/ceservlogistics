# Nigeria as the first market

Written 9 August 2026, when the target market changed from India to Nigeria.

The platform was built for India. This records what that actually meant in the
code and what has been changed — including the defects the move surfaced, which
were the more interesting half.

---

## 1. What turned out not to be a problem

Worth stating first, because the fear was larger than the reality.

**The tax engine is already country-agnostic.** `computeTax` lists applicable
rules and applies each one's basis points. The intra-state/inter-state split
that looks so Indian is expressed as a nullable `intra_state_only` column, and a
rule that leaves it NULL applies either way. **A Nigerian VAT rule is ordinary
configuration, not a code change** — insert a `tax_rules` row at 750 basis
points with `intra_state_only = NULL` and it works.

**Postcode format fits by luck.** Nigerian postal codes are six digits not
starting with zero (Lagos 100001, Abuja 900001), which is exactly what the
existing validation accepts. Whether they are *findable* was a real problem, and
a different one from whether they validate — §4.

**Phone validation was already generic** — 6–20 digits with an optional leading
`+`, so `+234` needs nothing.

**Currency was already per-organization.** Only the supported list was closed.

---

## 2. What changed

### Currency

`money.Supported()` accepted `INR` and `USD` only, so **an NGN booking would
have been refused by the ledger**. `NGN` is added. The supported list now has a
single source that both `Currency.Supported` and `SupportedCodes` read, with a
test pinning them together — previously the ledger's "supported" error message
was a hardcoded literal that could have disagreed with what the ledger accepted.

### Tax vocabulary

`tax_rules.tax_type` allowed only Indian names plus `CUSTOM`. A Nigerian rule
could have used `CUSTOM`, but a line reading **"CUSTOM 7.5%"** on a customer's
invoice is the kind of small dishonesty that erodes trust in a document.
Migration 0031 adds `VAT` and `WHT`.

### The organization's country

Organizations carried a currency and a timezone but not a country — India was
implied by being the only market. `organizations.country` is added (ISO-3166
alpha-2, existing rows backfilled to `IN`) and carried on the principal, because
it is what decides how a tax identifier is validated.

### Tax identifiers

`GSTIN` and `PAN` are Indian names for a general idea: the registration a
counterparty is invoiced under, and the one proving the company exists. A
Nigerian operator asked for a "GSTIN" will either leave it blank or enter their
TIN into a field validated against an Indian format and be refused for a reason
that makes no sense to them.

`validate.TaxRegistration` and `validate.BusinessRegistration` take the country:

| | India | Nigeria | Elsewhere |
|---|---|---|---|
| Tax registration | GSTIN, 15 chars | TIN, 8–14 digits (hyphen optional) | shape check only |
| Business registration | PAN, 10 chars | RC/BN number | shape check only |

**An unknown market is accepted, not guessed.** Refusing an identifier because
the platform has not learned its format yet would block a market for no safety
benefit.

The columns are generalised in place: `tax_registration_number` and
`business_registration_number` are added and backfilled, with `gst_number` and
`pan_number` kept and marked deprecated so no existing reader breaks.

### Geography

`NG` and its 36 states plus the FCT are seeded. Serviceability, zone mapping and
address validation all resolve through states, so a market with no states cannot
take a booking.

---

## 3. Notifications

### Nigeria is much easier than India here

India's DLT regime requires every SMS **template** to be pre-registered with
TRAI before it can be sent. Nigeria's NCC registers the **sender id** and not
the message. That removes the single largest integration cost.

### Termii

`internal/notification/termii.go` serves SMS and WhatsApp from one account.
Enabled by config; an unconfigured channel keeps its logging sender.

```
TERMII_API_KEY=
TERMII_SENDER_ID=          # the alphanumeric header registered with the NCC
TERMII_SMS_CHANNEL=dnd
TERMII_WHATSAPP_FROM=      # empty disables WhatsApp rather than half-enabling it
```

Three things in the adapter are worth knowing:

- **`dnd` is the default SMS route on purpose.** Nigerian subscribers can block
  promotional SMS, and a delivery notification is not marketing. The DND-exempt
  corporate route is what makes a transactional message arrive.
- **Termii answers HTTP 200 with a rejection in the body**, so the status code
  is only half the signal. `classifyTermii` is where the retryable/permanent
  judgement lives, and it defaults to *retryable* for anything unrecognised —
  an unknown provider error is more often a blip than a permanent refusal, and
  the attempt budget bounds the cost of guessing wrong. The provider's own words
  are recorded verbatim so a support engineer can add a new case.
- **Numbers are normalised.** Senders type `08031234567`, `8031234567`,
  `+2348031234567` and `2348031234567`; a provider handed the wrong form does
  not complain, it silently fails to deliver.

### A gap this closed

`notification_templates.provider_ref` has existed since migration 0026 and
`Message.ProviderRef` has existed in the adapter contract — documented as *"an
approved WhatsApp template name, an SMS sender id"*. **Nothing joined them.** The
value was never read, so no provider that needs one could have worked.

It is now snapshotted onto the notification at composition time rather than read
from the template at send time, for the same reason a shipment carries a price
snapshot: a template re-approved next month must not change what a message sent
today was addressed with.

### Channel mix

Per-message costs differ by roughly an order of magnitude, so the mix dominates
the bill far more than the provider choice does:

- **WhatsApp** for the moments that matter — out for delivery, delivered, NDR.
  Nigerian business communication runs on it and delivery beats SMS.
- **Email** for everything else. It is the cheapest channel by a wide margin.
- **SMS** as the fallback where there is no WhatsApp or email.

The preference model already expresses this: a recipient with no email falls
through, and the reason is recorded as `NO_ADDRESS` rather than vanishing.

---

## 4. Postcodes: what was actually wrong, and what fixed it

An earlier draft of this section left a choice open — reuse the postcode column
as an opaque area key, or add an area level to the geography model — and asked
for a decision. That was a false choice built on not having read the schema
carefully enough. **The area level already existed.**

### The diagnosis was wrong in a useful way

The problem was never the format. Nigerian postal codes are six digits with a
non-zero lead, which is exactly what validation already accepted, so nothing
ever *rejected* a Nigerian address. The problem was softer: postcodes are poorly
adopted in Nigerian practice — addresses are given by area, LGA and landmark —
and **every lookup the platform offered searched by code prefix and nothing
else.** A sender who did not already know their six digits had no way to find
them. Booking failed at the first field, for a reason no error message
explained.

So the fix is not a format change or a model change. It is making the postcode
*findable from what a sender knows*.

### Nothing structural was added

| Nigerian concept | Existing table |
|---|---|
| Local Government Area (774 of them) | `districts` — the level between state and city |
| Area / neighbourhood — "Ikeja GRA", "Wuse II" | `localities` — named places under a postcode |

Both tables were there from migration 0004 and had simply never been populated
or exposed for Nigeria. `pincodes.code` was never India-specific either: its
CHECK accepts alphanumerics up to 12 characters.

What was missing was three things, all now closed:

**1. No name search.** `SearchPincodes` filtered on code prefix, state, city and
remote flag. `GET /api/v1/geography/places` now matches a fragment against area
name, city, LGA, post office name and code prefix at once, deduplicating a
postcode matched several ways down to its strongest match.

**2. LGAs were invisible.** `districts` had no endpoint and was not a search
filter, so the single most-used component of a Nigerian address could not be
read over the API. `GET /api/v1/geography/districts?state=LA` now lists them,
and carries the market's own word for the level — `LGA` in Nigeria, `District`
in India — so a UI does not hardcode somebody else's vocabulary.

**3. The importer could not load area names.** It accepted district, city,
office and coordinates but not `locality`, so the one column that makes a
postcode findable was the one column a bulk load could not fill. It now does.

### The seeded data, and what it is not

Migration 0033 seeds the 20 Lagos LGAs, the 6 FCT area councils, 9 cities, the
9 NIPOST zone-anchor postcodes and 23 area names.

**This is a starter set, not the authoritative NIPOST table**, and the
distinction matters more than the row count. The administrative divisions are
official and complete for the states covered. The postcodes are the zone anchors
— the state-capital codes that are widely published and stable. Nothing finer is
seeded, deliberately: reference data that is wrong misroutes parcels, and
guessing at thousands of six-digit codes would have been worse than seeding
none.

The full dataset loads through the existing importer with no code change:

```
POST /api/v1/geography/imports/pincodes    (multipart CSV, max 25 MB)
```

Required columns `pincode`, `state`; optional `district` (the LGA), `city`,
`city_tier`, `locality`, `office_name`, `latitude`, `longitude`, `is_remote`.
Load `locality` — without it the dataset is only findable by people who already
know the code, which is the problem this section is about.

### Booking did not change

Worth being explicit, because it constrains the frontend: an address is still
submitted with a `pincode`. Search is a *lookup* that runs before the form is
submitted. The sender types an area, picks from the list, and the postcode goes
on the request as it always did. See `docs/contracts/release-1.md` §6.

## 5. Nigeria is now the default, not an option

An earlier draft of this document listed several India-shaped things as
"deliberately left". That was the wrong call: a default that has to be overridden
in every new tenant is a defect waiting for somebody to forget. **Nigeria is now
what the platform does unless told otherwise**, and the leftovers are closed:

| Was | Now |
|---|---|
| `geography.DefaultCountry = "IN"` | `"NG"` |
| Column defaults `'INR'` / `'Asia/Kolkata'` | `'NGN'` / `'Africa/Lagos'` / `'NG'` — migration 0032, 15 columns |
| Bootstrap and demo tenants seeded Indian | Nigerian: Lagos 100001, Abuja 900001, VAT 7.5% |
| "Indian PIN code" in validator docs | "postal code" — the user-facing message was already neutral |

The defaults were found by querying `information_schema.columns` for every
default containing `INR` or `Kolkata` rather than by grepping, because a default
lives in the catalogue and not in any file anybody would think to search.

### Two defects this surfaced

Neither was a localisation task. Both were bugs the market change exposed.

**`internal/network/handlers.go` hardcoded `CountryIso2: "IN"`** in two places
when resolving an operating unit's postcode. Creating an operating unit
therefore only ever worked for Indian postcodes — the country was never read
from the organization at all. It now resolves through `orgCountry(p)`.

**`internal/network/franchise.go` validated agreement currency against a literal
`{INR, USD}`**, so a franchise agreement could not be written in NGN. It now
defaults to the organization's currency and validates against
`money.SupportedCodes()` — the same list the ledger enforces, so the two cannot
drift apart again.

**`internal/platform/provision/provision.go` seeded the demo rate card as INR**
while the demo organization was NGN, and a rate card whose currency does not
match the tenant's is refused at booking. The demo tenant was unbootable end to
end. It now takes the organization's currency.

### What remains Indian, and correctly so

- `states.gst_state_code` — an Indian column, nullable, and null for Nigeria.
  Removing it would break the Indian market for no gain.
- `validate.GSTIN` / `validate.PAN` — retained for callers that genuinely mean
  the Indian identifiers, behind the country-aware wrappers in §2.
- Rounding is HALF_UP, which suits both GST and Nigerian VAT.

## 6. Evidence

A fresh database migrated and demo-provisioned from empty, then driven over HTTP:

```
organization  DEMO   currency NGN   timezone Africa/Lagos   country NG
tax rules     VAT75  "VAT 7.5%"
geography     100001 Lagos / Lagos NG      900001 Abuja / FCT NG

POST /api/v1/shipments   Lagos 100001 -> Abuja 900001, 1200 g
  awb        DMO260809000001
  currency   NGN
  freight    10000
  tax        889   [VAT75]
  total      12739
```

The arithmetic is worth checking by hand, because it is the whole point: freight
10000, fuel surcharge 18.5% = 1850, taxable 11850, VAT 7.5% of 11850 = 888.75
which rounds half-up to 889, total 12739.

Full suite green, including the booking test whose expectations moved from 18%
IGST to 7.5% VAT.

## 7. Before taking a Nigerian booking

Steps 1 and 2 are now the defaults rather than instructions — a new organization
is Nigerian unless overridden — but they are worth verifying on a tenant created
before migration 0032.

1. Confirm the organization has `country = 'NG'`, `currency = 'NGN'`,
   `timezone = 'Africa/Lagos'`.
2. Confirm a `tax_rules` row exists: `tax_type = 'VAT'`, `percentage_bp = 750`,
   `intra_state_only = NULL`.
3. Load postcodes — or area codes, per the §4 decision, which is still open —
   and map them to zones.
4. Set `TERMII_*` and confirm `GET /api/v1/notifications/channels` shows SMS and
   WhatsApp as configured.
5. Set `PUBLIC_TRACKING_URL` to the customer-facing page.
