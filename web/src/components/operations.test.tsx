import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ScanFeedback, ScannerInput } from "./operations";

function ScannerHarness({ onScan }: { onScan: (value: string) => void }) {
  const [value, setValue] = useState("");
  return (
    <>
      <button type="button">Other control</button>
      <ScannerInput value={value} onChange={setValue} onScan={onScan} />
    </>
  );
}

afterEach(cleanup);

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

  it("accepts the Tab suffix used by keyboard-wedge scanners", async () => {
    const user = userEvent.setup();
    const onScan = vi.fn();
    render(<ScannerHarness onScan={onScan} />);
    await user.type(screen.getByLabelText("Scan barcode"), "piece-1{Tab}");
    expect(onScan).toHaveBeenCalledWith("PIECE-1");
  });

  it("captures a scanner even when another non-input control has focus", async () => {
    const user = userEvent.setup();
    const onScan = vi.fn();
    render(<ScannerHarness onScan={onScan} />);
    await user.click(screen.getByRole("button", { name: "Other control" }));
    await user.keyboard("AWB-123{Enter}");
    expect(onScan).toHaveBeenCalledWith("AWB-123");
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
    expect(screen.getByRole("status")).toHaveTextContent("Rejected: BAD-001");
    expect(screen.getByRole("status")).toHaveTextContent(
      "Barcode was not found",
    );
  });
});
