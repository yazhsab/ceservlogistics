import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiRequest } from "../api/client";
import { CreateCustomerDialog } from "../components/CreateCustomerDialog";
import { CODCollectionForm } from "../components/CODCollectionForm";
import { NDRDetailPage } from "./ExceptionTrackingPages";
import { InvoiceDetailPage } from "./BillingPages";
import { DeliveryStopPage } from "./DeliveryPages";
import { RolesPage } from "./AdminPages";
import { ScannerConsolePage } from "./PickupScannerPages";
import { SettlementDetailPage, SettlementsPage } from "./SettlementPages";
import { TripDetailPage } from "./TripPages";

const qaPermissions = vi.hoisted(() => ({
  allowActions: true,
  denied: new Set<string>(),
}));

vi.mock("../api/client", async (original) => ({
  ...(await original<typeof import("../api/client")>()),
  apiRequest: vi.fn(),
}));
vi.mock("../auth/AuthProvider", () => ({
  useAuth: () => ({
    hasPermission: (permission: string) =>
      qaPermissions.allowActions && !qaPermissions.denied.has(permission),
  }),
}));
// Toasts deliberately do not render: persistent feedback must stand on its own.
vi.mock("../components/ToastProvider", () => ({
  useToast: () => ({ toast: vi.fn() }),
}));

function mount(element: React.ReactNode, path = "/") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>{element}</MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}
beforeEach(() => {
  vi.mocked(apiRequest).mockReset();
  qaPermissions.allowActions = true;
  qaPermissions.denied.clear();
  const values = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
    clear: () => values.clear(),
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("live QA regressions", () => {
  it("keeps a reopened delivered COD stop complete and exposes custody recording", async () => {
    vi.mocked(apiRequest).mockImplementation(async (path) => {
      await Promise.resolve();
      if (String(path).includes("ndr/reasons")) return { data: [] };
      return {
        id: "drn-qa",
        stops: [
          {
            awb: "QA-COD",
            shipmentId: "shp-qa",
            status: "DELIVERED",
            codAmountMinor: 100000,
            codCollectedMinor: 100000,
            currency: "NGN",
          },
        ],
      };
    });
    mount(
      <Routes>
        <Route path="/runs/:runId/stops/:awb" element={<DeliveryStopPage />} />
      </Routes>,
      "/runs/drn-qa/stops/QA-COD",
    );
    expect(
      await screen.findByRole("heading", { name: "Delivery completed" }),
    ).toBeVisible();
    expect(
      screen.getByRole("heading", { name: "Record COD custody" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Confirm delivered" }),
    ).not.toBeInTheDocument();
  });
  it("does not offer RTO to an NDR operator without return permission", async () => {
    const user = userEvent.setup();
    qaPermissions.denied.add("rto.manage");
    vi.mocked(apiRequest).mockResolvedValue({
      id: "ndr-qa",
      caseCode: "QA-NDR",
      availableActions: ["RTO", "CONTACT_REQUIRED"],
      currentReasonCode: "CUSTOMER_NOT_AVAILABLE",
      reasonName: "Customer not available",
    });
    mount(
      <Routes>
        <Route path="/ndr/:caseId" element={<NDRDetailPage />} />
      </Routes>,
      "/ndr/ndr-qa",
    );
    await user.click(
      await screen.findByRole("button", { name: "Set next action" }),
    );
    expect(
      screen.getByRole("option", { name: "CONTACT_REQUIRED" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "RTO" }),
    ).not.toBeInTheDocument();
  });

  it("retries uncertain COD recording with the original amount, details and event key", async () => {
    const user = userEvent.setup();
    vi.mocked(apiRequest)
      .mockRejectedValueOnce(new Error("Connection interrupted"))
      .mockResolvedValueOnce({
        collection: { amountMinor: 100000, currency: "NGN" },
        duplicate: true,
      });
    mount(
      <CODCollectionForm
        shipmentId="shp-qa"
        amountMinor={100000}
        currency="NGN"
      />,
    );
    await user.selectOptions(
      screen.getByRole("combobox", { name: "Collection payment method" }),
      "CARD",
    );
    expect(
      screen.getByRole("button", { name: "Record COD collection" }),
    ).toBeDisabled();
    await user.type(
      screen.getByRole("textbox", { name: "Collection reference" }),
      "QA-CARD",
    );
    await user.click(
      screen.getByRole("button", { name: "Record COD collection" }),
    );
    expect(await screen.findByText("Connection interrupted")).toBeVisible();
    expect(
      screen.getByRole("combobox", { name: "Collection payment method" }),
    ).toBeDisabled();
    await user.click(
      screen.getByRole("button", { name: "Retry COD recording" }),
    );
    expect(await screen.findByText("COD custody recorded")).toBeVisible();
    const calls = vi.mocked(apiRequest).mock.calls;
    expect(calls[0]).toEqual(calls[1]);
    expect(calls[0]?.[1]?.body).toEqual({
      shipmentId: "shp-qa",
      amountMinor: 100000,
      paymentMode: "CARD",
      reference: "QA-CARD",
    });
  });

  it("lets a checker retrieve and issue an existing invoice note", async () => {
    const user = userEvent.setup();
    const note = {
      id: "crn-qa",
      noteNumber: "QA-DRAFT",
      noteType: "CREDIT",
      reason: "QA correction",
      totalMinor: 2500,
      currency: "NGN",
      status: "DRAFT",
    };
    vi.mocked(apiRequest).mockImplementation(async (path, options) => {
      await Promise.resolve();
      if (
        path === "/api/v1/credit-notes/crn-qa/issue" &&
        options?.method === "POST"
      )
        return { ...note, status: "ISSUED" };
      if (String(path).includes("/credit-notes"))
        return { data: [note], pagination: { hasMore: false } };
      return {
        invoice: {
          id: "inv-qa",
          invoiceNumber: "QA-INV",
          status: "ISSUED",
          currency: "NGN",
        },
        lines: [],
      };
    });
    mount(
      <Routes>
        <Route path="/invoices/:invoiceId" element={<InvoiceDetailPage />} />
      </Routes>,
      "/invoices/inv-qa",
    );
    await user.click(
      await screen.findByRole("button", { name: "Review note" }),
    );
    await user.click(
      screen.getByRole("button", { name: "Issue note as checker" }),
    );
    expect(await screen.findByText("Credit note Issued")).toBeVisible();
    expect(apiRequest).toHaveBeenCalledWith(
      "/api/v1/credit-notes/crn-qa/issue",
      { method: "POST" },
    );
  });
  it("labels a zero-balance settlement without offering a payment", async () => {
    const settlement = {
      id: "stl-zero",
      settlementNumber: "QA-ZERO",
      status: "APPROVED",
      currency: "NGN",
      netAmountMinor: 0,
    };
    vi.mocked(apiRequest).mockResolvedValue({ data: [settlement], total: 1 });
    mount(<SettlementsPage />);
    expect(await screen.findByText("No payment due")).toBeVisible();
    expect(
      screen.queryByText("Head Office pays Franchise"),
    ).not.toBeInTheDocument();
    cleanup();
    vi.mocked(apiRequest).mockResolvedValue({ settlement, lines: [] });
    mount(
      <Routes>
        <Route
          path="/settlements/:settlementId"
          element={<SettlementDetailPage />}
        />
      </Routes>,
      "/settlements/stl-zero",
    );
    expect(await screen.findByText("No payment due")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Record payment" }),
    ).not.toBeInTheDocument();
  });
  it("keeps read-only scanner access from exposing a submit control", async () => {
    qaPermissions.allowActions = false;
    vi.mocked(apiRequest).mockResolvedValue({ data: [] });
    mount(<ScannerConsolePage />);
    expect(await screen.findByText("Read-only scan history")).toBeVisible();
    expect(
      screen.queryByRole("textbox", { name: "Receive scan" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Submit" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("Recorded scan history")).toBeVisible();
  });
  it("explains prepaid collections in the settlement total and source tab", async () => {
    const user = userEvent.setup();
    vi.mocked(apiRequest).mockResolvedValue({
      settlement: {
        id: "stl-qa",
        settlementNumber: "QA-STL",
        status: "CALCULATED",
        currency: "NGN",
        franchiseName: "QA franchise",
        periodStart: "2026-09-19",
        periodEnd: "2026-09-19",
        commissionMinor: 4250,
        collectionsMinor: -10000,
        netAmountMinor: -5750,
      },
      lines: [
        {
          id: "sln-qa",
          lineNo: 1,
          category: "CUSTOMER_COLLECTION",
          description: "Prepaid money for QA parcel",
          amountMinor: -10000,
          currency: "NGN",
          quantity: 1,
          sourceType: "FRANCHISE_COLLECTION",
          sourcePublicId: "fcl-qa",
          awb: "QA0001",
        },
      ],
    });
    mount(
      <Routes>
        <Route
          path="/settlements/:settlementId"
          element={<SettlementDetailPage />}
        />
      </Routes>,
      "/settlements/stl-qa",
    );
    expect(await screen.findByText("Franchise pays Head Office")).toBeVisible();
    expect(screen.getByText("Customer collections")).toBeVisible();
    expect(
      screen.getByText(
        "Prepaid money held by the franchise reduces what Head Office owes.",
      ),
    ).toBeVisible();
    await user.click(screen.getByRole("tab", { name: "Collections" }));
    expect(
      await screen.findByText("Prepaid money for QA parcel"),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "View shipment QA0001" }),
    ).toHaveAttribute("href", "/shipments?search=QA0001");
  });

  it("requires the actual arrival facility, sends its header, and keeps server errors inside the dialog", async () => {
    const user = userEvent.setup();
    vi.mocked(apiRequest).mockImplementation(async (path, options) => {
      await Promise.resolve();
      if (path === "/api/v1/trips/trip-qa/arrive" && options?.method === "POST")
        throw new Error("The selected facility is outside your role scope.");
      if (path === "/api/v1/trips/trip-qa")
        return {
          id: "trip-qa",
          tripCode: "QA-TRIP",
          status: "DEPARTED",
          destination: { id: "ou-destination", name: "Abuja branch" },
          legs: [
            {
              id: "leg-1",
              sequence: 1,
              destination: { id: "ou-destination", name: "Abuja branch" },
            },
          ],
          allowedTransitions: ["ARRIVED"],
        };
      return { data: [] };
    });
    mount(
      <Routes>
        <Route path="/trips/:tripId" element={<TripDetailPage />} />
      </Routes>,
      "/trips/trip-qa",
    );
    await user.click(
      await screen.findByRole("button", { name: "Record arrival" }),
    );
    const dialog = screen.getByRole("dialog", { name: "Arrive trip" });
    await user.click(
      within(dialog).getByRole("button", { name: "Confirm arrive" }),
    );
    expect(
      await within(dialog).findByText(
        "Choose the facility where the vehicle has arrived.",
      ),
    ).toBeVisible();
    expect(apiRequest).not.toHaveBeenCalledWith(
      "/api/v1/trips/trip-qa/arrive",
      expect.anything(),
    );
    await user.selectOptions(
      within(dialog).getByRole("combobox", { name: "Arrival facility" }),
      "ou-destination",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Confirm arrive" }),
    );
    expect(
      await within(dialog).findByText(
        "The selected facility is outside your role scope.",
      ),
    ).toBeVisible();
    const arrival = vi
      .mocked(apiRequest)
      .mock.calls.find(([path]) => path === "/api/v1/trips/trip-qa/arrive");
    expect(arrival).toBeDefined();
    expect(new Headers(arrival?.[1]?.headers).get("X-Operating-Unit")).toBe(
      "ou-destination",
    );
    expect(arrival?.[1]?.body).not.toHaveProperty("operatingUnitId");
  });
  it("loads each role's authoritative detail instead of interpreting a summary as no grants", async () => {
    const user = userEvent.setup();
    vi.mocked(apiRequest).mockImplementation(async (path) => {
      await Promise.resolve();
      if (path === "/api/v1/roles")
        return {
          data: [
            {
              id: "role-a",
              name: "Branch Manager",
              isSystem: true,
              permissionCount: 1,
            },
            {
              id: "role-b",
              name: "Read only",
              isSystem: true,
              permissionCount: 0,
            },
          ],
        };
      if (path === "/api/v1/permissions")
        return {
          data: [
            {
              code: "customer.create",
              module: "Customers",
              description: "Create customers",
            },
          ],
        };
      if (path === "/api/v1/roles/role-a")
        return {
          id: "role-a",
          name: "Branch Manager",
          isSystem: true,
          permissions: ["customer.create"],
        };
      if (path === "/api/v1/roles/role-b")
        return {
          id: "role-b",
          name: "Read only",
          isSystem: true,
          permissions: [],
        };
      throw new Error(`Unexpected API: ${path}`);
    });
    mount(<RolesPage />);
    const first = await screen.findByRole("table", {
      name: "Permission matrix for Branch Manager",
    });
    expect(within(first).getByLabelText("Granted")).toBeVisible();
    await user.click(screen.getByRole("button", { name: /^Read only/ }));
    const next = await screen.findByRole("table", {
      name: "Permission matrix for Read only",
    });
    expect(within(next).getByLabelText("Not granted")).toBeVisible();
    expect(within(next).queryByLabelText("Granted")).not.toBeInTheDocument();
  });

  it("shows clear customer field errors and does not submit invalid details", async () => {
    const user = userEvent.setup();
    mount(<CreateCustomerDialog open onOpenChange={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "Create customer" }));
    expect(
      await screen.findByText(
        "Enter at least 2 characters for the customer’s name.",
      ),
    ).toBeVisible();
    expect(
      screen.getByText(
        "Enter a contact phone number with at least 5 characters.",
      ),
    ).toBeVisible();
    await user.type(screen.getByLabelText("Email"), "invalid-email");
    await user.click(screen.getByRole("button", { name: "Create customer" }));
    expect(
      await screen.findByText("Enter a valid email address."),
    ).toBeVisible();
    expect(apiRequest).not.toHaveBeenCalled();
  });

  it("keeps failed scanner requests visible without a toast and returns keyboard focus", async () => {
    const user = userEvent.setup();
    localStorage.setItem("courier.scan-sound", "false");
    vi.mocked(apiRequest).mockImplementation(async (_path, options) => {
      await Promise.resolve();
      if (options?.method === "POST")
        throw new Error(
          "Barcode is not recognized. Check the label and scan again.",
        );
      return { data: [] };
    });
    mount(<ScannerConsolePage />);
    const scanner = screen.getByRole("textbox", { name: /receive scan/i });
    await user.type(scanner, "INVALID-QA{Enter}");
    expect(
      await screen.findByText(
        "Barcode is not recognized. Check the label and scan again.",
      ),
    ).toBeVisible();
    await waitFor(() => expect(scanner).toHaveFocus());
    expect(scanner).toHaveValue("INVALID-QA");
    expect(screen.queryByText("Awaiting scan")).not.toBeInTheDocument();
  });
});
