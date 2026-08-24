import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Boxes, MapPinned, Plus } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { apiRequest } from "../api/client";
import { useToast } from "../components/ToastProvider";
import {
  Button,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  Input,
  LoadingState,
  PageHeader,
  Panel,
  PanelHeader,
  Select,
  StatusBadge,
  TableCell,
  TableHead,
} from "../components/ui";
import {
  cmToMm,
  formatDimensionsCm,
  formatMoney,
  formatWeight,
  toMinorUnits,
} from "../lib/utils";

type State = { id: string; code: string; name: string };
type Zone = { id: string; code: string; name: string };
type Service = { code: string; name: string };
type StateRate = {
  id: string;
  stateId: string;
  stateCode: string;
  stateName: string;
  regionalZoneId: string;
  regionalZoneCode: string;
  serviceCode: string;
  baseWeightGrams: number;
  baseCostMinor: number;
  additionalStepGrams: number;
  additionalCostMinor: number;
  currency: string;
  isActive: boolean;
};
type PackageType = {
  id: string;
  code: string;
  name: string;
  lengthMm: number;
  widthMm: number;
  heightMm: number;
  volumetricDivisor: number;
  volumetricWeightGrams: number;
  maxWeightGrams?: number;
  isActive: boolean;
};

const stateSchema = z.object({
  stateId: z.string().min(1),
  regionalZoneId: z.string(),
  serviceCode: z.string(),
  baseWeightGrams: z.number().int().positive(),
  baseCost: z.string().regex(/^\d+(\.\d{1,2})?$/),
  additionalStepGrams: z.number().int().positive(),
  additionalCost: z.string().regex(/^\d+(\.\d{1,2})?$/),
});
const packageSchema = z.object({
  code: z.string().regex(/^[A-Z0-9][A-Z0-9_-]{1,31}$/),
  name: z.string().min(2),
  lengthCm: z.number().nonnegative().multipleOf(0.1),
  widthCm: z.number().nonnegative().multipleOf(0.1),
  heightCm: z.number().nonnegative().multipleOf(0.1),
  volumetricDivisor: z.number().int().positive(),
  maxWeightGrams: z.union([z.literal(""), z.number().int().positive()]),
});

export default function PricingMastersPage() {
  const client = useQueryClient();
  const { toast } = useToast();
  const [stateOpen, setStateOpen] = useState(false);
  const [packageOpen, setPackageOpen] = useState(false);
  const rates = useQuery({
    queryKey: ["state-base-rates"],
    queryFn: () =>
      apiRequest<{ data: StateRate[] }>("/api/v1/pricing/state-base-rates"),
  });
  const packages = useQuery({
    queryKey: ["package-types"],
    queryFn: () =>
      apiRequest<{ data: PackageType[] }>("/api/v1/pricing/package-types"),
  });
  const states = useQuery({
    queryKey: ["states", "NG"],
    queryFn: () =>
      apiRequest<{ data: State[] }>("/api/v1/geography/states?country=NG"),
  });
  const zones = useQuery({
    queryKey: ["zones", "masters"],
    queryFn: () =>
      apiRequest<{ data: Zone[] }>("/api/v1/geography/zones?limit=200"),
  });
  const services = useQuery({
    queryKey: ["services", "masters"],
    queryFn: () =>
      apiRequest<{ data: Service[] }>("/api/v1/courier-services?limit=200"),
  });
  const sf = useForm<z.infer<typeof stateSchema>>({
    resolver: zodResolver(stateSchema),
    defaultValues: {
      stateId: "",
      regionalZoneId: "",
      serviceCode: "",
      baseWeightGrams: 500,
      baseCost: "",
      additionalStepGrams: 500,
      additionalCost: "0",
    },
  });
  const pf = useForm<z.infer<typeof packageSchema>>({
    resolver: zodResolver(packageSchema),
    defaultValues: {
      code: "",
      name: "",
      lengthCm: 0,
      widthCm: 0,
      heightCm: 0,
      volumetricDivisor: 5000,
      maxWeightGrams: "",
    },
  });
  const saveState = useMutation({
    mutationFn: (v: z.infer<typeof stateSchema>) =>
      apiRequest("/api/v1/pricing/state-base-rates", {
        method: "POST",
        body: {
          ...v,
          baseCostMinor: toMinorUnits(v.baseCost),
          additionalCostMinor: toMinorUnits(v.additionalCost),
        },
      }),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["state-base-rates"] });
      setStateOpen(false);
      sf.reset();
      toast({ tone: "success", title: "State base cost saved" });
    },
  });
  const savePackage = useMutation({
    mutationFn: (v: z.infer<typeof packageSchema>) => {
      const { lengthCm, widthCm, heightCm, ...values } = v;
      return apiRequest("/api/v1/pricing/package-types", {
        method: "POST",
        body: {
          ...values,
          code: v.code.toUpperCase(),
          lengthMm: cmToMm(lengthCm),
          widthMm: cmToMm(widthCm),
          heightMm: cmToMm(heightCm),
          maxWeightGrams:
            v.maxWeightGrams === "" ? undefined : v.maxWeightGrams,
        },
      });
    },
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["package-types"] });
      setPackageOpen(false);
      pf.reset();
      toast({ tone: "success", title: "Package type saved" });
    },
  });
  return (
    <div className="space-y-6">
      <PageHeader
        title="Courier pricing masters"
        description="Manage the simple commercial rules used at the counter. Specific lane rates still take priority over state defaults."
      />
      <Panel>
        <PanelHeader
          title="State-wise base costs"
          description="Destination-state fallback price, optional service, regional zone and additional-weight step."
          actions={
            <Button variant="primary" onClick={() => setStateOpen(true)}>
              <Plus className="h-4 w-4" />
              Add state cost
            </Button>
          }
        />
        {rates.isLoading ? (
          <LoadingState label="Loading state prices" />
        ) : rates.error ? (
          <ErrorState error={rates.error} retry={() => void rates.refetch()} />
        ) : rates.data?.data.length ? (
          <DataTable label="State base cost master">
            <thead>
              <tr>
                <TableHead>State</TableHead>
                <TableHead>Regional zone</TableHead>
                <TableHead>Service</TableHead>
                <TableHead>Base weight</TableHead>
                <TableHead className="text-right">Base cost</TableHead>
                <TableHead>Additional step</TableHead>
                <TableHead className="text-right">Step cost</TableHead>
                <TableHead>Status</TableHead>
              </tr>
            </thead>
            <tbody>
              {rates.data.data.map((r) => (
                <tr key={r.id}>
                  <TableCell>
                    <strong>{r.stateName}</strong>
                    <div className="text-xs text-muted-foreground">
                      {r.stateCode}
                    </div>
                  </TableCell>
                  <TableCell>{r.regionalZoneCode || "Any zone"}</TableCell>
                  <TableCell>{r.serviceCode || "All services"}</TableCell>
                  <TableCell>{formatWeight(r.baseWeightGrams)}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatMoney(r.baseCostMinor, r.currency)}
                  </TableCell>
                  <TableCell>{formatWeight(r.additionalStepGrams)}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatMoney(r.additionalCostMinor, r.currency)}
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={r.isActive ? "ACTIVE" : "INACTIVE"} />
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            icon={MapPinned}
            title="No state base costs"
            description="Add a destination-state default or continue using only specific zone-to-zone lane rates."
          />
        )}
      </Panel>
      <Panel>
        <PanelHeader
          title="Package types"
          description="Reusable envelopes, PAKs, boxes and tubes with dimensions and volumetric divisor."
          actions={
            <Button onClick={() => setPackageOpen(true)}>
              <Plus className="h-4 w-4" />
              Add package type
            </Button>
          }
        />
        {packages.isLoading ? (
          <LoadingState label="Loading package types" />
        ) : packages.error ? (
          <ErrorState
            error={packages.error}
            retry={() => void packages.refetch()}
          />
        ) : packages.data?.data.length ? (
          <DataTable label="Package type master">
            <thead>
              <tr>
                <TableHead>Code</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Dimensions</TableHead>
                <TableHead>Divisor</TableHead>
                <TableHead>Volumetric weight</TableHead>
                <TableHead>Maximum weight</TableHead>
                <TableHead>Status</TableHead>
              </tr>
            </thead>
            <tbody>
              {packages.data.data.map((p) => (
                <tr key={p.id}>
                  <TableCell className="font-mono">{p.code}</TableCell>
                  <TableCell>{p.name}</TableCell>
                  <TableCell>
                    {formatDimensionsCm(p.lengthMm, p.widthMm, p.heightMm)}
                  </TableCell>
                  <TableCell>{p.volumetricDivisor}</TableCell>
                  <TableCell>{formatWeight(p.volumetricWeightGrams)}</TableCell>
                  <TableCell>
                    {p.maxWeightGrams
                      ? formatWeight(p.maxWeightGrams)
                      : "No limit"}
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={p.isActive ? "ACTIVE" : "INACTIVE"} />
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            icon={Boxes}
            title="No package types"
            description="Create reusable packaging presets for faster and more consistent booking."
          />
        )}
      </Panel>
      <Dialog
        open={stateOpen}
        onOpenChange={setStateOpen}
        title="Add state base cost"
        description="Used only when no more-specific lane rate or weight slab exists."
        footer={
          <>
            <Button onClick={() => setStateOpen(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={saveState.isPending}
              onClick={() => void sf.handleSubmit((v) => saveState.mutate(v))()}
            >
              Save state cost
            </Button>
          </>
        }
      >
        <form
          className="grid gap-4 sm:grid-cols-2"
          onSubmit={(e) => e.preventDefault()}
        >
          <Field
            htmlFor="stateRateState"
            label="Destination state"
            required
            error={sf.formState.errors.stateId?.message}
          >
            <Select id="stateRateState" {...sf.register("stateId")}>
              <option value="">Select state</option>
              {states.data?.data.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Regional zone" htmlFor="stateRateZone">
            <Select id="stateRateZone" {...sf.register("regionalZoneId")}>
              <option value="">No restriction</option>
              {zones.data?.data.map((z) => (
                <option key={z.id} value={z.id}>
                  {z.code} — {z.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Courier service" htmlFor="stateRateService">
            <Select id="stateRateService" {...sf.register("serviceCode")}>
              <option value="">All services</option>
              {services.data?.data.map((s) => (
                <option key={s.code} value={s.code}>
                  {s.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Base weight (g)" htmlFor="stateRateWeight" required>
            <Input
              id="stateRateWeight"
              type="number"
              {...sf.register("baseWeightGrams", { valueAsNumber: true })}
            />
          </Field>
          <Field label="Base cost (₦)" htmlFor="stateRateCost" required>
            <Input
              id="stateRateCost"
              inputMode="decimal"
              {...sf.register("baseCost")}
            />
          </Field>
          <Field label="Additional step (g)" htmlFor="stateRateStep" required>
            <Input
              id="stateRateStep"
              type="number"
              {...sf.register("additionalStepGrams", { valueAsNumber: true })}
            />
          </Field>
          <Field
            label="Cost per additional step (₦)"
            htmlFor="stateRateStepCost"
            required
          >
            <Input
              id="stateRateStepCost"
              inputMode="decimal"
              {...sf.register("additionalCost")}
            />
          </Field>
          {saveState.error ? (
            <p role="alert" className="sm:col-span-2 text-sm text-danger">
              {saveState.error.message}
            </p>
          ) : null}
        </form>
      </Dialog>
      <Dialog
        open={packageOpen}
        onOpenChange={setPackageOpen}
        title="Add package type"
        description="Dimensions are centimetres. Dimensional weight (kg) = length × width × height ÷ 5,000."
        footer={
          <>
            <Button onClick={() => setPackageOpen(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={savePackage.isPending}
              onClick={() =>
                void pf.handleSubmit((v) => savePackage.mutate(v))()
              }
            >
              Save package type
            </Button>
          </>
        }
      >
        <form
          className="grid gap-4 sm:grid-cols-2"
          onSubmit={(e) => e.preventDefault()}
        >
          <Field label="Code" htmlFor="packageCode" required>
            <Input
              id="packageCode"
              className="uppercase"
              {...pf.register("code")}
            />
          </Field>
          <Field label="Name" htmlFor="packageName" required>
            <Input id="packageName" {...pf.register("name")} />
          </Field>
          <Field label="Length (cm)" htmlFor="packageLength">
            <Input
              id="packageLength"
              type="number"
              min={0}
              step={0.1}
              {...pf.register("lengthCm", { valueAsNumber: true })}
            />
          </Field>
          <Field label="Width (cm)" htmlFor="packageWidth">
            <Input
              id="packageWidth"
              type="number"
              min={0}
              step={0.1}
              {...pf.register("widthCm", { valueAsNumber: true })}
            />
          </Field>
          <Field label="Height (cm)" htmlFor="packageHeight">
            <Input
              id="packageHeight"
              type="number"
              min={0}
              step={0.1}
              {...pf.register("heightCm", { valueAsNumber: true })}
            />
          </Field>
          <Field label="Volumetric divisor" htmlFor="packageDivisor">
            <Input
              id="packageDivisor"
              type="number"
              {...pf.register("volumetricDivisor", { valueAsNumber: true })}
            />
          </Field>
          <Field label="Maximum weight (g)" htmlFor="packageMaxWeight">
            <Input
              id="packageMaxWeight"
              type="number"
              {...pf.register("maxWeightGrams", {
                setValueAs: (value) => (value === "" ? "" : Number(value)),
              })}
            />
          </Field>
          {savePackage.error ? (
            <p role="alert" className="sm:col-span-2 text-sm text-danger">
              {savePackage.error.message}
            </p>
          ) : null}
        </form>
      </Dialog>
    </div>
  );
}
