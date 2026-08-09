# Release 3 frontend completion report

Date: 2026-08-08

Status: implemented against `docs/contracts/release-3.md` and generated types
from `docs/openapi.yaml`, subject to the residual capabilities recorded in
`docs/frontend-backend-gaps/release-3.md`.

## 1. Routes and pages added

- Commission rules and versions: `/finance/commission/rules`,
  `/finance/commission/rules/:ruleId`.
- Commission simulator and entries: `/finance/commission/simulator`,
  `/finance/commission/entries`,
  `/finance/commission/entries/:calculationId`.
- Ledger: `/finance/ledger/accounts`,
  `/finance/ledger/accounts/:accountId`, `/finance/ledger/journals`,
  `/finance/ledger/journals/:journalId`, `/finance/ledger/trial-balance`.
- COD: `/finance/cod`, `/finance/cod/obligations/:obligationId`.
- Settlement: `/finance/settlements`,
  `/finance/settlements/:settlementId`.
- Billing: `/finance/billing/invoices`,
  `/finance/billing/invoices/:invoiceId`.

All routes and sensitive controls use their distinct read, configure,
simulate, approve, pay, reverse, reconcile, issue, or correction permission.

## 2. Reusable components added

- `Money` with exact minor-unit formatting and unsafe-number rejection.
- `DebitCreditAmount` with explicit DR/CR semantics.
- `FinancialStatus`, `ReferenceLink`, and `ImmutableBadge`.
- `ApprovalTimeline` for maker/checker state.
- `FinancialSummary` with explicit Head Office/Franchise payment direction and
  no browser-authoritative summing.

## 3. APIs consumed

- Commission rule list/create/detail/version, simulation, calculation
  list/detail, posting, and reversal.
- Chart of accounts, account statement, journal register/detail/reversal,
  ledger health, trial balance, and accounting periods.
- COD summary, obligations, custody cross-check, and reconciliation
  open/count/complete.
- Settlement list/generate/detail/recalculate/submit/approve/pay/cancel.
- Invoice list/draft/detail/issue/payment and credit/debit-note raise/issue.

`web/src/api/schema.d.ts` was regenerated. Entity, request, response, and page
types are derived from generated OpenAPI `components` and `operations`.

## 4. Design and UX decisions

- Financial direction uses explicit language, sign, icon, and amount—not color
  alone.
- Posted journals, posted commissions, approved settlements, and issued
  invoices are visibly immutable and have no Edit action.
- Commission simulation shows the selected rule, exact version, candidate
  precedence, clamp state, and backend calculation trace.
- Ledger debit and credit remain separate columns; running balance and trial
  balance are never calculated in the browser.
- COD custody compares operational holding with the independent ledger result
  and treats disagreement as a finance incident.
- Settlement approval and immutable corrections use explicit confirmation;
  payment dialogs restate inbound/outbound direction.
- Draft invoice references are never presented as statutory numbers.

## 5. Keyboard behavior

All finance tables, filters, tabs, links, forms, and confirmation dialogs use
native keyboard-operable controls with visible focus. Finance workflows do not
introduce scanner shortcuts or surprise single-key actions.

## 6. Responsive behavior

- Dense financial registers retain horizontal table scrolling on tablet.
- Summaries and action headers stack without changing amount meaning.
- Amounts remain tabular and non-wrapping.
- Confirmation dialogs preserve a reachable footer on constrained heights.

## 7. Accessibility work

- Debit/credit, financial state, immutability, approval progress, and settlement
  direction have textual semantics.
- Tables have programmatic labels and column headings.
- Approval history is an ordered list.
- Errors preserve backend operator messages and request identifiers.
- Permission denial is explicit at route level; unavailable actions are hidden.

## 8. Tests and results

- Unit tests cover high-value exact money formatting, unsafe-number rejection,
  debit semantics, immutable status, accessible references, explicit
  settlement direction, and maker/checker history.
- Playwright Release 3 covers rule creation/version inspection, commission
  simulation and entry trace, balanced immutable journal, account statement,
  trial balance, COD custody/reconciliation, settlement direction/approval,
  settlement payment, invoice/payment/PDF state, and finance permission denial.
- `npm run api:generate`: passed; OpenAPI TypeScript regenerated and formatted.
- `npm run typecheck`: passed with TypeScript strict checks.
- `npm run lint`: passed with zero warnings.
- `npm test`: 4 files passed, 16/16 tests passed.
- `npm run build`: passed; Vite production output generated successfully.
- Focused Release 3 Playwright: 20/20 tests passed across desktop and tablet.
- Full Playwright regression: 52/52 tests passed across desktop, tablet, and
  the repository's mobile workflows.

## 9. Visual QA performed

- Inspected commission simulation, posted journal detail, COD control centre,
  settlement summary/approval confirmation, and immutable invoice detail with
  live contract-shaped fixtures.
- Checked 1440×900, 1024×768, and 768×1024 viewports. No page-level horizontal
  overflow was present; dense tables and settlement tabs retain contained
  scrolling where needed.
- Verified debit/credit alignment, large and signed currency values, immutable
  badges, correction actions, exception messaging, and settlement payment
  direction without relying on color.
- Confirmed the approval dialog repeats who owes whom and the amount before the
  final action. Browser console inspection returned no warnings or errors.

## 10. Backend contract gaps

See `docs/frontend-backend-gaps/release-3.md`. The largest limitations are COD
workflow discovery, opaque settlement audit collections, absent billing/note
registers, and missing server-side invoice-line pagination.

## 11. Unresolved UX risks

- Large int64 money values remain vulnerable if emitted as JSON numbers.
- Raw franchise/party/customer IDs remain in creation dialogs where the
  contract does not provide a role-scoped finance lookup.
- Maker/checker identity is ultimately enforced by the backend; a permitted
  user may still receive a meaningful 403 for a record they created.
- Invoice PDF, billing runs, and post-approval settlement adjustments are not
  available in Release 3.
- Commission simulation lacks stable rule/version IDs, so the exact winning
  code and version are shown but a canonical version deep link is not possible.
