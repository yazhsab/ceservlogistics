import { useQuery } from "@tanstack/react-query";
import { useDeferredValue, useState } from "react";
import {
  useFieldArray,
  useWatch,
  type Control,
  type FieldErrors,
  type UseFormRegister,
  type UseFormSetValue,
} from "react-hook-form";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type CustomerSummary,
  type OffsetPageOf,
} from "../api/client";
import type { components } from "../api/schema";
import {
  Button,
  Field,
  InlineNotice,
  Input,
  Panel,
  PanelHeader,
  Select,
  Textarea,
} from "../components/ui";
import { formatMoney, titleCase, toMinorUnits } from "../lib/utils";
import { insuranceRateLabel } from "../lib/insurance";
import type { BookingValues } from "./BookingPage";

const amount = (value: string) =>
  /^\d+(\.\d{1,2})?$/.test(value) &&
  Number.isSafeInteger(toMinorUnits(value)) &&
  (toMinorUnits(value) ?? 0) <= 1_000_000_000_000;
const partySchema = z
  .object({
    party: z.enum(["SHIPPER", "RECEIVER", "THIRD_PARTY"]),
    customerId: z.string(),
  })
  .superRefine((value, ctx) => {
    if (value.party === "THIRD_PARTY" && !value.customerId)
      ctx.addIssue({
        code: "custom",
        path: ["customerId"],
        message: "Choose the third-party billing account.",
      });
  });
export const billingSchema = z.object({
  transportation: partySchema,
  dutyTax: partySchema,
});
export const customsFormSchema = z
  .object({
    enabled: z.boolean(),
    invoiceNumber: z.string().max(80),
    declarationStatement: z.string().max(2000),
    reasonForExport: z.string().max(100),
    termsOfSale: z.string().max(100),
    currency: z.enum(["NGN", "INR", "USD"]),
    discount: z.string(),
    freight: z.string(),
    otherCharges: z.string(),
    items: z.array(
      z.object({
        description: z.string(),
        quantity: z.number(),
        unitOfMeasure: z.string(),
        unitValue: z.string(),
        countryOfOrigin: z.string(),
        hsCode: z.string(),
      }),
    ),
  })
  .superRefine((value, ctx) => {
    if (!value.enabled) return;
    const issue = (path: (string | number)[], message: string) =>
      ctx.addIssue({ code: "custom", path, message });
    if (value.reasonForExport.trim().length < 2)
      issue(["reasonForExport"], "Enter the reason for export.");
    if (!value.items.length || value.items.length > 100)
      issue(["items"], "Add between 1 and 100 goods lines.");
    value.items.forEach((item, i) => {
      if (item.description.trim().length < 2 || item.description.length > 500)
        issue(
          ["items", i, "description"],
          "Describe the goods (2–500 characters).",
        );
      if (
        !Number.isInteger(item.quantity) ||
        item.quantity < 1 ||
        item.quantity > 1_000_000
      )
        issue(
          ["items", i, "quantity"],
          "Enter a whole quantity from 1 to 1,000,000.",
        );
      if (!item.unitOfMeasure.trim() || item.unitOfMeasure.length > 20)
        issue(["items", i, "unitOfMeasure"], "Enter the unit (e.g. pieces).");
      if (!amount(item.unitValue))
        issue(
          ["items", i, "unitValue"],
          "Enter a non-negative value with up to 2 decimal places.",
        );
      if (!/^[A-Z]{2}$/.test(item.countryOfOrigin))
        issue(["items", i, "countryOfOrigin"], "Select the country of origin.");
      if (item.hsCode.length > 20)
        issue(["items", i, "hsCode"], "Use at most 20 characters.");
    });
    for (const key of ["discount", "freight", "otherCharges"] as const)
      if (!amount(value[key]))
        issue(
          [key],
          "Enter a non-negative amount with up to 2 decimal places.",
        );
  });
export const newCustomsItem = () => ({
  description: "",
  quantity: 1,
  unitOfMeasure: "pieces",
  unitValue: "",
  countryOfOrigin: "NG",
  hsCode: "",
});
export const commercialDefaults = {
  billing: {
    transportation: { party: "SHIPPER" as const, customerId: "" },
    dutyTax: { party: "RECEIVER" as const, customerId: "" },
  },
  customs: {
    enabled: false,
    invoiceNumber: "",
    declarationStatement: "",
    reasonForExport: "Sale",
    termsOfSale: "",
    currency: "NGN" as const,
    discount: "0",
    freight: "0",
    otherCharges: "0",
    items: [newCustomsItem()],
  },
};
export function commercialRequest(
  values: BookingValues,
): Pick<components["schemas"]["BookingRequest"], "customs" | "billing"> {
  const c = values.customs;
  const party = (p: BookingValues["billing"]["transportation"]) => ({
    party: p.party,
    customerId: p.party !== "SHIPPER" ? p.customerId || undefined : undefined,
  });
  return {
    billing: {
      transportation: party(values.billing.transportation),
      dutyTax: party(values.billing.dutyTax),
    },
    customs: c.enabled
      ? {
          invoiceNumber: c.invoiceNumber || undefined,
          declarationStatement: c.declarationStatement || undefined,
          reasonForExport: c.reasonForExport,
          termsOfSale: c.termsOfSale || undefined,
          currency: c.currency,
          discountMinor: toMinorUnits(c.discount),
          freightMinor: toMinorUnits(c.freight),
          otherChargesMinor: toMinorUnits(c.otherCharges),
          items: c.items.map(({ unitValue, ...item }) => ({
            ...item,
            unitValueMinor: toMinorUnits(unitValue) ?? 0,
          })),
        }
      : undefined,
  };
}
export function useCountries() {
  return useQuery({
    queryKey: ["geography-countries"],
    queryFn: () =>
      apiRequest<{ data: components["schemas"]["Country"][] }>(
        "/api/v1/geography/countries",
      ),
    staleTime: 60 * 60_000,
  });
}
type Props = {
  control: Control<BookingValues>;
  register: UseFormRegister<BookingValues>;
  setValue: UseFormSetValue<BookingValues>;
  errors: FieldErrors<BookingValues>;
};
export function BillingFields(props: Props) {
  return (
    <div className="grid gap-4 sm:col-span-2 sm:grid-cols-2">
      {(["transportation", "dutyTax"] as const).map((kind) => (
        <BillingPartyField key={kind} kind={kind} {...props} />
      ))}
    </div>
  );
}
function BillingPartyField({
  kind,
  control,
  register,
  setValue,
  errors,
}: Props & { kind: "transportation" | "dutyTax" }) {
  const value = useWatch({ control, name: `billing.${kind}` });
  const [search, setSearch] = useState("");
  const deferred = useDeferredValue(search);
  const [selected, setSelected] = useState("");
  const customers = useQuery({
    queryKey: ["billing-customers", deferred],
    queryFn: () =>
      apiRequest<OffsetPageOf<CustomerSummary>>(
        `/api/v1/customers${queryString({ search: deferred, status: "ACTIVE", limit: 10 })}`,
      ),
    enabled: deferred.trim().length >= 2 && !value.customerId,
    staleTime: 30_000,
  });
  const label =
    kind === "transportation"
      ? "Bill transportation to"
      : "Bill duty and tax to";
  return (
    <div className="space-y-3">
      <Field label={label} htmlFor={`billing-${kind}`}>
        <Select
          id={`billing-${kind}`}
          {...register(`billing.${kind}.party`)}
          onChange={(event) => {
            setValue(
              `billing.${kind}.party`,
              event.target.value as typeof value.party,
              { shouldDirty: true },
            );
            setValue(`billing.${kind}.customerId`, "", { shouldDirty: true });
            setSearch("");
            setSelected("");
          }}
        >
          <option value="SHIPPER">Shipper</option>
          <option value="RECEIVER">Receiver</option>
          <option value="THIRD_PARTY">Third party</option>
        </Select>
      </Field>
      {value.party !== "SHIPPER" ? (
        <Field
          label={`${kind === "transportation" ? "Transportation" : "Duty and tax"} billing account`}
          htmlFor={`billing-${kind}-account`}
          required={value.party === "THIRD_PARTY"}
          error={errors.billing?.[kind]?.customerId?.message}
          hint={
            value.party === "RECEIVER"
              ? "Optional for the receiver; required for transportation on credit."
              : "Choose an active customer account."
          }
        >
          <Input
            id={`billing-${kind}-account`}
            value={value.customerId ? selected : search}
            placeholder="Search customer name or code"
            autoComplete="off"
            onChange={(event) => {
              setSearch(event.target.value);
              setSelected("");
              setValue(`billing.${kind}.customerId`, "", { shouldDirty: true });
            }}
          />
          {!value.customerId && deferred.trim().length >= 2 ? (
            <div className="max-h-48 overflow-y-auto rounded border p-1">
              {customers.isLoading ? (
                <p className="p-2 text-xs">Searching…</p>
              ) : customers.error ? (
                <p role="alert" className="p-2 text-xs text-danger">
                  {customers.error.message}
                </p>
              ) : customers.data?.data?.length ? (
                customers.data.data.map((c) => (
                  <button
                    type="button"
                    className="block w-full rounded p-2 text-left text-sm hover:bg-muted"
                    key={c.id}
                    onClick={() => {
                      setValue(`billing.${kind}.customerId`, c.id ?? "", {
                        shouldDirty: true,
                        shouldValidate: true,
                      });
                      setSelected(`${c.name} · ${c.code}`);
                    }}
                  >
                    {c.name} · {c.code}
                  </button>
                ))
              ) : (
                <p className="p-2 text-xs">No active account found.</p>
              )}
            </div>
          ) : null}
        </Field>
      ) : null}
    </div>
  );
}
export function CustomsFields({ control, register, errors }: Props) {
  const enabled = useWatch({ control, name: "customs.enabled" });
  const items = useFieldArray({ control, name: "customs.items" });
  const countries = useCountries();
  return (
    <Panel id="customs" className="scroll-mt-32">
      <PanelHeader
        title="6. Customs declaration"
        description="Enter goods and valuation charges for the commercial invoice."
      />
      <div className="space-y-4 p-4">
        <label className="flex min-h-11 items-center gap-3 text-sm font-medium">
          <input
            type="checkbox"
            className="h-4 w-4 accent-primary"
            {...register("customs.enabled")}
          />{" "}
          Include customs declaration
        </label>
        {enabled ? (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                label="Invoice number"
                htmlFor="customs-invoice"
                error={errors.customs?.invoiceNumber?.message}
              >
                <Input
                  id="customs-invoice"
                  {...register("customs.invoiceNumber")}
                />
              </Field>
              <Field
                label="Customs currency"
                htmlFor="customs-currency"
                hint="Use the shipment currency."
              >
                <Select id="customs-currency" {...register("customs.currency")}>
                  <option>NGN</option>
                  <option>INR</option>
                  <option>USD</option>
                </Select>
              </Field>
              <Field
                label="Reason for export"
                htmlFor="customs-reason"
                required
                error={errors.customs?.reasonForExport?.message}
              >
                <Input
                  id="customs-reason"
                  {...register("customs.reasonForExport")}
                />
              </Field>
              <Field
                label="Terms of sale"
                htmlFor="customs-terms"
                error={errors.customs?.termsOfSale?.message}
              >
                <Input
                  id="customs-terms"
                  placeholder="e.g. DAP"
                  {...register("customs.termsOfSale")}
                />
              </Field>
            </div>
            {countries.error ? (
              <InlineNotice tone="danger" title="Countries unavailable">
                {countries.error.message}
                <Button type="button" onClick={() => void countries.refetch()}>
                  Retry
                </Button>
              </InlineNotice>
            ) : null}
            {items.fields.map((item, i) => (
              <fieldset key={item.id} className="min-w-0 rounded-md border p-3">
                <legend className="px-1 text-sm font-semibold">
                  Goods line {i + 1}
                </legend>
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                  <Field
                    label="Description of goods"
                    htmlFor={`goods-${i}-description`}
                    className="sm:col-span-2 lg:col-span-3"
                    required
                    error={errors.customs?.items?.[i]?.description?.message}
                  >
                    <Input
                      id={`goods-${i}-description`}
                      {...register(`customs.items.${i}.description`)}
                    />
                  </Field>
                  <Field
                    label="Quantity"
                    htmlFor={`goods-${i}-quantity`}
                    required
                    error={errors.customs?.items?.[i]?.quantity?.message}
                  >
                    <Input
                      id={`goods-${i}-quantity`}
                      type="number"
                      min={1}
                      max={1000000}
                      step={1}
                      {...register(`customs.items.${i}.quantity`, {
                        valueAsNumber: true,
                      })}
                    />
                  </Field>
                  <Field
                    label="Unit of measure"
                    htmlFor={`goods-${i}-unit`}
                    required
                    error={errors.customs?.items?.[i]?.unitOfMeasure?.message}
                  >
                    <Input
                      id={`goods-${i}-unit`}
                      {...register(`customs.items.${i}.unitOfMeasure`)}
                    />
                  </Field>
                  <Field
                    label="Value per unit"
                    htmlFor={`goods-${i}-value`}
                    required
                    error={errors.customs?.items?.[i]?.unitValue?.message}
                  >
                    <Input
                      id={`goods-${i}-value`}
                      inputMode="decimal"
                      {...register(`customs.items.${i}.unitValue`)}
                    />
                  </Field>
                  <Field
                    label="Country of origin"
                    htmlFor={`goods-${i}-origin`}
                    required
                    error={errors.customs?.items?.[i]?.countryOfOrigin?.message}
                  >
                    <Select
                      id={`goods-${i}-origin`}
                      {...register(`customs.items.${i}.countryOfOrigin`)}
                    >
                      <option value="">
                        {countries.isLoading
                          ? "Loading countries…"
                          : "Select country"}
                      </option>
                      {countries.data?.data.map((c) => (
                        <option key={c.iso2} value={c.iso2}>
                          {c.name} ({c.iso2})
                        </option>
                      ))}
                    </Select>
                  </Field>
                  <Field
                    label="HS / tariff code"
                    htmlFor={`goods-${i}-hs`}
                    error={errors.customs?.items?.[i]?.hsCode?.message}
                  >
                    <Input
                      id={`goods-${i}-hs`}
                      {...register(`customs.items.${i}.hsCode`)}
                    />
                  </Field>
                  <div className="flex items-end">
                    <Button
                      type="button"
                      variant="ghost"
                      disabled={items.fields.length === 1}
                      onClick={() => items.remove(i)}
                      aria-label={`Remove goods line ${i + 1}`}
                    >
                      Remove line
                    </Button>
                  </div>
                </div>
              </fieldset>
            ))}
            <Button
              type="button"
              disabled={items.fields.length >= 100}
              onClick={() => items.append(newCustomsItem())}
            >
              Add goods line
            </Button>
            <div className="grid gap-3 sm:grid-cols-3">
              {(
                [
                  ["discount", "Goods discount"],
                  ["freight", "Customs freight charge"],
                  ["otherCharges", "Other customs charges"],
                ] as const
              ).map(([name, label]) => (
                <Field
                  key={name}
                  label={label}
                  htmlFor={`customs-${name}`}
                  error={errors.customs?.[name]?.message}
                >
                  <Input
                    id={`customs-${name}`}
                    inputMode="decimal"
                    {...register(`customs.${name}`)}
                  />
                </Field>
              ))}
            </div>
            <Field
              label="Declaration statement"
              htmlFor="customs-statement"
              error={errors.customs?.declarationStatement?.message}
            >
              <Textarea
                id="customs-statement"
                rows={3}
                {...register("customs.declarationStatement")}
              />
            </Field>
            <p className="text-xs text-muted-foreground">
              Preview shows the goods subtotal, discount, declared value,
              insurance and invoice total. Customs freight and other charges
              describe invoice value; transportation is priced separately.
            </p>
          </>
        ) : null}
      </div>
    </Panel>
  );
}
export function CommercialSummary({
  commercial,
  currency,
}: {
  commercial: components["schemas"]["CommercialSnapshot"];
  currency?: string;
}) {
  const c = commercial.customs;
  return (
    <div className="space-y-3 text-xs">
      <dl className="grid grid-cols-2 gap-3">
        <div>
          <dt className="text-muted-foreground">Bill transportation to</dt>
          <dd className="mt-1 font-medium">
            {titleCase(commercial.billing.transportation.party)}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Bill duty and tax to</dt>
          <dd className="mt-1 font-medium">
            {titleCase(commercial.billing.dutyTax.party)}
          </dd>
        </div>
      </dl>
      {c ? (
        <div
          className="divide-y rounded-md border"
          aria-label="Customs value breakdown"
        >
          {(
            [
              ["Goods subtotal", c.goodsSubtotalMinor],
              ["Goods discount", -1 * (c.declaration.discountMinor ?? 0)],
              ["Declared goods value", c.declaredValueMinor],
              ["Freight", c.declaration.freightMinor],
              [
                insuranceRateLabel(commercial.insurance.quote)
                  ? `Insurance (${insuranceRateLabel(commercial.insurance.quote)})`
                  : "Insurance",
                c.insuranceMinor,
              ],
              ["Other charges", c.declaration.otherChargesMinor],
              ["Customs invoice total", c.invoiceTotalMinor],
            ] as const
          ).map(([label, value]) => (
            <div
              key={label}
              className={`flex justify-between gap-3 px-3 py-2 ${label === "Customs invoice total" ? "bg-muted font-semibold" : ""}`}
            >
              <span>{label}</span>
              <span className="shrink-0">
                {formatMoney(value, c.declaration.currency ?? currency)}
              </span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}
