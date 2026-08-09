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

test("API credential creation reveals the token exactly once", async ({
  page,
}) => {
  await login(page);
  await page.goto("/admin/integrations/api-keys");
  await expect(page.getByText("Warehouse platform")).toBeVisible();
  await page.getByRole("button", { name: "Issue credential" }).click();
  await page.getByLabel("Credential name").fill("Returns platform production");
  await page.getByLabel("Grant shipment:create").click();
  await page.getByLabel("Grant tracking:read").click();
  await page.getByLabel("Allowed CIDRs").fill("203.0.113.0/24");
  await page.getByRole("button", { name: "Issue once" }).click();

  const secret = page.getByRole("dialog", { name: "API credential issued" });
  await expect(secret).toContainText(
    "Copy this now; it will not be shown again.",
  );
  await expect(secret.getByLabel("Authorization token")).toHaveValue(
    "key_live_csv_new.one-time-secret-value",
  );
  await secret
    .getByRole("button", { name: "I have stored it securely" })
    .click();
  await expect(page.getByText("Returns platform production")).toBeVisible();
});

test("API credential usage shows request, error, status, and latency evidence", async ({
  page,
}) => {
  await login(page);
  await page.goto("/admin/integrations/api-keys");
  await page.getByRole("button", { name: "Usage" }).click();
  const usage = page.getByRole("dialog", { name: "Warehouse platform usage" });
  await expect(usage).toContainText("1,842");
  await expect(
    usage.getByRole("table", { name: "Recent API calls" }),
  ).toContainText("VALIDATION_FAILED");
  await expect(usage).toContainText("HTTP 422");
  await expect(usage).toContainText("210 ms");
});

test("webhook setup validates HTTPS and reveals its signing secret once", async ({
  page,
}) => {
  await login(page);
  await page.goto("/admin/integrations/webhooks");
  await expect(page.getByText("Order platform production")).toBeVisible();
  await page.getByRole("button", { name: "Register endpoint" }).click();
  await page.getByLabel("Endpoint name").fill("Returns event receiver");
  await page.getByLabel("HTTPS URL").fill("http://unsafe.example.com/events");
  await page.getByLabel("Subscribe to shipment.delivered").click();
  await page.getByRole("button", { name: "Register endpoint" }).last().click();
  await expect(page.getByText("Webhook URLs must use HTTPS")).toBeVisible();
  await page
    .getByLabel("HTTPS URL")
    .fill("https://returns.example.com/ceserve/events");
  await page.getByRole("button", { name: "Register endpoint" }).last().click();

  const secret = page.getByRole("dialog", {
    name: "Webhook endpoint registered",
  });
  await expect(secret.getByLabel("Signing secret")).toHaveValue(
    "whsec_one_time_signing_secret",
  );
  await expect(secret).toContainText("Webhook-Signature");
  await expect(secret).toContainText("Webhook-Timestamp");
});

test("webhook delivery detail preserves attempt evidence and queues replay", async ({
  page,
}) => {
  await login(page);
  await page.goto("/admin/integrations/webhooks/deliveries");
  await page.getByRole("link", { name: "shipment.delivered" }).click();
  await expect(
    page.getByRole("heading", { name: "shipment.delivered" }),
  ).toBeVisible();
  await expect(page.getByText(shipment.awb)).toBeVisible();
  const attempts = page.getByRole("table", {
    name: "Webhook attempt history",
  });
  await expect(attempts).toContainText("HTTP 503");
  await expect(attempts).toContainText("10000 ms");
  await page.getByRole("button", { name: "Replay delivery" }).click();
  const confirmation = page.getByRole("dialog", {
    name: "Queue a webhook replay?",
  });
  await expect(confirmation).toContainText("exact same payload and Webhook-Id");
  await confirmation.getByRole("button", { name: "Queue replay" }).click();
  await expect(page.getByText("Replay queued")).toBeVisible();
});

test("hub terminal mode is full-screen and keyboard navigable", async ({
  page,
}) => {
  await login(page);
  await page.goto("/operations/hub");
  await page.getByLabel("Hub facility ID").fill("ou_lag_01");
  await page.getByRole("button", { name: "Terminal mode" }).click();
  await expect(
    page.getByRole("heading", { name: "Hub / branch terminal" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: /Alt 2 Sort/ })).toBeVisible();
  await page.keyboard.press("Alt+2");
  await expect(page.getByText("Scanner-assisted sorting")).toBeVisible();
  await page.keyboard.press("Alt+1");
  await expect(
    page.getByRole("heading", { name: "Inbound manifests", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("MAN-ABJ-LAG-0042")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("heading", { name: "Hub console" }),
  ).toBeVisible();
});

test("integration navigation and routes are hidden without permissions", async ({
  page,
}) => {
  await page.unroute("**/api/v1/**");
  await installMockApi(page, {
    permissions: allPermissions.filter(
      (permission) =>
        !permission.startsWith("apikey.") && !permission.startsWith("webhook."),
    ),
  });
  await login(page);
  await expect(page.getByRole("link", { name: "API credentials" })).toHaveCount(
    0,
  );
  await expect(
    page.getByRole("link", { name: "Webhook endpoints" }),
  ).toHaveCount(0);
  await page.goto("/admin/integrations/api-keys");
  await expect(page.getByText(/do not have permission/i)).toBeVisible();
});

test("notification template creation validates variables through server preview", async ({
  page,
}) => {
  await login(page);
  await page.goto("/admin/notifications/templates");
  await expect(page.getByText("Out for delivery · SMS")).toBeVisible();
  await page.getByRole("button", { name: "New template" }).first().click();
  await page.getByLabel("Code").fill("SHIPMENT_DELIVERED_SMS_EN_NG");
  await page.getByLabel("Name").fill("Delivered · SMS · Nigeria");
  await page.getByLabel("Event trigger").selectOption("SHIPMENT_DELIVERED");
  await page.getByLabel("Locale").fill("en-NG");
  await page
    .getByLabel("Body")
    .fill("Shipment {{awb}} was delivered to {{recipientName}}.");
  await page.getByRole("button", { name: "Create and validate" }).click();
  const preview = page.getByRole("dialog", {
    name: "Preview · Delivered · SMS · Nigeria",
  });
  await preview.getByLabel("{{awb}}").fill("CSV260809000101");
  await preview.getByLabel("{{recipientName}}").fill("Amina Bello");
  await preview.getByRole("button", { name: "Render preview" }).click();
  await expect(preview).toContainText("All variables resolved");
  await expect(preview).toContainText(
    "Shipment CSV260809000101 was delivered to Amina Bello.",
  );
});

test("notification delivery detail shows provider attempt evidence", async ({
  page,
}) => {
  await login(page);
  await page.goto("/admin/notifications/deliveries");
  await page.getByRole("link", { name: "Inspect" }).click();
  const attempts = page.getByRole("table", {
    name: "Notification provider attempt history",
  });
  await expect(attempts).toContainText("Termii");
  await expect(attempts).toContainText("10000 ms");
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
});

test("customer portal redirects by subject and tracks a customer-safe journey", async ({
  page,
}) => {
  await page.unroute("**/api/v1/**");
  await installMockApi(page, {
    permissions: ["portal.customer"],
    user: {
      email: "customer@lagos.test",
      fullName: "Nneka Okafor",
      roles: ["CUSTOMER"],
      portal: {
        isCustomerUser: true,
        customers: [
          {
            id: "cus_test_01",
            code: "LAGRETAIL",
            name: "Lagos Retail Limited",
          },
        ],
        franchise: null,
      },
      hasOrganizationWideAccess: false,
    },
  });
  await page.goto("/login");
  await page.getByLabel("Work email").fill("customer@lagos.test");
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();
  await expect(
    page.getByRole("heading", { name: "Good to see you" }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Shipments" }).click();
  await page.getByRole("link", { name: /Track CSV260809000101/ }).click();
  await expect(page.getByRole("heading", { name: "On the way" })).toBeVisible();
  await expect(
    page.getByText("Your shipment is travelling to Abuja."),
  ).toBeVisible();
  await expect(page.getByText("Internal facility")).toHaveCount(0);
});

test("customer portal presents invoice payment state and NGN amounts", async ({
  page,
}) => {
  await page.unroute("**/api/v1/**");
  await installMockApi(page, {
    permissions: ["portal.customer"],
    user: {
      email: "customer@lagos.test",
      fullName: "Nneka Okafor",
      roles: ["CUSTOMER"],
      portal: {
        isCustomerUser: true,
        customers: [
          {
            id: "cus_test_01",
            code: "LAGRETAIL",
            name: "Lagos Retail Limited",
          },
        ],
        franchise: null,
      },
      hasOrganizationWideAccess: false,
    },
  });
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  await page.getByLabel("Work email").fill("customer@lagos.test");
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();
  await page.getByRole("link", { name: "Invoices" }).click();
  const invoices = page.getByRole("table", { name: "Customer invoices" });
  await expect(invoices).toContainText("INV/NG/2026/00142");
  await expect(invoices).toContainText("Overdue");
  await expect(invoices).toContainText("₦5,450.00");
});

test("customer portal dashboard fits a mobile viewport @mobile", async ({
  page,
}) => {
  await page.unroute("**/api/v1/**");
  await installMockApi(page, {
    permissions: ["portal.customer"],
    user: {
      email: "customer@lagos.test",
      fullName: "Nneka Okafor",
      roles: ["CUSTOMER"],
      portal: {
        isCustomerUser: true,
        customers: [
          {
            id: "cus_test_01",
            code: "LAGRETAIL",
            name: "Lagos Retail Limited",
          },
        ],
        franchise: null,
      },
      hasOrganizationWideAccess: false,
    },
  });
  await page.goto("/login");
  await page.getByLabel("Work email").fill("customer@lagos.test");
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();

  await expect(
    page.getByRole("heading", { name: "Good to see you" }),
  ).toBeVisible();
  await expect(
    page.getByRole("navigation", { name: "Customer portal" }),
  ).toBeVisible();
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(await page.evaluate(() => window.innerWidth));
  const trackLink = page.getByRole("link", { name: "Track" }).first();
  expect((await trackLink.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(
    40,
  );
});

test("staff permission alone cannot enter the customer portal", async ({
  page,
}) => {
  await login(page);
  await page.goto("/portal/customer");
  await expect(
    page.getByText(/not linked to a customer account/i),
  ).toBeVisible();
});

test("franchise portal exposes role-aware booking and scanning actions", async ({
  page,
}) => {
  await page.unroute("**/api/v1/**");
  await installMockApi(page, {
    user: {
      email: "owner@ikeja.test",
      fullName: "Chidi Eze",
      roles: ["FRANCHISE_OWNER"],
      portal: {
        isCustomerUser: false,
        customers: [],
        franchise: {
          id: "frn_portal_01",
          code: "LAG-IKJ",
          name: "Ikeja Franchise",
          operatingUnitCode: "LAG-IKJ",
        },
      },
      operatingUnitIds: ["ou_lag_01"],
      hasOrganizationWideAccess: false,
    },
  });
  await page.goto("/login");
  await page.getByLabel("Work email").fill("owner@ikeja.test");
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();
  await expect(
    page.getByRole("heading", { name: "Ikeja Franchise" }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Book shipment" }).click();
  await expect(
    page.getByRole("heading", { name: "Book shipment" }),
  ).toBeVisible();
  await page.goto("/portal/franchise");
  await page.getByRole("link", { name: "Scan" }).click();
  await expect(
    page.getByRole("heading", { name: "Scanner console" }),
  ).toBeVisible();
});

test("franchise portal shows COD position and settlement due", async ({
  page,
}) => {
  await page.unroute("**/api/v1/**");
  await installMockApi(page, {
    user: {
      email: "owner@ikeja.test",
      fullName: "Chidi Eze",
      roles: ["FRANCHISE_OWNER"],
      portal: {
        isCustomerUser: false,
        customers: [],
        franchise: {
          id: "frn_portal_01",
          code: "LAG-IKJ",
          name: "Ikeja Franchise",
          operatingUnitCode: "LAG-IKJ",
        },
      },
      operatingUnitIds: ["ou_lag_01"],
      hasOrganizationWideAccess: false,
    },
  });
  await page.goto("/login");
  await page.getByLabel("Work email").fill("owner@ikeja.test");
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();
  await expect(page.getByText("COD outstanding")).toBeVisible();
  await page.getByRole("link", { name: "Finance" }).click();
  const table = page.getByRole("table", { name: "Franchise settlements" });
  await expect(table).toContainText("SET/LAG-IKJ/2026-08-01");
  await expect(page.getByRole("link", { name: "COD" })).toBeVisible();
});

test("command-centre filter changes the authoritative operating scope", async ({
  page,
}) => {
  await login(page);
  await page.goto("/command-centre");
  await expect(
    page.getByRole("heading", { name: "Command centre" }),
  ).toBeVisible();
  await expect(page.getByText("1,280", { exact: true })).toBeVisible();
  await page.getByLabel("Region hub or branch").selectOption({ index: 1 });
  await expect(page.getByText("42", { exact: true }).first()).toBeVisible();
  await expect(
    page.getByText(/exact; transaction-maintained/i).first(),
  ).toBeVisible();
});

test("report creation is asynchronous and completed export downloads", async ({
  page,
}) => {
  await login(page);
  await page.goto("/reports");
  await page
    .getByRole("combobox", { name: "Report", exact: true })
    .selectOption("SLA");
  await page.getByRole("button", { name: "Run in background" }).click();
  await expect(page.getByText("Report queued")).toBeVisible();
  const queuedReceipt = page.getByRole("dialog", { name: "Sla" });
  await expect(queuedReceipt).toContainText("Queued");
  await queuedReceipt.getByRole("button", { name: "Close" }).first().click();
  await expect(page.getByRole("table", { name: "Report runs" })).toContainText(
    "Queued",
  );
  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download" }).first().click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toContain("shipment_volume");
});
