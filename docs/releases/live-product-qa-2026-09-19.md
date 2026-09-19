# Product QA and deployed fixes — 19 September 2026

The Chrome review covered 27 product areas and found 21 groups of defects. Corrections are deployed on Hostinger as `20260919-7410087-qa4`. Sixteen isolated test accounts were created and signed in interactively. A prepaid shipment completed booking through delivery and public tracking; a second completed COD delivery/custody; a third completed failed delivery, NDR and return to sender. These are synthetic transactions in `QA_20260919`, not real parcels or money transfers.

This is an initial product-wide QA pass, not complete acceptance of every feature. The coverage matrix distinguishes completed workflows, screen checks, automated checks and remaining work. There are 168 original screenshots, corresponding DOM snapshots and an offline gallery under `output/live-product-testing-2026-09-19`.

## Changes and API boundaries

| Area | Final behavior |
| --- | --- |
| Authentication and portals | Login and restored sessions return the same portal subject. Customers land in `/portal/customer`; eligible franchise users land in `/portal/franchise`. Customer navigation cannot retain a staff return path. Query caches and saved facility context clear at account transitions. Organization display name survives refresh. |
| Users and roles | Tenant-scoped batch queries supply actual role assignments. `/admin/roles` loads full role details before displaying the permission matrix and retains separate drafts for each role. No authority grants changed. |
| Customers and booking | Reusable `CreateCustomerDialog` supports customer creation from the register and booking sender section. Concrete validation replaces generic errors. Recipient country/postcode, transportation payer, duty/tax payer and customs goods are captured. |
| Customs and insurance | `POST /api/v1/shipments/customs/preview` supplies goods and invoice totals before route preview. Values and premiums remain server-authoritative. The synthetic tariff used 1.25%, producing NGN 25 insurance on NGN 2,000 declared value. No fixed 1% fallback was added. |
| Operational requests | CORS permits existing device-event and operating-unit headers for configured origins. Scanner request errors persist and focus returns; users without scan actions see history and read-only guidance. Single-unit users select their assigned facility. |
| Trips | Multi-unit operators select the actual arrival facility. The existing arrival request carries `X-Operating-Unit`; a second live trip verified the correction. |
| Finance responses | Explicit OpenAPI-aligned projections supply public IDs, lower-camel-case fields, values and date-only strings for commission, COD, settlement, billing and ledger. Public source links replace internal source IDs. Settlement prepaid collections have a subtotal and source tab. |
| COD delivery | Reusable `CODCollectionForm` exposes the existing collection endpoint to `cod.collect` users on completed stops. It uses the server-confirmed amount and requires method/reference. Network retries reuse the exact body and device-event key. A completed stop remains completed when reopened. |
| Credit-note handoff | New `GET /api/v1/invoices/{invoiceId}/credit-notes` provides tenant-scoped public-cursor pagination. Invoice detail shows notes for a separate checker to review and issue. The existing issue endpoint still denies the maker. |
| Settlement and returns | Zero settlement says “No payment due” and offers no payment action. Long metrics wrap; the final RTO leg says “Sender”; NDR reasons are readable and RTO actions respect existing permission. |
| Application delivery | HTML requires cache revalidation; versioned assets retain long-lived caching. Technical release/API wording was replaced with concrete feature guidance. |

No new application route or database migration is required. Existing pages, tables, dialogs, semantic feedback and focus styles are reused. New components are `CreateCustomerDialog` and `CODCollectionForm`; shared operational components and the invoice correction dialog were extended. OpenAPI, generated TypeScript, SQLC output where applicable and release contracts were updated together. Calculations, authorization and state transitions remain in backend services.

## Verification

- **51 frontend unit/component tests** passed across nine files. TypeScript strict checks, full ESLint and production build passed.
- **220 top-level integration test functions** passed in the complete local run. The configuration-skipped rate-limit test passed separately with throttling enabled: **221 distinct passing functions** total. Go internal package and contract tests passed.
- Contract checks cover finance fields, dates, pagination, invoice-note cursor ownership, maker/checker restrictions and cross-tenant access. Frontend checks cover COD retry identity, completed stops, note handoff, role matrix, routing, cache/facility isolation, scanner errors/focus and zero settlement.
- Chrome UI tests used the user's authorized session. No CLI-driven Playwright browser was used. Fixtures for parcels two and three were prepared through the API; delivery, NDR/RTO and custody outcomes were performed in Chrome and labeled accordingly.
- QA4 passed 60/60 API readiness and 60/60 web HTTP probes during rollout. All seven CESERVE containers are healthy. See [deployment evidence](deployment-2026-09-19-qa.md).
- Final main JS bundle: 542.82 kB, 166.15 kB gzip. Module pages are lazy loaded; a full performance benchmark remains pending.

## Live evidence

| Workflow | Result | Screenshot references |
| --- | --- | --- |
| Customers/addresses | Three customers created; default pickup address persisted and postcode resolved Lagos | 012–017, 084–085, 157 |
| Insurance/customs | Configured 1.25% premium saved; customs valuation returned NGN 2,200 independently of shipment quote | 100–102, 125–126 |
| Prepaid journey | `QAT260919000001` booked, picked up, scanned, bagged, manifested, transported, reconciled, delivered and publicly tracked | 101–117 |
| Access/facility fixes | Portal landing, actual assignments, role matrix, scanner and account-specific facility verified | 120–124, 129, 148–151 |
| COD journey | `QAT260919000002`: under-collection rejected; NGN 1,000 delivered; one custody record; repeat rejected; ledger and operational holdings both NGN 1,000 | 152–153, 162–165, 167–168 |
| NDR/RTO | `QAT260919000003`: failed delivery, NDR instruction, authorized RTO, three reverse facility legs and sender handover | 154–156, 166 |
| Invoice/correction | NGN 75 invoice, NGN 50 synthetic receipt, maker self-issue refusal, separate checker retrieved and issued NGN 25 credit note | 139–144, 147, 159–160 |
| Settlement | Zero-balance draft submitted and independently approved; no payment offered | 145–146, 161 |
| Reports | Shipment-volume export completed: three rows, 975 bytes; download receipt unverified | 158 |

Chronological PNGs preserve failures and retests. Screenshots establish only their visible state. Local captures 086–099 are distinguished from deployed evidence. Capture 144 shows the journal register; detail is 147. Capture 164 is desktop: its requested mobile viewport did not apply.

## Keyboard, accessibility and responsive review

Booking keyboard use, scanner Enter/Tab behavior and post-request focus were checked. Added controls use labels, semantic errors, status notices and existing accessible dialogs. No scanner hardware, printer output or screen-reader audio test was performed.

Desktop visual review used 1497 × 717. Live public tracking/login and the local scanner were inspected at 390 × 844; the scanner had no document overflow. Later COD and tablet viewport overrides failed. Full mobile/tablet acceptance and WCAG 2.2 AA certification are not claimed.

## Remaining work and handover

- The client supplied Nigeria but not both exact postcodes. The isolated `100001 → 900001` lane works; production coverage and approved pricing need confirmation for the actual lane.
- POD upload needs the browser extension's file permission. A configured test store/merchant mapping is needed for external e-commerce testing. No outbound customer messages or webhooks were sent.
- Continue forward intermediate hub sorting, COD transfer/reconciliation/remittance, nonzero settlement, commission accrual, ledger reversal, populated portal scope checks and download verification.
- Raw facility/party-ID inputs, incomplete COD registers, invoice PDF and customer self-service booking/address editing remain product gaps. See [the gap register](../frontend-backend-gaps/live-qa-2026-09-19.md).
- All sixteen QA users remain active. Follow `ACCOUNT_LIFECYCLE.md` in the evidence package after acceptance; preserve audit history. Private access details are excluded from shared reports.
- VPS disk is 97% used (7.5 GB available). Plan capacity maintenance before another image build. No unrelated services or images were removed.

The [19 September user-guide update](../user-guides/complete/05-live-qa-update-2026-09-19.md) supersedes earlier descriptions of these fixed flows in the four full-product manuals.
