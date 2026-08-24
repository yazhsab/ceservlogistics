import { describe, expect, it } from "vitest";
import {
  cmToMm,
  formatDimensionsCm,
  formatMoney,
  formatWeight,
  gramsToKg,
  kgToGrams,
  mmToCm,
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
    expect(kgToGrams(20)).toBe(20000);
    expect(kgToGrams(0.125)).toBe(125);
    expect(gramsToKg(125)).toBe(0.125);
  });

  it("keeps centimetre measurements exact at the millimetre API boundary", () => {
    expect(cmToMm(50)).toBe(500);
    expect(cmToMm(12.3)).toBe(123);
    expect(mmToCm(123)).toBe(12.3);
    expect(formatDimensionsCm(500, 400, 250)).toBe("50 × 40 × 25 cm");
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
