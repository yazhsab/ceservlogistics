import { expect, test, type Page } from "@playwright/test";
import { installMockApi } from "./mockApi";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Work email").fill("operator@ceserve.test");
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();
  await expect(page.getByRole("heading", { name: "Shipments" })).toBeVisible();
}

async function expectNoDocumentOverflow(page: Page) {
  const dimensions = await page.evaluate(() => {
    window.scrollTo(10_000, 0);
    const horizontalWindowScroll = window.scrollX;
    window.scrollTo(0, 0);
    return {
      body: document.body.scrollWidth,
      viewport: document.documentElement.clientWidth,
      horizontalWindowScroll,
    };
  });
  expect(dimensions.body).toBeLessThanOrEqual(dimensions.viewport);
  expect(dimensions.horizontalWindowScroll).toBe(0);
}

test.beforeEach(async ({ page }) => {
  await installMockApi(page);
});

test("internal surfaces contain wide content at every target width @responsive", async ({
  page,
}) => {
  await login(page);
  const pages = [
    ["/admin/users", "Users"],
    ["/operations/scanner", "Scanner console"],
    ["/operations/hub", "Hub console"],
    ["/finance/settlements", "Settlements"],
    ["/command-centre", "Command centre"],
    ["/reports", "Report centre"],
    ["/admin/integrations/api-keys", "API credentials"],
  ] as const;

  for (const [path, heading] of pages) {
    await page.goto(path);
    await expect(page.getByRole("heading", { name: heading })).toBeVisible();
    await expectNoDocumentOverflow(page);
  }
});

test("customer and public surfaces remain usable at every target width @responsive", async ({
  page,
}) => {
  await page.unroute("**/api/v1/**");
  await installMockApi(page, {
    user: {
      email: "customer@acme.test",
      fullName: "Amina Bello",
      roles: ["CUSTOMER_USER"],
      portal: {
        isCustomerUser: true,
        customers: [{ id: "cus_portal_01", code: "ACME", name: "Acme Retail" }],
        franchise: null,
      },
      operatingUnitIds: [],
      hasOrganizationWideAccess: false,
    },
  });
  await page.goto("/login");
  await page.getByLabel("Work email").fill("customer@acme.test");
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();
  await expect(page.getByRole("heading", { name: "Good to see you" })).toBeVisible();
  await expectNoDocumentOverflow(page);

  await page.goto("/portal/customer/shipments");
  await expect(page.getByRole("heading", { name: "Your shipments" })).toBeVisible();
  await expectNoDocumentOverflow(page);

  await page.goto("/track");
  await expect(
    page.getByRole("heading", {
      name: "See where your parcel is, without the warehouse jargon.",
    }),
  ).toBeVisible();
  await expectNoDocumentOverflow(page);
});

test("dialogs stay reachable and trap keyboard focus @responsive", async ({
  page,
}) => {
  await login(page);
  await page.goto("/customers");
  await page.getByRole("button", { name: "Add customer" }).click();
  const dialog = page.getByRole("dialog", { name: "Create customer" });
  await expect(dialog).toBeVisible();
  const box = await dialog.boundingBox();
  const viewport = page.viewportSize();
  expect(box).not.toBeNull();
  expect(viewport).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(viewport!.width);
  expect(box!.y + box!.height).toBeLessThanOrEqual(viewport!.height);

  for (let index = 0; index < 18; index += 1) await page.keyboard.press("Tab");
  await expect(dialog.locator(":focus")).toHaveCount(1);
});
