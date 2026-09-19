# CESERVE end user guides

**Superseded by the [complete product documentation](complete/README.md).** Use that 52-page pack for product-wide walkthroughs, configuration, operations, finance, reporting, portals, notifications and integrations. The four shorter guides below are retained as the earlier edition.

Edition: 18 September 2026. Prepared for staff onboarding, configuration and daily work.

| Guide | Audience | Contents | Pages |
| --- | --- | --- | --- |
| [Getting Started and Booking](01-getting-started.md) | Counter staff and customer service | Sign-in, customer creation and lookup, addresses, packages, payment, insurance acceptance, customs, booking, tracking and portal boundaries | 6 |
| [Administrator Configuration](02-administrator-configuration.md) | Administrators and commercial managers | Users, roles, facilities, coverage, products, rate versions, configurable insurance, accounts and readiness checks | 7 |
| [Daily Operations](03-daily-operations.md) | Pickup, warehouse, dispatch, delivery and finance teams | Shift checks, pickups, scanning, bags, manifests, trips, receiving, delivery, NDR, RTO, collections, reports and handover | 9 |
| [Common Questions](04-common-questions.md) | End users and support | Customer lookup, logins, identifiers, address formats, insurance, customs, failed actions, financial records and support details | 5 |

Finished PDF copies are in `output/pdf/` and editable Word copies in `output/docx/` at the repository root. The distribution archive is `output/CESERVE_End_User_Guides_2026-09-18.zip`. Share the PDFs with readers and keep Word copies for approved local customization.

## Evidence and scope

The guides were checked against repository revision `7410087357b0cbd8982ab27c14385609002f7fc8`, including `web/src/layout/AppShell.tsx`, the relevant screen implementations in `web/src/pages/`, `docs/openapi.yaml`, `docs/contracts/shipment-commercial.md`, and the release contracts. Current screen and implementation behavior takes precedence over older contract limitations where they differ.

Screenshots show the real frontend with mocked fictional training data. No live customer, shipment, payment or configuration was created to prepare the documentation. Screenshots do not establish the organization's current prices or insurer terms. Instructions state available-screen limitations, including customer login linking, domestic saved-address input, invoice PDF rendering and customs filing.

## Maintain the guides

Edit the adjacent Markdown source, preserving the explicit page breaks and updating contents-page references if topics move. The three PNG assets are training screenshots. They are embedded in Word with descriptive alternative text.

Use `build_guides.py` with the available bundled Python runtime and `python-docx`. Then render each generated DOCX using the Documents skill's `render_docx.py --emit_pdf`, inspect every page, verify PDF page counts and references, and copy the final PDFs into `output/pdf/`. Rebuild the distribution ZIP only from the reviewed final files and its end-user start-here note.

Whenever product behavior changes, recheck menu names, required fields, permissions, state transitions, pricing publication behavior and the stated limitations. Never insert live passwords, access tokens or customer personal information into the guides.
