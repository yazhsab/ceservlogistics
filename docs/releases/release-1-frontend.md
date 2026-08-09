# Release 1 frontend completion report

Date: 8 August 2026  
Scope: Foundation, administration, network/geography/routing, products/pricing/customers, shipment booking, and shipment management.

Release 1 is implemented to the current backend contract boundary in `web/`. OpenAPI-generated TypeScript is the DTO source of truth. Functionality not supported by the backend is recorded separately in `docs/frontend-backend-gaps/release-1.md` and was not simulated in production code.

## 1. Routes and pages added

- Authentication: `/login`, `/forgot-password`, `/reset-password`, `/change-password`, `/profile`.
- Shipments: `/shipments`, `/shipments/new`, `/shipments/:shipmentId`.
- Administration: `/admin/users`, `/admin/users/:userId`, `/admin/roles`, `/admin/organization`.
- Network: `/network/hubs`, `/network/branches`, `/network/units/:unitId`, `/network/franchises`.
- Geography: `/geography/pincodes`, `/geography/zones`, `/geography/imports`.
- Routing: `/routing/tester`, `/routing/rules`, `/routing/routes`, `/routing/overrides`.
- Products and pricing: `/products`, `/products/:serviceId`, `/pricing/simulator`, `/pricing/rate-cards`, `/pricing/rate-cards/:cardId`, `/pricing/versions/:versionId`.
- Customers: `/customers`, `/customers/:customerId` with summary, addresses, shipments, and credit views where the API permits them.
- Unknown paths render a designed not-found state; guarded routes render explicit permission-denied states.

## 2. Reusable frontend foundation

- Responsive `AppShell`, permission-aware sidebar, top bar, breadcrumb trail, user menu, command menu, skip link, and mobile off-canvas navigation.
- Semantic design tokens for background, surfaces, content, borders, brand, success, warning, danger, and information states.
- Buttons, icon buttons, fields, search/combobox inputs, selects, text areas, date pickers, checkboxes, radio groups, switches, badges, status badges, notices, tabs, panels, dialogs, sheets/drawers, confirmation flows, tooltips, toasts, tables/data-grid wrapper, filter bars, pagination, loading, skeleton, empty, error, permission, and application error-boundary states.
- Shared formatters for currency, weights, dates, labels, error messages, and guarded collection normalization.
- TanStack Query keys, request cancellation, cache invalidation, mutation state, and bounded import-status polling.

## 3. APIs consumed

- Auth and sessions: login, rotating refresh, logout, current user, password recovery/change, and session list.
- Identity and organization: user list/create/update/status, role grant/revoke, custom-role create and permission replacement, permission catalogue, organization read/update, and audit events.
- Network and geography: operating-unit list/create/detail/update/deactivation and hierarchy, hubs, branches, franchise list/create, PIN-code search, zone list/create, and asynchronous imports/error downloads.
- Routing: serviceability check/debug, service-area list/create, closure list/create, route list/create, and route-override create.
- Commercial: courier-service list/create/detail/update; rate-card list/create; version list/create/detail/activation; zone-rate upsert; surcharge create; authoritative pricing quotes; customer list/create/detail/update; address list/create; credit read/update; and customer shipment queries.
- Shipments: list, detail, create, cancel, events, and labels.

`web/src/api/schema.d.ts` is generated from `docs/openapi.yaml` by `npm run api:generate` and checked for drift in CI.

## 4. Design and workflow decisions

- The visual system is restrained and dense: typography, alignment, table rhythm, and state clarity carry the hierarchy instead of oversized cards or decoration.
- Shipment booking keeps server-resolved route and price visible on desktop and presents a sticky action bar at narrower internal widths.
- A fresh quote is obtained immediately before booking. The browser never treats calculated finance values as authoritative.
- A booking form owns an idempotency key for its lifetime. Timed-out/in-progress retries reuse it, rapid double submission is blocked, and a completed booking cannot be accidentally submitted again.
- Serviceability uses an operational facility sequence with explicit route source, SLA, promise, restrictions, and optional decision trace.
- Shipment details preserve the booking snapshot and separate overview, route, packages, charge explanation, tracking events, customer/address, and audit concerns.
- JSON label data is rendered as a browser-printable Code 128/QR label; ZPL is downloaded when supplied by the backend.

## 5. Keyboard and scanner-adjacent behavior

- `Cmd/Ctrl+K` opens command navigation; `Cmd/Ctrl+B` opens booking.
- `Cmd/Ctrl+Enter` previews or submits booking depending on the verified state.
- Dialogs use managed focus, Escape handling, and focus return through Radix primitives.
- Native form order, visible focus rings, Enter submission, and large named actions support counter operators without requiring a mouse.
- Release 1 contains no physical scanning workflow; scanner-specific input behavior belongs to Release 2.

## 6. Responsive behavior

- Internal operations are desktop-first and tested at 1366×768 and 1024×768.
- Visual review also covered 1440×900, 1920×1080, 768×1024, and a 390×844 compact safety check.
- The sidebar becomes off-canvas, table containers preserve dense data with controlled overflow/column prioritization, filters stack, form columns collapse, and booking actions become sticky at narrower widths.

## 7. Accessibility

- Semantic landmarks, labelled primary navigation and breadcrumbs, real buttons/links, heading hierarchy, form labels, associated error text, status text in addition to color, and an accessible skip link.
- Visible focus treatment, keyboard-operable menus/dialogs/tabs, no icon-only ambiguous primary actions, and responsive touch targets.
- Loading, empty, error, retry, permission-denied, and mutation-progress states are explicit rather than blank.

## 8. Tests and results

- `npm run api:generate`: passed; generated schema is deterministic.
- `npm run typecheck`: passed under TypeScript strict mode.
- `npm run lint`: passed with zero warnings.
- `npm test`: 7 unit/component tests passed.
- `npm run build`: passed; the application is route-split. The largest shared application chunk is approximately 484 kB (151 kB gzip), and the shipment chunk is approximately 113 kB (29 kB gzip).
- `npm run test:e2e`: 15 Playwright tests passed across desktop, tablet, and a focused mobile-auth project. Covered login/navigation, route and action permission guards, mobile login/profile overflow, serviceability, pricing explanation, customer creation, booking and double-submit prevention, shipment search/detail, label, and cancellation.
- `npm audit --omit=dev`: zero vulnerabilities. The tooling-only `js-yaml` advisory was removed with a patch-level package override; full `npm audit` also reports zero vulnerabilities.
- CI now executes OpenAPI drift detection, typecheck, lint, unit tests, production build, and Playwright workflows.

Mock responses exist only under `web/tests/`; production pages never ship fake product data.

## 9. Visual QA

Browser inspection was performed on the login shell, shipment list, booking form, resolved serviceability route, and shipment detail. Checks included hierarchy, density, alignment, table overflow, responsive navigation, sticky actions, long operational identifiers, status visibility, and focusable semantics at all target widths listed above. No blocking clipping, overlap, or unreadable state was found.

## 10. Backend contract gaps

The detailed and actionable gap register is `docs/frontend-backend-gaps/release-1.md`. Major blockers are Regions, franchise detail/lifecycle after creation, typed generic collection rows, complete routing mutation coverage, discounts and weight slabs, customer contacts/address maintenance, and PDF label support.

## 11. Unresolved UX and release risks

- Permission guards depend on semantic permission codes from the backend catalog; explicit endpoint-to-permission metadata would make this more robust.
- Refresh tokens are kept in `sessionStorage` because the current token contract returns them to JavaScript. The access token is memory-only, cross-tab refresh is coordinated with `BroadcastChannel` and Web Locks, and token loss clears all local auth state. An HttpOnly secure-cookie refresh design would further reduce XSS exposure if the backend adopts it.
- Generic `OffsetPage.data` responses reduce compile-time protection for several tables; runtime guards prevent crashes but cannot replace typed response schemas.
- Large shared UI/application chunk size is acceptable for Release 1 but should be monitored as Release 2 scanner and operations modules arrive.
- Visual QA used contract-faithful local API fixtures because a seeded backend environment was not provided. The Playwright suite verifies browser/API integration behavior, but a staging smoke run against deployed backend data remains a production-gate requirement.
