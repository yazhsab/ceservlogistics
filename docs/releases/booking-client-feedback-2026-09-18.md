# Booking customer access and customs valuation

Implemented locally on 18 September 2026 and subsequently deployed with the 19 September QA corrections. The observations below preserve the original local verification. See [live QA and deployment results](live-product-qa-2026-09-19.md) for current status; the earlier repository lint findings are now resolved. No real customer coverage or tariff was changed by this booking fix.

## Client findings

The screenshot shows a postal-reference lookup failure, not an insurance calculation error. Nigeria is supported, but the selected country must contain an active configured record for the actual postal code. The previous error hid whether the missing code belonged to the sender or recipient. The client's exact sender code, recipient country/code and signed-in role are still required to identify the live configuration issue.

Customer creation already exists under Commercial → Customers, guarded by `customer.create`. Booking had no direct creation action, and a failed customer search was incorrectly displayed as no matching customers. Customs totals were only visible after a successful full shipment preview, so a postal lookup failure prevented users from seeing them.

## Routes components and APIs

- Updated `/shipments/new`: permission-aware Add customer action opens the shared `CreateCustomerDialog`, selects the returned customer and preserves the booking fields. Users without creation permission receive guidance. Existing `/customers` creation still opens the customer detail after success.
- Customer lookup now distinguishes a failed API request from an empty search and offers retry.
- Added `POST /api/v1/shipments/customs/preview`, requiring `shipment.create`. It accepts the existing CustomsDeclaration contract and returns server-calculated line totals, subtotal, value after discount and total before insurance. It needs no customer, postal record or route, and makes no persistent writes.
- Full preview and standalone customs valuation share the same backend calculation and bounded arithmetic. No financial calculation was introduced in the browser.
- Updated OpenAPI and regenerated frontend types. Existing customer creation and lookup, country lookup, shipment preview and booking APIs continue to be consumed.
- Postal errors include the address role, code and country; the UI marks the affected field and provides a button to focus it.

## Interaction accessibility and responsive review

Valid customs edits trigger a calculation after 400 ms. Invalid or changed inputs immediately hide outdated totals. Requests are cancellable; failures provide a retry action. Unit value and customs freight remain explicit commercial-invoice inputs. Insurance remains pending until full shipment preview; only a current successful shipment preview supplies the final customs invoice total.

The existing accessible dialog, labels, field error associations and buttons are reused. The shipment keyboard shortcut is suppressed while customer creation is open, preventing accidental underlying booking submission. The postal correction action moves keyboard focus to the exact field. Goods totals use read-only outputs and loading/error states. Scanner behavior is outside this change.

Browser checks cover 1366×768 desktop, 768×1024 tablet and 390×844 mobile. Visual inspection checked customer selection, valuation layout, aligned money values, the postal recovery notice and overflow. Screenshots use fictional test data:

- [Desktop customs valuation](assets/booking-customs-2026-09-18-desktop-1366.png)
- [Mobile customs valuation](assets/booking-customs-2026-09-18-mobile-390.png)

## Verification

- Shipment package unit tests pass, including shared totals and overflow rejection.
- OpenAPI and contract tests pass.
- Eleven commercial/customs integration scenarios pass against isolated local PostgreSQL and Redis. Coverage includes authentication, denied permissions, valuation despite postal failure, structured postal details, discount/total bounds, no shipment writes and existing insurance consent/pricing/billing behavior.
- All 27 frontend unit tests pass.
- Browser regressions pass for inline creation and preserved data, permission denial guidance, lookup failure, automatic valuation, stale totals, validation recovery and the existing Customers creation flow. Existing booking/insurance and customs/billing scenarios also pass. Initial test locator mistakes for required-field labels were corrected and the affected tests rerun successfully.
- TypeScript and the production build pass. Changed-file ESLint passes.
- Repository-wide ESLint remains blocked by two unchanged findings: a hook-dependency warning in `web/src/components/operations.tsx:243` and a promise-handler error in `web/src/pages/CollectionPages.tsx:127`.

## Remaining configuration and release work

No database migration is needed. Deploy API and frontend together for the new valuation endpoint. Local code changes do not provision postal coverage or grant account permissions. Review the client's actual postal records, service areas, routes and role before changing production configuration. Any insurance rule must still be explicitly configured using the business-approved rate.

The current customs contract provides valuation and payer instructions; it does not infer item prices, file customs declarations, assess duties or issue insurance certificates. Backend contracts are complete for the changes above. The remaining live issue is unverified configuration, pending the client identifiers.
