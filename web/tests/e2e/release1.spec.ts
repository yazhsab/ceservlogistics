import { expect, test } from "@playwright/test";
import { installMockApi, shipment } from "./mockApi";

async function login(page: import("@playwright/test").Page) {
  await page.goto("/login");
  await page.getByLabel("Work email").fill("operator@ceserve.test");
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();
  await expect(page.getByRole("heading", { name: "Shipments" })).toBeVisible();
}

test.beforeEach(async ({ page }) => {
  await installMockApi(page);
});

test("login and permission-aware navigation", async ({ page }) => {
  await login(page);
  await expect(
    page.getByRole("navigation", { name: "Primary navigation" }),
  ).toContainText("Shipments");
  await page.getByRole("link", { name: "Users" }).click();
  await expect(page.getByRole("heading", { name: "Users" })).toBeVisible();
  await expect(page.getByText("Kemi Adeyemi").first()).toBeVisible();
});

test("route and action guards remove unauthorized capabilities", async ({
  page,
}) => {
  await page.unroute("**/api/v1/**");
  await installMockApi(page, { permissions: ["shipment.read"] });
  await login(page);
  await expect(page.getByRole("link", { name: "Users" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Book shipment" })).toHaveCount(
    0,
  );
  await page.goto("/admin/users");
  await expect(page.getByText(/do not have permission/i)).toBeVisible();
});

test("login and profile remain responsive @mobile", async ({ page }) => {
  await page.goto("/login");
  await expect(
    page.getByRole("heading", { name: "Welcome back" }),
  ).toBeVisible();
  await expect(page.getByRole("complementary")).toBeHidden();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page
    .getByRole("textbox", { name: "Work email" })
    .fill("operator@ceserve.test");
  await page
    .getByRole("textbox", { name: "Password" })
    .fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();
  await page.goto("/profile");
  await expect(page.getByRole("heading", { name: "My profile" })).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("serviceability lookup renders operational route", async ({ page }) => {
  await login(page);
  await page.goto("/routing/tester");
  await page.getByLabel("Origin postal code").fill("100001");
  await page.getByLabel("Destination postal code").fill("900001");
  await page.getByLabel("Courier product").selectOption("EXPRESS");
  await page.getByRole("button", { name: "Check serviceability" }).click();
  await expect(page.getByText("Serviceable", { exact: true })).toBeVisible();
  await expect(page.getByText("LOS01")).toBeVisible();
  await expect(page.getByText("ABV01")).toBeVisible();
});

test("pricing simulation explains weight and total", async ({ page }) => {
  await login(page);
  await page.goto("/pricing/simulator");
  await page.getByLabel("Origin postal code").fill("100001");
  await page.getByLabel("Destination postal code").fill("900001");
  await page.getByLabel("Destination city").fill("Abuja");
  await page.getByLabel("Courier product").selectOption("EXPRESS");
  await page.getByRole("button", { name: "Calculate price" }).click();
  await expect(page.getByText("Chargeable weight")).toBeVisible();
  await expect(page.getByText("Volumetric basis selected")).toBeVisible();
  await expect(page.getByText(/104\.88/).first()).toBeVisible();
});

test("customer creation is available to authorized users", async ({ page }) => {
  await login(page);
  await page.goto("/customers");
  await page.getByRole("button", { name: "Add customer" }).click();
  await page.getByLabel("Customer name").fill("New Customer");
  await page.getByLabel("Phone").fill("08035551234");
  await page.getByRole("button", { name: "Create customer" }).click();
  await expect(page.getByText("Customer created")).toBeVisible();
});

test("booking is keyboard-friendly and duplicate submission is blocked", async ({
  page,
}) => {
  const api = await installMockApi(page, { bookingDelayMs: 300 });
  await page.route("**/api/v1/geography/states?*", (route) =>
    route.fulfill({
      json: {
        data: [
          { id: "state-lagos", code: "LA", name: "Lagos" },
          { id: "state-fct", code: "FC", name: "FCT" },
        ],
      },
    }),
  );
  await login(page);
  await page.goto("/shipments/new");
  await page
    .getByPlaceholder("Search customer code, name, email, or phone")
    .fill("Acme");
  await page.getByRole("button", { name: /Acme Retail/ }).click();
  await page.getByLabel("Contact name").first().fill("Chiamaka Okafor");
  await page.getByLabel("Phone").first().fill("08031234567");
  await page.getByLabel("Address line 1").first().fill("12 Marina Road");
  await page.getByLabel("Postal code").first().fill("100001");
  await page
    .getByRole("combobox", { name: "State", exact: true })
    .first()
    .selectOption("Lagos");
  await page.getByLabel("City / state capital").first().fill("Lagos");
  await page.getByLabel("Contact name").nth(1).fill("Amina Bello");
  await page.getByLabel("Phone").nth(1).fill("08037654321");
  await page.getByLabel("Address line 1").nth(1).fill("8 Gimbiya Street");
  await page.getByLabel("Postal code").nth(1).fill("900001");
  await page
    .getByRole("combobox", { name: "State", exact: true })
    .nth(1)
    .selectOption("FCT");
  await page.getByLabel("City / state capital").nth(1).fill("Abuja");
  await page.getByLabel("Courier product").selectOption("EXPRESS");
  await page.getByLabel("General description of item").fill("Documents");
  await page.getByRole("button", { name: "Preview shipment" }).last().click();
  await expect(
    page.locator("strong:visible", { hasText: "Shipment charges total" }),
  ).toBeVisible();
  const book = page
    .getByRole("button", { name: /^Confirm & book(?: shipment)?$/ })
    .filter({ visible: true });
  await book.dblclick();
  await expect(page.getByText("Shipment booked successfully")).toBeVisible();
  expect(api.shipmentPosts).toBe(1);
  await expect(page.getByRole("heading", { name: shipment.awb })).toBeVisible();
});

test("shipment search, detail, label, and cancellation", async ({ page }) => {
  await login(page);
  await page
    .getByPlaceholder("Search AWB, reference, recipient")
    .fill(shipment.awb);
  await page.getByRole("link", { name: shipment.awb }).click();
  await expect(page.getByRole("heading", { name: shipment.awb })).toBeVisible();
  await page.getByRole("button", { name: "Label" }).click();
  await expect(page.getByText("Routing code")).toBeVisible();
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: "Cancel" }).click();
  await page
    .getByLabel("Cancellation reason")
    .fill("Customer requested cancellation");
  await page.getByRole("button", { name: "Cancel shipment" }).click();
  await expect(page.getByText("Shipment cancelled")).toBeVisible();
});
