import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { formatMoney } from "../lib/utils";
import {
  ApprovalTimeline,
  DebitCreditAmount,
  FinancialStatus,
  FinancialSummary,
  ImmutableBadge,
  ReferenceLink,
} from "./financial";

describe("finance formatting", () => {
  it("formats Nigerian minor units without losing integer precision", () => {
    expect(formatMoney("123456789", "NGN")).toBe("₦1,234,567.89");
    expect(formatMoney("900719925474099399", "NGN")).toBe(
      "₦9,007,199,254,740,993.99",
    );
    expect(formatMoney(-50, "INR")).toBe("-₹0.50");
  });

  it("refuses unsafe numeric minor units instead of silently rounding", () => {
    expect(() => formatMoney(Number.MAX_SAFE_INTEGER + 1, "INR")).toThrow(
      "safe integer",
    );
  });
});

describe("financial semantics", () => {
  it("labels debit and credit independently of color", () => {
    render(<DebitCreditAmount amountMinor={5000} direction="DEBIT" />);
    expect(screen.getByLabelText("Debit ₦50.00")).toBeVisible();
    expect(screen.getByText("DR")).toBeVisible();
  });

  it("makes immutable and financial state explicit", () => {
    render(
      <>
        <FinancialStatus status="POSTED" detail="Journal cannot be edited" />
        <ImmutableBadge />
      </>,
    );
    expect(screen.getByText("Posted")).toBeVisible();
    expect(screen.getByText("Journal cannot be edited")).toBeVisible();
    expect(screen.getByText("Posted · Immutable")).toBeVisible();
  });

  it("links references with a descriptive accessible name", () => {
    render(
      <MemoryRouter>
        <ReferenceLink
          to="/shipments/shp_1"
          type="shipment"
          reference="AWB1001"
        />
      </MemoryRouter>,
    );
    expect(
      screen.getByRole("link", { name: "View shipment AWB1001" }),
    ).toHaveAttribute("href", "/shipments/shp_1");
  });

  it("states settlement direction in words and does not calculate line totals", () => {
    render(
      <FinancialSummary
        direction="FRANCHISE_PAYS_HEAD_OFFICE"
        totalMinor={45000}
        items={[
          { label: "COD liability", amountMinor: 50000 },
          { label: "Commission", amountMinor: 5000 },
        ]}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "Franchise pays Head Office" }),
    ).toBeVisible();
    expect(
      screen.getByText("This is an amount receivable from the franchise."),
    ).toBeVisible();
    expect(screen.getByText("₦450.00")).toBeVisible();
    expect(
      screen.getByText(/does not calculate authoritative totals/i),
    ).toBeVisible();
  });

  it("presents maker-checker progress as a named ordered history", () => {
    render(
      <ApprovalTimeline
        steps={[
          {
            id: "prepared",
            label: "Prepared",
            status: "COMPLETE",
            actor: "Finance Maker",
          },
          { id: "approval", label: "Approval", status: "CURRENT" },
        ]}
      />,
    );
    expect(
      screen.getByRole("list", { name: "Approval history" }),
    ).toBeVisible();
    expect(screen.getByText("Finance Maker")).toBeVisible();
    expect(screen.getByText("Current")).toBeVisible();
  });
});
