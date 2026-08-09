# ADR 0009 — RTO as reverse movement, not a mirrored status ladder

**Status:** accepted · Release 2

## Context

The Constitution (§18) requires that return to sender be "modelled as reverse
courier movement" and explicitly not as "a single status update". A returned
parcel really does travel: destination branch → destination hub → origin hub →
origin branch → sender, through the same bags, manifests and trips as forward
traffic.

The obvious implementation is a mirrored set of statuses —
`RTO_DESTINATION_HUB_RECEIVED`, `RTO_ORIGIN_HUB_RECEIVED`, and so on. That
doubles a 22-state machine to express a journey that uses identical facilities
and identical scans, and every downstream consumer (tracking, dashboards,
reporting, the frontend) has to learn both ladders.

It is also not what the Constitution asks for: §13 enumerates the permitted
statuses, and there are exactly three RTO states in that list.

## Decision

A shipment carries `movement_direction`, either `FORWARD` or `REVERSE`. The RTO
statuses are the three the Constitution names: `RTO_INITIATED`,
`RTO_IN_TRANSIT`, `RTO_DELIVERED`.

Position on the return journey is tracked by two things instead:

- **`rto_legs`** — the planned reverse path, resolved by the same routing engine
  with origin and destination swapped, with each leg advanced as the parcel
  reaches a facility.
- **The append-only event stream** — every reverse receipt appends an event
  carrying the facility, so "where is my return" is answerable from history.

The transition table gates on direction: a transition marked `FORWARD` is
refused for a reverse-moving parcel and vice versa. That is what prevents a
returning parcel being delivered to the original consignee.

Bags, manifests and trips carry a `direction` too, so forward and reverse
traffic never mix inside one container.

## Consequences

The state machine stays at 22 states. Tracking, dashboards and reporting learn
one ladder, and `isReturning` is a boolean rather than a status prefix check.

The cost is that "received at the origin hub on the way back" is not a status —
a client wanting that granularity reads the RTO case's legs. The frontend
contract says so explicitly, because a developer expecting a status change at
each facility would otherwise conclude the return is stuck.

The reverse route is resolved once, at initiation, and stored with an
explanation. A network change afterwards does not silently reroute a return in
flight.
