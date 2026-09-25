# Release 1 frontend/backend contract gaps

Reviewed against `docs/openapi.yaml` and `docs/contracts/release-1.md` on 8 August 2026.

The Release 1 frontend implements every workflow that can be represented safely by the current OpenAPI contract. The items below are deliberately not emulated in browser code. No fake API, authoritative pricing logic, or client-owned workflow transition was introduced.

## Network and franchise administration

1. **Regions are not a contract resource.** Operating units expose `regionId`/`regionCode`, but OpenAPI has no region list, detail, create, update, or deactivate operation. A standalone Regions workspace and region selector cannot be implemented safely.
2. **Franchise administration stops after list/create.** `GET` and `POST /api/v1/network/franchises` exist, but there is no franchise detail, update, activate, or deactivate operation. The list's `OffsetPage.data` is also untyped, so agreement, owner, and operational relationship detail cannot be rendered safely after creation.
3. **Operating-unit relationships are incomplete.** The hierarchy and unit detail can be shown, but there are no unit-specific child or assigned-user operations. Audit data can only be queried through the organization-wide audit endpoint.
4. **Operating-unit capabilities and operating hours are weakly typed.** They are inline objects without a reusable schema, constraining safe form generation and validation.

## Geography and routing

5. **PIN-code server filters are narrower than the release contract.** `GET /api/v1/geography/pincodes` supports code and remote-area filtering, but not the requested state, district, city, zone, and status filters. The generic page response does not type its row shape.
6. **Import error downloads are URL-only.** Import status may return `errorFileUrl`, but the authentication, expiry, and response-content contract for that URL is unspecified.
7. **Several routing collections are untyped.** Service areas, closures, route lists, and other offset pages use generic `OffsetPage.data`, requiring guarded normalization and limiting compile-time coverage.
8. **Routing rule CRUD is not a distinct resource.** The frontend maps the requested “Routing rules” workspace to service areas and closures. There is no rule entity or rule update/delete operation.
9. **Route override administration is create-only.** `POST /api/v1/routes/overrides` exists, but list, detail, update, and delete operations do not.
10. **Route, service-area, and closure mutation coverage is incomplete.** The contract exposes selected creates and reads but not a complete edit/deactivate lifecycle for each resource.

## Products and pricing

11. **Courier-product updates are limited.** The PATCH schema does not expose every field described by the release contract, so limits, service levels, and handling restrictions cannot all be edited after creation.
12. **Discount administration is partial.** OpenAPI now exposes draft discount creation and the version detail lists its rules. Update/delete operations are still absent; correct a published rule through a new version.
13. **Weight-slab administration is partial.** OpenAPI now exposes draft slab creation, lists generic slabs and returns the versioned domestic price list. Bulk spreadsheet editing and row update/delete operations remain absent.
14. **Rate-card lifecycle operations are incomplete.** There is no rate-card detail or update operation, and version update/delete is absent. The rate-card list is a generic untyped page.

## Customers, identity, and authorization

15. **Customer contacts/users are not resources.** The requested contacts and users tabs have no supporting operations.
16. **Saved addresses cannot be maintained fully.** Customer addresses can be listed and created, but there is no update or delete operation.
17. **Customer updates are intentionally narrow.** Only the fields exposed by `PATCH /api/v1/customers/{customerId}` are editable; other account attributes remain read-only.
18. **User filtering and role response typing are incomplete.** The users list has no operating-unit filter, and some user/role collection responses are inline or generic rather than reusable typed schemas.
19. **Permission-code metadata is incomplete for UI discovery.** The frontend consumes the permission catalog and backend authorization responses, but several endpoint descriptions do not state their exact permission code. Route/action guards therefore depend on the catalog’s semantic codes and still defer to 403 responses.

## Shipments and labels

20. **PDF labels are not supported in Release 1.** The current label response is JSON or ZPL. The frontend renders the JSON as an accessible browser-print label and supports ZPL download; it does not invent a PDF endpoint.
21. **The label path contains an ambiguous POST operation.** OpenAPI defines the valid label retrieval contract on GET and also includes an empty POST operation for the same path. The frontend uses GET only.

## Requested backend follow-up

Prioritize typed collection schemas and missing mutation lifecycles first. They unlock the greatest amount of safe frontend work and remove defensive normalization. Regions, franchise detail/lifecycle, pricing slabs/discounts, route override listing, customer contact/address maintenance, and explicit permission metadata are the largest functional blockers.
