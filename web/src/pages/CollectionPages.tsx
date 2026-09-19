import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Banknote, ReceiptText } from "lucide-react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { apiRequest } from "../api/client";
import { useToast } from "../components/ToastProvider";
import {
  Button,
  DataTable,
  EmptyState,
  ErrorState,
  Field,
  Input,
  LoadingState,
  PageHeader,
  Panel,
  PanelHeader,
  Select,
  TableCell,
  TableHead,
} from "../components/ui";
import { formatDateTime, formatMoney, toMinorUnits } from "../lib/utils";

type Collection = {
  publicId: string;
  shipmentId: number;
  awb?: string;
  franchiseCode?: string;
  franchiseName?: string;
  unitCode?: string;
  amountMinor: number;
  currency: string;
  paymentMode: string;
  reference?: string;
  status: string;
  collectedAt: string;
  collectedByName?: string;
};

const schema = z
  .object({
    shipmentId: z
      .string()
      .regex(/^shp_/, "Enter the shipment ID beginning with shp_."),
    amount: z.string().min(1, "Enter the exact amount collected."),
    paymentMode: z.enum(["CASH", "POS", "TRANSFER", "BANK_DEPOSIT"]),
    reference: z.string().max(120).optional(),
    notes: z.string().max(500).optional(),
  })
  .superRefine((value, ctx) => {
    if (
      value.paymentMode !== "CASH" &&
      (value.reference?.trim().length ?? 0) < 3
    ) {
      ctx.addIssue({
        code: "custom",
        path: ["reference"],
        message: "Enter the POS or bank transaction reference.",
      });
    }
  });
type FormValues = z.infer<typeof schema>;

export default function FranchiseCollectionsPage() {
  const { toast } = useToast();
  const client = useQueryClient();
  const query = useQuery({
    queryKey: ["franchise-collections"],
    queryFn: () =>
      apiRequest<{ data: Collection[] }>(
        "/api/v1/franchise-collections?limit=100",
      ),
  });
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { paymentMode: "CASH" },
  });
  const mutation = useMutation({
    mutationFn: (values: FormValues) =>
      apiRequest("/api/v1/franchise-collections", {
        method: "POST",
        body: JSON.stringify({
          shipmentId: values.shipmentId.trim(),
          amountMinor: toMinorUnits(values.amount),
          paymentMode: values.paymentMode,
          reference: values.reference?.trim() || undefined,
          notes: values.notes?.trim() || undefined,
        }),
      }),
    onSuccess: async () => {
      toast({
        title: "Customer payment recorded",
        description:
          "The collection will be netted in the franchise settlement.",
        tone: "success",
      });
      reset({
        paymentMode: "CASH",
        shipmentId: "",
        amount: "",
        reference: "",
        notes: "",
      });
      await client.invalidateQueries({ queryKey: ["franchise-collections"] });
    },
  });

  return (
    <div className="space-y-6">
      <PageHeader
        title="Franchise customer collections"
        description="Record prepaid cash, POS and transfers received at the origin counter. These are distinct from destination COD."
      />
      <Panel>
        <PanelHeader
          title="Record full shipment payment"
          description="The amount must exactly match the authoritative shipment total. Non-cash payments require a reference."
        />
        <form
          className="grid gap-4 p-4 md:grid-cols-2 xl:grid-cols-5"
          onSubmit={(event) =>
            void handleSubmit((v) => mutation.mutate(v))(event)
          }
        >
          <Field
            label="Shipment ID"
            htmlFor="collection-shipment"
            error={errors.shipmentId?.message}
            required
          >
            <Input
              id="collection-shipment"
              placeholder="shp_..."
              {...register("shipmentId")}
            />
          </Field>
          <Field
            label="Amount collected (₦)"
            htmlFor="collection-amount"
            error={errors.amount?.message}
            required
          >
            <Input
              id="collection-amount"
              inputMode="decimal"
              placeholder="0.00"
              {...register("amount")}
            />
          </Field>
          <Field
            label="Payment method"
            htmlFor="collection-mode"
            error={errors.paymentMode?.message}
            required
          >
            <Select id="collection-mode" {...register("paymentMode")}>
              <option>CASH</option>
              <option>POS</option>
              <option>TRANSFER</option>
              <option>BANK_DEPOSIT</option>
            </Select>
          </Field>
          <Field
            label="Transaction reference"
            htmlFor="collection-reference"
            error={errors.reference?.message}
          >
            <Input id="collection-reference" {...register("reference")} />
          </Field>
          <div className="flex items-end">
            <Button
              type="submit"
              variant="primary"
              disabled={mutation.isPending}
            >
              <Banknote className="h-4 w-4" />
              {mutation.isPending ? "Recording…" : "Record collection"}
            </Button>
          </div>
          {mutation.error ? (
            <p
              role="alert"
              className="text-sm text-danger md:col-span-2 xl:col-span-5"
            >
              {mutation.error.message}
            </p>
          ) : null}
        </form>
      </Panel>
      <Panel>
        <PanelHeader
          title="Collection register"
          description="Every row identifies who collected the money and whether it has entered a settlement."
        />
        {query.isLoading ? (
          <LoadingState label="Loading collections" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : query.data?.data.length ? (
          <DataTable label="Franchise customer collections">
            <thead>
              <tr>
                <TableHead>Collected</TableHead>
                <TableHead>AWB</TableHead>
                <TableHead>Franchise</TableHead>
                <TableHead>Method</TableHead>
                <TableHead>Reference</TableHead>
                <TableHead>Collector</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Amount</TableHead>
              </tr>
            </thead>
            <tbody>
              {query.data.data.map((row) => (
                <tr key={row.publicId}>
                  <TableCell>{formatDateTime(row.collectedAt)}</TableCell>
                  <TableCell className="font-mono">
                    {row.awb ?? row.shipmentId}
                  </TableCell>
                  <TableCell>
                    {row.franchiseCode ?? row.unitCode ?? "—"}
                  </TableCell>
                  <TableCell>{row.paymentMode}</TableCell>
                  <TableCell>{row.reference ?? "Cash"}</TableCell>
                  <TableCell>{row.collectedByName ?? "—"}</TableCell>
                  <TableCell>{row.status}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatMoney(row.amountMinor, row.currency)}
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            icon={ReceiptText}
            title="No customer collections"
            description="Record the first prepaid payment after booking at a franchise counter."
          />
        )}
      </Panel>
    </div>
  );
}
