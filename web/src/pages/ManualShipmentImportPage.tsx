import {
  CircleCheck,
  CloudOff,
  Download,
  FileUp,
  RefreshCw,
  Trash2,
  UploadCloud,
} from "lucide-react";
import { useEffect, useState } from "react";
import type { BookingRequest } from "../api/client";
import { useToast } from "../components/ToastProvider";
import {
  Button,
  DataTable,
  EmptyState,
  InlineNotice,
  PageHeader,
  Panel,
  PanelHeader,
  TableCell,
  TableHead,
} from "../components/ui";
import {
  bookOfflineBookingDraft,
  deleteOfflineBookingDraft,
  listOfflineBookingDrafts,
  saveOfflineBookingDraft,
  type OfflineBookingDraft,
} from "../lib/offlineBookings";
import { formatDateTime } from "../lib/utils";

const MAX_ROWS = 250;
export const templateHeaders = [
  "manual_waybill_number",
  "customer_id",
  "reference_number",
  "service_code",
  "payment_mode",
  "booking_unit_id",
  "content_description",
  "special_instructions",
  "insurance_required",
  "insurance_accepted",
  "declared_value_minor",
  "cod_amount_minor",
  "sender_contact_name",
  "sender_company_name",
  "sender_phone",
  "sender_email",
  "sender_line1",
  "sender_line2",
  "sender_landmark",
  "sender_city",
  "sender_state",
  "sender_postal_code",
  "sender_country",
  "recipient_contact_name",
  "recipient_company_name",
  "recipient_phone",
  "recipient_email",
  "recipient_line1",
  "recipient_line2",
  "recipient_landmark",
  "recipient_city",
  "recipient_state",
  "recipient_postal_code",
  "recipient_country",
  "package_weights_grams",
  "package_lengths_mm",
  "package_widths_mm",
  "package_heights_mm",
  "package_references",
] as const;

export const templateExample = [
  "MAN-0001",
  "cus_replace_me",
  "OFFLINE-0001",
  "DOMESTIC_STANDARD",
  "PREPAID",
  "",
  "Documents",
  "Captured from paper waybill",
  "false",
  "false",
  "0",
  "0",
  "Sender Name",
  "",
  "+2348000000001",
  "",
  "1 Origin Street",
  "",
  "",
  "Calabar",
  "Cross River",
  "540001",
  "NG",
  "Recipient Name",
  "",
  "+2348000000002",
  "",
  "2 Destination Street",
  "",
  "",
  "Port Harcourt",
  "Rivers",
  "500001",
  "NG",
  "500|750",
  "300|300",
  "200|200",
  "100|100",
  "MAN-0001-01|MAN-0001-02",
];

export interface ParsedManualShipment {
  rowNumber: number;
  request?: BookingRequest;
  insuranceAccepted: boolean;
  errors: string[];
}

function csvEscape(value: unknown) {
  const text = String(value ?? "");
  return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

export function parseCsv(text: string) {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (character === '"') {
      if (quoted && text[index + 1] === '"') {
        field += '"';
        index += 1;
      } else quoted = !quoted;
    } else if (character === "," && !quoted) {
      row.push(field);
      field = "";
    } else if ((character === "\n" || character === "\r") && !quoted) {
      if (character === "\r" && text[index + 1] === "\n") index += 1;
      row.push(field);
      if (row.some((value) => value.trim())) rows.push(row);
      row = [];
      field = "";
    } else field += character;
  }
  if (quoted) throw new Error("The CSV contains an unclosed quoted value.");
  row.push(field);
  if (row.some((value) => value.trim())) rows.push(row);
  return rows;
}

function positiveInteger(value: string, label: string, errors: string[]) {
  const number = Number(value);
  if (!/^\d+$/.test(value) || !Number.isSafeInteger(number) || number < 1) {
    errors.push(`${label} must contain positive whole numbers.`);
    return undefined;
  }
  return number;
}

function optionalMinor(value: string, label: string, errors: string[]) {
  if (!value.trim()) return undefined;
  const number = Number(value);
  if (!/^\d+$/.test(value) || !Number.isSafeInteger(number) || number < 0) {
    errors.push(`${label} must be a non-negative amount in minor units.`);
    return undefined;
  }
  return number;
}

function splitValues(value: string) {
  return value.split("|").map((item) => item.trim());
}

function parseBoolean(value: string, label: string, errors: string[]) {
  const normalized = value.trim().toLowerCase();
  if (["true", "yes", "1"].includes(normalized)) return true;
  if (["false", "no", "0", ""].includes(normalized)) return false;
  errors.push(`${label} must be true or false.`);
  return false;
}

function required(
  values: Record<string, string>,
  key: string,
  errors: string[],
) {
  const value = values[key]?.trim() ?? "";
  if (!value) errors.push(`${key.replaceAll("_", " ")} is required.`);
  return value;
}

export function parseManualShipmentCsv(text: string): ParsedManualShipment[] {
  const rows = parseCsv(text);
  if (rows.length < 2) throw new Error("The CSV must contain a header and at least one shipment row.");
  if (rows.length - 1 > MAX_ROWS)
    throw new Error(`Upload at most ${MAX_ROWS} shipments at a time.`);
  const headers = rows[0]!.map((header) => header.trim().toLowerCase());
  const missingHeaders = templateHeaders.filter((header) => !headers.includes(header));
  if (missingHeaders.length)
    throw new Error(`Missing CSV columns: ${missingHeaders.join(", ")}.`);

  return rows.slice(1).map((row, rowIndex) => {
    const values = Object.fromEntries(
      headers.map((header, index) => [header, row[index]?.trim() ?? ""]),
    );
    const errors: string[] = [];
    const manualWaybillNumber = required(values, "manual_waybill_number", errors);
    if (
      manualWaybillNumber &&
      !/^[A-Za-z0-9][A-Za-z0-9._:/ -]{0,63}$/.test(manualWaybillNumber)
    )
      errors.push("manual waybill number has unsupported characters or is longer than 64 characters.");
    const customerId = required(values, "customer_id", errors);
    const serviceCode = required(values, "service_code", errors).toUpperCase();
    const paymentMode = required(values, "payment_mode", errors).toUpperCase();
    if (!["PREPAID", "COD", "CREDIT", "TO_PAY"].includes(paymentMode))
      errors.push("payment mode must be PREPAID, COD, CREDIT, or TO_PAY.");
    const contentDescription = required(values, "content_description", errors);
    const weightValues = splitValues(required(values, "package_weights_grams", errors));
    const references = values.package_references ? splitValues(values.package_references) : [];
    const dimensions = [
      ["package_lengths_mm", values.package_lengths_mm],
      ["package_widths_mm", values.package_widths_mm],
      ["package_heights_mm", values.package_heights_mm],
    ] as const;
    for (const [label, value] of dimensions)
      if (value && splitValues(value).length !== weightValues.length)
        errors.push(`${label} must contain one pipe-separated value per package.`);
    if (references.length && references.length !== weightValues.length)
      errors.push("package_references must contain one value per package.");

    const declaredValueMinor = optionalMinor(
      values.declared_value_minor ?? "",
      "declared_value_minor",
      errors,
    );
    const codAmountMinor = optionalMinor(
      values.cod_amount_minor ?? "",
      "cod_amount_minor",
      errors,
    );
    if (paymentMode === "COD" && !codAmountMinor)
      errors.push("cod_amount_minor is required and must be positive for COD.");
    if (paymentMode !== "COD" && (codAmountMinor ?? 0) > 0)
      errors.push("cod_amount_minor is only allowed for COD.");
    const insuranceRequired = parseBoolean(
      values.insurance_required ?? "",
      "insurance_required",
      errors,
    );
    const insuranceAccepted = parseBoolean(
      values.insurance_accepted ?? "",
      "insurance_accepted",
      errors,
    );
    if (insuranceRequired && !(declaredValueMinor ?? 0))
      errors.push("declared_value_minor must be positive when insurance is requested.");

    const packageCount = weightValues.length;
    const baseDeclared = declaredValueMinor
      ? Math.floor(declaredValueMinor / packageCount)
      : 0;
    const declaredRemainder = declaredValueMinor
      ? declaredValueMinor % packageCount
      : 0;
    const packageDimensions = dimensions.map(([, value]) =>
      value ? splitValues(value) : [],
    );
    const packages = weightValues.map((weight, index) => {
      const length = packageDimensions[0]?.[index];
      const width = packageDimensions[1]?.[index];
      const height = packageDimensions[2]?.[index];
      return {
        reference: references[index] || undefined,
        actualWeightGrams:
          positiveInteger(weight, "package_weights_grams", errors) ?? 0,
        lengthMm: length
          ? positiveInteger(length, "package_lengths_mm", errors)
          : undefined,
        widthMm: width
          ? positiveInteger(width, "package_widths_mm", errors)
          : undefined,
        heightMm: height
          ? positiveInteger(height, "package_heights_mm", errors)
          : undefined,
        contentDescription,
        declaredValueMinor: declaredValueMinor
          ? baseDeclared + (index < declaredRemainder ? 1 : 0)
          : undefined,
      };
    });

    const address = (prefix: "sender" | "recipient") => ({
      contactName: required(values, `${prefix}_contact_name`, errors),
      companyName: values[`${prefix}_company_name`] || undefined,
      phone: required(values, `${prefix}_phone`, errors),
      email: values[`${prefix}_email`] || undefined,
      line1: required(values, `${prefix}_line1`, errors),
      line2: values[`${prefix}_line2`] || undefined,
      landmark: values[`${prefix}_landmark`] || undefined,
      city: required(values, `${prefix}_city`, errors),
      state: required(values, `${prefix}_state`, errors),
      pincode: required(values, `${prefix}_postal_code`, errors),
      countryCode: required(values, `${prefix}_country`, errors).toUpperCase(),
    });

    const request: BookingRequest = {
      customerId,
      referenceNumber: values.reference_number || undefined,
      manualWaybillNumber,
      serviceCode,
      paymentMode: paymentMode as BookingRequest["paymentMode"],
      bookingUnitId: values.booking_unit_id || undefined,
      contentDescription,
      specialInstructions: values.special_instructions || undefined,
      insuranceRequired,
      declaredValueMinor,
      codAmountMinor,
      sender: address("sender"),
      recipient: address("recipient"),
      packages,
      metadata: { captureMethod: "MANUAL_WAYBILL_CSV" },
    };
    return {
      rowNumber: rowIndex + 2,
      request: errors.length ? undefined : request,
      insuranceAccepted,
      errors,
    };
  });
}

function downloadTemplate() {
  const csv = [templateHeaders, templateExample]
    .map((row) => row.map(csvEscape).join(","))
    .join("\r\n");
  const link = document.createElement("a");
  link.href = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
  link.download = "ceserv-manual-shipments-template.csv";
  link.click();
  URL.revokeObjectURL(link.href);
}

export default function ManualShipmentImportPage() {
  const { toast } = useToast();
  const [online, setOnline] = useState(navigator.onLine);
  const [parsed, setParsed] = useState<ParsedManualShipment[]>([]);
  const [fileError, setFileError] = useState("");
  const [drafts, setDrafts] = useState<OfflineBookingDraft[]>([]);
  const [busy, setBusy] = useState(false);
  const [activeDraft, setActiveDraft] = useState("");
  const [booked, setBooked] = useState<Array<{ awb?: string; manual?: string }>>([]);

  const refreshDrafts = async () => {
    try {
      setDrafts(await listOfflineBookingDrafts());
    } catch (error) {
      setFileError(error instanceof Error ? error.message : "Could not read offline drafts.");
    }
  };
  useEffect(() => {
    void refreshDrafts();
    const connected = () => setOnline(true);
    const disconnected = () => setOnline(false);
    window.addEventListener("online", connected);
    window.addEventListener("offline", disconnected);
    return () => {
      window.removeEventListener("online", connected);
      window.removeEventListener("offline", disconnected);
    };
  }, []);

  const readFile = async (file?: File) => {
    setFileError("");
    setParsed([]);
    if (!file) return;
    try {
      setParsed(parseManualShipmentCsv(await file.text()));
    } catch (error) {
      setFileError(error instanceof Error ? error.message : "The CSV could not be read.");
    }
  };

  const saveRows = async () => {
    const validRows = parsed.filter((row) => row.request);
    if (!validRows.length) return [];
    setBusy(true);
    try {
      const saved = [] as OfflineBookingDraft[];
      for (const row of validRows)
        saved.push(
          await saveOfflineBookingDraft(row.request!, {
            source: "MANUAL_IMPORT",
            insuranceAccepted: row.insuranceAccepted,
          }),
        );
      setParsed([]);
      await refreshDrafts();
      toast({
        tone: "success",
        title: "Manual shipments saved",
        description: `${saved.length} encrypted draft${saved.length === 1 ? "" : "s"} stored on this device.`,
      });
      return saved;
    } finally {
      setBusy(false);
    }
  };

  const bookDraft = async (draft: OfflineBookingDraft) => {
    setActiveDraft(draft.id);
    try {
      const result = await bookOfflineBookingDraft(draft.id);
      setBooked((items) => [
        { awb: result.shipment.awb, manual: draft.manualWaybillNumber },
        ...items,
      ]);
      toast({
        tone: "success",
        title: "Shipment booked",
        description: `CESERV AWB ${result.shipment.awb ?? "allocated"}`,
      });
    } catch (error) {
      toast({
        tone: "error",
        title: "Draft needs attention",
        description: error instanceof Error ? error.message : "Booking failed.",
      });
    } finally {
      setActiveDraft("");
      await refreshDrafts();
    }
  };

  const validCount = parsed.filter((row) => row.request).length;
  const invalidCount = parsed.length - validCount;
  return (
    <>
      <PageHeader
        eyebrow="Operations · Shipments"
        title="Manual shipment upload"
        description="Capture paper waybills in bulk, keep encrypted drafts through connectivity outages, then revalidate serviceability and price before CESERV assigns the official AWB."
        actions={
          <Button onClick={downloadTemplate}>
            <Download aria-hidden className="h-4 w-4" /> Download CSV template
          </Button>
        }
      />
      {!online ? (
        <InlineNotice tone="warning" title="Working offline">
          You can parse and securely save drafts. Reconnect before validating and booking them.
        </InlineNotice>
      ) : null}
      {fileError ? (
        <div className="mt-4">
          <InlineNotice tone="danger" title="Import could not continue">
            {fileError}
          </InlineNotice>
        </div>
      ) : null}

      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1.3fr)_minmax(320px,0.7fr)]">
        <Panel>
          <PanelHeader
            title="1. Prepare and check the CSV"
            description={`One row per shipment, up to ${MAX_ROWS} rows. Separate package values with a vertical bar (|).`}
          />
          <div className="space-y-4 p-4">
            <label className="flex min-h-36 cursor-pointer flex-col items-center justify-center rounded-lg border border-dashed border-slate-300 bg-slate-50 p-6 text-center hover:border-primary hover:bg-emerald-50">
              <FileUp aria-hidden className="h-8 w-8 text-primary" />
              <span className="mt-2 text-sm font-semibold">Choose a completed CSV</span>
              <span className="mt-1 text-xs text-slate-500">
                The browser checks every row before anything is saved or submitted.
              </span>
              <input
                className="sr-only"
                type="file"
                accept=".csv,text/csv"
                onChange={(event) => void readFile(event.target.files?.[0])}
              />
            </label>
            {parsed.length ? (
              <>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <p className="text-sm text-slate-600">
                    <strong className="text-slate-950">{validCount}</strong> ready ·{" "}
                    <strong className={invalidCount ? "text-danger" : "text-slate-950"}>
                      {invalidCount}
                    </strong>{" "}
                    need correction
                  </p>
                  <Button
                    variant="primary"
                    loading={busy}
                    disabled={!validCount}
                    onClick={() => void saveRows()}
                  >
                    <CloudOff aria-hidden className="h-4 w-4" /> Save encrypted drafts
                  </Button>
                </div>
                <DataTable label="Manual shipment import preview">
                  <thead>
                    <tr>
                      <TableHead>Row</TableHead>
                      <TableHead>Manual waybill</TableHead>
                      <TableHead>Route</TableHead>
                      <TableHead>Packages</TableHead>
                      <TableHead>Check</TableHead>
                    </tr>
                  </thead>
                  <tbody>
                    {parsed.map((row) => (
                      <tr key={row.rowNumber} className="border-t">
                        <TableCell>{row.rowNumber}</TableCell>
                        <TableCell className="font-mono">
                          {row.request?.manualWaybillNumber ?? "—"}
                        </TableCell>
                        <TableCell>
                          {row.request
                            ? `${row.request.sender.city} → ${row.request.recipient.city}`
                            : "—"}
                        </TableCell>
                        <TableCell>{row.request?.packages.length ?? "—"}</TableCell>
                        <TableCell>
                          {row.errors.length ? (
                            <ul className="max-w-md list-disc pl-4 text-xs text-danger">
                              {row.errors.map((error) => (
                                <li key={error}>{error}</li>
                              ))}
                            </ul>
                          ) : (
                            <span className="inline-flex items-center gap-1 text-xs font-semibold text-success">
                              <CircleCheck aria-hidden className="h-4 w-4" /> Ready
                            </span>
                          )}
                        </TableCell>
                      </tr>
                    ))}
                  </tbody>
                </DataTable>
              </>
            ) : null}
          </div>
        </Panel>

        <Panel>
          <PanelHeader
            title="2. Validate and book"
            description="Each explicit retry reuses its idempotency key. The server recalculates the lane, price, insurance and routing before booking."
            actions={
              <Button size="sm" onClick={() => void refreshDrafts()}>
                <RefreshCw aria-hidden className="h-3.5 w-3.5" /> Refresh
              </Button>
            }
          />
          {drafts.length ? (
            <div className="divide-y">
              {drafts.map((draft) => (
                <article key={draft.id} className="space-y-3 p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <p className="font-mono text-sm font-semibold">
                        {draft.manualWaybillNumber ?? "Offline booking draft"}
                      </p>
                      <p className="mt-1 text-xs text-slate-500">
                        {draft.routeLabel} · {draft.packageCount} package
                        {draft.packageCount === 1 ? "" : "s"} · {formatDateTime(draft.updatedAt)}
                      </p>
                    </div>
                    <span
                      className={`rounded-full px-2 py-1 text-[10px] font-bold ${draft.status === "FAILED" ? "bg-red-50 text-danger" : "bg-amber-50 text-amber-800"}`}
                    >
                      {draft.status}
                    </span>
                  </div>
                  {draft.lastError ? (
                    <p className="rounded bg-red-50 p-2 text-xs text-danger">
                      {draft.lastError}
                    </p>
                  ) : null}
                  <div className="flex gap-2">
                    <Button
                      variant="primary"
                      size="sm"
                      className="flex-1"
                      disabled={!online}
                      loading={activeDraft === draft.id}
                      onClick={() => void bookDraft(draft)}
                    >
                      <UploadCloud aria-hidden className="h-4 w-4" /> Validate & book
                    </Button>
                    <Button
                      size="sm"
                      aria-label={`Delete ${draft.manualWaybillNumber ?? "draft"}`}
                      onClick={() => {
                        void (async () => {
                          await deleteOfflineBookingDraft(draft.id);
                          await refreshDrafts();
                        })();
                      }}
                    >
                      <Trash2 aria-hidden className="h-4 w-4" />
                    </Button>
                  </div>
                </article>
              ))}
            </div>
          ) : (
            <EmptyState
              icon={UploadCloud}
              title="No offline drafts"
              description="Uploaded shipments and booking-form drafts appear here until the server validates and books them."
            />
          )}
        </Panel>
      </div>
      {booked.length ? (
        <div className="mt-4">
          <InlineNotice tone="success" title="Booked in this session">
            {booked.map((item) => (
              <span key={`${item.awb}-${item.manual}`} className="mr-3 inline-block font-mono">
                {item.manual ? `${item.manual} → ` : ""}{item.awb}
              </span>
            ))}
          </InlineNotice>
        </div>
      ) : null}
    </>
  );
}
