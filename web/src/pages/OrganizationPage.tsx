import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Building2, Pencil } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import type { components } from "../api/schema";
import { apiRequest } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { useToast } from "../components/ToastProvider";
import {
  Button,
  Dialog,
  ErrorState,
  Field,
  Input,
  LoadingState,
  PageHeader,
  Panel,
  StatusBadge,
} from "../components/ui";

type Organization = components["schemas"]["Organization"];

export default function OrganizationPage() {
  const { hasPermission } = useAuth();
  const [editOpen, setEditOpen] = useState(false);
  const query = useQuery({
    queryKey: ["organization"],
    queryFn: () => apiRequest<Organization>("/api/v1/organization"),
  });
  if (query.isLoading) return <LoadingState label="Loading organization" />;
  if (query.error || !query.data)
    return (
      <ErrorState
        error={query.error ?? new Error("Organization not found")}
        retry={() => void query.refetch()}
      />
    );
  const org = query.data;
  return (
    <>
      <PageHeader
        eyebrow="Administration"
        title="Organization"
        description="Tenant identity and immutable shipment numbering settings."
        actions={
          hasPermission("organization.update") ? (
            <Button variant="primary" onClick={() => setEditOpen(true)}>
              <Pencil aria-hidden className="h-4 w-4" /> Edit organization
            </Button>
          ) : undefined
        }
      />
      <Panel className="max-w-4xl">
        <div className="flex items-center gap-4 border-b border-border p-5">
          <span className="grid h-12 w-12 place-items-center rounded-lg bg-emerald-50">
            <Building2 aria-hidden className="h-6 w-6 text-primary" />
          </span>
          <div>
            <h2 className="text-lg font-semibold">{org.name}</h2>
            <p className="text-sm text-slate-500">{org.legalName}</p>
          </div>
          <StatusBadge status={org.status} />
        </div>
        <dl className="grid gap-px bg-border sm:grid-cols-2">
          <Item label="Organization code" value={org.code} />
          <Item label="AWB prefix" value={org.awbPrefix} />
          <Item label="Currency" value={org.currency} />
          <Item label="Timezone" value={org.timezone} />
          <Item label="Contact email" value={org.contactEmail} />
          <Item label="Contact phone" value={org.contactPhone} />
          <Item label="Tax registration number (TIN)" value={org.gstNumber} />
        </dl>
      </Panel>
      <EditOrganizationDialog
        organization={org}
        open={editOpen}
        onOpenChange={setEditOpen}
      />
    </>
  );
}

const organizationSchema = z.object({
  name: z.string().min(2).max(160),
  legalName: z.string().max(200).optional(),
  timezone: z.string().min(3).max(80),
  contactEmail: z.union([z.email(), z.literal("")]),
  contactPhone: z.string().max(30).optional(),
  gstNumber: z.string().max(64).optional(),
});
type OrganizationValues = z.infer<typeof organizationSchema>;

function EditOrganizationDialog({
  organization,
  open,
  onOpenChange,
}: {
  organization: Organization;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<OrganizationValues>({
    resolver: zodResolver(organizationSchema),
    values: {
      name: organization.name ?? "",
      legalName: organization.legalName ?? "",
      timezone: organization.timezone ?? "Africa/Lagos",
      contactEmail: organization.contactEmail ?? "",
      contactPhone: organization.contactPhone ?? "",
      gstNumber: organization.gstNumber ?? "",
    },
  });
  const mutation = useMutation({
    mutationFn: (values: OrganizationValues) =>
      apiRequest<Organization>("/api/v1/organization", {
        method: "PATCH",
        body: {
          ...values,
          legalName: values.legalName || undefined,
          contactEmail: values.contactEmail || undefined,
          contactPhone: values.contactPhone || undefined,
          gstNumber: values.gstNumber || undefined,
        },
      }),
    onSuccess: (updated) => {
      client.setQueryData(["organization"], updated);
      toast({ tone: "success", title: "Organization updated" });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Edit organization"
      description="Code, currency, AWB prefix, and status remain immutable."
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
            Save changes
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Display name"
          htmlFor="orgName"
          required
          error={errors.name?.message}
        >
          <Input id="orgName" {...register("name")} />
        </Field>
        <Field
          label="Legal name"
          htmlFor="orgLegalName"
          error={errors.legalName?.message}
        >
          <Input id="orgLegalName" {...register("legalName")} />
        </Field>
        <Field
          label="Timezone"
          htmlFor="orgTimezone"
          required
          error={errors.timezone?.message}
        >
          <Input id="orgTimezone" {...register("timezone")} />
        </Field>
        <Field
          label="Contact email"
          htmlFor="orgEmail"
          error={errors.contactEmail?.message}
        >
          <Input id="orgEmail" type="email" {...register("contactEmail")} />
        </Field>
        <Field
          label="Contact phone"
          htmlFor="orgPhone"
          error={errors.contactPhone?.message}
        >
          <Input id="orgPhone" {...register("contactPhone")} />
        </Field>
        <Field
          label="Tax registration number (TIN)"
          htmlFor="orgGst"
          error={errors.gstNumber?.message}
        >
          <Input id="orgGst" className="uppercase" {...register("gstNumber")} />
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
      <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">
        {label}
      </dt>
      <dd className="mt-1.5 text-sm font-medium text-slate-900">
        {value || "—"}
      </dd>
    </div>
  );
}
