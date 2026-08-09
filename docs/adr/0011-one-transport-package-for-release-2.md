# ADR 0011 — One transport package for the Release 2 modules

**Status:** accepted · Release 2

## Context

Release 1 put HTTP handlers inside each module, which works when a module owns a
self-contained resource: `internal/customer` serves `/customers`, and nothing
else needs to.

Release 2 does not look like that. A scan touches shipments, bags and
exceptions. A manifest dispatch drives bags, trips and shipments. A delivery
failure opens an NDR case, which may start an RTO. The modules already depend on
one another through explicit hooks (`delivery → ndr → rto`), and giving each its
own handler package would mean either duplicating the request plumbing twelve
times — facility resolution, the device envelope, pagination, permission
gating — or introducing import cycles between transport layers.

## Decision

The twelve operational modules keep their business logic pure: no `net/http`, no
`chi`, no request parsing. A single package, `internal/opsapi`, owns the
transport for all of them — request decoding, validation, permission gating and
response shaping — split across files by domain.

Shared request plumbing lives in `internal/ops`: facility resolution, the device
envelope, operational code allocation, and the error translations every module
needs.

## Consequences

There is one larger transport package instead of twelve small ones. It is
navigable because it is split by domain and because each handler is short —
parse, validate, delegate, render.

The benefit is that "which facility is this clerk working at, and are they
allowed to be there" has exactly one implementation. Twelve copies of that check
would be twelve chances to write it slightly differently, and the difference
that matters is the one nobody notices.

The modules stay independently testable: they take typed inputs and return typed
results, so a unit test does not need an HTTP request.

Release 1's per-module handlers are left where they are. Moving healthy code to
match a new convention would be churn without benefit, and the Constitution (§4)
says as much.
