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
          {
            id: "state-lagos",
            code: "LA",
            name: "Lagos",
            capitalCity: "Ikeja",
          },
          {
            id: "state-fct",
            code: "FC",
            name: "FCT",
            capitalCity: "Abuja",
          },
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
  await expect(page.getByLabel("City / state capital").first()).toHaveValue(
    "Ikeja",
  );
  await page.getByLabel("Contact name").nth(1).fill("Amina Bello");
  await page.getByLabel("Phone").nth(1).fill("08037654321");
  await page.getByLabel("Address line 1").nth(1).fill("8 Gimbiya Street");
  await page.getByLabel("Postal code").nth(1).fill("900001");
  await page
    .getByRole("combobox", { name: "State", exact: true })
    .nth(1)
    .selectOption("FCT");
  await expect(page.getByLabel("City / state capital").nth(1)).toHaveValue(
    "Abuja",
  );
  await page.getByLabel("Courier product").selectOption("EXPRESS");
  await page.getByLabel("General description of item").fill("Documents");
  await page.getByLabel("Number of packages").fill("3");
  await page.getByLabel("Total weight (kg)").fill("1");
  await page.getByLabel("Total weight (kg)").press("Tab");
  const weights = page.getByLabel("Shipment weight (kg)");
  await expect(weights).toHaveCount(3);
  await expect(weights.nth(0)).toHaveValue("0.334");
  await expect(weights.nth(1)).toHaveValue("0.333");
  await expect(weights.nth(2)).toHaveValue("0.333");
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
  const labelDialog = page.getByRole("dialog", { name: "Shipment label" });
  await expect(labelDialog.getByText("Routing code")).toBeVisible();
  await expect(labelDialog.getByText("CESERV", { exact: true })).toBeVisible();
  const routingCode = labelDialog.getByTestId("routing-code");
  await expect(routingCode).toHaveText("HUB_PORT_HARCOURT/BR_RI/500103");
  expect(
    await routingCode.evaluate(
      (element) => element.scrollWidth <= element.clientWidth,
    ),
  ).toBe(true);
  await labelDialog.getByLabel("Print format").selectOption("COURIER_SHEET");
  const customerCopy = labelDialog.getByRole("region", {
    name: "Customer copy",
  });
  await expect(customerCopy).toBeVisible();
  await expect(
    customerCopy.getByText("Customer copy", { exact: true }),
  ).toBeVisible();
  await expect(customerCopy.getByText("₦12,500.00")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Print labels & customer copy" }),
  ).toBeVisible();
  await page.emulateMedia({ media: "print" });
  const printPanels = labelDialog.locator(".courier-sheet-panel");
  await expect(printPanels).toHaveCount(2);
  const printPanelMetrics = await printPanels.evaluateAll((panels) =>
    panels.map((panel) => ({
      clientWidth: panel.clientWidth,
      scrollWidth: panel.scrollWidth,
      clientHeight: panel.clientHeight,
      scrollHeight: panel.scrollHeight,
    })),
  );
  expect(
    printPanelMetrics.every(
      (panel) =>
        panel.scrollWidth <= panel.clientWidth + 1 &&
        panel.scrollHeight <= panel.clientHeight + 1,
    ),
  ).toBe(true);
  await page.emulateMedia({ media: "screen" });
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: "Cancel" }).click();
  await page
    .getByLabel("Cancellation reason")
    .fill("Customer requested cancellation");
  await page.getByRole("button", { name: "Cancel shipment" }).click();
  await expect(page.getByText("Shipment cancelled")).toBeVisible();
});

test("booked shipment can be corrected from the Edit column without changing its AWB", async ({
  page,
}) => {
  let correction: Record<string, unknown> | undefined;
  await page.route(
    `**/api/v1/shipments/${shipment.id}`,
    async (route, request) => {
      if (request.method() !== "PATCH") return route.fallback();
      correction = request.postDataJSON() as Record<string, unknown>;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          ...shipment,
          version: shipment.version + 1,
          referenceNumber: correction.referenceNumber,
          addresses: {
            ...shipment.addresses,
            recipient: {
              ...shipment.addresses.recipient,
              ...(correction.recipient as Record<string, unknown>),
            },
          },
        }),
      });
    },
  );
  await login(page);
  await expect(
    page.getByRole("columnheader", { name: "Action" }),
  ).toBeVisible();
  await page.getByLabel(`Edit shipment ${shipment.awb}`).click();
  const dialog = page.getByRole("dialog", {
    name: `Edit shipment ${shipment.awb}`,
  });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(/Locked route: Lagos/)).toBeVisible();
  await dialog.getByLabel("Customer reference").fill("WEB-001-CORRECTED");
  await dialog
    .getByLabel("Correction reason")
    .fill("Corrected recipient contact details");
  await dialog.locator("#recipient-correction-phone").fill("08039990000");
  await dialog
    .locator("#recipient-correction-line1")
    .fill("21 Corrected Gimbiya Street");
  await dialog.getByRole("button", { name: "Save correction" }).click();
  await expect(page.getByText("Shipment corrected")).toBeVisible();
  expect(correction).toMatchObject({
    expectedVersion: 3,
    reason: "Corrected recipient contact details",
    referenceNumber: "WEB-001-CORRECTED",
    recipient: {
      phone: "08039990000",
      line1: "21 Corrected Gimbiya Street",
    },
  });
  await expect(page.getByRole("heading", { name: shipment.awb })).toBeVisible();
});
