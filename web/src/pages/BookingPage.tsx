import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  Box,
  Check,
  CheckCircle2,
  ChevronRight,
  CircleDollarSign,
  Copy,
  MapPin,
  PackagePlus,
  Plus,
  Printer,
  Route,
  Search,
  ShieldCheck,
  Trash2,
  UserRound,
} from "lucide-react";
import { useDeferredValue, useEffect, useRef, useState } from "react";
import {
  useFieldArray,
  useForm,
  type FieldErrors,
  type UseFormRegister,
} from "react-hook-form";
import { useNavigate } from "react-router-dom";
import { z } from "zod";
import {
  ApiError,
  apiRequest,
  queryString,
  type BookingRequest,
  type CustomerAddress,
  type CustomerSummary,
  type OffsetPageOf,
  type Quote,
  type QuoteRequest,
  type ServiceabilityResult,
  type ServiceListResponse,
  type Shipment,
} from "../api/client";
import { useToast } from "../components/ToastProvider";
import {
  Badge,
  Button,
  EmptyState,
  Field,
  InlineNotice,
  Input,
  PageHeader,
  Panel,
  PanelHeader,
  Select,
  StatusBadge,
  Textarea,
} from "../components/ui";
import {
  formatMoney,
  formatWeight,
  titleCase,
  toMinorUnits,
} from "../lib/utils";

type CustomerPage = OffsetPageOf<CustomerSummary>;
interface AddressResponse {
  data?: CustomerAddress[];
}
const addressSchema = z.object({
  contactName: z.string().min(2, "Enter a contact name."),
  companyName: z.string().optional(),
  phone: z.string().min(5, "Enter a valid phone number."),
  altPhone: z.string().optional(),
  email: z.union([z.email(), z.literal("")]).optional(),
  line1: z.string().min(3, "Enter the street address."),
  line2: z.string().optional(),
  landmark: z.string().optional(),
  city: z.string().optional(),
  state: z.string().optional(),
  pincode: z
    .string()
    .regex(/^[1-9][0-9]{5}$/, "Enter a valid 6-digit postal code."),
});
const packageSchema = z.object({
  reference: z.string().optional(),
  actualWeightGrams: z.number().int().min(1, "Weight must be at least 1 gram."),
  lengthMm: z.number().int().min(1).optional(),
  widthMm: z.number().int().min(1).optional(),
  heightMm: z.number().int().min(1).optional(),
  contentDescription: z.string().optional(),
});
const bookingSchema = z
  .object({
    customerId: z.string().min(1, "Choose a customer."),
    referenceNumber: z.string().max(64).optional(),
    bookingUnitId: z.string().optional(),
    sender: addressSchema,
    recipient: addressSchema,
    serviceCode: z.string().min(1, "Choose a courier product."),
    paymentMode: z.enum(["PREPAID", "COD", "CREDIT", "TO_PAY"]),
    packages: z.array(packageSchema).min(1).max(50),
    declaredValue: z.string().optional(),
    codAmount: z.string().optional(),
    insuranceRequired: z.boolean(),
    contentDescription: z
      .string()
      .min(2, "Describe the shipment contents.")
      .max(500),
    specialInstructions: z.string().max(1000).optional(),
    isFragile: z.boolean(),
    isDangerousGoods: z.boolean(),
  })
  .superRefine((value, context) => {
    if (value.paymentMode === "COD" && !toMinorUnits(value.codAmount))
      context.addIssue({
        code: "custom",
        path: ["codAmount"],
        message: "Enter the COD amount.",
      });
  });
type BookingValues = z.infer<typeof bookingSchema>;
interface Preview {
  serviceability: ServiceabilityResult;
  quote?: Quote;
}

const sections = [
  { id: "sender", label: "Sender", icon: UserRound },
  { id: "recipient", label: "Recipient", icon: MapPin },
  { id: "service", label: "Service", icon: Route },
  { id: "packages", label: "Package", icon: Box },
  { id: "payment", label: "Payment / COD", icon: CircleDollarSign },
  { id: "review", label: "Review", icon: Check },
];

export default function BookingPage() {
  const { toast } = useToast();
  const formRef = useRef<HTMLFormElement>(null);
  const [preview, setPreview] = useState<Preview>();
  const [previewStale, setPreviewStale] = useState(false);
  const [success, setSuccess] = useState<Shipment>();
  const [customerQuery, setCustomerQuery] = useState("");
  const deferredCustomerQuery = useDeferredValue(customerQuery);
  const [selectedCustomer, setSelectedCustomer] = useState<CustomerSummary>();
  const [customerMenuOpen, setCustomerMenuOpen] = useState(false);
  const [idempotencyKey, setIdempotencyKey] = useState(() =>
    crypto.randomUUID(),
  );
  const lastBody = useRef<string | undefined>(undefined);
  const [submissionMessage, setSubmissionMessage] = useState("");
  const {
    register,
    control,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<BookingValues>({
    resolver: zodResolver(bookingSchema),
    defaultValues: {
      customerId: "",
      referenceNumber: "",
      sender: {
        contactName: "",
        companyName: "",
        phone: "",
        line1: "",
        pincode: "",
      },
      recipient: {
        contactName: "",
        companyName: "",
        phone: "",
        line1: "",
        pincode: "",
      },
      serviceCode: "",
      paymentMode: "PREPAID",
      packages: [
        { reference: "", actualWeightGrams: 500, contentDescription: "" },
      ],
      declaredValue: "",
      codAmount: "",
      insuranceRequired: false,
      contentDescription: "",
      specialInstructions: "",
      isFragile: false,
      isDangerousGoods: false,
    },
  });
  const packages = useFieldArray({ control, name: "packages" });
  const paymentMode = watch("paymentMode");
  const customerId = watch("customerId");
  const customers = useQuery({
    queryKey: ["customers", "lookup", deferredCustomerQuery],
    queryFn: () =>
      apiRequest<CustomerPage>(
        `/api/v1/customers${queryString({ search: deferredCustomerQuery, status: "ACTIVE", limit: 10 })}`,
      ),
    enabled: deferredCustomerQuery.trim().length >= 2,
    staleTime: 30_000,
  });
  const addresses = useQuery({
    queryKey: ["customer-addresses", customerId],
    queryFn: () =>
      apiRequest<AddressResponse>(`/api/v1/customers/${customerId}/addresses`),
    enabled: Boolean(customerId),
  });
  const services = useQuery({
    queryKey: ["courier-services", "active"],
    queryFn: () =>
      apiRequest<ServiceListResponse>(
        "/api/v1/courier-services?status=ACTIVE&limit=100",
      ),
    staleTime: 5 * 60_000,
  });

  useEffect(() => {
    const subscription = watch(() => {
      if (preview) setPreviewStale(true);
    });
    return () => subscription.unsubscribe();
  }, [preview, watch]);
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
        event.preventDefault();
        if (preview?.quote && !previewStale) formRef.current?.requestSubmit();
        else void handleSubmit((values) => previewMutation.mutate(values))();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  });

  const createQuoteRequest = (values: BookingValues): QuoteRequest => ({
    originPincode: values.sender.pincode,
    destinationPincode: values.recipient.pincode,
    serviceCode: values.serviceCode,
    customerId: values.customerId,
    paymentMode: values.paymentMode,
    declaredValueMinor: toMinorUnits(values.declaredValue),
    codAmountMinor: toMinorUnits(values.codAmount),
    insuranceRequired: values.insuranceRequired,
    packages: values.packages.map((item) => ({
      reference: item.reference || undefined,
      actualWeightGrams: item.actualWeightGrams,
      lengthMm: item.lengthMm,
      widthMm: item.widthMm,
      heightMm: item.heightMm,
      contentDescription: item.contentDescription || values.contentDescription,
    })),
  });
  const createBookingRequest = (values: BookingValues): BookingRequest => ({
    customerId: values.customerId,
    referenceNumber: values.referenceNumber || undefined,
    serviceCode: values.serviceCode,
    paymentMode: values.paymentMode,
    sender: cleanAddress(values.sender),
    recipient: cleanAddress(values.recipient),
    packages: createQuoteRequest(values).packages,
    declaredValueMinor: toMinorUnits(values.declaredValue),
    codAmountMinor: toMinorUnits(values.codAmount),
    insuranceRequired: values.insuranceRequired,
    contentDescription: values.contentDescription,
    specialInstructions: values.specialInstructions || undefined,
    isFragile: values.isFragile,
    isDangerousGoods: values.isDangerousGoods,
    bookingUnitId: values.bookingUnitId || undefined,
  });
  const getPreview = async (values: BookingValues) => {
    const serviceability = await apiRequest<ServiceabilityResult>(
      "/api/v1/serviceability/check",
      {
        method: "POST",
        body: {
          originPincode: values.sender.pincode,
          destinationPincode: values.recipient.pincode,
          serviceCode: values.serviceCode,
        },
      },
    );
    if (!serviceability.serviceable) return { serviceability };
    const quote = await apiRequest<Quote>("/api/v1/pricing/quote", {
      method: "POST",
      body: createQuoteRequest(values),
    });
    return { serviceability, quote };
  };
  const previewMutation = useMutation({
    mutationFn: getPreview,
    onSuccess: (value) => {
      setPreview(value);
      setPreviewStale(false);
      document
        .getElementById("review")
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    },
  });
  const bookingMutation = useMutation({
    mutationFn: async (values: BookingValues) => {
      setSubmissionMessage("Confirming current price…");
      const freshPreview = await getPreview(values);
      if (!freshPreview.serviceability.serviceable || !freshPreview.quote) {
        setPreview(freshPreview);
        throw new Error(
          freshPreview.serviceability.reasonMessage ??
            "This lane is no longer serviceable.",
        );
      }
      if (freshPreview.quote.totalMinor !== preview?.quote?.totalMinor) {
        setPreview(freshPreview);
        setPreviewStale(false);
        throw new Error(
          "The price changed. Review the updated breakdown, then book again.",
        );
      }
      const request = createBookingRequest(values);
      const serialized = JSON.stringify(request);
      if (lastBody.current && lastBody.current !== serialized)
        setIdempotencyKey(crypto.randomUUID());
      lastBody.current = serialized;
      setSubmissionMessage("Allocating AWB and booking shipment…");
      const book = () =>
        apiRequest<Shipment>("/api/v1/shipments", {
          method: "POST",
          headers: { "Idempotency-Key": idempotencyKey },
          body: request,
        });
      try {
        return await book();
      } catch (error) {
        if (
          error instanceof ApiError &&
          error.code === "IDEMPOTENCY_IN_PROGRESS"
        ) {
          setSubmissionMessage(
            "The first request is still completing. Checking again…",
          );
          await new Promise((resolve) => window.setTimeout(resolve, 1000));
          return book();
        }
        throw error;
      }
    },
    onSuccess: (shipment) => {
      setSuccess(shipment);
      toast({
        tone: "success",
        title: "Shipment booked",
        description: `AWB ${shipment.awb ?? "allocated"}`,
      });
      window.scrollTo({ top: 0, behavior: "smooth" });
    },
    onError: (error) => setSubmissionMessage(error.message),
  });

  const selectCustomer = (customer: CustomerSummary) => {
    if (!customer.id) return;
    setSelectedCustomer(customer);
    setValue("customerId", customer.id, {
      shouldValidate: true,
      shouldDirty: true,
    });
    setCustomerQuery(`${customer.code ?? ""} · ${customer.name ?? ""}`);
    setCustomerMenuOpen(false);
  };
  const selectSenderAddress = (address: CustomerAddress) => {
    setValue("sender.contactName", address.contactName ?? "", {
      shouldDirty: true,
    });
    setValue("sender.phone", address.contactPhone ?? "", { shouldDirty: true });
    setValue("sender.altPhone", address.altPhone ?? "", { shouldDirty: true });
    setValue("sender.line1", address.line1 ?? "", { shouldDirty: true });
    setValue("sender.line2", address.line2 ?? "", { shouldDirty: true });
    setValue("sender.landmark", address.landmark ?? "", { shouldDirty: true });
    setValue("sender.city", address.city ?? "", { shouldDirty: true });
    setValue("sender.state", address.state ?? "", { shouldDirty: true });
    setValue("sender.pincode", address.pincode ?? "", { shouldDirty: true });
  };
  const startNew = () => {
    reset();
    packages.replace([
      { reference: "", actualWeightGrams: 500, contentDescription: "" },
    ]);
    setPreview(undefined);
    setPreviewStale(false);
    setSuccess(undefined);
    setSelectedCustomer(undefined);
    setCustomerQuery("");
    setIdempotencyKey(crypto.randomUUID());
    lastBody.current = undefined;
  };
  if (success) return <BookingSuccess shipment={success} onNew={startNew} />;
  return (
    <>
      <PageHeader
        eyebrow="Operations"
        title="Book shipment"
        description="Keyboard-first booking with server-verified serviceability, route, price, and duplicate protection."
        actions={
          <div className="hidden text-right text-xs text-slate-500 sm:block">
            <kbd className="rounded border bg-white px-1.5 py-0.5">⌘ Enter</kbd>
            <span className="ml-2">Preview / book</span>
          </div>
        }
      />
      <div className="mb-4 flex gap-1 overflow-x-auto rounded-md border bg-white p-1">
        {sections.map(({ id, label, icon: Icon }) => (
          <button
            key={id}
            type="button"
            onClick={() =>
              document
                .getElementById(id)
                ?.scrollIntoView({ behavior: "smooth", block: "start" })
            }
            className="flex min-w-fit flex-1 items-center justify-center gap-2 rounded px-3 py-2 text-xs font-semibold text-slate-600 hover:bg-muted"
          >
            <Icon aria-hidden className="h-3.5 w-3.5" /> {label}
          </button>
        ))}
      </div>
      <form
        ref={formRef}
        onSubmit={(event) =>
          void handleSubmit((values) => bookingMutation.mutate(values))(event)
        }
      >
        <div className="grid items-start gap-5 xl:grid-cols-[minmax(0,1fr)_400px]">
          <div className="space-y-5">
            <Panel id="sender" className="scroll-mt-32">
              <PanelHeader
                title="1. Sender"
                description="Choose a customer, then use a saved pickup address or type a one-off address."
              />
              <div className="grid gap-4 p-4 sm:grid-cols-2">
                <Field
                  label="Customer"
                  htmlFor="customerLookup"
                  required
                  error={errors.customerId?.message}
                  className="relative sm:col-span-2"
                >
                  <div className="relative">
                    <Search
                      aria-hidden
                      className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400"
                    />
                    <Input
                      id="customerLookup"
                      value={customerQuery}
                      onFocus={() => setCustomerMenuOpen(true)}
                      onChange={(event) => {
                        setCustomerQuery(event.target.value);
                        setCustomerMenuOpen(true);
                        setSelectedCustomer(undefined);
                        setValue("customerId", "", { shouldDirty: true });
                      }}
                      className="pl-9"
                      placeholder="Search customer code, name, email, or phone"
                      autoComplete="off"
                    />
                  </div>
                  {customerMenuOpen && deferredCustomerQuery.length >= 2 ? (
                    <div className="absolute left-0 right-0 top-[68px] z-20 max-h-64 overflow-y-auto rounded-md border bg-white p-1 shadow-overlay">
                      {customers.isLoading ? (
                        <p className="p-3 text-sm text-slate-500">Searching…</p>
                      ) : customers.data?.data?.length ? (
                        customers.data.data.map((customer) => (
                          <button
                            key={customer.id}
                            type="button"
                            onClick={() => selectCustomer(customer)}
                            className="flex w-full items-center justify-between rounded px-3 py-2 text-left hover:bg-muted"
                          >
                            <span>
                              <strong className="block text-sm">
                                {customer.name}
                              </strong>
                              <span className="text-xs text-slate-500">
                                {customer.code} · {customer.customerType}
                              </span>
                            </span>
                            <StatusBadge status={customer.status} />
                          </button>
                        ))
                      ) : (
                        <p className="p-3 text-sm text-slate-500">
                          No active customer found.
                        </p>
                      )}
                    </div>
                  ) : null}
                </Field>
                {selectedCustomer ? (
                  <div className="sm:col-span-2 flex flex-wrap items-center gap-2 rounded-md bg-emerald-50 p-3 text-sm">
                    <CheckCircle2
                      aria-hidden
                      className="h-4 w-4 text-success"
                    />
                    <strong>{selectedCustomer.code}</strong>
                    <span>{selectedCustomer.name}</span>
                    <Badge tone="primary">
                      {selectedCustomer.customerType}
                    </Badge>
                  </div>
                ) : null}
                {addresses.data?.data?.length ? (
                  <Field
                    label="Saved pickup address"
                    htmlFor="savedSender"
                    className="sm:col-span-2"
                  >
                    <Select
                      id="savedSender"
                      defaultValue=""
                      onChange={(event) => {
                        const address = addresses.data?.data?.find(
                          (item) => item.id === event.target.value,
                        );
                        if (address) selectSenderAddress(address);
                      }}
                    >
                      <option value="">Type a new address</option>
                      {addresses.data.data
                        .filter((address) =>
                          ["PICKUP", "BOTH"].includes(
                            address.addressType ?? "",
                          ),
                        )
                        .map((address) => (
                          <option key={address.id} value={address.id}>
                            {address.label} — {address.line1}, {address.pincode}
                          </option>
                        ))}
                    </Select>
                  </Field>
                ) : null}
                <AddressFields
                  prefix="sender"
                  register={register}
                  errors={errors.sender}
                />
              </div>
            </Panel>
            <Panel id="recipient" className="scroll-mt-32">
              <PanelHeader
                title="2. Recipient"
                description="Delivery contact and destination address."
              />
              <div className="grid gap-4 p-4 sm:grid-cols-2">
                <AddressFields
                  prefix="recipient"
                  register={register}
                  errors={errors.recipient}
                />
              </div>
            </Panel>
            <Panel id="service" className="scroll-mt-32">
              <PanelHeader
                title="3. Service"
                description="Choose the requested courier product. The route and promise are resolved by the server."
              />
              <div className="grid gap-4 p-4 sm:grid-cols-2">
                <Field
                  label="Courier product"
                  htmlFor="bookingService"
                  required
                  error={errors.serviceCode?.message}
                >
                  <Select id="bookingService" {...register("serviceCode")}>
                    <option value="">Select product</option>
                    {services.data?.data?.map((service) => (
                      <option key={service.id} value={service.code}>
                        {service.code} — {service.name} ({service.mode})
                      </option>
                    ))}
                  </Select>
                </Field>
                <Field
                  label="Booking unit ID"
                  htmlFor="bookingUnitId"
                  hint="Optional; server scope still applies."
                >
                  <Input id="bookingUnitId" {...register("bookingUnitId")} />
                </Field>
                <Field
                  label="Customer reference"
                  htmlFor="referenceNumber"
                  error={errors.referenceNumber?.message}
                  hint="Unique per active shipment for this customer."
                >
                  <Input
                    id="referenceNumber"
                    maxLength={64}
                    {...register("referenceNumber")}
                  />
                </Field>
                <Field
                  label="Contents"
                  htmlFor="contentDescription"
                  required
                  error={errors.contentDescription?.message}
                >
                  <Input
                    id="contentDescription"
                    maxLength={500}
                    {...register("contentDescription")}
                  />
                </Field>
              </div>
            </Panel>
            <Panel id="packages" className="scroll-mt-32">
              <PanelHeader
                title="4. Packages"
                description="Enter measured package values. Chargeable weight is always returned by the server."
                actions={
                  <Button
                    type="button"
                    size="sm"
                    onClick={() =>
                      packages.append({
                        reference: "",
                        actualWeightGrams: 500,
                        contentDescription: "",
                      })
                    }
                    disabled={packages.fields.length >= 50}
                  >
                    <Plus aria-hidden className="h-4 w-4" /> Add piece
                  </Button>
                }
              />
              <div className="divide-y">
                {packages.fields.map((field, index) => (
                  <div key={field.id} className="p-4">
                    <div className="mb-3 flex items-center justify-between">
                      <p className="text-sm font-semibold">Piece {index + 1}</p>
                      {packages.fields.length > 1 ? (
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          onClick={() => packages.remove(index)}
                        >
                          <Trash2 aria-hidden className="h-4 w-4" /> Remove
                        </Button>
                      ) : null}
                    </div>
                    <div className="grid gap-3 sm:grid-cols-6">
                      <Field
                        label="Reference"
                        htmlFor={`package-${index}-reference`}
                        className="sm:col-span-2"
                      >
                        <Input
                          id={`package-${index}-reference`}
                          {...register(`packages.${index}.reference`)}
                        />
                      </Field>
                      <Field
                        label="Weight (g)"
                        htmlFor={`package-${index}-weight`}
                        required
                        error={
                          errors.packages?.[index]?.actualWeightGrams?.message
                        }
                        className="sm:col-span-2"
                      >
                        <Input
                          id={`package-${index}-weight`}
                          type="number"
                          min={1}
                          {...register(`packages.${index}.actualWeightGrams`, {
                            valueAsNumber: true,
                          })}
                        />
                      </Field>
                      <Field
                        label="Length (mm)"
                        htmlFor={`package-${index}-length`}
                      >
                        <Input
                          id={`package-${index}-length`}
                          type="number"
                          {...register(`packages.${index}.lengthMm`, {
                            setValueAs: (value) =>
                              value === "" ? undefined : Number(value),
                          })}
                        />
                      </Field>
                      <Field
                        label="Width (mm)"
                        htmlFor={`package-${index}-width`}
                      >
                        <Input
                          id={`package-${index}-width`}
                          type="number"
                          {...register(`packages.${index}.widthMm`, {
                            setValueAs: (value) =>
                              value === "" ? undefined : Number(value),
                          })}
                        />
                      </Field>
                      <Field
                        label="Height (mm)"
                        htmlFor={`package-${index}-height`}
                      >
                        <Input
                          id={`package-${index}-height`}
                          type="number"
                          {...register(`packages.${index}.heightMm`, {
                            setValueAs: (value) =>
                              value === "" ? undefined : Number(value),
                          })}
                        />
                      </Field>
                    </div>
                  </div>
                ))}
              </div>
            </Panel>
            <Panel id="payment" className="scroll-mt-32">
              <PanelHeader title="5. Payment, value & handling" />
              <div className="grid gap-4 p-4 sm:grid-cols-2">
                <Field label="Payment mode" htmlFor="bookingPayment">
                  <Select id="bookingPayment" {...register("paymentMode")}>
                    <option>PREPAID</option>
                    <option>COD</option>
                    <option>CREDIT</option>
                    <option>TO_PAY</option>
                  </Select>
                </Field>
                {paymentMode === "COD" ? (
                  <Field
                    label="COD amount (₦)"
                    htmlFor="bookingCod"
                    required
                    error={errors.codAmount?.message}
                  >
                    <Input
                      id="bookingCod"
                      inputMode="decimal"
                      {...register("codAmount")}
                    />
                  </Field>
                ) : null}
                <Field label="Declared value (₦)" htmlFor="bookingDeclared">
                  <Input
                    id="bookingDeclared"
                    inputMode="decimal"
                    {...register("declaredValue")}
                  />
                </Field>
                <label className="flex items-center gap-2 self-end pb-2 text-sm">
                  <input
                    type="checkbox"
                    className="h-4 w-4 accent-emerald-800"
                    {...register("insuranceRequired")}
                  />{" "}
                  Insurance required
                </label>
                <div className="flex flex-wrap gap-6 sm:col-span-2">
                  <label className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      className="h-4 w-4 accent-emerald-800"
                      {...register("isFragile")}
                    />{" "}
                    Fragile
                  </label>
                  <label className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      className="h-4 w-4 accent-emerald-800"
                      {...register("isDangerousGoods")}
                    />{" "}
                    Dangerous goods
                  </label>
                </div>
                <Field
                  label="Special instructions"
                  htmlFor="specialInstructions"
                  className="sm:col-span-2"
                  error={errors.specialInstructions?.message}
                >
                  <Textarea
                    id="specialInstructions"
                    rows={3}
                    {...register("specialInstructions")}
                  />
                </Field>
              </div>
            </Panel>
            <div id="review" className="scroll-mt-32 xl:hidden">
              <ReviewSummary preview={preview} stale={previewStale} />
            </div>
          </div>
          <aside className="sticky top-24 hidden space-y-4 xl:block">
            <ReviewSummary preview={preview} stale={previewStale} />
            <Button
              type="button"
              size="lg"
              className="w-full"
              loading={previewMutation.isPending}
              onClick={() =>
                void handleSubmit((values) => previewMutation.mutate(values))()
              }
            >
              <Route aria-hidden className="h-4 w-4" />{" "}
              {preview ? "Refresh route & price" : "Check route & price"}
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="lg"
              className="w-full"
              disabled={!preview?.quote || previewStale}
              loading={bookingMutation.isPending}
            >
              <PackagePlus aria-hidden className="h-4 w-4" /> Book shipment
            </Button>
            {bookingMutation.error ? (
              <InlineNotice tone="danger" title="Booking not completed">
                {submissionMessage || bookingMutation.error.message}
              </InlineNotice>
            ) : null}
            <div className="flex items-start gap-2 rounded-md bg-slate-100 p-3 text-xs text-slate-600">
              <ShieldCheck aria-hidden className="mt-0.5 h-4 w-4 shrink-0" />
              <span>
                Duplicate protection is active for this form. A timed-out retry
                reuses the same idempotency key.
              </span>
            </div>
          </aside>
        </div>
        <div className="sticky bottom-0 z-20 -mx-4 mt-5 border-t border-border bg-white/95 p-3 shadow-[0_-10px_30px_rgba(15,23,42,.08)] backdrop-blur xl:hidden">
          <div className="flex gap-2">
            <Button
              type="button"
              className="flex-1"
              loading={previewMutation.isPending}
              onClick={() =>
                void handleSubmit((values) => previewMutation.mutate(values))()
              }
            >
              Check route & price
            </Button>
            <Button
              type="submit"
              variant="primary"
              className="flex-1"
              disabled={!preview?.quote || previewStale}
              loading={bookingMutation.isPending}
            >
              Book shipment
            </Button>
          </div>
          {bookingMutation.error ? (
            <p role="alert" className="mt-2 text-xs text-danger">
              {submissionMessage || bookingMutation.error.message}
            </p>
          ) : null}
        </div>
      </form>
    </>
  );
}

function AddressFields({
  prefix,
  register,
  errors,
}: {
  prefix: "sender" | "recipient";
  register: UseFormRegister<BookingValues>;
  errors?: FieldErrors<BookingValues["sender"]>;
}) {
  return (
    <>
      <Field
        label="Contact name"
        htmlFor={`${prefix}-contactName`}
        required
        error={errors?.contactName?.message}
      >
        <Input
          id={`${prefix}-contactName`}
          {...register(`${prefix}.contactName`)}
        />
      </Field>
      <Field label="Company" htmlFor={`${prefix}-companyName`}>
        <Input
          id={`${prefix}-companyName`}
          {...register(`${prefix}.companyName`)}
        />
      </Field>
      <Field
        label="Phone"
        htmlFor={`${prefix}-phone`}
        required
        error={errors?.phone?.message}
      >
        <Input
          id={`${prefix}-phone`}
          inputMode="tel"
          {...register(`${prefix}.phone`)}
        />
      </Field>
      <Field
        label="Email"
        htmlFor={`${prefix}-email`}
        error={errors?.email?.message}
      >
        <Input
          id={`${prefix}-email`}
          type="email"
          {...register(`${prefix}.email`)}
        />
      </Field>
      <Field
        label="Address line 1"
        htmlFor={`${prefix}-line1`}
        required
        error={errors?.line1?.message}
        className="sm:col-span-2"
      >
        <Input id={`${prefix}-line1`} {...register(`${prefix}.line1`)} />
      </Field>
      <Field label="Address line 2" htmlFor={`${prefix}-line2`}>
        <Input id={`${prefix}-line2`} {...register(`${prefix}.line2`)} />
      </Field>
      <Field label="Landmark" htmlFor={`${prefix}-landmark`}>
        <Input id={`${prefix}-landmark`} {...register(`${prefix}.landmark`)} />
      </Field>
      <Field
        label="Postal code"
        htmlFor={`${prefix}-pincode`}
        required
        error={errors?.pincode?.message}
      >
        <Input
          id={`${prefix}-pincode`}
          inputMode="numeric"
          maxLength={6}
          {...register(`${prefix}.pincode`)}
        />
      </Field>
      <Field label="City" htmlFor={`${prefix}-city`}>
        <Input id={`${prefix}-city`} {...register(`${prefix}.city`)} />
      </Field>
      <Field label="State" htmlFor={`${prefix}-state`}>
        <Input id={`${prefix}-state`} {...register(`${prefix}.state`)} />
      </Field>
    </>
  );
}

function ReviewSummary({
  preview,
  stale,
}: {
  preview?: Preview;
  stale: boolean;
}) {
  return (
    <Panel>
      <PanelHeader
        title="6. Review"
        description="Server-resolved route and price"
      />
      {!preview ? (
        <EmptyState
          icon={Route}
          title="Route and price not checked"
          description="Complete the form, then check serviceability and pricing."
        />
      ) : !preview.serviceability.serviceable ? (
        <div className="p-4">
          <InlineNotice tone="danger" title="Not serviceable">
            {preview.serviceability.reasonMessage ??
              titleCase(preview.serviceability.reasonCode ?? "Unavailable")}
          </InlineNotice>
        </div>
      ) : (
        <div className="p-4">
          {stale ? (
            <InlineNotice tone="warning" title="Inputs changed">
              Refresh the route and price before booking.
            </InlineNotice>
          ) : null}
          <div className="mt-1 flex items-center gap-2 text-sm">
            <span className="font-semibold">
              {preview.serviceability.origin?.pincode}
            </span>
            <ArrowRight aria-hidden className="h-4 w-4 text-slate-400" />
            <span className="font-semibold">
              {preview.serviceability.destination?.pincode}
            </span>
            <Badge tone="success">Serviceable</Badge>
          </div>
          <p className="mt-2 text-xs text-slate-500">
            {preview.serviceability.originBranch?.code} →{" "}
            {preview.serviceability.originHub?.code} →{" "}
            {preview.serviceability.destinationHub?.code} →{" "}
            {preview.serviceability.destinationBranch?.code}
          </p>
          <div className="mt-4 grid grid-cols-2 gap-2">
            <Summary
              label="Service"
              value={preview.serviceability.serviceName ?? "—"}
            />
            <Summary
              label="SLA"
              value={`${preview.serviceability.slaHours ?? 0} hours`}
            />
            <Summary
              label="Actual"
              value={formatWeight(preview.quote?.weight?.actualWeightGrams)}
            />
            <Summary
              label="Chargeable"
              value={formatWeight(preview.quote?.weight?.chargeableWeightGrams)}
            />
          </div>
          {preview.quote ? (
            <div className="mt-4 divide-y rounded-md border">
              {preview.quote.lineItems?.map((line, index) => (
                <div
                  key={`${line.code}-${index}`}
                  className="flex items-start justify-between gap-3 px-3 py-2 text-xs"
                >
                  <div>
                    <span className="font-medium">{line.label}</span>
                    <p className="mt-0.5 text-[10px] leading-4 text-slate-500">
                      {line.explanation}
                    </p>
                  </div>
                  <span className="shrink-0 font-semibold">
                    {formatMoney(line.amountMinor, preview.quote?.currency)}
                  </span>
                </div>
              ))}
              <div className="flex items-center justify-between bg-slate-50 px-3 py-3">
                <strong className="text-sm">Final total</strong>
                <strong className="text-xl">
                  {formatMoney(
                    preview.quote.totalMinor,
                    preview.quote.currency,
                  )}
                </strong>
              </div>
            </div>
          ) : null}
        </div>
      )}
    </Panel>
  );
}
function Summary({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded bg-slate-50 p-2.5">
      <p className="text-[10px] uppercase tracking-wide text-slate-500">
        {label}
      </p>
      <p className="mt-1 text-sm font-semibold">{value}</p>
    </div>
  );
}

function BookingSuccess({
  shipment,
  onNew,
}: {
  shipment: Shipment;
  onNew: () => void;
}) {
  const { toast } = useToast();
  const navigate = useNavigate();
  return (
    <div className="mx-auto max-w-3xl">
      <Panel className="overflow-hidden">
        <div className="bg-[#123f36] px-6 py-8 text-white">
          <span className="grid h-12 w-12 place-items-center rounded-full bg-[#d8f25a] text-[#123f36]">
            <Check aria-hidden className="h-6 w-6" />
          </span>
          <p className="mt-5 text-xs font-semibold uppercase tracking-[0.18em] text-emerald-100/70">
            Shipment booked successfully
          </p>
          <h1 className="mt-2 font-mono text-4xl font-bold tracking-tight">
            {shipment.awb}
          </h1>
          <p className="mt-2 text-sm text-emerald-50/75">
            {shipment.origin?.pincode} → {shipment.destination?.pincode} ·{" "}
            {shipment.service?.name}
          </p>
        </div>
        <div className="grid gap-px bg-border sm:grid-cols-3">
          <SummaryTile
            label="Status"
            value={titleCase(shipment.status ?? "BOOKED")}
          />
          <SummaryTile
            label="Chargeable weight"
            value={formatWeight(shipment.chargeableWeightGrams)}
          />
          <SummaryTile
            label="Total"
            value={formatMoney(shipment.totalAmountMinor, shipment.currency)}
          />
        </div>
        <div className="flex flex-wrap gap-2 p-5">
          <Button
            variant="primary"
            onClick={() => void navigate(`/shipments/${shipment.id}`)}
          >
            View shipment <ChevronRight aria-hidden className="h-4 w-4" />
          </Button>
          <Button
            onClick={() => void navigate(`/shipments/${shipment.id}?label=1`)}
          >
            <Printer aria-hidden className="h-4 w-4" /> Print label
          </Button>
          <Button
            onClick={() => {
              void navigator.clipboard.writeText(shipment.awb ?? "");
              toast({ tone: "success", title: "AWB copied" });
            }}
          >
            <Copy aria-hidden className="h-4 w-4" /> Copy AWB
          </Button>
          <Button onClick={onNew}>
            <Plus aria-hidden className="h-4 w-4" /> New booking
          </Button>
        </div>
      </Panel>
    </div>
  );
}
function SummaryTile({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-white p-5">
      <p className="text-xs uppercase tracking-wide text-slate-500">{label}</p>
      <p className="mt-1 text-lg font-semibold">{value}</p>
    </div>
  );
}

function cleanAddress(address: BookingValues["sender"]) {
  return {
    countryCode: "IN",
    ...Object.fromEntries(
      Object.entries(address).filter(([, value]) => value !== ""),
    ),
  };
}
