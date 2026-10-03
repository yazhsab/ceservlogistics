import { describe, expect, it } from "vitest";
import {
  parseCsv,
  parseManualShipmentCsv,
  templateExample,
  templateHeaders,
} from "./ManualShipmentImportPage";

describe("manual shipment CSV import", () => {
  it("handles quoted commas and escaped quotes", () => {
    expect(parseCsv('one,"two, three","said ""yes"""\r\n')).toEqual([
      ["one", "two, three", 'said "yes"'],
    ]);
  });

  it("creates a multi-package request and distributes declared value exactly", () => {
    const values = [...templateExample];
    values[templateHeaders.indexOf("declared_value_minor")] = "10001";
    values[templateHeaders.indexOf("insurance_required")] = "true";
    values[templateHeaders.indexOf("insurance_accepted")] = "true";
    const csv = `${templateHeaders.join(",")}\n${values.join(",")}`;

    const [row] = parseManualShipmentCsv(csv);

    expect(row?.errors).toEqual([]);
    expect(row?.insuranceAccepted).toBe(true);
    expect(row?.request?.manualWaybillNumber).toBe("MAN-0001");
    expect(row?.request?.packages).toHaveLength(2);
    expect(row?.request?.packages.map((item) => item.actualWeightGrams)).toEqual([
      500, 750,
    ]);
    expect(
      row?.request?.packages.reduce(
        (total, item) => total + (item.declaredValueMinor ?? 0),
        0,
      ),
    ).toBe(10001);
  });

  it("reports missing required shipment data without creating a request", () => {
    const values = [...templateExample];
    values[templateHeaders.indexOf("recipient_postal_code")] = "";
    const [row] = parseManualShipmentCsv(
      `${templateHeaders.join(",")}\n${values.join(",")}`,
    );

    expect(row?.request).toBeUndefined();
    expect(row?.errors.join(" ")).toContain("recipient postal code is required");
  });
});
