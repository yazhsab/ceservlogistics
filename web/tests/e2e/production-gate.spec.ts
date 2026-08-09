import { expect, test, type Page } from "@playwright/test";
import { installMockApi } from "./mockApi";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Work email").fill("operator@ceserve.test");
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();
  await expect(page.getByRole("heading", { name: "Shipments" })).toBeVisible();
}

test.beforeEach(async ({ page }) => {
  await installMockApi(page);
});

test("admin can configure a network facility from contract-backed fields", async ({
  page,
}) => {
  await login(page);
  await page.goto("/network/hubs");
  await expect(
    page.getByRole("heading", { name: "Hubs", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Add hub" }).click();
  const dialog = page.getByRole("dialog", { name: "Add operating unit" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("textbox", { name: "Code", exact: true }).fill("KAN-HUB");
  await dialog.getByRole("textbox", { name: "Name", exact: true }).fill("Kano Regional Hub");
  await dialog.getByRole("textbox", { name: "Address line 1", exact: true }).fill("12 Airport Road");
  await dialog.getByRole("textbox", { name: "Postal code", exact: true }).fill("700001");
  await dialog.getByRole("button", { name: "Create unit" }).click();
  await expect(page.getByText("Operating unit created")).toBeVisible();
});

test("line-haul departure and arrival remain distinct from parcel receipt", async ({
  page,
}) => {
  await login(page);
  await page.goto("/operations/trips/trip_01");
  await expect(
    page.getByRole("heading", { name: "TRIP-LOS-ABV-0001" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Depart trip" }).click();
  await page.getByRole("button", { name: "Confirm depart" }).click();
  await expect(page.getByText("Trip Depart")).toBeVisible();
  await expect(page.getByText("In Transit", { exact: true }).first()).toBeVisible();
  await page.getByRole("button", { name: "Record arrival" }).click();
  await expect(
    page.getByText(/Receive each manifest separately afterward/i),
  ).toBeVisible();
  await page.getByRole("button", { name: "Confirm arrive" }).click();
  await expect(page.getByText("Trip Arrive")).toBeVisible();
  await expect(page.getByText("Arrived", { exact: true }).first()).toBeVisible();
});

test("slow, empty, and retryable list states are explicit", async ({ page }) => {
  await login(page);
  await page.route("**/api/v1/shipments?*", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 400));
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ data: [], pagination: { hasMore: false } }),
    });
  });
  await page.goto("/shipments");
  await expect(page.getByRole("status")).toContainText("Loading shipments");
  await expect(page.getByText("No shipments found")).toBeVisible();
});

test("HTTP conflict and failure responses preserve actionable API messages", async ({
  page,
}) => {
  await login(page);
  const cases = [
    [403, "FORBIDDEN", "Finance visibility is not granted for this account."],
    [404, "NOT_FOUND", "The requested settlement no longer exists."],
    [409, "STALE_SETTLEMENT", "Settlement was recalculated by another approver."],
    [422, "VALIDATION_FAILED", "Period end must follow period start."],
    [429, "RATE_LIMITED", "Wait 30 seconds before running this report again."],
    [500, "INTERNAL_ERROR", "Ledger service is temporarily unavailable."],
  ] as const;

  for (const [status, code, message] of cases) {
    await page.route("**/api/v1/shipments?*", (route) =>
      route.fulfill({
        status,
        contentType: "application/json",
        headers: { "X-Request-Id": `req_${status}` },
        body: JSON.stringify({ error: { code, message } }),
      }),
    );
    await page.goto("/shipments");
    await expect(page.getByRole("alert")).toContainText(message);
    await expect(page.getByText(`Request req_${status}`)).toBeVisible();
    await page.unroute("**/api/v1/shipments?*");
  }
});

test("an invalid session returns to sign-in instead of leaking a protected view", async ({
  page,
}) => {
  await login(page);
  await page.route("**/api/v1/shipments?*", (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({
        error: { code: "TOKEN_INVALID", message: "Session is invalid." },
      }),
    }),
  );
  await page.goto("/shipments");
  await expect(page.getByRole("heading", { name: "Welcome back" })).toBeVisible();
});
