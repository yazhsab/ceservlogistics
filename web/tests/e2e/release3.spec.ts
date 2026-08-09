import { expect, test, type Page } from "@playwright/test";
import { allPermissions, installMockApi, shipment } from "./mockApi";

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

test("create commission rule and inspect its effective version", async ({
  page,
}) => {
  await login(page);
  await page.goto("/finance/commission/rules");
  await expect(page.getByText("DEL-GOLD")).toBeVisible();
  await page.getByRole("button", { name: "Create rule" }).click();
  await page.getByLabel("Rule code").fill("DEL-GOLD");
  await page.getByLabel("Rule name").fill("Gold franchise delivery commission");
  await page.getByRole("button", { name: "Create rule" }).last().click();
  await expect(page.getByRole("heading", { name: /DEL-GOLD/ })).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Commission rule versions" }),
  ).toContainText("10.00%");
  await expect(page.getByText("Specificity")).toBeVisible();
});

test("commission simulation explains the selected rule and arithmetic", async ({
  page,
}) => {
  await login(page);
  await page.goto("/finance/commission/simulator");
  await page.getByLabel("Freight (₦)").fill("500");
  await page.getByRole("button", { name: "Run simulation" }).click();
  await expect(page.getByText("Selected rule and calculation")).toBeVisible();
  await expect(page.getByText("DEL-GOLD · v2")).toBeVisible();
  await expect(page.getByText("Apply 10% of basis")).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Commission candidates" }),
  ).toContainText("Selected");
});

test("commission entry links shipment, rule version, and immutable calculation trace", async ({
  page,
}) => {
  await login(page);
  await page.goto("/finance/commission/entries");
  await expect(
    page.getByRole("table", { name: "Commission entries" }),
  ).toContainText(shipment.awb);
  await page.getByRole("link", { name: "ccl_01" }).click();
  await expect(
    page.getByRole("heading", { name: `Commission for ${shipment.awb}` }),
  ).toBeVisible();
  await expect(page.getByText("Apply 10% of basis")).toBeVisible();
  await expect(page.getByText("₦50.00").last()).toBeVisible();
});

test("posted journal is balanced, immutable, and has no edit action", async ({
  page,
}) => {
  await login(page);
  await page.goto("/finance/ledger/journals/jrn_01");
  await expect(
    page.getByRole("heading", { name: "JV-202608-000001" }),
  ).toBeVisible();
  const journal = page.getByRole("table", { name: "Journal entries" });
  await expect(journal).toContainText("Trade Receivable");
  await expect(journal).toContainText("Freight Revenue");
  await expect(journal).toContainText("Balanced");
  await expect(page.getByText("Posted · Immutable")).toBeVisible();
  await expect(page.getByRole("button", { name: /edit/i })).toHaveCount(0);
});

test("account statement uses backend running balance and trial balance self-check", async ({
  page,
}) => {
  await login(page);
  await page.goto("/finance/ledger/accounts/lac_01");
  await expect(
    page.getByRole("table", { name: "Account statement" }),
  ).toContainText("₦678.50");
  await expect(page.getByText(/browser does not accumulate/i)).toBeVisible();
  await page.goto("/finance/ledger/trial-balance");
  await expect(
    page.getByRole("table", { name: "Trial balance" }),
  ).toContainText("BALANCED");
  await expect(
    page.getByRole("table", { name: "Trial balance" }),
  ).toContainText("₦678.50");
});

test("COD custody cross-check and reconciliation preserve accountability", async ({
  page,
}) => {
  await login(page);
  await page.goto("/finance/cod");
  await expect(page.getByText("Currently in custody")).toBeVisible();
  await page.getByRole("button", { name: "Check custody" }).click();
  await page.getByLabel("Party ID").fill("12");
  await page
    .getByRole("button", { name: "Compare custody and ledger" })
    .click();
  await expect(page.getByText("Custody reconciles")).toBeVisible();
  await page
    .getByRole("dialog", { name: "Cross-check COD custody" })
    .getByRole("button", { name: "Close" })
    .last()
    .click();
  await page.getByRole("button", { name: "Reconcile custody" }).click();
  await page.getByLabel("Party ID").fill("12");
  await page.getByLabel("Period start").fill("2026-08-01");
  await page.getByLabel("Period end").fill("2026-08-31");
  await page.getByRole("button", { name: "Open count" }).click();
  await expect(page.getByText(/Expected ₦2,000.00/)).toBeVisible();
  await page.getByLabel("Obligation ID").fill("cod_01");
  await page.getByLabel("Counted amount (₦)").fill("2000");
  await page.getByRole("button", { name: "Record counted line" }).click();
  await page.getByRole("button", { name: "Complete reconciliation" }).click();
  await expect(
    page.getByRole("heading", { name: "COD control centre" }),
  ).toBeVisible();
});

test("settlement view states who owes whom and approval requires confirmation", async ({
  page,
}) => {
  await login(page);
  await page.goto("/finance/settlements/stl_01");
  await expect(
    page.getByRole("heading", { name: "Franchise pays Head Office" }),
  ).toBeVisible();
  await expect(page.getByText("₦400.00").first()).toBeVisible();
  await page.getByRole("button", { name: "Approve" }).click();
  const confirmation = page.getByRole("dialog", {
    name: "Approve and post this settlement?",
  });
  await expect(confirmation).toContainText("Franchise will owe Head Office");
  await confirmation
    .getByRole("button", { name: "Approve settlement" })
    .click();
  await expect(page.getByText("Approved · Read only")).toBeVisible();
});

test("settlement payment keeps inbound direction explicit", async ({
  page,
}) => {
  await login(page);
  await page.goto("/finance/settlements/stl_01");
  await page.getByRole("button", { name: "Approve" }).click();
  await page.getByRole("button", { name: "Approve settlement" }).click();
  await page.getByRole("button", { name: "Record payment" }).click();
  await expect(
    page.getByRole("dialog", { name: "Record settlement payment" }),
  ).toContainText("Franchise pays Head Office");
  await page.getByLabel("Amount (₦)").fill("400");
  await page.getByLabel("Reference").fill("SETTLE-01");
  await page.getByRole("button", { name: "Record payment" }).last().click();
  await expect(page.getByText("Paid · Read only")).toBeVisible();
});

test("invoice shows immutable snapshots, payment state, and unavailable PDF truthfully", async ({
  page,
}) => {
  await login(page);
  await page.goto("/finance/billing/invoices/inv_01");
  await expect(
    page.getByRole("heading", { name: "INV/2026-27/000042" }),
  ).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Invoice lines" }),
  ).toContainText(shipment.awb);
  await expect(page.getByText("Issued · Immutable")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "PDF unavailable" }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Record payment" }).click();
  await page.getByLabel("Amount (₦)").fill("678.50");
  await page.getByLabel("Unique payment reference").fill("PAY-01");
  await page.getByRole("button", { name: "Record payment" }).last().click();
  await expect(page.getByText("Paid · Immutable")).toBeVisible();
});

test("financial route and action permissions deny unauthorized users", async ({
  page,
}) => {
  await page.unroute("**/api/v1/**");
  await installMockApi(page, {
    permissions: allPermissions.filter(
      (permission) => !permission.startsWith("ledger."),
    ),
  });
  await login(page);
  await expect(page.getByRole("link", { name: "Ledger" })).toHaveCount(0);
  await page.goto("/finance/ledger/accounts");
  await expect(page.getByText(/do not have permission/i)).toBeVisible();
});
