import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Download,
  FileSpreadsheet,
  MapPin,
  Plus,
  SearchX,
  UploadCloud,
} from "lucide-react";
import { useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { useNavigate } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type ImportJob,
  type OffsetPageOf,
  type PincodeRecord,
  type Zone,
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
  SearchInput,
  Select,
  StatusBadge,
  TableCell,
  TableHead,
} from "../components/ui";
import { safeDownloadName, titleCase } from "../lib/utils";

type PincodePage = OffsetPageOf<PincodeRecord>;
type ZonePage = OffsetPageOf<Zone>;
const zoneSchema = z.object({
  code: z.string().regex(/^[A-Z0-9][A-Z0-9_-]{1,31}$/),
  name: z.string().min(2),
  type: z.enum([
    "LOCAL",
    "INTRA_STATE",
    "METRO",
    "REGIONAL",
    "NATIONAL",
    "SPECIAL",
    "INTERNATIONAL",
  ]),
  description: z.string().optional(),
  sortOrder: z.number().int().optional(),
});
type ZoneValues = z.infer<typeof zoneSchema>;

export function PincodesPage() {
  const navigate = useNavigate();
  const [page, setPage] = useState(1);
  const [code, setCode] = useState("");
  const [remote, setRemote] = useState("");
  const query = useQuery({
    queryKey: ["pincodes", { page, code, remote }],
    queryFn: () =>
      apiRequest<PincodePage>(
        `/api/v1/geography/pincodes${queryString({ page, limit: 50, code, isRemote: remote === "" ? undefined : remote === "true" })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Network / Geography"
        title="Postal code master"
        description="Search the national geography reference and review zone-ready coverage attributes."
        actions={
          <div className="flex gap-2">
            <Button onClick={() => void navigate("/geography/zones")}>
              Zones
            </Button>
            <Button
              variant="primary"
              onClick={() => void navigate("/geography/imports")}
            >
              <UploadCloud aria-hidden className="h-4 w-4" /> Bulk import
            </Button>
          </div>
        }
      />
      <Panel>
        <div className="flex flex-wrap gap-3 border-b p-4">
          <SearchInput
            value={code}
            onChange={(event) => {
              setCode(event.target.value.replace(/\D/g, "").slice(0, 6));
              setPage(1);
            }}
            placeholder="Search postal code"
            className="min-w-[240px] flex-1"
            inputMode="numeric"
            aria-label="Search postal codes"
          />
          <Select
            aria-label="Filter by remote-area status"
            value={remote}
            onChange={(event) => {
              setRemote(event.target.value);
              setPage(1);
            }}
            className="w-48"
          >
            <option value="">All areas</option>
            <option value="true">Remote areas</option>
            <option value="false">Non-remote areas</option>
          </Select>
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading postal codes" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={SearchX}
            title="No postal codes found"
            description="Try a different 6-digit code or remote-area filter."
          />
        ) : (
          <>
            <DataTable label="Postal codes">
              <thead>
                <tr>
                  <TableHead>Postal code</TableHead>
                  <TableHead>City</TableHead>
                  <TableHead>District</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Tier</TableHead>
                  <TableHead>Remote</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((record) => (
                  <tr
                    key={record.id ?? record.code}
                    className="hover:bg-slate-50"
                  >
                    <TableCell>
                      <strong className="font-mono text-primary">
                        {record.code}
                      </strong>
                    </TableCell>
                    <TableCell>
                      {record.city || record.officeName || "—"}
                    </TableCell>
                    <TableCell>{record.district || "—"}</TableCell>
                    <TableCell>
                      {record.state}{" "}
                      <span className="text-xs text-slate-400">
                        {record.stateCode}
                      </span>
                    </TableCell>
                    <TableCell>
                      {titleCase(record.cityTier ?? "Other")}
                    </TableCell>
                    <TableCell>
                      {record.isRemote ? (
                        <Badge tone="warning">Remote</Badge>
                      ) : (
                        <span className="text-xs text-slate-500">Standard</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={record.status} />
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <Pagination
              page={query.data?.pagination?.page ?? page}
              totalPages={query.data?.pagination?.totalPages ?? 1}
              onPageChange={setPage}
              label={`${query.data?.pagination?.totalItems ?? rows.length} postal codes`}
            />
          </>
        )}
      </Panel>
    </>
  );
}

export function ZonesPage() {
  const { hasPermission } = useAuth();
  const [page, setPage] = useState(1);
  const [status, setStatus] = useState("");
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: ["zones", { page, status }],
    queryFn: () =>
      apiRequest<ZonePage>(
        `/api/v1/geography/zones${queryString({ page, limit: 25, status })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Network / Geography"
        title="Pricing zones"
        description="Tenant-defined regions used by the authoritative pricing engine."
        actions={
          hasPermission("zone.manage") ? (
            <Button variant="primary" onClick={() => setOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Add zone
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="flex gap-3 border-b p-4">
          <Select
            value={status}
            onChange={(event) => setStatus(event.target.value)}
            className="w-48"
          >
            <option value="">All statuses</option>
            <option>ACTIVE</option>
            <option>INACTIVE</option>
          </Select>
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading zones" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={MapPin}
            title="No zones configured"
            description="Create a pricing zone before mapping postal codes or setting rate lanes."
          />
        ) : (
          <>
            <DataTable label="Pricing zones">
              <thead>
                <tr>
                  <TableHead>Code</TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Description</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((zone) => (
                  <tr key={zone.id}>
                    <TableCell>
                      <strong className="text-primary">{zone.code}</strong>
                    </TableCell>
                    <TableCell>{zone.name}</TableCell>
                    <TableCell>
                      <Badge>{titleCase(zone.type ?? "Zone")}</Badge>
                    </TableCell>
                    <TableCell className="max-w-md">
                      {zone.description || "—"}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={zone.status} />
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <Pagination
              page={query.data?.pagination?.page ?? page}
              totalPages={query.data?.pagination?.totalPages ?? 1}
              onPageChange={setPage}
            />
          </>
        )}
      </Panel>
      <CreateZoneDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

function CreateZoneDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<ZoneValues>({
    resolver: zodResolver(zoneSchema),
    defaultValues: { type: "REGIONAL", sortOrder: 100 },
  });
  const mutation = useMutation({
    mutationFn: (values: ZoneValues) =>
      apiRequest<Zone>("/api/v1/geography/zones", {
        method: "POST",
        body: {
          ...values,
          code: values.code.toUpperCase(),
          description: values.description || undefined,
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["zones"] });
      toast({ tone: "success", title: "Zone created" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create pricing zone"
      description="Zone codes become pricing references across rate cards."
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
            Create zone
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Code"
          htmlFor="code"
          required
          error={errors.code?.message}
        >
          <Input id="code" className="uppercase" {...register("code")} />
        </Field>
        <Field
          label="Name"
          htmlFor="name"
          required
          error={errors.name?.message}
        >
          <Input id="name" {...register("name")} />
        </Field>
        <Field label="Zone type" htmlFor="type" required>
          <Select id="type" {...register("type")}>
            <option>LOCAL</option>
            <option>INTRA_STATE</option>
            <option>METRO</option>
            <option>REGIONAL</option>
            <option>NATIONAL</option>
            <option>SPECIAL</option>
            <option>INTERNATIONAL</option>
          </Select>
        </Field>
        <Field label="Sort order" htmlFor="sortOrder">
          <Input
            id="sortOrder"
            type="number"
            {...register("sortOrder", {
              setValueAs: (value) => (value === "" ? undefined : Number(value)),
            })}
          />
        </Field>
        <Field
          label="Description"
          htmlFor="description"
          className="sm:col-span-2"
        >
          <Input id="description" {...register("description")} />
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

export function GeographyImportsPage() {
  const { hasPermission } = useAuth();
  const [type, setType] = useState<"pincodes" | "zone-mappings">("pincodes");
  const [file, setFile] = useState<File>();
  const [jobId, setJobId] = useState<string>();
  const inputRef = useRef<HTMLInputElement>(null);
  const upload = useMutation({
    mutationFn: async () => {
      if (!file) throw new Error("Choose a CSV file first.");
      if (file.size > 25 * 1024 * 1024)
        throw new Error("The file must be 25 MB or smaller.");
      const data = new FormData();
      data.append("file", file);
      return apiRequest<ImportJob>(`/api/v1/geography/imports/${type}`, {
        method: "POST",
        body: data,
      });
    },
    onSuccess: (job) => setJobId(job.id),
  });
  const job = useQuery({
    queryKey: ["geography-import", jobId],
    queryFn: () => apiRequest<ImportJob>(`/api/v1/geography/imports/${jobId}`),
    enabled: Boolean(jobId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status &&
        ["COMPLETED", "COMPLETED_WITH_ERRORS", "FAILED", "CANCELLED"].includes(
          status,
        )
        ? false
        : 2500;
    },
  });
  const progress = job.data?.totalRows
    ? Math.min(
        100,
        Math.round(((job.data.processedRows ?? 0) / job.data.totalRows) * 100),
      )
    : 0;
  const reset = () => {
    setFile(undefined);
    setJobId(undefined);
    upload.reset();
    if (inputRef.current) inputRef.current.value = "";
  };
  return (
    <>
      <PageHeader
        eyebrow="Network / Geography"
        title="Bulk import"
        description="Upload large reference datasets asynchronously without blocking operational work."
      />
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_380px]">
        <Panel className="p-5">
          <ol className="mb-6 grid grid-cols-4 gap-2 text-xs font-semibold">
            <li className={jobId ? "text-success" : "text-primary"}>
              1. Select
            </li>
            <li className={jobId ? "text-success" : "text-slate-400"}>
              2. Upload
            </li>
            <li className={job.data ? "text-primary" : "text-slate-400"}>
              3. Validate
            </li>
            <li
              className={
                job.data?.status?.startsWith("COMPLETED")
                  ? "text-success"
                  : "text-slate-400"
              }
            >
              4. Results
            </li>
          </ol>
          {!jobId ? (
            <div className="space-y-4">
              <Field label="Import type" htmlFor="importType">
                <Select
                  id="importType"
                  value={type}
                  onChange={(event) =>
                    setType(event.target.value as typeof type)
                  }
                >
                  <option value="pincodes">Postal code master</option>
                  <option value="zone-mappings">Zone mappings</option>
                </Select>
              </Field>
              <button
                type="button"
                onClick={() => inputRef.current?.click()}
                className="flex min-h-48 w-full flex-col items-center justify-center rounded-lg border-2 border-dashed border-slate-300 bg-slate-50 px-6 text-center hover:border-primary hover:bg-emerald-50/30"
              >
                <UploadCloud aria-hidden className="h-8 w-8 text-slate-400" />
                <strong className="mt-3 text-sm">
                  {file ? file.name : "Choose a CSV file"}
                </strong>
                <span className="mt-1 text-xs text-slate-500">
                  CSV only · Maximum 25 MB
                </span>
              </button>
              <input
                ref={inputRef}
                type="file"
                accept=".csv,text/csv"
                className="sr-only"
                onChange={(event) => setFile(event.target.files?.[0])}
              />
              {upload.error ? (
                <p role="alert" className="text-sm text-danger">
                  {upload.error.message}
                </p>
              ) : null}
              <Button
                variant="primary"
                loading={upload.isPending}
                disabled={
                  !file ||
                  (type === "pincodes" && !hasPermission("pincode.manage")) ||
                  (type === "zone-mappings" && !hasPermission("zone.manage"))
                }
                onClick={() => upload.mutate()}
              >
                <UploadCloud aria-hidden className="h-4 w-4" /> Upload and
                validate
              </Button>
            </div>
          ) : job.isLoading ? (
            <LoadingState label="Preparing import" />
          ) : job.error ? (
            <ErrorState error={job.error} retry={() => void job.refetch()} />
          ) : (
            <div>
              <div className="flex items-start justify-between gap-4">
                <div>
                  <p className="text-sm font-semibold">{job.data?.fileName}</p>
                  <p className="mt-1 text-xs text-slate-500">
                    Job {job.data?.id}
                  </p>
                </div>
                <StatusBadge status={job.data?.status} />
              </div>
              <div className="mt-6 h-2 overflow-hidden rounded-full bg-slate-100">
                <div
                  className="h-full bg-primary transition-[width]"
                  style={{ width: `${progress}%` }}
                />
              </div>
              <div className="mt-2 flex justify-between text-xs text-slate-500">
                <span>
                  {job.data?.processedRows ?? 0} of {job.data?.totalRows ?? 0}{" "}
                  rows
                </span>
                <span>{progress}%</span>
              </div>
              <div className="mt-6 grid grid-cols-2 gap-3">
                <Result
                  label="Accepted"
                  value={job.data?.successRows ?? 0}
                  tone="success"
                />
                <Result
                  label="Rejected"
                  value={job.data?.failedRows ?? 0}
                  tone="danger"
                />
              </div>
              {job.data?.failureReason ? (
                <p className="mt-4 rounded-md bg-red-50 p-3 text-sm text-danger">
                  {job.data.failureReason}
                </p>
              ) : null}
              <div className="mt-5 flex flex-wrap gap-2">
                {job.data?.errorFileUrl ? (
                  <Button
                    onClick={() =>
                      void downloadCsv(
                        job.data?.errorFileUrl,
                        job.data?.fileName,
                      )
                    }
                  >
                    <Download aria-hidden className="h-4 w-4" /> Download
                    rejected rows
                  </Button>
                ) : null}
                <Button onClick={reset}>Start another import</Button>
              </div>
            </div>
          )}
        </Panel>
        <Panel className="p-5">
          <FileSpreadsheet aria-hidden className="h-6 w-6 text-primary" />
          <h2 className="mt-3 text-sm font-semibold">Expected columns</h2>
          {type === "pincodes" ? (
            <p className="mt-2 text-sm leading-6 text-slate-600">
              <code>pincode</code> and <code>state</code> are required. City,
              district, latitude, longitude, and <code>is_remote</code> are
              optional.
            </p>
          ) : (
            <p className="mt-2 text-sm leading-6 text-slate-600">
              <code>pincode</code> and <code>zone_code</code> are required.{" "}
              <code>is_remote</code> is optional.
            </p>
          )}
          <p className="mt-4 text-xs leading-5 text-slate-500">
            Valid rows are committed even when other rows fail. Download
            rejected rows, correct them, and upload only those records again.
          </p>
        </Panel>
      </div>
    </>
  );
}

function Result({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: "success" | "danger";
}) {
  return (
    <div
      className={`rounded-md border p-4 ${tone === "success" ? "border-emerald-200 bg-emerald-50" : "border-red-200 bg-red-50"}`}
    >
      <p className="text-xs font-medium text-slate-600">{label}</p>
      <p className="mt-1 text-2xl font-bold text-slate-950">
        {value.toLocaleString("en-NG")}
      </p>
    </div>
  );
}

async function downloadCsv(
  url: string | undefined,
  fileName: string | undefined,
) {
  if (!url) return;
  const content = await apiRequest<string>(url);
  const objectUrl = URL.createObjectURL(
    new Blob([content], { type: "text/csv" }),
  );
  const anchor = document.createElement("a");
  anchor.href = objectUrl;
  anchor.download = `${safeDownloadName(fileName, "import")}-errors.csv`;
  anchor.click();
  URL.revokeObjectURL(objectUrl);
}
