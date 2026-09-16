# Insurance, customs and international booking

Updated 2026-09-16 after the user explicitly authorized backend implementation.

The originally reported contract gaps are implemented in OpenAPI, migration 0038, the shared booking/pricing services, and the operations booking/detail UI. See [the commercial booking contract](../contracts/shipment-commercial.md) and [the implementation report](../releases/booking-insurance-reference-2026-09-16.md).

| Requested behavior | Implemented behavior |
| --- | --- |
| Customer goods value and total | Server-calculated goods subtotal, discount, declared value, freight, insurance, other charges and customs invoice total |
| Configurable insurance and customer acceptance | Existing versioned INSURANCE surcharge rules determine the premium; exact-premium acceptance, stale-quote rejection, immutable applied rules, actor and time |
| Customs declaration | Goods lines, quantity/unit/value/origin/HS code, export reason, invoice reference, terms and statement |
| Bill transportation to | Shipper, receiver or third-party account; selected account owns credit and customer invoicing |
| Bill duty and tax to | Independent shipper, receiver or third-party instructions |
| Recipient country and postal code | Country selection, country-aware lookup and validation, GB alphanumeric postcode support, saved and shipment address persistence |

Deployment remains required. New insured API calls require the preview/acceptance handshake; existing versioned insurance surcharge rules determine new quotes, with no fallback percentage. Configure 1% explicitly if required; missing applicable rules return `INSURANCE_RATE_NOT_CONFIGURED`. Historical shipment amounts and records are unchanged.

Separate capabilities not requested or implemented here: insurer policy/certificate issuance, electronic customs filing, duty assessment/collection, FX conversion, document uploads, countries without postal identifiers, and provisioning international country/state/postcode/service coverage. The existing bulk importer remains domestic; availability of GB in the country list alone does not make a route serviceable.
