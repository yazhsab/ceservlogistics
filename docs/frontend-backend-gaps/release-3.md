# Release 3 frontend/backend contract gaps

Date: 2026-08-08

The Release 3 contract and OpenAPI surface are present and were used for the
finance frontend. The following residual gaps prevent some requested finance
views from being fully server-driven.

## 1. Commission scheme administration

Rules carry `schemeCode`, but there is no commission scheme list, detail, or
mutation endpoint. The UI exposes scheme membership on rules and does not
fabricate editable scheme records.

Requested backend action: expose permission-scoped scheme list/detail and
lifecycle operations if schemes are intended to be administered in Release 3.

The simulator identifies the winning rule by `ruleCode` and `versionNo`, but
does not return stable rule/version IDs. The UI can state the exact winning
code and version but cannot provide the requested canonical detail deep link.

Requested backend action: include `ruleId` and `ruleVersionId` in commission
simulation results.

## 2. Register pagination metadata

Several cursor-filtered registers (`journals`, commission calculations, COD
obligations, settlements, and invoices) return only `data`. They do not return
`nextCursor`, `hasMore`, or a total. The frontend can request bounded pages but
cannot safely offer Next without guessing a database cursor.

Requested backend action: return a consistent cursor page envelope.

## 3. COD workflow queues and metrics

COD exposes obligation list/detail and a summary, plus mutations for transfers,
reconciliations, remittances, adjustments, and disputes. It has no list/detail
reads for those workflow records. Pending Handover, Branch Confirmation,
Reconciliation, Shortage/Excess, Dispute, and Ready for Remittance cannot be
implemented as auditable server-paginated queues.

The summary also lacks reconciled and disputed amount/count metrics requested
for the control centre.

Requested backend action: add permission-scoped list/detail operations and
aggregate counters with aging/current-holder filters.

## 4. Settlement detail auditability

`byCategory`, `approvals`, and `payments` are typed as opaque objects in
OpenAPI. Settlement detail also has no journal public IDs, audit event schema,
or settlement-adjustment endpoints even though the contract names adjustments
as the correction path after approval.

The settlement list exposes commission and incentive separately, but no
authoritative `grossEarningsMinor` field.

Requested backend action: publish typed category, approval, payment, audit,
ledger-reference, and adjustment read/write models, plus gross earnings if that
list column is required.

## 5. Billing runs and correction-note discovery

The Release 3 contract explicitly excludes a billing-run endpoint. Credit and
debit notes can be raised and issued but cannot be listed or retrieved. Those
requested registers therefore cannot be built without fake browser data.

Requested backend action: expose billing-run and credit/debit-note list/detail
operations in the release that owns those capabilities.

## 6. Invoice-line server pagination

Invoice detail returns every line in one response and exposes no line cursor.
The UI can paginate the rendered table, but it cannot avoid downloading the
full payload for a 4,000-shipment invoice.

Requested backend action: add a server-paginated invoice-line endpoint while
preserving the immutable snapshot response.

## 7. Invoice PDF lifecycle

The contract explicitly states that PDF rendering is not in Release 3 and
`documentObjectId` is always null. The UI truthfully shows PDF unavailable
rather than simulating a generation state.

Requested backend action: later expose queued/running/ready/failed state and an
authorized download operation.

## 8. JavaScript-safe minor-unit precision

OpenAPI describes money as JSON `integer`/`int64`. Values outside JavaScript's
safe-integer range may lose precision during JSON parsing. The frontend exact
formatter accepts integer strings or `bigint` and rejects unsafe numeric input,
but it cannot recover digits already rounded by `response.json()`.

Requested backend action: encode int64 minor amounts as decimal integer strings
or contract a JavaScript-safe maximum.
