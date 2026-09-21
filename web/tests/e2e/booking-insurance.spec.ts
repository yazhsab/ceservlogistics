import { expect, test, type Page } from "@playwright/test";
import type { BookingRequest } from "../../src/api/client";
import type { components } from "../../src/api/schema";
import { allPermissions, installMockApi, shipment } from "./mockApi";

const insuredPreview: components["schemas"]["BookingPreview"] = {
  serviceability: {
    serviceable: true,
    serviceName: "Express Air",
    origin: { countryCode: "NG", pincode: "100001" },
    destination: { countryCode: "NG", pincode: "900001" },
  },
  declaredValueMinor: 5000000,
  quote: {
    currency: "NGN",
    totalMinor: 135488,
    lineItems: [
      {
        kind: "FREIGHT",
        code: "FREIGHT",
        label: "Transportation",
        amountMinor: 10488,
      },
      {
        kind: "SURCHARGE",
        code: "COVER",
        label: "Shipment insurance",
        amountMinor: 125000,
        explanation: "2.50% on declared value of NGN 50000.00 = NGN 1250.00",
      },
    ],
    insurance: {
      policyVersion: "rcv_configured",
      rateBp: 250,
      currency: "NGN",
      declaredValueMinor: 5000000,
      premiumMinor: 125000,
      rules: [
        {
          ruleId: "sur_cover",
          code: "COVER",
          name: "Shipment insurance",
          calcType: "PERCENTAGE",
          appliesTo: "DECLARED_VALUE",
          rateBp: 250,
          isTaxable: false,
          basisMinor: 5000000,
          premiumMinor: 125000,
          explanation: "2.50% on declared value of NGN 50000.00 = NGN 1250.00",
        },
      ],
      quoteFingerprint: "a".repeat(64),
    },
  },
  commercial: {
    insurance: { status: "AWAITING_ACCEPTANCE" },
    billing: {
      transportation: { party: "SHIPPER" },
      dutyTax: { party: "RECEIVER" },
    },
  },
};

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Work email").fill("operator@ceserve.test");
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in securely" }).click();
  await expect(page.getByRole("heading", { name: "Shipments" })).toBeVisible();
}

async function fillBooking(page: Page) {
  await page.goto("/shipments/new");
  await page
    .getByPlaceholder("Search customer code, name, email, or phone")
    .fill("Acme");
  await page.getByRole("button", { name: /Acme Retail/ }).click();
  for (const [prefix, name, city, state, postalCode] of [
    ["sender", "Chiamaka Okafor", "Lagos", "Lagos", "100001"],
    ["recipient", "Amina Bello", "Abuja", "FCT", "900001"],
  ]) {
    const address = page.locator(`#${prefix}`);
    await address.getByLabel("Contact name").fill(name!);
    await address.getByLabel("Phone").fill("08031234567");
    await address.getByLabel("Address line 1").fill("12 Example Road");
    await address.getByLabel("Postal code").fill(postalCode!);
    await address
      .getByRole("combobox", { name: "State", exact: true })
      .selectOption(state!);
    await address.getByLabel("City / state capital").fill(city!);
  }
  await page.getByLabel("Courier product").selectOption("EXPRESS");
  await page
    .getByLabel("General description of item")
    .fill("Two packaged electronics");
}

function previewButton(page: Page) {
  return page
    .getByRole("button", {
      name: /^(Preview shipment|Refresh shipment preview|Refresh preview)$/,
    })
    .filter({ visible: true });
}

function confirmButton(page: Page) {
  return page
    .getByRole("button", { name: /^Confirm & book/ })
    .filter({ visible: true });
}

test.beforeEach(async ({ page }) => {
  await installMockApi(page);
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
});

test("insurance validates goods value and preserves the server quote and booking intent", async ({
  page,
}) => {
  const quotes: BookingRequest[] = [];
  await page.route("**/api/v1/shipments/preview", async (route) => {
    quotes.push(route.request().postDataJSON() as BookingRequest);
    await route.fulfill({ json: insuredPreview });
  });
  await login(page);
  await fillBooking(page);
  await expect(page.getByLabel("Destination country")).toHaveValue("NG");
  const insurance = page.getByLabel("Customer requests shipment insurance");
  await expect(insurance).not.toBeChecked();
  await insurance.focus();
  await page.keyboard.press("Space");
  await expect(insurance).toBeChecked();
  const declared = page.getByLabel("Total declared goods value (₦)");
  for (const invalid of [
    "",
    "0",
    "-1",
    "100.123",
    "invalid",
    "9007199254740999999",
  ]) {
    await declared.fill(invalid);
    await previewButton(page).click();
    await expect(declared).toHaveAttribute("aria-invalid", "true");
    await expect(declared).toBeFocused();
    await expect(
      page.getByText(
        "Enter a positive declared goods value to request insurance.",
      ),
    ).toBeVisible();
  }
  expect(quotes).toHaveLength(0);
  await declared.fill("50000");
  await previewButton(page).click();
  await expect(
    page.getByText("Requested", { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  await expect(
    page.getByText("₦50,000.00", { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  await expect(
    page.getByText("₦1,250.00", { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  await expect(
    page.getByText("₦1,354.88", { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  expect(quotes[0]).toMatchObject({
    insuranceRequired: true,
    declaredValueMinor: 5000000,
  });
  // Editing the goods value must invalidate the old quote, even while the
  // previous preview is visible. Invalid/oversized values must not crash it.
  await declared.fill("9007199254740999999");
  await expect(confirmButton(page)).toBeDisabled();
  await expect(
    page.getByRole("heading", { name: "Book shipment" }),
  ).toBeVisible();
  await declared.fill("50000");
  await previewButton(page).click();
  await expect(confirmButton(page)).toBeDisabled();
  await page
    .getByLabel(
      "Customer accepts insurance at 2.5% of the declared goods value",
    )
    .filter({ visible: true })
    .check();
  const bookingRequest = page.waitForRequest(
    (request) =>
      request.url().endsWith("/api/v1/shipments") &&
      request.method() === "POST",
  );
  await confirmButton(page).click();
  const request = await bookingRequest;
  const body = request.postDataJSON() as BookingRequest;
  expect(body).toMatchObject({
    insuranceRequired: true,
    insuranceAcceptance: { accepted: true, quoteFingerprint: "a".repeat(64) },
    declaredValueMinor: 5000000,
    sender: { countryCode: "NG", pincode: "100001" },
    recipient: { countryCode: "NG", pincode: "900001" },
  });
  expect(request.headers()["idempotency-key"]).toBeTruthy();
  expect(body).not.toHaveProperty("insurancePremiumMinor");
  expect(body).not.toHaveProperty("totalAmountMinor");
  await expect(page.getByText("Shipment booked successfully")).toBeVisible();
});

test("customs insurance automatically shows the configured server premium", async ({
  page,
}) => {
  const preview = structuredClone(insuredPreview);
  const insurance = {
    ...preview.quote.insurance!,
    rateBp: 100,
    declaredValueMinor: 10000000,
    premiumMinor: 100000,
    rules: preview.quote.insurance!.rules?.map((rule) => ({
      ...rule,
      rateBp: 100,
      basisMinor: 10000000,
      premiumMinor: 100000,
      explanation: "1.00% on declared value of NGN 100000.00 = NGN 1000.00",
    })),
  };
  const customs: components["schemas"]["CustomsSummary"] = {
    declaration: {
      currency: "NGN",
      reasonForExport: "Sale",
      discountMinor: 0,
      freightMinor: 0,
      otherChargesMinor: 0,
      items: [
        {
          description: "Personal effects",
          quantity: 1,
          unitOfMeasure: "pieces",
          unitValueMinor: 10000000,
          countryOfOrigin: "NG",
        },
      ],
    },
    lineTotalsMinor: [10000000],
    goodsSubtotalMinor: 10000000,
    declaredValueMinor: 10000000,
    insuranceMinor: 100000,
    invoiceTotalMinor: 10100000,
  };
  preview.declaredValueMinor = 10000000;
  preview.quote.insurance = insurance;
  preview.commercial = {
    ...preview.commercial,
    insurance: { status: "AWAITING_ACCEPTANCE", quote: insurance },
    customs,
  };
  await page.route("**/api/v1/shipments/customs/preview", (route) =>
    route.fulfill({
      json: {
        currency: "NGN",
        lineTotalsMinor: [10000000],
        goodsSubtotalMinor: 10000000,
        declaredValueMinor: 10000000,
        totalBeforeInsuranceMinor: 10000000,
      },
    }),
  );
  await page.route("**/api/v1/shipments/preview", (route) =>
    route.fulfill({ json: preview }),
  );

  await login(page);
  await fillBooking(page);
  await page.getByLabel("Include customs declaration").check();
  await page.getByLabel("Description of goods").fill("Personal effects");
  await page.getByLabel("Value per unit").fill("100000");
  const automaticPreview = page.waitForRequest("**/api/v1/shipments/preview");
  await page.getByLabel("Customer requests shipment insurance").check();
  const request = await automaticPreview;
  expect(request.postDataJSON()).toMatchObject({
    insuranceRequired: true,
    customs: {
      items: [{ unitValueMinor: 10000000 }],
    },
  });

  const panel = page.locator("#customs");
  await expect(
    panel.getByText("Insurance (1%)", { exact: true }),
  ).toBeVisible();
  await expect(panel.getByText("₦1,000.00", { exact: true })).toBeVisible();
  await expect(panel.getByText(/Awaiting shipment preview/)).toHaveCount(0);
});

test("unchecked insurance remains optional and does not imply customer acceptance", async ({
  page,
}) => {
  await login(page);
  await fillBooking(page);
  const quoteRequest = page.waitForRequest("**/api/v1/shipments/preview");
  await previewButton(page).click();
  expect((await quoteRequest).postDataJSON()).toMatchObject({
    insuranceRequired: false,
  });
  await expect(
    page.getByText("Not requested", { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  const request = page.waitForRequest(
    (candidate) =>
      candidate.url().endsWith("/api/v1/shipments") &&
      candidate.method() === "POST",
  );
  await confirmButton(page).click();
  const body = (await request).postDataJSON() as BookingRequest;
  expect(body.insuranceRequired).toBe(false);
  expect(body).not.toHaveProperty("declaredValueMinor");
  expect(body).not.toHaveProperty("insuranceAcceptance");
});

test("a service that disallows insurance cannot preview an insurance request", async ({
  page,
}) => {
  await page.route("**/api/v1/courier-services?*", (route) =>
    route.fulfill({
      json: {
        data: [
          {
            id: "svc_test_01",
            code: "EXPRESS",
            name: "Express Air",
            status: "ACTIVE",
            insuranceAllowed: false,
          },
        ],
      },
    }),
  );
  const quoteRequests: string[] = [];
  page.on("request", (request) => {
    if (request.url().endsWith("/api/v1/shipments/preview"))
      quoteRequests.push(request.url());
  });
  await login(page);
  await fillBooking(page);
  await page.getByLabel("Total declared goods value (₦)").fill("50000");
  await page.getByLabel("Customer requests shipment insurance").check();
  await previewButton(page).click();
  await expect(
    page
      .getByText(/This courier product does not offer insurance/)
      .filter({ visible: true }),
  ).toBeVisible();
  expect(quoteRequests).toHaveLength(0);
  await expect(confirmButton(page)).toBeDisabled();
});

test("shipment detail represents a request without claiming issued cover", async ({
  page,
}) => {
  await page.route(`**/api/v1/shipments/${shipment.id}`, (route) =>
    route.fulfill({ json: { ...shipment, insuranceRequired: true } }),
  );
  await login(page);
  await page.goto(`/shipments/${shipment.id}`);
  await expect(
    page.getByText("Insurance requested", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Insured", { exact: true })).toHaveCount(0);
});

test("insurance is configurable on a draft rate card with explicit limits and tax", async ({
  page,
}, testInfo) => {
  await page.route("**/api/v1/rate-cards/versions/rcv_insurance", (route) =>
    route.fulfill({
      json: {
        id: "rcv_insurance",
        status: "DRAFT",
        editable: true,
        currency: "NGN",
      },
    }),
  );
  const rules: components["schemas"]["CreateSurchargeRequest"][] = [];
  await page.route(
    "**/api/v1/rate-cards/versions/rcv_insurance/surcharges",
    (route) => {
      rules.push(
        route
          .request()
          .postDataJSON() as components["schemas"]["CreateSurchargeRequest"],
      );
      return route.fulfill({ status: 201, json: { id: "sur_cover" } });
    },
  );
  await login(page);
  await page.goto("/pricing/versions/rcv_insurance");
  await page
    .getByRole("button", { name: "Add surcharge", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "Add surcharge" });
  await dialog.getByLabel(/^Code/).fill("COVER");
  await dialog.getByLabel(/^Name/).fill("Shipment insurance");
  await dialog.getByLabel("Type", { exact: true }).selectOption("INSURANCE");
  await expect(dialog.getByLabel("Calculation")).toHaveValue("PERCENTAGE");
  await expect(dialog.getByLabel("Applies to")).toHaveValue("DECLARED_VALUE");
  await expect(dialog.getByLabel("Percentage")).toHaveValue("");
  await dialog.getByLabel("Percentage").fill("2.5");
  await dialog.getByLabel("Service code").fill("EXPRESS");
  await dialog.getByLabel("Taxable").uncheck();
  await dialog.getByLabel("Minimum charge").fill("100");
  await dialog.getByLabel("Maximum charge").fill("50");
  await dialog
    .getByRole("button", { name: "Add surcharge", exact: true })
    .click();
  await expect(
    dialog.getByText("Maximum cannot be lower than minimum."),
  ).toBeVisible();
  expect(rules).toHaveLength(0);
  await dialog.getByLabel("Maximum charge").fill("5000");
  await dialog.screenshot({
    path: testInfo.outputPath("insurance-configuration.png"),
  });
  await dialog
    .getByRole("button", { name: "Add surcharge", exact: true })
    .click();
  await expect(
    page.getByText("Surcharge added", { exact: true }),
  ).toBeVisible();
  expect(rules).toEqual([
    expect.objectContaining({
      code: "COVER",
      surchargeType: "INSURANCE",
      calcType: "PERCENTAGE",
      percentageBp: 250,
      appliesTo: "DECLARED_VALUE",
      serviceCode: "EXPRESS",
      minAmountMinor: 10000,
      maxAmountMinor: 500000,
      isTaxable: false,
      conditions: { requiresInsurance: true },
    }),
  ]);
});

test("missing insurance configuration displays the server error and prevents booking", async ({
  page,
}) => {
  await page.route("**/api/v1/shipments/preview", (route) =>
    route.fulfill({
      status: 409,
      json: {
        error: {
          code: "INSURANCE_RATE_NOT_CONFIGURED",
          message:
            "No insurance rate applies to this shipment. Configure an INSURANCE surcharge on the applicable rate-card version before requesting insurance.",
        },
      },
    }),
  );
  await login(page);
  await fillBooking(page);
  await page.getByLabel("Total declared goods value (₦)").fill("50000");
  await page.getByLabel("Customer requests shipment insurance").check();
  await previewButton(page).click();
  await expect(
    page
      .getByText(/No insurance rate applies to this shipment/)
      .filter({ visible: true }),
  ).toBeVisible();
  await expect(confirmButton(page)).toBeDisabled();
  await expect(page.getByLabel(/^Customer accepts insurance/)).toHaveCount(0);
});

test("fixed premiums display the quoted amount without inventing a percentage", async ({
  page,
}) => {
  const fixed = structuredClone(insuredPreview);
  const insurance = fixed.quote.insurance!;
  delete insurance.rateBp;
  insurance.rules = [
    {
      ruleId: "sur_fixed",
      code: "COVER",
      name: "Fixed insurance",
      calcType: "FIXED",
      appliesTo: "DECLARED_VALUE",
      valueMinor: 125000,
      isTaxable: false,
      basisMinor: 5000000,
      premiumMinor: 125000,
      explanation: "Fixed insurance premium of NGN 1250.00",
    },
  ];
  await page.route("**/api/v1/shipments/preview", (route) =>
    route.fulfill({ json: fixed }),
  );
  await login(page);
  await fillBooking(page);
  await page.getByLabel("Total declared goods value (₦)").fill("50000");
  await page.getByLabel("Customer requests shipment insurance").check();
  await previewButton(page).click();
  await page
    .getByLabel("Customer accepts the quoted insurance premium of ₦1,250.00")
    .filter({ visible: true })
    .check();
  await expect(confirmButton(page)).toBeEnabled();
  await expect(
    page
      .getByText(/Fixed insurance premium of NGN 1250.00/)
      .filter({ visible: true }),
  ).toBeVisible();
});

test("booking value, country and insurance controls remain usable @responsive", async ({
  page,
}, testInfo) => {
  await page.route("**/api/v1/shipments/preview", (route) =>
    route.fulfill({ json: insuredPreview }),
  );
  await login(page);
  await fillBooking(page);
  await page.getByLabel("Total declared goods value (₦)").fill("50000");
  await page.getByLabel("Customer requests shipment insurance").check();
  await page.locator("#payment").scrollIntoViewIfNeeded();
  await page.locator("#payment").screenshot({
    path: testInfo.outputPath("booking-goods-value-insurance.png"),
  });
  await previewButton(page).click();
  await page
    .getByLabel(
      "Customer accepts insurance at 2.5% of the declared goods value",
    )
    .filter({ visible: true })
    .check();
  await expect(confirmButton(page)).toBeEnabled();
  await expect(
    page.getByText("NG · 900001").filter({ visible: true }),
  ).toBeVisible();
  const review = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "7. Shipment preview" }) })
    .filter({ visible: true });
  await review.screenshot({ path: testInfo.outputPath("booking-preview.png") });
  await page
    .getByText("Shipment charges total", { exact: true })
    .filter({ visible: true })
    .scrollIntoViewIfNeeded();
  await page.screenshot({
    path: testInfo.outputPath("booking-preview-viewport.png"),
  });
  const dimensions = await page.evaluate(() => ({
    width: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }));
  expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.width);
});

test("customs goods, independent billing parties and UK postcode reach the booking API @responsive", async ({
  page,
}, testInfo) => {
  await page.route("**/api/v1/geography/states?country=GB", (route) =>
    route.fulfill({
      json: { data: [{ id: "state-essex", code: "ESS", name: "Essex" }] },
    }),
  );
  const customs: components["schemas"]["CustomsSummary"] = {
    declaration: {
      currency: "NGN",
      reasonForExport: "Sale",
      invoiceNumber: "INV-100",
      declarationStatement: "The contents and values are correct.",
      discountMinor: 50000,
      freightMinor: 10000,
      otherChargesMinor: 2000,
      items: [
        {
          description: "Cotton shirts",
          quantity: 2,
          unitOfMeasure: "pieces",
          unitValueMinor: 250000,
          countryOfOrigin: "NG",
          hsCode: "610910",
        },
      ],
    },
    lineTotalsMinor: [500000],
    goodsSubtotalMinor: 500000,
    declaredValueMinor: 450000,
    insuranceMinor: 0,
    invoiceTotalMinor: 462000,
  };
  const billing: components["schemas"]["BillingInstructions"] = {
    transportation: { party: "RECEIVER" },
    dutyTax: { party: "THIRD_PARTY", customerId: "cus_test_01" },
  };
  await page.route("**/api/v1/shipments/preview", (route) =>
    route.fulfill({
      json: {
        serviceability: {
          serviceable: true,
          serviceName: "Express Air",
          origin: { countryCode: "NG", pincode: "100001" },
          destination: { countryCode: "GB", pincode: "SS0 7JJ" },
        },
        quote: {
          currency: "NGN",
          totalMinor: 10488,
          lineItems: shipment.charges.lineItems,
        },
        commercial: {
          insurance: { status: "NOT_REQUESTED" },
          customs,
          billing,
        },
        declaredValueMinor: 450000,
      },
    }),
  );
  await login(page);
  await fillBooking(page);
  await page.getByLabel("Destination country").selectOption("GB");
  const recipient = page.locator("#recipient");
  await expect(recipient.getByLabel("Postal code")).toHaveValue("");
  await recipient.getByLabel("Postal code").fill("SS0 7JJ");
  await recipient
    .getByRole("combobox", { name: "State", exact: true })
    .selectOption("Essex");
  await recipient.getByLabel("City / state capital").fill("Southend-on-Sea");
  await page.getByLabel("Bill transportation to").selectOption("RECEIVER");
  await page.getByLabel("Bill duty and tax to").selectOption("THIRD_PARTY");
  await page.getByLabel("Duty and tax billing account").fill("Acme");
  await page.getByRole("button", { name: /Acme Retail/ }).click();
  await page.getByLabel("Include customs declaration").check();
  await page.getByLabel("Invoice number").fill("INV-100");
  await page.getByLabel("Description of goods").fill("Cotton shirts");
  await page.getByLabel("Quantity").fill("2");
  await page.getByLabel("Value per unit").fill("2500");
  await page.getByLabel("HS / tariff code").fill("610910");
  await page.getByLabel("Goods discount").fill("500");
  await page.getByLabel("Customs freight charge").fill("100");
  await page.getByLabel("Other customs charges").fill("20");
  await page
    .getByLabel("Declaration statement")
    .fill("The contents and values are correct.");
  await page
    .locator("#customs")
    .screenshot({ path: testInfo.outputPath("customs-form.png") });
  const previewRequest = page.waitForRequest("**/api/v1/shipments/preview");
  await previewButton(page).click();
  const submittedPreview = (
    await previewRequest
  ).postDataJSON() as BookingRequest;
  expect(submittedPreview.customs).toMatchObject(customs.declaration);
  expect(submittedPreview).not.toHaveProperty("declaredValueMinor");
  await expect(
    page.getByText("₦4,620.00").filter({ visible: true }),
  ).toBeVisible();
  await expect(
    page.getByText("GB · SS0 7JJ").filter({ visible: true }),
  ).toBeVisible();
  const review = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "7. Shipment preview" }) })
    .filter({ visible: true });
  await review.screenshot({ path: testInfo.outputPath("customs-review.png") });
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth <=
        document.documentElement.clientWidth,
    ),
  ).toBe(true);
  const posted = page.waitForRequest(
    (r) => r.url().endsWith("/api/v1/shipments") && r.method() === "POST",
  );
  await confirmButton(page).click();
  expect((await posted).postDataJSON()).toMatchObject({
    recipient: { countryCode: "GB", pincode: "SS0 7JJ" },
    billing,
    customs: customs.declaration,
  });
});

test("a changed insurance quote requires new acceptance before any booking request", async ({
  page,
}) => {
  let previews = 0;
  let bookings = 0;
  page.on("request", (r) => {
    if (r.url().endsWith("/api/v1/shipments") && r.method() === "POST")
      bookings++;
  });
  await page.route("**/api/v1/shipments/preview", (route) => {
    previews++;
    return route.fulfill({
      json:
        previews === 1
          ? insuredPreview
          : {
              ...insuredPreview,
              quote: {
                ...insuredPreview.quote,
                totalMinor: 197988,
                lineItems: insuredPreview.quote.lineItems?.map((line) =>
                  line.code === "COVER"
                    ? { ...line, amountMinor: 187500 }
                    : line,
                ),
                insurance: {
                  ...insuredPreview.quote.insurance,
                  policyVersion: "rcv_revised",
                  rateBp: 375,
                  premiumMinor: 187500,
                  rules: insuredPreview.quote.insurance?.rules?.map((rule) => ({
                    ...rule,
                    rateBp: 375,
                    premiumMinor: 187500,
                    explanation:
                      "3.75% on declared value of NGN 50000.00 = NGN 1875.00",
                  })),
                  quoteFingerprint: "b".repeat(64),
                },
              },
            },
    });
  });
  await login(page);
  await fillBooking(page);
  await page.getByLabel("Total declared goods value (₦)").fill("50000");
  await page.getByLabel("Customer requests shipment insurance").check();
  await previewButton(page).click();
  const acceptance = page
    .getByLabel(/^Customer accepts insurance/)
    .filter({ visible: true });
  await acceptance.check();
  await confirmButton(page).click();
  await expect(
    page.getByText(/The price changed/).filter({ visible: true }),
  ).toBeVisible();
  await expect(acceptance).not.toBeChecked();
  await expect(
    page
      .getByLabel(
        "Customer accepts insurance at 3.75% of the declared goods value",
      )
      .filter({ visible: true }),
  ).toBeVisible();
  await expect(confirmButton(page)).toBeDisabled();
  expect(bookings).toBe(0);
  await acceptance.check();
  await confirmButton(page).click();
  await expect(page.getByText("Shipment booked successfully")).toBeVisible();
  expect(bookings).toBe(1);
});

test("shipment detail displays the saved insurance decision and customs goods", async ({
  page,
}, testInfo) => {
  await page.route(`**/api/v1/shipments/${shipment.id}`, (route) =>
    route.fulfill({
      json: {
        ...shipment,
        insuranceRequired: true,
        commercial: {
          insurance: {
            status: "ACCEPTED",
            quote: insuredPreview.quote.insurance,
            recordedAt: "2026-09-16T10:00:00Z",
            recordedBy: "usr_test_01",
            actorType: "USER",
          },
          billing: {
            transportation: { party: "THIRD_PARTY", customerId: "cus_test_01" },
            dutyTax: { party: "SHIPPER" },
          },
          customs: {
            declaration: {
              currency: "NGN",
              reasonForExport: "Sale",
              termsOfSale: "DAP",
              invoiceNumber: "INV-101",
              declarationStatement:
                "All goods and values are accurately declared.",
              discountMinor: 0,
              freightMinor: 0,
              otherChargesMinor: 0,
              items: [
                {
                  description: "Packaged electronics",
                  quantity: 2,
                  unitOfMeasure: "pieces",
                  unitValueMinor: 2500000,
                  countryOfOrigin: "NG",
                  hsCode: "847130",
                },
              ],
            },
            lineTotalsMinor: [5000000],
            goodsSubtotalMinor: 5000000,
            declaredValueMinor: 5000000,
            insuranceMinor: 125000,
            invoiceTotalMinor: 5125000,
          },
        },
      },
    }),
  );
  await login(page);
  await page.goto(`/shipments/${shipment.id}`);
  const panel = page.locator("section").filter({
    has: page.getByRole("heading", { name: "Insurance, billing & customs" }),
  });
  await expect(panel.getByText("Accepted", { exact: true })).toBeVisible();
  await expect(panel.getByText("₦51,250.00", { exact: true })).toBeVisible();
  await expect(
    panel.getByRole("cell", { name: "Packaged electronics" }),
  ).toBeVisible();
  await expect(panel.getByRole("cell", { name: "847130" })).toBeVisible();
  await panel.screenshot({
    path: testInfo.outputPath("commercial-detail.png"),
  });
});

test("booking can create and select a customer without losing entered addresses @responsive", async ({
  page,
}, testInfo) => {
  await login(page);
  await page.goto("/shipments/new");
  await page.locator("#sender-line1").fill("42 Training Avenue");
  await page.getByRole("button", { name: "Add customer", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Create customer" });
  await dialog.getByLabel("Customer name").fill("New Customer");
  await dialog.getByLabel("Phone").fill("08000000000");
  await dialog
    .getByRole("button", { name: "Create customer", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  await expect(page).toHaveURL(/\/shipments\/new$/);
  await expect(
    page.getByPlaceholder("Search customer code, name, email, or phone"),
  ).toHaveValue("CUS-002 · New Customer");
  await expect(page.locator("#sender-line1")).toHaveValue("42 Training Avenue");
  await expect(page.getByText("CUS-002", { exact: true })).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("booking-new-customer.png"),
    fullPage: false,
  });
});

test("booking explains missing customer permission and does not offer creation", async ({
  page,
}) => {
  await installMockApi(page, {
    permissions: allPermissions.filter((p) => p !== "customer.create"),
  });
  await login(page);
  await page.goto("/shipments/new");
  await expect(page.getByRole("button", { name: "Add customer" })).toHaveCount(
    0,
  );
  await expect(
    page.getByText(/Your role can select existing customers/),
  ).toBeVisible();
});

test("customer lookup failure is distinguished from no matching customers", async ({
  page,
}) => {
  await page.route("**/api/v1/customers?*", (route) =>
    route.fulfill({
      status: 503,
      json: {
        error: {
          code: "SERVICE_UNAVAILABLE",
          message: "Customer search is temporarily unavailable.",
        },
      },
    }),
  );
  await login(page);
  await page.goto("/shipments/new");
  await page
    .getByPlaceholder("Search customer code, name, email, or phone")
    .fill("Acme");
  await expect(page.getByText(/Customer search failed:/)).toBeVisible();
  await expect(page.getByText("No active customer found.")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Retry search" }),
  ).toBeVisible();
});

test("automatic customs valuation survives postal errors and clears stale totals @responsive", async ({
  page,
}, testInfo) => {
  let failValuation = false;
  const bodies: components["schemas"]["CustomsDeclaration"][] = [];
  await page.route("**/api/v1/shipments/customs/preview", async (route) => {
    bodies.push(
      route
        .request()
        .postDataJSON() as components["schemas"]["CustomsDeclaration"],
    );
    if (failValuation)
      return route.fulfill({
        status: 422,
        json: {
          error: {
            code: "VALIDATION_FAILED",
            message: "Discount cannot exceed the goods subtotal.",
          },
        },
      });
    return route.fulfill({
      json: {
        currency: "NGN",
        lineTotalsMinor: [500000],
        goodsSubtotalMinor: 500000,
        declaredValueMinor: 450000,
        totalBeforeInsuranceMinor: 462000,
      },
    });
  });
  await page.route("**/api/v1/shipments/preview", (route) =>
    route.fulfill({
      status: 422,
      json: {
        error: {
          code: "VALIDATION_FAILED",
          message: "The sender postal code 999999 is not configured for NG.",
          details: {
            field: "sender.pincode",
            countryCode: "NG",
            postalCode: "999999",
          },
        },
      },
    }),
  );
  await login(page);
  await fillBooking(page);
  await page.locator("#sender-pincode").fill("999999");
  await page.getByLabel("Customer requests shipment insurance").check();
  await page.getByLabel("Include customs declaration").check();
  await page.getByLabel("Description of goods").fill("Cotton shirts");
  await page.getByLabel("Quantity").fill("2");
  await page.getByLabel("Value per unit").fill("2500");
  await page.getByLabel("Goods discount", { exact: true }).fill("500");
  await page.getByLabel("Customs freight charge").fill("100");
  await page.getByLabel("Other customs charges").fill("20");
  const valuation = page.getByRole("region", { name: "Customs valuation" });
  await expect(valuation.getByText("₦4,620.00")).toBeVisible();
  await expect(page.getByLabel("Goods line 1 total")).toHaveText("₦5,000.00");
  await expect(valuation.getByText("Awaiting shipment preview")).toBeVisible();
  expect(bodies.at(-1)).toMatchObject({
    currency: "NGN",
    discountMinor: 50000,
    freightMinor: 10000,
    otherChargesMinor: 2000,
    items: [{ quantity: 2, unitValueMinor: 250000 }],
  });
  await previewButton(page).click();
  const addressButton = page
    .getByRole("button", { name: "Check sender address" })
    .filter({ visible: true });
  await expect(addressButton).toBeVisible();
  await addressButton.click();
  await expect(page.locator("#sender-pincode")).toBeFocused();
  await expect(page.locator("#sender-pincode")).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  await valuation.scrollIntoViewIfNeeded();
  await expect(valuation.getByText("₦4,620.00")).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("automatic-customs.png"),
    fullPage: false,
  });
  failValuation = true;
  await page.getByLabel("Goods discount", { exact: true }).fill("6000");
  await expect(valuation.getByText("₦4,620.00")).toHaveCount(0);
  await expect(valuation.getByText(/Discount cannot exceed/)).toBeVisible();
  await expect(page.getByLabel("Goods line 1 total")).toHaveText("—");
  await page.getByLabel("Quantity").fill("");
  await expect(valuation.getByText(/Complete the goods lines/)).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});
