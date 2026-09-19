import { describe, expect, it } from "vitest";
import type { UserProfile } from "../api/client";
import { signedInHome, signInDestination } from "./navigation";

const staff: UserProfile = { id: "staff", permissions: ["shipment.read"] };
const customer: UserProfile = {
  id: "customer",
  portal: { isCustomerUser: true, customers: [{ id: "customer-account" }] },
};
const franchise: UserProfile = {
  id: "franchise",
  permissions: ["portal.franchise"],
  portal: { franchise: { id: "franchise-account" } },
};

describe("authenticated landing pages", () => {
  it("uses the server's account binding, not a permission alone", () => {
    expect(signedInHome(customer)).toBe("/portal/customer");
    expect(signedInHome(franchise)).toBe("/portal/franchise");
    expect(
      signedInHome({
        ...staff,
        permissions: ["portal.customer", "portal.franchise"],
      }),
    ).toBe("/shipments");
    expect(signedInHome({ ...franchise, permissions: [] })).toBe("/shipments");
  });
  it("sends customer logins away from staff routes while preserving customer deep links", () => {
    for (const from of [undefined, "/", "/shipments", "/users", "/login"])
      expect(signInDestination(customer, from)).toBe("/portal/customer");
    expect(
      signInDestination(customer, "/portal/customer/shipments/shp_123"),
    ).toBe("/portal/customer/shipments/shp_123");
  });
  it("gives franchise users their dashboard but preserves operational deep links", () => {
    expect(signInDestination(franchise, "/shipments")).toBe(
      "/portal/franchise",
    );
    expect(signInDestination(franchise, "/operations/scanner")).toBe(
      "/operations/scanner",
    );
  });
  it("discards a previous user's portal when switching account types", () => {
    expect(signInDestination(franchise, "/portal/customer")).toBe(
      "/portal/franchise",
    );
    expect(signInDestination(staff, "/portal/customer/shipments")).toBe(
      "/shipments",
    );
    expect(signInDestination(staff, "/portal/franchise")).toBe("/shipments");
    expect(signInDestination(customer, "/portal/franchise")).toBe(
      "/portal/customer",
    );
  });
  it("requires password changes before any destination", () => {
    expect(
      signInDestination(
        { ...customer, mustChangePassword: true },
        "/portal/customer",
      ),
    ).toBe("/change-password");
  });
  it.each([
    "https://example.test",
    "//example.test",
    "/\\example.test",
    "/\n/example.test",
    {},
    42,
  ])("rejects unsafe return destinations: %s", (from) => {
    expect(signInDestination(staff, from)).toBe("/shipments");
  });
});
