# Release 2 frontend completion report

Date: 2026-08-08

Status: implemented against `docs/contracts/release-2.md` and the generated
types from `docs/openapi.yaml`, with the residual contract limitations listed
in `docs/frontend-backend-gaps/release-2.md`.

## 1. Routes and pages added

- Pickup: `/operations/pickups`, `/operations/pickups/runs`,
  `/operations/pickups/my-stops`, `/operations/pickups/:pickupId`.
- Scanner: `/operations/scanner` with Receive, Arrival, Departure, Sort, Hold,
  Release, Damage, and Exception modes.
- Bags: `/operations/bags`, `/operations/bags/receive`,
  `/operations/bags/:bagId`.
- Manifests: `/operations/manifests`, `/operations/manifests/receive`,
  `/operations/manifests/:manifestId`.
- Line haul: `/operations/trips`, `/operations/trips/:tripId`,
  `/operations/fleet`.
- Hub and destination: `/operations/hub`,
  `/operations/hub/reconciliations/:reconciliationId`,
  `/operations/destination`.
- Delivery: `/operations/delivery`, `/operations/delivery/my-run`,
  `/operations/delivery/runs/:runId`,
  `/operations/delivery/runs/:runId/stops/:awb`.
- Exceptions and returns: `/operations/ndr`, `/operations/ndr/:caseId`,
  `/operations/rto`, `/operations/rto/:caseId`.
- POD: `/operations/pod/new`, `/operations/pod/:podId`.
- Anonymous tracking: `/track`, `/track/:awb` outside the authenticated shell.

All internal routes have route-level permission guards; action controls also
use the relevant contract permission.

## 2. Reusable components added or extended

- `OperationalMetricStrip` with count-aware responsive density.
- Cursor pagination for operational collections.
- `ScannerInput` with autofocus, Enter submission, normalization, and an
  imperative refocus handle.
- `ScanFeedback` with accepted, duplicate, and rejected semantics announced as
  text and color.
- `JourneyTimeline`, `EntityLink`, and a genuinely fixed mobile action bar with
  safe-area padding.
- Extended status tones for pickup, custody, delivery, NDR, and RTO states.
- Shared optional-number validation that treats blank optional fields as
  absent instead of incorrectly coercing them to zero.
- Mobile-safe dialog structure with independently scrollable content and an
  unobstructed footer.

## 3. APIs consumed

- Pickup requests, agent stops, assignment responses, visits, completion, and
  pickup runs.
- Scan write/history endpoints with device id and device event id headers.
- Bags, contents, close, dispatch, receipt, seal verification, opening, and
  reconciliation creation.
- Manifests, contents, close, dispatch, receipt, and reconciliation creation.
- Carriers, vehicles, drivers, trips, assignments, manifests, departure,
  arrival, close, and cancel.
- Hub summary, inbound, custody, exceptions, and reconciliation scan/complete.
- Delivery queue, runs, stops, assignment, dispatch, OTP issue, attempts, and
  NDR reasons.
- NDR queues, detail, and action; RTO list, initiate, dispatch, receive, and
  complete.
- Multipart POD creation, secure metadata, and lazy authorized artifact
  retrieval.
- Anonymous public tracking with 404, 429, and temporary-error handling.

`web/src/api/schema.d.ts` was regenerated from the current OpenAPI document.
API schemas are imported through `web/src/api/client.ts`; no duplicate Release
2 DTO model was introduced.

## 4. Design and UX decisions

- Preserved the Release 1 application shell, semantic tokens, typography, and
  component language.
- Kept operational screens dense, table-led, and action-oriented, with no fake
  dashboards or decorative charts.
- Used server-provided `allowedTransitions` for mutable custody controls and
  made bag/manifest closure visibly immutable.
- Made COD due, NDR next action, RTO reverse movement, reconciliation
  exceptions, and delivery completion visually dominant.
- Kept public tracking separate, mobile-first, privacy-safe, and free of
  internal facility terminology.
- Used bounded 30-second polling only on live operational summaries/history;
  other lists refresh through mutations and query invalidation.

## 5. Keyboard and scanner behavior

- Scanner input focuses on entry, submits on Enter, records an immediate API
  outcome, clears successful input, and restores focus after every response.
- Alt+1 through Alt+8 switches scanner mode without a mouse.
- Accepted, duplicate, and rejected session counters remain visible.
- Optional audible tones use the browser audio API; visual status remains
  authoritative when audio is unavailable.
- Bag, manifest, and reconciliation scanners use the same focus-preserving
  interaction.
- Scanner writes carry stable device identity and per-request device event IDs.

## 6. Responsive behavior

- Internal operations remain desktop-first with tablet overflow handling.
- Pickup and delivery field workflows use large touch targets and a fixed
  bottom action bar on small screens.
- Dialog content scrolls independently while its footer remains reachable.
- Long AWB breadcrumb values truncate instead of widening the document.
- Public tracking reflows to a single-column customer journey on mobile.

## 7. Accessibility work

- Semantic headings, labeled inputs, table labels, live scanner status, and
  descriptive non-color status text.
- Visible focus rings and scanner focus restoration.
- Native buttons, links, tables, form fields, and dialog focus management.
- Permission-denied and read-only views are explicit rather than silently
  blank.
- Mobile touch actions meet the existing minimum control height and are not
  obscured by dialog overlays or page content.

## 8. Tests and results

- Vitest: scanner autofocus/Enter normalization, scan status announcements,
  shared state components, and formatting utilities.
- Playwright Release 2: pickup assignment, pickup completion, accepted and
  rejected scans, keyboard mode switching, bag create/close, manifest
  create/close/receive, reconciliation scan/complete, delivery run creation,
  successful delivery, NDR action, RTO initiation, POD submit/view, and public
  tracking/not-found.
- The complete Playwright suite also preserves every Release 1 workflow across
  desktop, tablet, and mobile projects.
- Final gate results: OpenAPI generation passed; strict TypeScript passed;
  ESLint passed with zero warnings; Vitest passed 9/9 tests; the production
  Vite build passed; and Playwright passed 32/32 tests across desktop, tablet,
  and exact 390x844 mobile projects.

## 9. Visual QA performed

Browser inspection was completed at:

- 1366×768: scanner console, accepted scan feedback, refocus, history density.
- 1440×900: hub counters, tabs, inbound manifests and trips.
- 1920×1080: delivery dispatcher, run table, agent workload.
- 390×844: delivery stop, COD emphasis, contact/navigation, fixed actions.
- 430×932: public tracking hero, search, milestone timeline, summary reflow.

No horizontal document overflow or browser console errors remained. Visual QA
directly led to fixes for blank optional numeric submissions, reconciliation
refocus, dialog footer hit testing, long breadcrumb overflow, field action
stickiness, and operational metric distribution.

## 10. Backend contract gaps

See `docs/frontend-backend-gaps/release-2.md`. The largest visible limitations
are the missing destination sub-queues, missing scanner route/destination
fields, and absence of a bulk pickup assignment operation.

## 11. Unresolved UX risks

- The contract describes durable offline queuing/replay for field writes. This
  web release sends the required idempotency/device headers but does not yet
  persist a sensitive offline mutation queue across reloads. Field deployments
  that must operate without connectivity need a reviewed encrypted storage and
  replay design.
- Hub, scanner, pickup assignment, and delivery planning still fall back to raw
  facility/user IDs where a role-scoped searchable selector is not available
  in the Release 2 contract.
- RTO age counts are page-local until the backend exposes aging filters or
  aggregates.
- Route optimisation, live vehicle tracking, notifications, and financial
  settlement are explicitly outside Release 2.
