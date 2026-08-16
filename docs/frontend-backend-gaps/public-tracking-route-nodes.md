# Public tracking route-node projection

The public tracking contract currently returns origin, destination, and scan-derived milestone events, but it does not return the shipment's planned route legs. The customer tracker can safely render origin, scanned locations, and destination now. To distinguish the complete planned path from the actual scanned path without exposing internal identifiers, the API should add a customer-safe `routeNodes` projection containing only city/state labels, sequence, node type, and reached timestamp.

The internal route planner and PIN-code override APIs already exist under `/api/v1/routes`, `/api/v1/routes/overrides`, and `/api/v1/serviceability`; no browser-side routing rules should be invented.
