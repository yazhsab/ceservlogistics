import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Banknote,
  MapPin,
  PackageSearch,
  Pencil,
  Plus,
  SearchX,
  UserRound,
} from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { Link, useNavigate, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type CreditProfile,
  type Customer,
  type CustomerAddress,
  type CustomerSummary,
  type OffsetPageOf,
  type ShipmentListResponse,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { useToast } from "../components/ToastProvider";
import {
  Badge,
  Button,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  Input,
  LoadingState,
  PageHeader,
  Pagination,
  Panel,
  PanelHeader,
  SearchInput,
  Select,
  StatusBadge,
  TableCell,
  TableHead,
} from "../components/ui";
import {
  formatDateTime,
  formatMoney,
  titleCase,
  toMinorUnits,
} from "../lib/utils";

type CustomerPage = OffsetPageOf<CustomerSummary>;
interface AddressResponse {
  data?: CustomerAddress[];
}
const customerSchema = z
  .object({
    customerType: z.enum(["RETAIL", "BUSINESS"]),
    name: z.string().min(2).max(160),
    email: z.union([z.email(), z.literal("")]).optional(),
    phone: z.string().min(5),
    owningUnitId: z.string().optional(),
    gstNumber: z.string().optional(),
    panNumber: z.string().optional(),
    notes: z.string().optional(),
    accountCode: z.string().optional(),
    legalName: z.string().optional(),
    creditLimit: z.string().optional(),
    paymentTermsDays: z.number().int().min(0).max(180).optional(),
    billingCycle: z.enum(["WEEKLY", "FORTNIGHTLY", "MONTHLY"]).optional(),
    rateCardId: z.string().optional(),
  })
  .superRefine((value, context) => {
    if (value.customerType === "BUSINESS") {
      if (!value.accountCode)
        context.addIssue({
          code: "custom",
          path: ["accountCode"],
          message: "Account code is required.",
        });
      if (!value.legalName)
        context.addIssue({
          code: "custom",
          path: ["legalName"],
          message: "Legal name is required.",
        });
    }
  });
type CustomerValues = z.infer<typeof customerSchema>;

export function CustomersPage() {
  const { hasPermission } = useAuth();
  const navigate = useNavigate();
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState("");
  const [type, setType] = useState("");
  const [status, setStatus] = useState("");
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: ["customers", { page, search, type, status }],
    queryFn: () =>
      apiRequest<CustomerPage>(
        `/api/v1/customers${queryString({ page, limit: 25, search, customerType: type, status })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Commercial"
        title="Customers"
        description="Fast customer lookup, account status, addresses, shipments, and credit profile."
        actions={
          hasPermission("customer.create") ? (
            <Button variant="primary" onClick={() => setOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Add customer
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="flex flex-wrap gap-3 border-b p-4">
          <SearchInput
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(1);
            }}
            placeholder="Search name, code, email, or phone"
            className="min-w-[260px] flex-1"
            aria-label="Search customers"
          />
          <Select
            aria-label="Filter by customer type"
            value={type}
            onChange={(event) => setType(event.target.value)}
            className="w-44"
          >
            <option value="">All types</option>
            <option>RETAIL</option>
            <option>BUSINESS</option>
          </Select>
          <Select
            aria-label="Filter by customer status"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
            className="w-44"
          >
            <option value="">All statuses</option>
            <option>ACTIVE</option>
            <option>SUSPENDED</option>
            <option>CLOSED</option>
          </Select>
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading customers" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={SearchX}
            title="No customers found"
            description="Adjust the filters or create a customer account."
          />
        ) : (
          <>
            <DataTable label="Customers">
              <thead>
                <tr>
                  <TableHead>Customer</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Version</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((customer) => (
                  <tr
                    key={customer.id}
                    onClick={() => void navigate(`/customers/${customer.id}`)}
                    className="cursor-pointer hover:bg-slate-50"
                  >
                    <TableCell>
                      <Link
                        to={`/customers/${customer.id}`}
                        onClick={(event) => event.stopPropagation()}
                        className="font-semibold text-primary hover:underline"
                      >
                        {customer.code}
                      </Link>
                      <span className="ml-2 font-medium">{customer.name}</span>
                    </TableCell>
                    <TableCell>
                      <Badge>
                        {titleCase(customer.customerType ?? "Customer")}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={customer.status} />
                    </TableCell>
                    <TableCell>v{customer.version ?? 1}</TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <Pagination
              page={query.data?.pagination?.page ?? page}
              totalPages={query.data?.pagination?.totalPages ?? 1}
              onPageChange={setPage}
              label={`${query.data?.pagination?.totalItems ?? rows.length} customers`}
            />
          </>
        )}
      </Panel>
      <CreateCustomerDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

function CreateCustomerDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const navigate = useNavigate();
  const {
    register,
    watch,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<CustomerValues>({
    resolver: zodResolver(customerSchema),
    defaultValues: {
      customerType: "RETAIL",
      billingCycle: "MONTHLY",
      paymentTermsDays: 30,
    },
  });
  const customerType = watch("customerType");
  const mutation = useMutation({
    mutationFn: (values: CustomerValues) =>
      apiRequest<CustomerSummary>("/api/v1/customers", {
        method: "POST",
        body: {
          customerType: values.customerType,
          name: values.name,
          email: values.email || undefined,
          phone: values.phone,
          owningUnitId: values.owningUnitId || undefined,
          gstNumber: values.gstNumber || undefined,
          panNumber: values.panNumber || undefined,
          notes: values.notes || undefined,
          ...(values.customerType === "BUSINESS"
            ? {
                businessAccount: {
                  accountCode: values.accountCode?.toUpperCase(),
                  legalName: values.legalName,
                  creditLimitMinor: toMinorUnits(values.creditLimit),
                  paymentTermsDays: values.paymentTermsDays,
                  billingCycle: values.billingCycle,
                  rateCardId: values.rateCardId || undefined,
                },
              }
            : {}),
        },
      }),
    onSuccess: (customer) => {
      void client.invalidateQueries({ queryKey: ["customers"] });
      toast({
        tone: "success",
        title: "Customer created",
        description: `${customer.code ?? "Customer"} is ready for bookings.`,
      });
      reset();
      onOpenChange(false);
      if (customer.id) void navigate(`/customers/${customer.id}`);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create customer"
      description="Business accounts include contractual billing and credit terms."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            loading={mutation.isPending}
            onClick={() =>
              void handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Create customer
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field label="Customer type" htmlFor="customerType" required>
          <Select id="customerType" {...register("customerType")}>
            <option>RETAIL</option>
            <option>BUSINESS</option>
          </Select>
        </Field>
        <Field
          label="Customer name"
          htmlFor="customerName"
          required
          error={errors.name?.message}
        >
          <Input id="customerName" {...register("name")} />
        </Field>
        <Field
          label="Phone"
          htmlFor="customerPhone"
          required
          error={errors.phone?.message}
        >
          <Input id="customerPhone" inputMode="tel" {...register("phone")} />
        </Field>
        <Field
          label="Email"
          htmlFor="customerEmail"
          error={errors.email?.message}
        >
          <Input id="customerEmail" type="email" {...register("email")} />
        </Field>
        <Field label="Tax identification number (TIN)" htmlFor="gstNumber">
          <Input
            id="gstNumber"
            className="uppercase"
            {...register("gstNumber")}
          />
        </Field>
        <Field label="CAC / RC number" htmlFor="panNumber">
          <Input
            id="panNumber"
            className="uppercase"
            {...register("panNumber")}
          />
        </Field>
        {customerType === "BUSINESS" ? (
          <>
            <div className="sm:col-span-2 border-t pt-4">
              <h3 className="text-sm font-semibold">Business account</h3>
            </div>
            <Field
              label="Account code"
              htmlFor="accountCode"
              required
              error={errors.accountCode?.message}
            >
              <Input
                id="accountCode"
                className="uppercase"
                {...register("accountCode")}
              />
            </Field>
            <Field
              label="Legal name"
              htmlFor="legalName"
              required
              error={errors.legalName?.message}
            >
              <Input id="legalName" {...register("legalName")} />
            </Field>
            <Field label="Credit limit (₦)" htmlFor="creditLimit">
              <Input
                id="creditLimit"
                inputMode="decimal"
                {...register("creditLimit")}
              />
            </Field>
            <Field label="Payment terms (days)" htmlFor="paymentTermsDays">
              <Input
                id="paymentTermsDays"
                type="number"
                {...register("paymentTermsDays", {
                  setValueAs: (value) =>
                    value === "" ? undefined : Number(value),
                })}
              />
            </Field>
            <Field label="Billing cycle" htmlFor="billingCycle">
              <Select id="billingCycle" {...register("billingCycle")}>
                <option>WEEKLY</option>
                <option>FORTNIGHTLY</option>
                <option>MONTHLY</option>
              </Select>
            </Field>
            <Field label="Rate card ID" htmlFor="rateCardId">
              <Input id="rateCardId" {...register("rateCardId")} />
            </Field>
          </>
        ) : null}
        <Field label="Notes" htmlFor="customerNotes" className="sm:col-span-2">
          <Input id="customerNotes" {...register("notes")} />
        </Field>
        {mutation.error ? (
          <p role="alert" className="sm:col-span-2 text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

export function CustomerDetailPage() {
  const { customerId = "" } = useParams();
  const { hasPermission } = useAuth();
  const [editOpen, setEditOpen] = useState(false);
  const [tab, setTab] = useState<
    "overview" | "addresses" | "shipments" | "billing"
  >("overview");
  const customer = useQuery({
    queryKey: ["customer", customerId],
    queryFn: () => apiRequest<Customer>(`/api/v1/customers/${customerId}`),
  });
  if (customer.isLoading) return <LoadingState label="Loading customer" />;
  if (customer.error || !customer.data)
    return (
      <ErrorState
        error={customer.error ?? new Error("Customer not found")}
        retry={() => void customer.refetch()}
      />
    );
  const data = customer.data;
  return (
    <>
      <PageHeader
        eyebrow="Commercial / Customers"
        title={`${data.code ?? ""} · ${data.name ?? "Customer"}`}
        description={`${titleCase(data.customerType ?? "Customer")} account`}
        actions={
          <>
            <StatusBadge status={data.status} />
            {hasPermission("customer.update") ? (
              <Button onClick={() => setEditOpen(true)}>
                <Pencil aria-hidden className="h-4 w-4" /> Edit customer
              </Button>
            ) : null}
          </>
        }
      />
      <div className="mb-5 flex gap-1 overflow-x-auto border-b border-border">
        {(["overview", "addresses", "shipments", "billing"] as const).map(
          (item) => (
            <button
              key={item}
              onClick={() => setTab(item)}
              className={`whitespace-nowrap border-b-2 px-4 py-2.5 text-sm font-semibold ${tab === item ? "border-primary text-primary" : "border-transparent text-slate-500"}`}
            >
              {titleCase(item)}
            </button>
          ),
        )}
      </div>
      {tab === "overview" ? (
        <Overview customer={data} />
      ) : tab === "addresses" ? (
        <Addresses customerId={customerId} />
      ) : tab === "shipments" ? (
        <CustomerShipments customerId={customerId} />
      ) : (
        <BillingProfile customer={data} />
      )}
      <EditCustomerDialog
        customer={data}
        open={editOpen}
        onOpenChange={setEditOpen}
      />
    </>
  );
}

const editCustomerSchema = z.object({
  name: z.string().min(2).max(160),
  email: z.union([z.email(), z.literal("")]),
  phone: z.string().min(5).max(30),
  gstNumber: z.string().optional(),
  panNumber: z.string().optional(),
});
type EditCustomerValues = z.infer<typeof editCustomerSchema>;

function EditCustomerDialog({
  customer,
  open,
  onOpenChange,
}: {
  customer: Customer;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<EditCustomerValues>({
    resolver: zodResolver(editCustomerSchema),
    values: {
      name: customer.name ?? "",
      email: customer.email ?? "",
      phone: customer.phone ?? "",
      gstNumber: customer.gstNumber ?? "",
      panNumber: customer.panNumber ?? "",
    },
  });
  const mutation = useMutation({
    mutationFn: (values: EditCustomerValues) =>
      apiRequest<void>(`/api/v1/customers/${customer.id}`, {
        method: "PATCH",
        body: {
          expectedVersion: customer.version ?? 1,
          name: values.name,
          email: values.email || undefined,
          phone: values.phone,
          gstNumber: values.gstNumber || undefined,
          panNumber: values.panNumber || undefined,
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["customer", customer.id] });
      void client.invalidateQueries({ queryKey: ["customers"] });
      toast({ tone: "success", title: "Customer updated" });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Edit customer"
      description={`${customer.code} and ${titleCase(customer.customerType ?? "customer")} type are immutable. Version ${customer.version ?? 1} is checked before saving.`}
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            loading={mutation.isPending}
            onClick={() =>
              void handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Save customer
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Customer name"
          htmlFor="editCustomerName"
          required
          error={errors.name?.message}
          className="sm:col-span-2"
        >
          <Input id="editCustomerName" {...register("name")} />
        </Field>
        <Field
          label="Phone"
          htmlFor="editCustomerPhone"
          required
          error={errors.phone?.message}
        >
          <Input id="editCustomerPhone" {...register("phone")} />
        </Field>
        <Field
          label="Email"
          htmlFor="editCustomerEmail"
          error={errors.email?.message}
        >
          <Input id="editCustomerEmail" type="email" {...register("email")} />
        </Field>
        <Field
          label="Tax identification number (TIN)"
          htmlFor="editCustomerGst"
        >
          <Input
            id="editCustomerGst"
            className="uppercase"
            {...register("gstNumber")}
          />
        </Field>
        <Field label="CAC / RC number" htmlFor="editCustomerPan">
          <Input
            id="editCustomerPan"
            className="uppercase"
            {...register("panNumber")}
          />
        </Field>
        {mutation.error ? (
          <p role="alert" className="sm:col-span-2 text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

function Overview({ customer }: { customer: Customer }) {
  return (
    <div className="grid gap-5 xl:grid-cols-2">
      <Panel>
        <PanelHeader title="Contact & tax" />
        <dl className="grid gap-px bg-border sm:grid-cols-2">
          <Item label="Email" value={customer.email} />
          <Item label="Phone" value={customer.phone} />
          <Item
            label="Tax identification number (TIN)"
            value={customer.gstNumber}
          />
          <Item label="CAC / RC number" value={customer.panNumber} />
          <Item label="Owning unit" value={customer.owningUnitCode} />
          <Item label="Updated" value={formatDateTime(customer.updatedAt)} />
        </dl>
      </Panel>
      <Panel>
        <PanelHeader title="Business account" />
        <div className="p-4">
          {customer.businessAccount ? (
            <dl className="grid gap-4 sm:grid-cols-2">
              <ItemInline
                label="Account code"
                value={customer.businessAccount.accountCode}
              />
              <ItemInline
                label="Legal name"
                value={customer.businessAccount.legalName}
              />
              <ItemInline
                label="Billing cycle"
                value={titleCase(customer.businessAccount.billingCycle ?? "—")}
              />
              <ItemInline
                label="Payment terms"
                value={`${customer.businessAccount.paymentTermsDays ?? 0} days`}
              />
              <ItemInline
                label="Rate card"
                value={customer.businessAccount.rateCardCode}
              />
            </dl>
          ) : (
            <EmptyState
              icon={UserRound}
              title="Retail customer"
              description="This customer does not have business billing terms."
            />
          )}
        </div>
      </Panel>
    </div>
  );
}

function Addresses({ customerId }: { customerId: string }) {
  const { hasPermission } = useAuth();
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: ["customer-addresses", customerId],
    queryFn: () =>
      apiRequest<AddressResponse>(`/api/v1/customers/${customerId}/addresses`),
  });
  return (
    <Panel>
      <PanelHeader
        title="Saved addresses"
        description="Booking copies the selected address into an immutable shipment snapshot."
        actions={
          hasPermission("customer.update") ? (
            <Button size="sm" onClick={() => setOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Add address
            </Button>
          ) : undefined
        }
      />
      {query.isLoading ? (
        <LoadingState />
      ) : query.error ? (
        <ErrorState error={query.error} />
      ) : query.data?.data?.length ? (
        <div className="grid gap-3 p-4 md:grid-cols-2 xl:grid-cols-3">
          {query.data.data.map((address) => (
            <div key={address.id} className="rounded-md border p-4">
              <div className="flex items-start justify-between gap-2">
                <strong className="text-sm">{address.label}</strong>
                <div className="flex gap-1">
                  {address.isDefault ? (
                    <Badge tone="primary">Default</Badge>
                  ) : null}
                  <StatusBadge status={address.status} />
                </div>
              </div>
              <p className="mt-3 text-sm leading-6 text-slate-700">
                {address.contactName}
                <br />
                {address.line1}
                {address.line2 ? (
                  <>
                    <br />
                    {address.line2}
                  </>
                ) : null}
                <br />
                {address.city}, {address.state} {address.pincode}
              </p>
              <p className="mt-2 text-xs text-slate-500">
                {address.contactPhone}
              </p>
            </div>
          ))}
        </div>
      ) : (
        <EmptyState
          icon={MapPin}
          title="No saved addresses"
          description="Add a pickup, delivery, or billing address."
        />
      )}
      <AddAddressDialog
        open={open}
        onOpenChange={setOpen}
        customerId={customerId}
      />
    </Panel>
  );
}

const addressSchema = z.object({
  label: z.string().min(1).max(80),
  addressType: z.enum(["PICKUP", "DELIVERY", "BILLING", "BOTH"]),
  contactName: z.string().min(2),
  contactPhone: z.string().min(5),
  line1: z.string().min(3).max(200),
  line2: z.string().optional(),
  landmark: z.string().optional(),
  pincode: z.string().regex(/^[1-9][0-9]{5}$/),
  isDefault: z.boolean(),
});
type AddressValues = z.infer<typeof addressSchema>;
function AddAddressDialog({
  open,
  onOpenChange,
  customerId,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
  customerId: string;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<AddressValues>({
    resolver: zodResolver(addressSchema),
    defaultValues: { addressType: "BOTH", isDefault: false },
  });
  const mutation = useMutation({
    mutationFn: (values: AddressValues) =>
      apiRequest<CustomerAddress>(`/api/v1/customers/${customerId}/addresses`, {
        method: "POST",
        body: {
          ...values,
          line2: values.line2 || undefined,
          landmark: values.landmark || undefined,
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({
        queryKey: ["customer-addresses", customerId],
      });
      toast({ tone: "success", title: "Address added" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add saved address"
      description="This address can be selected during booking."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            loading={mutation.isPending}
            onClick={() =>
              void handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Add address
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Label"
          htmlFor="addressLabel"
          required
          error={errors.label?.message}
        >
          <Input
            id="addressLabel"
            placeholder="Main warehouse"
            {...register("label")}
          />
        </Field>
        <Field label="Address type" htmlFor="addressType">
          <Select id="addressType" {...register("addressType")}>
            <option>PICKUP</option>
            <option>DELIVERY</option>
            <option>BILLING</option>
            <option>BOTH</option>
          </Select>
        </Field>
        <Field
          label="Contact name"
          htmlFor="addressContact"
          required
          error={errors.contactName?.message}
        >
          <Input id="addressContact" {...register("contactName")} />
        </Field>
        <Field
          label="Contact phone"
          htmlFor="addressPhone"
          required
          error={errors.contactPhone?.message}
        >
          <Input id="addressPhone" {...register("contactPhone")} />
        </Field>
        <Field
          label="Address line 1"
          htmlFor="addressLine"
          required
          error={errors.line1?.message}
          className="sm:col-span-2"
        >
          <Input id="addressLine" {...register("line1")} />
        </Field>
        <Field label="Address line 2" htmlFor="addressLine2">
          <Input id="addressLine2" {...register("line2")} />
        </Field>
        <Field label="Landmark" htmlFor="landmark">
          <Input id="landmark" {...register("landmark")} />
        </Field>
        <Field
          label="Postal code"
          htmlFor="addressPincode"
          required
          error={errors.pincode?.message}
        >
          <Input
            id="addressPincode"
            inputMode="numeric"
            maxLength={6}
            {...register("pincode")}
          />
        </Field>
        <label className="flex items-center gap-2 self-end pb-2 text-sm">
          <input
            type="checkbox"
            className="h-4 w-4 accent-emerald-800"
            {...register("isDefault")}
          />{" "}
          Make default
        </label>
        {mutation.error ? (
          <p role="alert" className="sm:col-span-2 text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

function CustomerShipments({ customerId }: { customerId: string }) {
  const query = useQuery({
    queryKey: ["shipments", { customerId }],
    queryFn: () =>
      apiRequest<ShipmentListResponse>(
        `/api/v1/shipments${queryString({ customerId, limit: 25 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <Panel>
      {query.isLoading ? (
        <LoadingState />
      ) : query.error ? (
        <ErrorState error={query.error} />
      ) : rows.length === 0 ? (
        <EmptyState
          icon={PackageSearch}
          title="No shipments"
          description="This customer has no shipments in the current scope."
        />
      ) : (
        <DataTable label="Customer shipments">
          <thead>
            <tr>
              <TableHead>AWB</TableHead>
              <TableHead>Destination</TableHead>
              <TableHead>Service</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Booked</TableHead>
            </tr>
          </thead>
          <tbody>
            {rows.map((shipment) => (
              <tr key={shipment.id}>
                <TableCell>
                  <Link
                    to={`/shipments/${shipment.id}`}
                    className="font-mono font-semibold text-primary"
                  >
                    {shipment.awb}
                  </Link>
                </TableCell>
                <TableCell>
                  {shipment.recipientCity} · {shipment.destinationPincode}
                </TableCell>
                <TableCell>{shipment.service?.name}</TableCell>
                <TableCell>
                  <StatusBadge status={shipment.status} />
                </TableCell>
                <TableCell>{formatDateTime(shipment.bookedAt)}</TableCell>
              </tr>
            ))}
          </tbody>
        </DataTable>
      )}
    </Panel>
  );
}

function BillingProfile({ customer }: { customer: Customer }) {
  const { hasPermission } = useAuth();
  const [editOpen, setEditOpen] = useState(false);
  const customerId = customer.id ?? "";
  const query = useQuery({
    queryKey: ["customer-credit", customerId],
    queryFn: () =>
      apiRequest<CreditProfile>(`/api/v1/customers/${customerId}/credit`),
    enabled: customer.customerType === "BUSINESS",
  });
  if (customer.customerType !== "BUSINESS")
    return (
      <Panel>
        <EmptyState
          icon={Banknote}
          title="No billing profile"
          description="Retail customers do not have business credit terms."
        />
      </Panel>
    );
  if (query.isLoading)
    return (
      <Panel>
        <LoadingState />
      </Panel>
    );
  if (query.error || !query.data)
    return (
      <Panel>
        <ErrorState
          error={query.error ?? new Error("Credit profile not found")}
        />
      </Panel>
    );
  const credit = query.data;
  return (
    <div className="grid gap-5 lg:grid-cols-3">
      <Panel className="p-4">
        <p className="text-xs text-slate-500">Credit limit</p>
        <p className="mt-1 text-2xl font-bold">
          {formatMoney(credit.creditLimitMinor, credit.currency)}
        </p>
      </Panel>
      <Panel className="p-4">
        <p className="text-xs text-slate-500">Used</p>
        <p className="mt-1 text-2xl font-bold">
          {formatMoney(credit.creditUsedMinor, credit.currency)}
        </p>
      </Panel>
      <Panel className="p-4">
        <p className="text-xs text-slate-500">Available</p>
        <p className="mt-1 text-2xl font-bold">
          {formatMoney(credit.availableMinor, credit.currency)}
        </p>
        <div className="mt-2">
          <StatusBadge status={credit.creditStatus} />
        </div>
        {hasPermission("customer_credit.manage") ? (
          <Button size="sm" className="mt-3" onClick={() => setEditOpen(true)}>
            <Pencil aria-hidden className="h-3.5 w-3.5" /> Edit credit terms
          </Button>
        ) : null}
      </Panel>
      <EditCreditDialog
        credit={credit}
        customerId={customerId}
        open={editOpen}
        onOpenChange={setEditOpen}
      />
    </div>
  );
}

const creditSchema = z.object({
  creditLimit: z
    .string()
    .regex(/^\d+(\.\d{1,2})?$/, "Enter an amount with up to two decimals."),
  paymentTermsDays: z.number().int().min(0).max(180),
  creditStatus: z.enum(["GOOD", "WARNING", "ON_HOLD", "BLOCKED"]),
  blockedReason: z.string().max(500).optional(),
});
type CreditValues = z.infer<typeof creditSchema>;

function EditCreditDialog({
  credit,
  customerId,
  open,
  onOpenChange,
}: {
  credit: CreditProfile;
  customerId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<CreditValues>({
    resolver: zodResolver(creditSchema),
    values: {
      creditLimit: ((credit.creditLimitMinor ?? 0) / 100).toFixed(2),
      paymentTermsDays: credit.paymentTermsDays ?? 0,
      creditStatus: credit.creditStatus ?? "GOOD",
      blockedReason: credit.blockedReason ?? "",
    },
  });
  const mutation = useMutation({
    mutationFn: (values: CreditValues) =>
      apiRequest<CreditProfile>(`/api/v1/customers/${customerId}/credit`, {
        method: "PUT",
        body: {
          creditLimitMinor: toMinorUnits(values.creditLimit),
          paymentTermsDays: values.paymentTermsDays,
          creditStatus: values.creditStatus,
          blockedReason: values.blockedReason || undefined,
        },
      }),
    onSuccess: (updated) => {
      client.setQueryData(["customer-credit", customerId], updated);
      void client.invalidateQueries({ queryKey: ["customer", customerId] });
      toast({ tone: "success", title: "Credit terms updated" });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Edit credit terms"
      description="Credit used is server-derived and cannot be edited here."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            loading={mutation.isPending}
            onClick={() =>
              void handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Save credit terms
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Credit limit (₦)"
          htmlFor="creditLimitEdit"
          required
          error={errors.creditLimit?.message}
        >
          <Input
            id="creditLimitEdit"
            inputMode="decimal"
            {...register("creditLimit")}
          />
        </Field>
        <Field
          label="Payment terms (days)"
          htmlFor="creditTermsEdit"
          required
          error={errors.paymentTermsDays?.message}
        >
          <Input
            id="creditTermsEdit"
            type="number"
            {...register("paymentTermsDays", { valueAsNumber: true })}
          />
        </Field>
        <Field label="Credit status" htmlFor="creditStatusEdit">
          <Select id="creditStatusEdit" {...register("creditStatus")}>
            <option>GOOD</option>
            <option>WARNING</option>
            <option>ON_HOLD</option>
            <option>BLOCKED</option>
          </Select>
        </Field>
        <Field
          label="Blocked reason"
          htmlFor="creditReasonEdit"
          error={errors.blockedReason?.message}
        >
          <Input id="creditReasonEdit" {...register("blockedReason")} />
        </Field>
        {mutation.error ? (
          <p role="alert" className="sm:col-span-2 text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

function Item({ label, value }: { label: string; value?: string }) {
  return (
    <div className="bg-white p-4">
      <dt className="text-xs uppercase tracking-wide text-slate-500">
        {label}
      </dt>
      <dd className="mt-1 text-sm font-medium">{value || "—"}</dd>
    </div>
  );
}
function ItemInline({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <dt className="text-xs text-slate-500">{label}</dt>
      <dd className="mt-1 text-sm font-semibold">{value || "—"}</dd>
    </div>
  );
}
