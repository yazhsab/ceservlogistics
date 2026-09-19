import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { useNavigate } from "react-router-dom";
import { z } from "zod";
import { apiRequest, type CustomerSummary } from "../api/client";
import { toMinorUnits } from "../lib/utils";
import { useToast } from "./ToastProvider";
import { Button, Dialog, Field, Input, Select } from "./ui";

const customerSchema = z
  .object({
    customerType: z.enum(["RETAIL", "BUSINESS"]),
    name: z
      .string({ error: "Enter the customer’s name." })
      .trim()
      .min(2, "Enter at least 2 characters for the customer’s name.")
      .max(160, "Use no more than 160 characters."),
    email: z
      .string()
      .trim()
      .refine(
        (value) => value === "" || z.email().safeParse(value).success,
        "Enter a valid email address.",
      )
      .optional(),
    phone: z
      .string({ error: "Enter a contact phone number." })
      .trim()
      .min(5, "Enter a contact phone number with at least 5 characters."),
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

export function CreateCustomerDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
  onCreated?: (customer: CustomerSummary) => void;
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
      if (onCreated) onCreated(customer);
      else if (customer.id) void navigate(`/customers/${customer.id}`);
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
