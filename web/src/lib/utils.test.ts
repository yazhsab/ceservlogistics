import { describe, expect, it } from "vitest";
import {
  formatMoney,
  formatWeight,
  pincodeIsValid,
  safeDownloadName,
  titleCase,
  toMinorUnits,
} from "./utils";

describe("courier display utilities", () => {
  it("formats minor currency units without changing the source value", () => {
    expect(formatMoney(10488)).toBe("₦104.88");
    expect(toMinorUnits("104.88")).toBe(10488);
  });

  it("formats operational weights", () => {
    expect(formatWeight(500)).toBe("500 g");
    expect(formatWeight(1250)).toBe("1.25 kg");
  });

  it("validates six-digit Nigerian postal codes", () => {
    expect(pincodeIsValid("100001")).toBe(true);
    expect(pincodeIsValid("000001")).toBe(false);
    expect(pincodeIsValid("10001")).toBe(false);
  });

  it("converts API enums into readable labels", () => {
    expect(titleCase("OUT_FOR_DELIVERY")).toBe("Out For Delivery");
  });

  it("removes path and control characters from browser download names", () => {
    expect(safeDownloadName("../../August\u0000report", "report")).toBe(
      "-..-August-report",
    );
    expect(safeDownloadName("...", "report")).toBe("report");
  });
});
