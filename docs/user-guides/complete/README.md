# CESERVE complete product documentation

Edition: 18 September 2026. This pack supersedes the earlier booking-focused guides in the parent directory. It documents the implemented product across staff operations, configuration, finance, management, portals and integrations.

| Manual | Pages | Audience and scope |
| --- | --- | --- |
| [Complete Product Walkthrough](01-product-walkthrough.md) | 12 | Whole product map, roles, shipment journey, customers, booking, insurance, customs, labels, customer portal and franchise portal |
| [Configuration and Administration](02-configuration-administration.md) | 14 | Organization, users, roles, facilities, geography, routes, products, pricing, insurance, customer setup, notifications, credentials, webhooks and merchant onboarding |
| [Operations Finance and Reporting](03-operations-finance-reporting.md) | 19 | Pickups, scanning, bags, manifests, fleet, trips, receiving, delivery, NDR, RTO, POD, collections, COD, commission, ledger, settlements, invoices, reports and command centre |
| [Questions and Support](04-questions-support.md) | 7 | Common end-user questions, identifiers, state explanations, escalation details and role-based training exercises |
| [Live QA walkthrough update](05-live-qa-update-2026-09-19.md) | Markdown addendum | Deployed corrections for customer setup, customs, COD custody, credit-note handoff, scanning and returns |

Finished files are in `output/complete-product/docx` and `output/complete-product/pdf`. The complete distribution archive is `output/CESERVE_Complete_Product_Documentation_2026-09-18.zip`. It contains all four PDFs, all four editable Word documents and a start-here index.

## Evidence and limitations

The original manuals were checked against repository revision `7410087357b0cbd8982ab27c14385609002f7fc8`, routes, sidebar, page controls, OpenAPI, release contracts, shipment-commercial/ecommerce contracts and financial/notification adapters. The 19 September addendum reflects later deployed corrections. The [live QA release note](../../releases/live-product-qa-2026-09-19.md) records evidence and acceptance limits.

Screenshots use the current frontend with fictional mocked training data. No live records, payments, messages, permissions or configuration were changed to prepare these documents. This review verifies product behavior in the repository, not the current live tenant's commercial configuration or provider setup.

The manuals distinguish available user screens from backend or administrator-assisted workflows. Explicit boundaries include customer portal self-service and account linking, COD transfer/remittance queues, scheme administration, settlement correction/detail gaps, correction-note checker handoff, invoice PDF generation, report previews, notification provider readiness, stored but unenforced webhook filters and store-adapter availability. Current code takes precedence over stale release notes about automatic commission hooks or logging-only notification providers.

## Maintain and rebuild

Edit the four Markdown files in this directory. Keep explicit page breaks and update page references when content moves. Reuse `../assets/` for the three existing fictional training screenshots. Run `../build_complete_guides.py` with the bundled Python runtime, then render every DOCX with the Documents skill renderer and `--emit_pdf`. Inspect all final page images, confirm the contents references and archive contents, and publish only reviewed final files.

Do not publish renderer PNGs or scratch assembly scripts. The Markdown files are the maintainable source. Do not insert real passwords, one-time secrets, tokens, customer personal data or production test transactions into training content.
