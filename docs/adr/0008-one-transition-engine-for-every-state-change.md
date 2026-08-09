# ADR 0008 — One transition engine for every operational state change

**Status:** accepted · Release 2

## Context

Release 2 introduces twelve modules that all move shipments: pickup, scanning,
bagging, manifest, line haul, hub operations, delivery, NDR, RTO and POD. Each
one changes `shipments.current_status`.

Every such change has to do five things together, or the system is wrong:

1. confirm the transition is legal from the current state,
2. confirm the actor holds the permission that transition demands,
3. confirm the actor's facility actually has custody of the parcel,
4. write the change as a compare-and-swap so two concurrent actors cannot both
   succeed,
5. append an event to the append-only history, and audit where required.

Twelve modules each remembering all five is twelve chances to forget one. The
one most likely to be forgotten is custody, because it is the only one whose
absence produces no error — just a parcel marked delivered from a branch two
states away.

## Decision

All twelve modules change shipment state through a single type,
`shipment.Transitioner`. It takes a locked shipment row and a target state and
performs all five steps in a fixed order. No module writes to
`shipments.current_status` directly.

The transition table itself is data: each entry declares its permission, its
custody rule, whether it needs a reason, whether a hold blocks it, and which
direction of travel it belongs to. A new transition is a table entry, not new
code, and a table entry with no custody rule fails a unit test.

Two escape hatches exist and both are explicit:

- `SkipCustodyCheck` requires the `shipment.override_custody` permission and a
  written reason, and always audits the rule it bypassed.
- `SystemInitiated` skips the actor's permission and custody checks for changes
  the platform makes by policy rather than by request — an automatic RTO once
  the NDR attempt budget is exhausted. The event is attributed to `SYSTEM` so
  the history says who really decided. It exists because a delivery agent
  recording their third failed attempt must not need `rto.manage` for the
  configured policy to fire.

## Consequences

The engine is a chokepoint, which is the point: every operational state change
in the system passes through roughly two hundred lines that are easy to read and
heavily tested. Custody rules can be strengthened in one place.

The cost is that a module cannot make an unusual state change without either
adding a table entry or using an escape hatch. That has been the right trade so
far — every case that seemed to need a bypass turned out to be a missing table
entry or a genuine policy action.

`Transitioner.RecordEvent` covers the other case: recording that something
happened without changing state. Reverse movement uses it heavily, since an RTO
parcel crossing a hub is a fact worth recording but not a lifecycle change.
