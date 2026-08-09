# AGENTS.md — Courier Operating System Frontend / UI / UX Constitution

## 1. Role

You are the Principal Product Designer, Design Systems Lead, UX Architect, and Staff Frontend Engineer responsible for **all web UI/UX** in this repository.

You are implementing the frontend for a production-grade, multi-tenant Courier Operating System.

The product supports:

- Head Office
- Operations
- Hubs
- Branches
- Franchises
- Pickup staff
- Delivery staff
- Finance users
- Customer support
- Retail customers
- Business customers
- External partner administration

This is a professional enterprise logistics product.

The quality target is a polished commercial product suitable for national-scale courier operations.

---

# 2. Separation of Responsibilities

Claude Code owns:

- backend
- database
- business rules
- financial logic
- authorization truth
- state-machine truth
- OpenAPI
- backend validation

Codex owns:

- frontend architecture
- interaction design
- visual design
- design system
- responsive behavior
- accessibility
- frontend state
- API consumption
- frontend validation for user experience
- operational workflow optimization
- Playwright/frontend testing

Do NOT duplicate backend business logic.

Do NOT invent backend behavior.

Do NOT make browser-calculated financial values authoritative.

---

# 3. Authoritative Backend Contract

Before implementing each frontend module, inspect:

```text
docs/openapi.yaml
```

and the relevant:

```text
docs/contracts/
```

files.

Generate typed API definitions from OpenAPI where practical.

Do not manually maintain duplicate DTO definitions when generated types are available.

If a required backend capability is missing:

1. do not invent a fake frontend-only API;
2. do not implement backend business logic in the browser;
3. record the gap under:

```text
docs/frontend-backend-gaps/
```

4. continue all frontend work that can be completed safely.

---

# 4. Frontend Stack

Use:

- React
- TypeScript
- Vite
- Tailwind CSS
- shadcn/ui primitives
- TanStack Query
- TanStack Table
- React Hook Form
- Zod
- React Router or existing approved router
- Lucide icons
- Recharts only when a chart improves comprehension
- Playwright
- Vitest or repository-standard unit test framework

Use TypeScript strict mode.

Avoid unnecessary frontend dependencies.

---

# 5. Product Experience Target

The UI should feel:

- professional
- enterprise-grade
- trustworthy
- operationally efficient
- visually restrained
- modern without being trendy
- dense where operations require density
- simple where customers require simplicity
- consistent
- fast

Aim for excellent B2B SaaS quality.

Do NOT produce generic AI-generated dashboard aesthetics.

Avoid:

- excessive gradients
- decorative glassmorphism
- giant KPI cards
- huge empty spacing
- excessive shadows
- excessive border radius
- random color usage
- oversized hero sections in internal tools
- distracting animation
- icon-only ambiguity
- excessive modal usage
- nested cards inside cards inside cards

Visual polish should come from:

- typography
- hierarchy
- spacing
- alignment
- density
- state clarity
- interaction quality
- consistency

---

# 6. Primary UX Principle

Courier operators may perform hundreds or thousands of repetitive operations daily.

Optimize for:

- fewer clicks
- fewer keystrokes
- barcode scanner compatibility
- keyboard operation
- fast focus movement
- clear success/error response
- table density
- quick filtering
- understandable state
- minimal navigation depth

For repetitive operational workflows, operational speed outranks decorative design.

---

# 7. Design System

Build and reuse a proper design system.

Required foundations:

- typography
- spacing
- colors
- semantic tokens
- borders
- radius
- elevation
- motion
- breakpoints
- icon sizing
- focus treatment
- form layout
- table density
- status semantics

Use semantic CSS/design tokens such as:

```text
--background
--foreground
--surface
--surface-muted
--border
--primary
--primary-foreground
--success
--warning
--danger
--info
--muted
```

Do not scatter arbitrary hard-coded colors.

Status colors must have consistent meaning throughout the product.

---

# 8. Core Reusable Components

Prefer building/reusing stable components such as:

- AppShell
- Sidebar
- TopBar
- Breadcrumb
- PageHeader
- SectionHeader
- CommandMenu
- Button
- IconButton
- Input
- SearchInput
- Textarea
- Select
- Combobox
- Checkbox
- Radio
- Switch
- DatePicker
- DateRangePicker
- CurrencyDisplay
- WeightDisplay
- AddressDisplay
- StatusBadge
- PermissionGuard
- EmptyState
- ErrorState
- LoadingState
- Skeleton
- Toast
- Dialog
- Drawer
- Sheet
- Tabs
- Tooltip
- Popover
- FilterBar
- ActiveFilters
- DataTable
- Pagination
- BulkActionBar
- ConfirmAction
- Timeline
- ActivityFeed
- KeyValueList
- SummaryPanel
- FinancialSummary
- ScannerInput
- ScanFeedback
- EntityLink

Do not create one-off variants of existing primitives without a real need.

---

# 9. Navigation

Primary internal navigation may group areas such as:

```text
Operations
  Shipments
  Pickups
  Scanning
  Bags
  Manifests
  Trips
  Hub Operations
  Delivery
  NDR
  RTO

Network
  Regions
  Hubs
  Branches
  Franchises
  Geography
  Serviceability

Commercial
  Customers
  Services
  Rate Cards
  Pricing

Finance
  Commission
  COD
  Ledger
  Settlement
  Billing

Reports
  Operations
  SLA
  Finance
  Franchise
  Exceptions

Administration
  Users
  Roles
  Integrations
  Notifications
  System Health
```

Navigation must be permission-aware.

Do not show unusable navigation options to unauthorized users.

---

# 10. Responsive Strategy

Internal operations UI:
- desktop-first
- tablet-capable

Customer portal:
- fully responsive
- mobile-friendly

Delivery/pickup field workflows:
- mobile-first where applicable

Primary test widths:

Desktop:
- 1366x768
- 1440x900
- 1920x1080

Tablet:
- 768x1024
- 1024x768

Mobile:
- 390x844
- 430x932

Do not design only for wide 1920 screens.

---

# 11. Accessibility

Target WCAG 2.2 AA as far as practicable.

Requirements:

- keyboard navigation
- visible focus
- proper labels
- proper heading hierarchy
- accessible dialog focus handling
- ARIA when native semantics are insufficient
- sufficient contrast
- no color-only status
- error text associated with inputs
- touch target sizing appropriate for mobile
- no keyboard traps
- accessible tables where practical

Do not trade away accessibility for visual polish.

---

# 12. Loading, Empty, Error, and Permission States

Every asynchronous page must explicitly support:

- loading
- empty
- error
- permission denied
- partial failure where relevant
- retry

Do not leave blank content areas while waiting.

Use skeletons selectively.

Do not overuse spinners.

Error messages should be actionable.

Do not replace domain-specific errors with generic "Something went wrong" when the API provides a meaningful code/message.

---

# 13. Forms

Use:

- React Hook Form
- Zod
- backend contract constraints

Frontend validation improves UX but does not replace backend validation.

Forms should:

- preserve typed data where reasonable
- explain validation errors
- avoid excessive required fields
- group logically
- use progressive disclosure where appropriate
- prevent accidental duplicate submission
- clearly show save/progress state

For operational forms, optimize speed.

For finance forms, optimize safety and clarity.

---

# 14. Tables and Data Density

Enterprise courier software is table-heavy.

Use server-side:

- pagination
- filtering
- sorting
- search

Do not load massive datasets into browser memory.

Tables should support:

- clear primary identifier
- meaningful status
- dense but readable rows
- sticky headers where useful
- responsive overflow
- keyboard navigation where helpful
- column alignment
- empty/error states
- bulk actions when justified

Avoid hiding critical data behind repeated tooltips.

Use virtualization only when it genuinely improves performance.

---

# 15. Search

Search is a core operational capability.

Where backend supports it, provide fast lookup for:

- AWB
- customer
- pincode
- branch
- hub
- franchise
- manifest
- bag
- settlement
- invoice

Search fields used in fast operational workflows should support keyboard-first behavior.

---

# 16. Barcode / Scanner UX

Barcode scanners often act as keyboard input.

Scanner workflows must usually behave:

```text
focus scan input
→ scan
→ submit automatically
→ API response
→ clear success/error feedback
→ append recent scan
→ restore focus
```

Mouse must not be required for repetitive scanning.

Scanner screens should provide:

- operation mode
- current facility
- target context
- large focused input
- recent scans
- accepted/rejected count
- clear error reason
- optional audible feedback
- keyboard shortcuts

Do not open a modal for every scan.

---

# 17. Operational State Presentation

Courier workflows have many states.

Create consistent state semantics.

Examples:

- booked
- pickup pending
- picked up
- in transit
- at hub
- destination received
- OFD
- delivered
- NDR
- RTO
- cancelled
- damaged
- lost

Use:

- badge
- text label
- icon when helpful

Do not rely solely on color.

Expose next action prominently on workflow screens.

---

# 18. Shipment Booking UX

Shipment booking is a flagship flow.

Optimize for branch/franchise counter operators.

Sections may include:

- Sender
- Recipient
- Service
- Package
- Payment/COD
- Pricing
- Review

Desktop should keep charge summary visible where useful.

Features:

- customer lookup
- saved address
- pincode/serviceability validation
- weight/dimension entry
- volumetric/chargeable weight display
- service options
- SLA
- price breakdown
- COD
- declared value
- reference
- instructions

Before submit show:

- route summary
- selected service
- SLA
- complete price breakdown
- payment/COD details

After success prominently show:

- AWB
- barcode/QR if provided
- Print Label
- View Shipment
- New Booking

Prevent accidental duplicate booking.

Respect backend idempotency.

---

# 19. Routing / Serviceability UX

Serviceability tester should clearly display:

- serviceable yes/no
- origin branch
- origin hub
- transit path
- destination hub
- destination branch
- SLA
- remote area
- restrictions

Show route as a clean operational step flow.

Do not use decorative diagrams that obscure information.

---

# 20. Pricing UX

Pricing must be transparent.

Pricing screens include:

- rate cards
- versions
- weight slabs
- surcharge rules
- discounts
- simulator

Simulator should show:

- actual weight
- volumetric weight
- chargeable weight
- base charge
- weight charge
- fuel
- remote-area
- COD
- insurance
- handling
- discount
- tax
- total

Do not show unexplained final totals.

Use spreadsheet-like slab editing only where it materially improves editing.

---

# 21. Pickup UX

Dispatcher should see:

- unassigned
- scheduled
- late
- assigned
- failed
- completed

Support bulk assignment.

Field/mobile pickup UI should emphasize:

- next pickup
- contact
- address
- package count
- instructions
- arrived
- completed
- failed/reschedule

Minimize typing.

---

# 22. Bagging UX

Bagging screens should support:

- create bag
- scan shipments
- show destination
- show seal
- show piece count
- show weight
- rejected scan reason
- close bag
- print label
- receive
- reconcile

Closing a bag is important and should require a clear summary/confirmation.

After closure, UI should visually communicate immutability.

---

# 23. Manifest UX

Manifest builder should support:

- origin
- destination
- trip
- bags
- loose shipments if supported
- piece counts
- weight
- state

Scanner-assisted bag addition should be fast.

State changes must be visually clear.

---

# 24. Hub Operations UX

Hub Operations is an operational console, not a CRUD interface.

Primary workspaces:

- Expected Inbound
- Receiving
- Reconciliation
- Sorting
- Outbound
- Exceptions

Important counters:

- expected
- received
- pending
- missing
- excess
- damaged
- misrouted
- outbound pending

Reconciliation should allow rapid exception handling.

---

# 25. Delivery UX

Dispatcher UI:

- runs
- unassigned
- agents
- OFD
- delivery progress

Field/mobile delivery UI:

- run summary
- stop list
- shipment detail
- navigation
- contact
- OTP
- COD
- POD
- failure/NDR

Use large mobile touch targets.

Keep primary action sticky when useful.

Avoid long typing-heavy forms.

---

# 26. NDR UX

NDR Workbench should group:

- new
- contact required
- reattempt
- rescheduled
- escalated
- RTO candidate

NDR detail should show timeline:

- attempt
- reason
- customer contact
- evidence
- next action
- scheduled retry

Make the next required action obvious.

---

# 27. RTO UX

RTO should visually look like a reverse courier journey.

Example:

```text
RTO Initiated
→ Destination Branch
→ Destination Hub
→ Transit
→ Origin Hub
→ Origin Branch
→ Returned to Sender
```

Support aging filters and exceptions.

Do not reduce RTO UX to one status badge.

---

# 28. POD UX

POD viewer may display:

- recipient
- delivery timestamp
- OTP verification
- signature
- photos
- approximate location where authorized

Use lazy loading for images.

Respect permissions.

Provide print/download only if backend supports it.

---

# 29. Public Tracking UX

Public tracking must be:

- white-label ready
- mobile-first
- fast
- customer-friendly

Input:
- AWB

Display:
- current status
- expected delivery where available
- customer-safe timeline
- major facility/city milestones
- final delivery result

Do not expose internal operational terminology unnecessarily.

Handle:

- invalid AWB
- not found
- rate limited
- temporary error

professionally.

---

# 30. Finance UX Principles

Finance screens must always answer:

- What happened?
- Why?
- Which shipment/reference?
- Which rule?
- Who owes whom?
- How much?
- What state is it in?
- Is it immutable?
- What correction workflow is available?

Never make finance visually ambiguous.

Use currency consistently.

Do not make red/green the only indication of debit/credit meaning.

---

# 31. Commission UX

Screens may include:

- schemes
- rules
- versions
- simulator
- entries

Rule builder should clearly show:

- type
- recipient
- method
- scope
- effective period
- priority

Simulation should show the selected rule and calculation.

Never calculate authoritative commission independently in frontend.

---

# 32. Ledger UX

Ledger screens:

- chart of accounts
- journal transactions
- journal detail
- account statement
- trial balance

Journal detail must clearly show:

- account
- debit
- credit
- reference
- description

Show:

- Total Debit
- Total Credit

Posted entries should be visibly immutable.

Do not show "Edit" for posted journals.

Use explicit reversal/adjustment workflows if authorized.

---

# 33. COD UX

COD Control Center metrics may include:

- expected
- agent held
- branch held
- franchise outstanding
- reconciled
- remitted
- disputed

COD detail should show custody timeline.

Highlight:

- aging
- shortage
- excess
- disputes
- missing handover

Make current holder/state obvious.

---

# 34. Settlement UX

Settlement is a flagship franchise finance screen.

List may show:

- period
- franchise
- gross earnings
- commission
- COD liability
- adjustments
- net
- status

Detail:

- Summary
- Commission
- COD
- Charges
- Adjustments
- Ledger
- Approval
- Payments
- Audit

The summary must clearly communicate:

```text
Head Office pays Franchise
```

or:

```text
Franchise pays Head Office
```

Do not rely on a negative number alone.

High-risk actions require explicit confirmation.

Paid/closed settlement should be read-only except approved corrective workflows.

---

# 35. Billing UX

Billing screens:

- invoices
- billing runs
- credit notes
- debit notes

Invoice detail:

- customer
- number
- period
- shipment count
- subtotal
- tax
- total
- payment state

Large invoice lines must paginate.

Show PDF generation state if asynchronous.

---

# 36. Customer Portal UX

Customer portal should be simpler than internal operations.

Main areas:

- Dashboard
- Book Shipment
- Price Estimate
- Pickup
- Track
- Shipment History
- Addresses
- Invoices
- POD
- Profile

Customer terminology should be understandable.

Do not expose internal branch/hub controls or finance internals.

Mobile responsiveness is important.

---

# 37. Franchise Portal UX

The franchise portal is a commercial flagship interface.

Dashboard may include:

- bookings today
- pickup pending
- inbound
- outbound
- delivery pending
- COD outstanding
- commission MTD
- settlement due
- exceptions

Quick actions:

- Book Shipment
- Scan
- Create Bag
- Create Manifest
- Receive
- Assign Delivery

Finance area:

- COD
- Commission
- Ledger
- Settlement

Role-aware navigation is mandatory.

---

# 38. Command Center UX

Head-office command center should prioritize actionability.

KPIs:

- bookings
- pickup pending
- in transit
- hub backlog
- OFD
- delivered
- NDR
- RTO
- SLA breach

Filters:

- Today
- Yesterday
- 7 Days
- Custom
- Region
- Hub
- Branch
- Service

Use charts only when they improve operational decisions.

Prioritize actionable queues:

- late pickups
- hub backlog
- SLA breaches
- NDR aging
- COD exceptions

---

# 39. Reporting UX

Report center groups may include:

- Operations
- SLA
- Franchise
- Finance
- COD
- Commission
- Settlement
- Exceptions

Report workflow:

- select report
- configure filters
- run
- preview
- export

Large exports should show:

- queued
- running
- ready
- failed

Do not freeze browser while a report is generated.

---

# 40. Integration UX

API credential screen:

- name
- scopes
- created
- last used
- expiration
- status

If secret is shown only once, clearly state this.

Webhook management:

- endpoint
- events
- secret status
- enabled/disabled

Delivery logs:

- timestamp
- event
- HTTP status
- attempt
- latency
- error

Replay only when authorized.

Never expose stored secrets unnecessarily.

---

# 41. Permission UX

Authorization belongs to backend.

Frontend should still be permission-aware.

Hide or disable controls according to product UX conventions.

For sensitive operations, hiding is usually preferable when user has no relevant capability.

Do not fetch unauthorized sensitive data merely because the UI hides it.

Test route-level and action-level permission behavior.

---

# 42. API Handling

Use TanStack Query consistently.

Implement:

- query keys
- cache invalidation
- retries appropriate to operation
- stale-time appropriate to data
- mutation status
- cancellation where useful

Do not retry unsafe state-changing operations blindly unless backend idempotency semantics support it.

Do not aggressively poll every screen.

Use bounded polling or realtime support only where it provides real operational value.

---

# 43. Frontend Performance

Use:

- route lazy loading
- server-side pagination
- small API payloads
- query caching
- code splitting
- image lazy loading
- memoization only when justified

Avoid:

- enormous global stores
- fetching complete datasets
- duplicate API calls
- unnecessary re-renders
- massive dependencies
- huge icon bundles
- charting everything

Run bundle analysis before release.

---

# 44. Security

Audit frontend for:

- unsafe token storage
- XSS
- unsafe HTML
- URL injection
- sensitive data in browser storage
- PII exposure
- secret exposure
- unsafe file links
- overly verbose errors
- authorization leakage

Never use `dangerouslySetInnerHTML` without a reviewed sanitization strategy.

Never treat disabled/hidden controls as security.

Backend remains authoritative.

---

# 45. Testing

Required tools:

- TypeScript strict checks
- lint
- unit tests
- component tests where relevant
- Playwright
- production build

Critical Playwright workflows should eventually cover:

1. login
2. network configuration
3. serviceability
4. pricing
5. customer creation
6. shipment booking
7. pickup
8. scanning
9. bagging
10. manifest
11. line haul
12. hub receive/reconcile
13. delivery run
14. delivery/POD
15. public tracking
16. NDR
17. RTO
18. commission
19. COD
20. settlement
21. invoice
22. customer portal
23. franchise portal
24. reporting
25. integrations

Do not skip critical tests merely to make CI green.

---

# 46. Visual QA

After implementing a meaningful screen, inspect it visually if the environment permits.

Check:

- hierarchy
- alignment
- spacing
- typography
- density
- overflow
- responsive behavior
- focus
- empty state
- loading state
- error state
- permission state
- long text
- long names
- large numbers
- small viewport

Do not declare a complex screen finished without visual self-review.

---

# 47. Error-Code UX

Respect meaningful backend error codes.

Examples:

- invalid shipment state
- duplicate AWB request
- rate not found
- service unavailable
- insufficient permissions
- stale settlement
- duplicate COD posting
- bag already closed
- shipment already delivered

Render domain-specific feedback.

Do not reduce all HTTP 409/422 responses to generic errors.

---

# 48. Release Model

The frontend is organized into five releases.

## Release 1 — Foundation and Booking

- application shell
- authentication
- users/RBAC
- network
- geography
- serviceability/routing
- products
- pricing
- customers
- shipment booking
- shipment list/detail

## Release 2 — Physical Courier Operations

- pickup
- scanner console
- bags
- manifests
- line haul
- hub operations
- destination branch
- delivery
- NDR
- RTO
- POD
- public tracking

## Release 3 — Franchise Finance

- commission
- ledger
- COD
- settlement
- billing

## Release 4 — Productization

- notification administration
- customer portal
- franchise portal
- hub/branch terminal mode
- command center
- reporting
- integrations

## Release 5 — Production Gate

- security review
- visual consistency
- responsive QA
- accessibility
- performance
- E2E testing
- error-state audit
- operational UX audit
- production build hardening

Do not implement later release placeholder screens unless useful for navigation and clearly marked as unavailable.

Never fake backend data.

---

# 49. Frontend Definition of Done

A frontend module is complete only when applicable requirements are satisfied:

- route/page exists
- API integration exists
- generated/typed API usage exists
- permission behavior exists
- forms are validated
- loading state exists
- empty state exists
- error state exists
- permission-denied state exists
- responsive behavior exists
- keyboard behavior exists
- accessibility reviewed
- tests exist
- Playwright exists for critical workflow
- TypeScript passes
- lint passes
- production build passes
- visual QA completed

Do not mark completion with:
- placeholder screens,
- fake data,
- disabled tests,
- unsafe `any`,
- broken responsive layouts,
- backend logic duplicated in frontend.

---

# 50. Completion Report Required

After significant module/release implementation, report:

1. routes/pages added
2. reusable components added
3. APIs consumed
4. design/UX decisions
5. keyboard/scanner behavior
6. responsive behavior
7. accessibility work
8. tests/results
9. visual QA performed
10. backend contract gaps
11. unresolved UX risks

Do not merely say "implemented successfully."

Provide evidence.

---

# 51. Final Principle

The backend owns truth.

The frontend owns clarity.

Operational speed outranks decoration.

Financial clarity outranks clever visuals.

Consistency outranks novelty.

Accessibility is part of quality.

OpenAPI is the integration boundary.

The product should feel intentionally designed by an experienced enterprise product team — not generated screen by screen.
