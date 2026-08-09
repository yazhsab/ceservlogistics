import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { ScanFeedback, ScannerInput } from "./operations";

function ScannerHarness({ onScan }: { onScan: (value: string) => void }) {
  const [value, setValue] = useState("");
  return (
    <ScannerInput value={value} onChange={setValue} onScan={onScan} />
  );
}

describe("operational scanner components", () => {
  it("auto-focuses, normalizes, and submits keyboard scanner input", async () => {
    const user = userEvent.setup();
    const onScan = vi.fn();
    render(<ScannerHarness onScan={onScan} />);
    const input = screen.getByLabelText("Scan barcode");
    expect(input).toHaveFocus();
    await user.type(input, "csv-001{Enter}");
    expect(onScan).toHaveBeenCalledOnce();
    expect(onScan).toHaveBeenCalledWith("CSV-001");
  });

  it("announces accepted and rejected outcomes with text", () => {
    const { rerender } = render(
      <ScanFeedback
        result={{
          barcode: "CSV-001",
          outcome: "ACCEPTED",
          nextAction: "Sort to Delhi hub",
        }}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Accepted: CSV-001");
    rerender(
      <ScanFeedback
        result={{
          barcode: "BAD-001",
          outcome: "REJECTED",
          rejectionCode: "BARCODE_NOT_FOUND",
          rejectionMessage: "Barcode was not found",
        }}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "Rejected: BAD-001",
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "Barcode was not found",
    );
  });
});
