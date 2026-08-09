import { expect, test, type Page } from "@playwright/test";
import { installMockApi, shipment } from "./mockApi";

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

test("pickup assignment commits selected work", async ({ page }) => {
  await login(page);
  await page.goto("/operations/pickups");
  await page.getByLabel("Select pickup PU-260808-001").check();
  await page.getByRole("button", { name: "Assign selected" }).click();
  await page.getByLabel("Agent user ID").fill("usr_agent");
  await page.getByRole("button", { name: "Assign pickups" }).click();
  await expect(page.getByText("1 pickups assigned")).toBeVisible();
});

test("pickup completion is touch-friendly and confirms the outcome @mobile", async ({
  page,
}) => {
  await login(page);
  await page.goto("/operations/pickups/my-stops");
  await expect(page.getByText("Next pickup")).toBeVisible();
  await page.getByRole("button", { name: "Record pickup" }).click();
  await page.getByLabel("Pieces collected").fill("3");
  await page.getByRole("button", { name: "Record outcome" }).click();
  await expect(page.getByText("Pickup Completed")).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("scanner accepts keyboard input, rejects invalid scans, and restores focus", async ({
  page,
}) => {
  await login(page);
  await page.goto("/operations/scanner");
  const scanner = page.getByLabel("Receive scan");
  await expect(scanner).toBeFocused();
  await scanner.fill(shipment.awb);
  await scanner.press("Enter");
  await expect(page.getByText(`Accepted: ${shipment.awb}`)).toBeVisible();
  await expect(scanner).toBeFocused();
  await scanner.fill("BAD-0001");
  await scanner.press("Enter");
  await expect(
    page.getByRole("status").getByText("Barcode was not found"),
  ).toBeVisible();
  await expect(
    page.getByText("Rejected", { exact: true }).first(),
  ).toBeVisible();
  await page.keyboard.press("Alt+4");
  await expect(page.getByLabel("Sort scan")).toBeFocused();
});

test("bag creation and explicit close freeze the declaration", async ({
  page,
}) => {
  await login(page);
  await page.goto("/operations/bags");
  await page.getByRole("button", { name: "Create bag" }).click();
  await page.getByLabel("Destination facility").selectOption("ou_del_01");
  await page.getByRole("button", { name: "Create and scan" }).click();
  await expect(
    page.getByRole("heading", { name: "BAG-BLR-0001" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Review and close" }).click();
  await page.getByLabel("Seal numbers").fill("SEAL-001");
  await page.getByRole("button", { name: "Close bag" }).click();
  await expect(page.getByText("Bag closed and frozen")).toBeVisible();
  await expect(page.getByText("Contents are immutable")).toBeVisible();
});

test("manifest creation, close, and receipt expose immutable movement", async ({
  page,
}) => {
  await login(page);
  await page.goto("/operations/manifests");
  await page.getByRole("button", { name: "Create manifest" }).click();
  await page.getByLabel("Destination").selectOption("ou_del_01");
  await page.getByRole("button", { name: "Create manifest" }).last().click();
  await expect(
    page.getByRole("heading", { name: "MNF-BLR-DEL-0001" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Review and close" }).click();
  await page.getByRole("button", { name: "Close manifest" }).click();
  await expect(page.getByText("Manifest closed")).toBeVisible();
  await page.getByLabel("Current facility").fill("ou_del_01");
  await page.getByRole("button", { name: "Receive manifest" }).click();
  await expect(page.getByText("Manifest received")).toBeVisible();
  await expect(page.getByText("Manifest declaration is frozen")).toBeVisible();
});

test("hub reconciliation scans by Enter and completes a balanced count", async ({
  page,
}) => {
  await login(page);
  await page.goto("/operations/hub/reconciliations/rec_01");
  await page.getByLabel("Current facility").fill("ou_del_01");
  const input = page.getByLabel("Scan counted item");
  await input.fill(shipment.awb);
  await input.press("Enter");
  await expect(
    page.getByText("Matched", { exact: true }).first(),
  ).toBeVisible();
  await expect(input).toBeFocused();
  await page.getByRole("button", { name: "Complete reconciliation" }).click();
  await page.getByRole("button", { name: "Complete count" }).click();
  await expect(
    page.getByText("Completed", { exact: true }).first(),
  ).toBeVisible();
});

test("delivery run creation and successful delivery confirm final state @mobile", async ({
  page,
}) => {
  await login(page);
  await page.goto("/operations/delivery");
  await page.getByRole("button", { name: "Create run" }).click();
  const dialog = page.getByRole("dialog", { name: "Create delivery run" });
  await dialog.getByLabel("Agent user ID").fill("usr_agent");
  await dialog.getByRole("button", { name: "Create run" }).tap();
  await expect(page.getByText("Delivery run created")).toBeVisible();
  await page.goto(`/operations/delivery/runs/dr_01/stops/${shipment.awb}`);
  await page.getByLabel("Recipient name").fill("Ravi Shah");
  await page.getByRole("button", { name: "Confirm delivered" }).click();
  await expect(
    page.getByRole("heading", { name: "Delivery completed" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Submit proof of delivery" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("NDR action and RTO initiation keep the next operation explicit", async ({
  page,
}) => {
  await login(page);
  await page.goto("/operations/ndr/ndr_01");
  await expect(page.getByText("Next required action")).toBeVisible();
  await page.getByRole("button", { name: "Set next action" }).click();
  await page.getByRole("combobox", { name: /^Action/ }).selectOption("RTO");
  await page.getByRole("button", { name: "Set action" }).click();
  await expect(page.getByText("Next action set")).toBeVisible();
  await page.goto("/operations/rto");
  await page.getByRole("button", { name: "Initiate RTO" }).click();
  await page.getByLabel("AWB or barcode").fill(shipment.awb);
  await page.getByLabel("Decision notes").fill("Delivery attempts exhausted");
  await page.getByRole("button", { name: "Initiate RTO" }).last().click();
  await expect(page.getByText("Return initiated")).toBeVisible();
  await expect(page.getByText("Reverse movement")).toBeVisible();
});

test("POD submission and secure viewer preserve append-only evidence", async ({
  page,
}) => {
  await login(page);
  await page.goto(`/operations/pod/new?awb=${shipment.awb}`);
  await page.getByLabel("Recipient name").fill("Ravi Shah");
  await page.getByLabel("Signature image").setInputFiles({
    name: "signature.png",
    mimeType: "image/png",
    buffer: Buffer.from("pod"),
  });
  await page.getByRole("button", { name: "Submit proof of delivery" }).click();
  await expect(page.getByText("Proof of delivery secured")).toBeVisible();
  await expect(page.getByText("Immutable")).toBeVisible();
  await expect(page.getByText("Delivered in good condition")).toBeVisible();
});

test("public tracking is customer-safe, mobile responsive, and handles missing AWBs @mobile", async ({
  page,
}) => {
  await page.goto("/track");
  await page.getByLabel("Air waybill number").fill(shipment.awb);
  await page.getByRole("button", { name: "Track" }).click();
  await expect(page.getByRole("heading", { name: "On the way" })).toBeVisible();
  await expect(page.getByText("Abuja").first()).toBeVisible();
  await expect(page.getByText(/warehouse jargon/i)).toBeVisible();
  await page.getByLabel("Air waybill number").fill("UNKNOWN-AWB");
  await page.getByRole("button", { name: "Track" }).click();
  await expect(page.getByText("Tracking information not found")).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
