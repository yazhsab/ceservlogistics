import type { components } from "../api/schema";

// Display the server's headline rate only when it represents a single,
// uncapped declared-value percentage. Other offers show their rule breakdown.
export function insuranceRateLabel(
  quote?: components["schemas"]["InsuranceQuote"],
) {
  return quote?.rateBp === undefined
    ? undefined
    : `${new Intl.NumberFormat("en", { maximumFractionDigits: 2 }).format(quote.rateBp / 100)}%`;
}
