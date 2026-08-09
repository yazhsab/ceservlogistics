# Frontend production readiness

Date: 2026-08-09  
Scope: Admin, Operations, Franchise, Customer, Finance, and Public Tracking  
Target market: Nigeria (`en-NG`, `NGN`, `Africa/Lagos`)

## Decision

**FAIL — NO-GO for an unqualified commercial launch of the complete advertised
Courier OS.**

The implemented, contract-backed frontend surfaces meet a defensible production
engineering bar: the strict build, lint, unit/component tests, 119 Playwright
tests, exact responsive matrix, public-page accessibility checks, dependency
audit, and production bundle all pass. The remaining launch blockers are not
cosmetic frontend defects:

1. the full journey suite currently uses deterministic OpenAPI-shaped frontend
   fixtures rather than a deployed backend and production-like database;
2. contracted APIs are still absent for several promised customer self-service
   and finance workflows; and
3. production hosting controls and the refresh-token cookie boundary cannot be
   proved or completed in this frontend repository.

A limited launch of the surfaces explicitly supported by the current OpenAPI
contract is technically defensible after the deployment/security risks below
are accepted by the accountable release owner. This document does not claim
perfect coverage.

## Build

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Production build | `npm run build` completed with Vite 7.3.6; 1,948 modules transformed and no build warnings or errors. |
| **PASS** | Static quality gates | `npm run typecheck` and `npm run lint` completed successfully; ESLint emitted zero warnings. |
| **PASS** | Unit/component suite | `npm test -- --run`: 6 files, 24/24 tests passed. Tests cover API authentication/URL safety/error fallbacks, shared UI accessibility, safe filenames, operational primitives, integration secrets, and finance formatting. |
| **PASS** | Production dependencies | `npm audit --omit=dev --json`: 0 known vulnerabilities across 74 production dependencies. |
| **PASS** | No suppressed failures | Static search found no `test.skip`, `describe.skip`, `test.fixme`, `.only(`, `@ts-ignore`, `@ts-nocheck`, or ESLint-disable directives under `web/src` or `web/tests`. |
| **RISK ACCEPTED** | Generated artifacts | `dist` is 1.4 MiB with hashed assets. Deployment-specific compression, cache headers, rollback, and asset-integrity policy must be verified by the hosting pipeline. |

## Type Safety

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Strict TypeScript | `tsc --noEmit` passed. The project has strict mode enabled and no unsafe application/test `any` usage was found; natural-language occurrences of “any” were the only search matches. |
| **PASS** | API contract boundary | `web/src/api/schema.d.ts` is generated from `docs/openapi.yaml`; Release 1–4 request/response aliases are derived from generated schemas and operations. |
| **PASS** | Financial precision guard | Shared financial formatting accepts backend minor units, rejects unsafe numeric integers, and supports exact string/`bigint` presentation. Browser calculations are not treated as authoritative. |
| **RISK ACCEPTED** | JSON `int64` transport | OpenAPI still describes money as JSON numeric `integer`/`int64`. Digits beyond JavaScript's safe-integer range can be lost before the formatter sees them. The backend should return decimal integer strings or contract a safe maximum. |

## Accessibility

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Automated public/auth audit | Lighthouse on `/track`: Accessibility 100. Lighthouse on `/login`: Accessibility 100 after correcting label association and contrast defects. |
| **PASS** | Form state association | Shared `Field` context now passes `aria-describedby`, `aria-invalid`, and `aria-required` to nested inputs, selects, and textareas, including controls wrapped by icons/layout elements. A component test proves error association. |
| **PASS** | Keyboard and focus | Global visible focus treatment is present. Hidden mobile navigation is removed from the tab order. The seven-viewport Playwright matrix verifies reachable dialogs and retained focus trapping. Scanner workflows verify Enter submission and focus restoration. |
| **PASS** | Semantics | Loading regions use polite live status, errors use alert semantics, permission denial has an explicit heading, tables have named keyboard-scroll regions, and status/finance meaning uses text rather than colour alone. |
| **RISK ACCEPTED** | Manual assistive-technology coverage | Lighthouse, semantic snapshots, component tests, and keyboard automation passed, but no production screen-reader session with VoiceOver/NVDA or switch-control user study was performed. |

## Responsive

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Required viewport matrix | Playwright passed layout, public/customer usability, and dialog/focus checks at 1366×768, 1440×900, 1920×1080, 768×1024, 1024×768, 390×844, and 430×932: 21/21 responsive checks. |
| **PASS** | Internal containment | Admin, scanner, hub, settlement, command-centre, report, and integration pages were swept at every target size. Wide data remains in labelled local scroll regions and the window cannot scroll horizontally. |
| **PASS** | Customer shipment history | Phone layouts now use scannable shipment cards with labelled recipient, destination, service, booked time, status, and a 44-pixel tracking action. Tablet/desktop retain a contained, narrower table. |
| **PASS** | Dialog reachability | Shared dialogs use dynamic viewport height, contained scrolling, wrapping footers, and bounds assertions at all seven sizes. |
| **RISK ACCEPTED** | Device/browser rendering | Viewports emulate dimensions and touch in Chromium. Physical iOS Safari/Android Chrome keyboard, safe-area, font-scaling, and browser-toolbar behaviour remain device-lab checks. |

## Performance

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Route splitting | `App.tsx` contains 91 lazy route modules. The production build emits 55 JavaScript chunks rather than one all-feature payload. |
| **PASS** | Bundle result | Initial application JavaScript is 539.07 kB raw / 165.08 kB gzip. The largest lazy feature chunk is Shipment Pages at 112.52 kB raw / 28.47 kB gzip. CSS is 43.83 kB raw / 8.49 kB gzip. |
| **PASS** | Public tracking Lighthouse | Performance 97, Accessibility 100, Best Practices 100, SEO 100; FCP 1.9 s, LCP 2.3 s, TBT 0 ms, CLS 0, Speed Index 1.9 s on the local production preview. |
| **PASS** | Sign-in Lighthouse | Performance 99, Accessibility 100, Best Practices 100, SEO 63; FCP 1.7 s, LCP 1.9 s, TBT 20 ms, CLS 0. Login SEO is intentionally reduced because `robots.txt` disallows authentication/internal routes. |
| **PASS** | Query policy | TanStack Query does not retry 400/401/403/404/409/422 responses, makes only one bounded retry for rate-limit/server/network failures, and never retries mutations by default. |
| **RISK ACCEPTED** | Initial unused JavaScript | Lighthouse estimates about 94 KiB of unused JavaScript on public tracking. The 165 KiB gzip entry contains the shared React/router/query/UI runtime; feature routes are already lazy, but a future budget should prevent regression. |
| **RISK ACCEPTED** | Source maps | Production source maps are disabled to avoid publishing source without a private telemetry upload pipeline. This protects source exposure but reduces production stack-trace quality until such a pipeline exists. |

## Security

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Token exposure reduction | Access tokens are memory-only. Token pairs are no longer broadcast between tabs; cross-tab communication carries logout only. Reload restoration rotates the session refresh token before an authenticated request. |
| **PASS** | URL and bearer-token safety | The API client rejects non-root-relative, protocol-relative, and backslash-containing request paths, preventing bearer tokens from being sent to an attacker-controlled URL. Unit coverage verifies refusal. |
| **PASS** | Authorization-aware UI | Route and action tests verify denied customer, finance, credential, and webhook access. Sensitive actions/navigation are hidden when permissions are absent. Backend authorization remains authoritative. |
| **PASS** | HTML/XSS review | Static review found no `dangerouslySetInnerHTML`, direct `innerHTML` assignment, `eval`, `new Function`, or `document.write` in application code. Notification preview is rendered as text. |
| **PASS** | External URL handling | Webhook links require a validated HTTPS URL and use `rel="noreferrer"`. Navigation coordinates are typed backend numeric fields. API helpers refuse arbitrary absolute download URLs. |
| **PASS** | File/POD handling | POD previews allow only JPEG, PNG, WebP, and GIF rather than active SVG. Authorized blobs use object URLs that are revoked. Download filenames are normalized, stripped of path/control characters, and extension-allowlisted where applicable. |
| **PASS** | Secret visibility | API keys and webhook secrets are held in component state, shown only in explicit one-time dialogs, and are never stored in browser storage. The UI states that the value cannot be shown again. |
| **PASS** | Browser storage review | Local storage contains only scanner/hub preferences, sidebar state, and a random operations device ID. No PII, financial records, access tokens, API keys, or webhook secrets are persisted there. |
| **RISK ACCEPTED** | Refresh-token storage | The refresh token remains in `sessionStorage` because the current backend contract returns/rotates it in JSON. An `HttpOnly`, `Secure`, `SameSite` cookie requires a backend authentication contract change and should be paired with CSRF controls. |
| **RISK ACCEPTED** | Deployment headers | CSP, HSTS, `frame-ancestors`, `Referrer-Policy`, permissions policy, cache policy for authenticated HTML, and TLS configuration cannot be proved from Vite preview. These are mandatory deployment acceptance checks. |

## Operational UX

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Scanner workflow | Playwright verifies keyboard scan submission, accepted/rejected/duplicate feedback, recent history, automatic clear, and focus restoration. In-app visual review confirmed visible facility, mode, counters, shortcut labels, and active scan focus. |
| **PASS** | Keyboard shortcuts | Scanner mode supports `Alt+1`–`Alt+8`; hub terminal mode supports `Alt+1`–`Alt+6` and Escape. These controls have visible text labels and are tested. |
| **PASS** | Custody transitions | Pickup, bag close, manifest close/receipt, line-haul departure/arrival, hub reconciliation, destination, delivery/POD, NDR, and RTO journeys pass. Immutable/confirmation states are explicit. |
| **PASS** | Error friction | Slow/empty/retry states pass. HTTP 401, 403, 404, 409, 422, 429, and 500 tests retain domain messages and request IDs; finance conflicts are not collapsed into generic copy. |
| **RISK ACCEPTED** | Physical operations validation | Barcode hardware suffix timing, audible feedback in noisy hubs, intermittent mobile connectivity, glove use, and real high-volume scan throughput were not tested with production devices/operators. |

## Customer UX

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Supported customer portal | Account-bound dashboard, customer-safe shipment history/tracking, invoice/payment state, profile, NGN formatting, identity denial, mobile navigation, and responsive shipment cards pass fixture-backed E2E. Internal branch/hub operations are not exposed. |
| **PASS** | Public tracking | Public tracking supports valid, missing, rate-limited, and temporary-error states with customer-safe language and no internal movement data. Lighthouse scores 97/100/100/100. |
| **FAIL** | Promised self-service scope | The backend contract has no customer-bound booking, price-estimate, pickup, saved-address maintenance, or POD retrieval APIs. These Release 4 features cannot be made production-ready without inventing unsafe organization-wide access. The UI truthfully directs users to support instead. |
| **RISK ACCEPTED** | Authenticated portal Lighthouse | The customer dashboard was visually and responsively reviewed and exercised by E2E, but Lighthouse was run on public tracking and authentication, not an authenticated production customer session. |

## Finance UX

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Financial clarity | Commission, ledger, COD, settlement, and invoice E2E passes on desktop and tablet. Tests prove balanced journal totals, immutable posted state, backend running balances, custody evidence, explicit payer/payee language, confirmed settlement approval/payment, invoice snapshots, and permission denial. |
| **PASS** | Safe actions | Approval, payment, reversal/replay/correction-like actions are permission and state gated and use explicit confirmation. Posted/paid immutable records do not expose generic edit actions. |
| **PASS** | Nigerian formatting | Shared money components use `en-NG`/`NGN`, preserve backend minor units, show explicit direction/sign labels, and do not rely on red/green alone. |
| **FAIL** | Complete contracted finance workspace | Backend gaps still prevent commission-scheme administration and canonical simulation version links; auditable COD workflow queues; typed settlement audit/ledger/adjustment detail; billing-run and credit/debit-note registers; server-paginated invoice lines; and invoice PDF lifecycle. The frontend does not fabricate them. |
| **RISK ACCEPTED** | Finance visual fixture depth | The in-app finance list was visually reviewed, and populated finance states pass Playwright fixtures. The separate visual mock did not provide a populated settlement, so a production-like high-value/long-reference finance visual session remains required. |

## Browser Testing

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Chromium production preview | The full 119-test suite ran against the local production preview in Google Chrome/Chromium projects. Visual browser review covered public tracking, customer dashboard, finance list/empty state, and scanner console. |
| **PASS** | Visual consistency sampling | Reviewed internal navigation, breadcrumbs, table/empty states, scanner hierarchy/focus, customer portal hierarchy, and public tracking. The audit preserved the established restrained design system rather than redesigning functioning pages. |
| **PASS** | Public/customer automated audit | Lighthouse production-preview audits completed for `/track` and `/login` with the scores recorded above. |
| **RISK ACCEPTED** | Cross-browser coverage | Firefox, WebKit/Safari, Edge policy mode, browser zoom above 100%, and OS high-contrast modes were not part of this run. They remain pre-launch smoke tests. |

## E2E

| Status | Item | Evidence |
|---|---|---|
| **PASS** | Complete frontend fixture suite | `npm run test:e2e`: 119/119 passed in 1.5 minutes, with no skipped critical tests. Desktop and tablet run the complete Release 1–4 workflows; mobile projects cover login/profile, pickup, delivery, public tracking, and customer portal. |
| **PASS** | Required 26 journeys represented | Admin login, network creation, serviceability, pricing, customer creation, booking, pickup, origin scan, bag, manifest, line haul, hub reconciliation/receipt, destination receipt, delivery/POD, public tracking, NDR, RTO, commission, COD, settlement, invoice, customer portal, franchise portal, reports, and integrations are covered. |
| **PASS** | Permission and failure coverage | Route/action denial, invalid sessions, one-time secret handling, HTTPS webhook validation, financial permission denial, slow loading, empty results, retry, and HTTP 401/403/404/409/422/429/500 are automated. |
| **FAIL** | Full-stack production proof | The repository has no backend-provided seeded customer/franchise/finance/integration fixture set. Playwright intercepts requests with stable OpenAPI-shaped fixtures, so it proves frontend behaviour but not migrations, authorization implementation, database state machines, provider delivery, object storage, or deployed network behaviour. A staging run is mandatory before launch. |

## Known Issues

| Status | Issue | Release impact / required action |
|---|---|---|
| **FAIL** | Customer self-service APIs are incomplete | Add caller-bound booking, quote, pickup, address, and POD APIs or reduce the marketed portal scope. |
| **FAIL** | Finance APIs do not expose all requested auditable workflows | Close the Release 3 gaps for scheme lifecycle/version links, COD queues, settlement audit/adjustments, billing/note registers, invoice-line pagination, and PDF lifecycle. |
| **FAIL** | No backend-seeded staging E2E | Provide test-environment setup data and run all 26 journeys against the real deployed API, database, object storage, and authorization. |
| **RISK ACCEPTED** | Refresh token is script-readable | Move refresh rotation to an `HttpOnly`, `Secure`, `SameSite` cookie contract with CSRF protection. Until then, enforce a strict CSP and minimize third-party script. |
| **RISK ACCEPTED** | Hosting security controls are unverified | Validate TLS, HSTS, CSP, framing, referrer/permissions policy, authenticated cache headers, secret redaction, and request-ID observability in staging. |
| **RISK ACCEPTED** | No cross-browser/device-lab sign-off | Run current Chrome, Edge, Firefox, iOS Safari, and Android Chrome smoke tests plus a physical scanner/mobile workflow session. |
| **RISK ACCEPTED** | No manual screen-reader sign-off | Run VoiceOver and NVDA on sign-in, booking, scanner, settlement approval, customer tracking, and credential secret disclosure. |
| **RISK ACCEPTED** | Notification/integration runtime limitations | Real notification providers are not registered; `pod.captured` and `cod.collected` webhook events are not emitted; stored webhook filters are not applied. UI copy does not claim otherwise. |
| **RISK ACCEPTED** | Country-neutral transport fields remain incomplete | OpenAPI retains legacy `pincode`, `gstNumber`, and `panNumber` names/patterns. UI presents Nigerian postal code, TIN, and CAC/RC terminology without changing the wire contract. |
| **RISK ACCEPTED** | Public initial bundle has removable code | Lighthouse reports about 94 KiB potential unused JavaScript. Current performance is strong, but CI should add route-specific gzip and Lighthouse budgets. |

## Required exit criteria for commercial GO

1. Resolve or formally remove the two **FAIL** product-scope groups for
   customer self-service and finance.
2. Run the 26 critical journeys against production-like backend fixtures in
   staging, with no skipped workflows.
3. Obtain deployment-security-header, cross-browser/device, and manual
   assistive-technology sign-off.
4. Record accountable acceptance for the remaining `RISK ACCEPTED` items.
