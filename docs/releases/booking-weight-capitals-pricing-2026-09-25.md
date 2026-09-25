# Booking weight, capital defaults and pricing administration — 2026-09-25

## Delivered behavior

- Shipment booking now presents **Total weight (kg)** beside **Number of packages**. Changing the package count preserves the total and distributes it to the nearest gram. Any indivisible remainder is assigned from piece 1; operators may still correct individual measured weights.
- Nigerian state reference data now includes all 36 state capitals and Abuja for the FCT. Selecting a state fills its capital as the initial city without replacing the operator-entered postal code.
- Booking includes a permission-aware **Price inquiry** link to the existing authoritative Pricing simulator.
- Rate-card version detail now returns and displays generic weight slabs, the versioned domestic tariff rows, surcharge rules and discount rules.
- Draft rate-card versions now expose **Add weight price** and **Add discount** forms. BUSINESS rate-card creation uses a customer search instead of requiring staff to know an internal customer ID.

## API and data changes

- Migration `0043_state_capitals` adds nullable `capital_city` and `capital_pincode` reference fields to `states` and populates the reviewed Nigerian catalogue.
- `GET /api/v1/geography/states` returns `capitalCity` and `capitalPincode` when configured.
- `GET /api/v1/rate-cards/versions/{versionId}` now returns `weightSlabs`, `domesticWeightSlabs` and `discounts` in addition to zone rates and surcharges.
- OpenAPI now documents draft weight-slab and discount creation operations.

The postal code entered for the actual address remains authoritative for serviceability and routing. Capital selection is an entry convenience only. Prices, discounts and chargeable weight remain server-calculated.

## Validation

- `go test ./...`
- `npm run typecheck`
- `npm run lint`
- `npm test -- --run` — 51 tests passed
- `npm run build`
- Playwright booking workflow at 1366×768, including capital defaults and a 1 kg / 3-piece distribution of 0.334, 0.333 and 0.333 kg
- Generated OpenAPI TypeScript and sqlc outputs refreshed

## Remaining limits

Rate-card versions remain immutable after activation. Spreadsheet-style bulk editing and update/delete operations for individual weight or discount rules are not yet exposed; corrections use a new complete draft version. The current supplied CESERV domestic tariff is read-only in the UI and remains managed through its reviewed import source.
