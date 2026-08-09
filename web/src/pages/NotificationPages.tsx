import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  BellRing,
  Braces,
  CheckCircle2,
  Eye,
  Mail,
  MessageCircle,
  Pencil,
  Plus,
  RotateCcw,
  Send,
  Smartphone,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { Link, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type CreateNotificationTemplateRequest,
  type NotificationChannel,
  type NotificationChannelListResponse,
  type NotificationDetail,
  type NotificationHealthResponse,
  type NotificationListResponse,
  type NotificationPreviewResponse,
  type NotificationTemplate,
  type NotificationTemplateListResponse,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { useToast } from "../components/ToastProvider";
import {
  Badge,
  Button,
  ConfirmAction,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  FilterBar,
  InlineNotice,
  Input,
  LoadingState,
  PageHeader,
  Panel,
  PanelHeader,
  Select,
  StatusBadge,
  Switch,
  TableCell,
  TableHead,
  Textarea,
} from "../components/ui";
import { cn, formatDateTime, titleCase } from "../lib/utils";

const notificationEvents = [
  "SHIPMENT_BOOKED",
  "PICKUP_COMPLETED",
  "SHIPMENT_IN_TRANSIT",
  "SHIPMENT_OUT_FOR_DELIVERY",
  "SHIPMENT_DELIVERED",
  "SHIPMENT_NDR",
  "SHIPMENT_RTO",
] as const;
const channels: NotificationChannel[] = ["SMS", "EMAIL", "WHATSAPP", "PUSH"];

const templateSchema = z
  .object({
    code: z.string().trim().min(2).max(80),
    name: z.string().trim().min(2).max(120),
    eventType: z.string().trim().min(2).max(100),
    channel: z.enum(["SMS", "EMAIL", "WHATSAPP", "PUSH"]),
    locale: z.string().trim().min(2).max(20),
    subject: z.string().trim().max(240),
    body: z.string().trim().min(2).max(10_000),
    providerRef: z.string().trim().max(240),
    isActive: z.boolean(),
  })
  .superRefine((value, context) => {
    if (value.channel === "EMAIL" && !value.subject) {
      context.addIssue({
        code: "custom",
        path: ["subject"],
        message: "Email templates require a subject",
      });
    }
  });
type TemplateForm = z.infer<typeof templateSchema>;

function templateDefaults(template?: NotificationTemplate): TemplateForm {
  return {
    code: template?.code ?? "",
    name: template?.name ?? "",
    eventType: template?.eventType ?? "SHIPMENT_BOOKED",
    channel: template?.channel ?? "SMS",
    locale: template?.locale ?? "en-NG",
    subject: template?.subject ?? "",
    body: template?.body ?? "",
    providerRef: template?.providerRef ?? "",
    isActive: template?.isActive ?? true,
  };
}

function ChannelIcon({
  channel,
  className,
}: {
  channel?: string;
  className?: string;
}) {
  const Icon: LucideIcon =
    channel === "EMAIL"
      ? Mail
      : channel === "PUSH"
        ? Smartphone
        : channel === "WHATSAPP"
          ? MessageCircle
          : Send;
  return <Icon aria-hidden className={cn("h-4 w-4", className)} />;
}

export function NotificationTemplatesPage() {
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const [channel, setChannel] = useState("");
  const [eventType, setEventType] = useState("");
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<NotificationTemplate>();
  const [previewing, setPreviewing] = useState<NotificationTemplate>();
  const form = useForm<TemplateForm>({
    resolver: zodResolver(templateSchema),
    defaultValues: templateDefaults(),
  });
  const query = useQuery({
    queryKey: ["notification-templates", channel, eventType],
    queryFn: () =>
      apiRequest<NotificationTemplateListResponse>(
        `/api/v1/notifications/templates${queryString({ channel, eventType, limit: 200, offset: 0 })}`,
      ),
  });
  const create = useMutation({
    mutationFn: (values: TemplateForm) => {
      const body: CreateNotificationTemplateRequest = {
        code: values.code,
        name: values.name,
        eventType: values.eventType,
        channel: values.channel,
        locale: values.locale,
        subject: values.subject || undefined,
        body: values.body,
        providerRef: values.providerRef || undefined,
      };
      return apiRequest<NotificationTemplate>(
        "/api/v1/notifications/templates",
        { method: "POST", body },
      );
    },
    onSuccess: (template) => {
      setEditorOpen(false);
      setEditing(undefined);
      form.reset(templateDefaults());
      setPreviewing(template);
      void queryClient.invalidateQueries({
        queryKey: ["notification-templates"],
      });
    },
  });
  const update = useMutation({
    mutationFn: (values: TemplateForm) =>
      apiRequest<NotificationTemplate>(
        `/api/v1/notifications/templates/${editing?.id}`,
        {
          method: "PATCH",
          body: {
            name: values.name,
            subject: values.subject,
            body: values.body,
            providerRef: values.providerRef,
            isActive: values.isActive,
          },
        },
      ),
    onSuccess: (template) => {
      setEditorOpen(false);
      setEditing(undefined);
      form.reset(templateDefaults());
      setPreviewing(template);
      void queryClient.invalidateQueries({
        queryKey: ["notification-templates"],
      });
    },
  });
  const openCreate = () => {
    setEditing(undefined);
    form.reset(templateDefaults());
    setEditorOpen(true);
  };
  const openEdit = (template: NotificationTemplate) => {
    setEditing(template);
    form.reset(templateDefaults(template));
    setEditorOpen(true);
  };
  const rows = query.data?.data ?? [];
  const watchedChannel = form.watch("channel");
  const watchedBody = form.watch("body");
  const draftVariables = useMemo(
    () =>
      [...watchedBody.matchAll(/{{\s*([a-zA-Z0-9_.-]+)\s*}}/g)]
        .map((match) => match[1] ?? "")
        .filter(Boolean),
    [watchedBody],
  );
  return (
    <>
      <PageHeader
        eyebrow="Administration · Notifications"
        title="Message templates"
        description="Create provider-neutral templates by event, channel, and locale. The backend derives and validates every {{variable}} reference."
        actions={
          hasPermission("notification.template") ? (
            <Button variant="primary" onClick={openCreate}>
              <Plus aria-hidden className="h-4 w-4" /> New template
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <FilterBar>
          <Select
            aria-label="Template event"
            value={eventType}
            onChange={(event) => setEventType(event.target.value)}
          >
            <option value="">All events</option>
            {notificationEvents.map((event) => (
              <option key={event}>{event}</option>
            ))}
          </Select>
          <Select
            aria-label="Template channel"
            value={channel}
            onChange={(event) => setChannel(event.target.value)}
          >
            <option value="">All channels</option>
            {channels.map((item) => (
              <option key={item}>{item}</option>
            ))}
          </Select>
        </FilterBar>
        {query.isLoading ? (
          <LoadingState label="Loading notification templates" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length ? (
          <DataTable label="Notification templates">
            <thead>
              <tr>
                <TableHead>Template</TableHead>
                <TableHead>Event</TableHead>
                <TableHead>Channel</TableHead>
                <TableHead>Locale</TableHead>
                <TableHead>Variables</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Updated</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </tr>
            </thead>
            <tbody>
              {rows.map((template) => (
                <tr key={template.id}>
                  <TableCell>
                    <strong>{template.name}</strong>
                    <span className="block font-mono text-xs text-slate-500">
                      {template.code}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs">
                      {template.eventType}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="inline-flex items-center gap-2">
                      <ChannelIcon channel={template.channel} />{" "}
                      {titleCase(template.channel ?? "")}
                    </span>
                  </TableCell>
                  <TableCell>{template.locale}</TableCell>
                  <TableCell>
                    {template.variables?.length ? (
                      <div className="flex max-w-72 flex-wrap gap-1">
                        {template.variables.map((variable) => (
                          <Badge key={variable}>{`{{${variable}}}`}</Badge>
                        ))}
                      </div>
                    ) : (
                      <span className="text-slate-500">None</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <StatusBadge
                      status={template.isActive ? "ACTIVE" : "INACTIVE"}
                    />
                  </TableCell>
                  <TableCell>{formatDateTime(template.updatedAt)}</TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <Button size="sm" onClick={() => setPreviewing(template)}>
                        <Eye aria-hidden className="h-4 w-4" /> Preview
                      </Button>
                      {hasPermission("notification.template") ? (
                        <Button size="sm" onClick={() => openEdit(template)}>
                          <Pencil aria-hidden className="h-4 w-4" /> Edit
                        </Button>
                      ) : null}
                    </div>
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            icon={BellRing}
            title="No notification templates"
            description="Create a template for a customer-facing shipment milestone."
            action={
              hasPermission("notification.template") ? (
                <Button onClick={openCreate}>New template</Button>
              ) : undefined
            }
          />
        )}
      </Panel>
      <Dialog
        open={editorOpen}
        onOpenChange={setEditorOpen}
        title={
          editing
            ? "Edit notification template"
            : "Create notification template"
        }
        description={
          editing
            ? "Event, channel, locale, and code identify this template and cannot be changed."
            : "Only one active template can serve an event, channel, and locale combination."
        }
        footer={
          <>
            <Button onClick={() => setEditorOpen(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={create.isPending || update.isPending}
              onClick={() =>
                void form.handleSubmit((values) =>
                  editing ? update.mutate(values) : create.mutate(values),
                )()
              }
            >
              {editing ? "Save and validate" : "Create and validate"}
            </Button>
          </>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Code"
            htmlFor="template-code"
            required
            error={form.formState.errors.code?.message}
          >
            <Input
              id="template-code"
              disabled={Boolean(editing)}
              {...form.register("code")}
              placeholder="SHIPMENT_BOOKED_SMS_EN_NG"
            />
          </Field>
          <Field
            label="Name"
            htmlFor="template-name"
            required
            error={form.formState.errors.name?.message}
          >
            <Input id="template-name" {...form.register("name")} />
          </Field>
          <Field
            label="Event trigger"
            htmlFor="template-event"
            required
            error={form.formState.errors.eventType?.message}
          >
            <Select
              id="template-event"
              disabled={Boolean(editing)}
              {...form.register("eventType")}
            >
              {notificationEvents.map((event) => (
                <option key={event}>{event}</option>
              ))}
            </Select>
          </Field>
          <Field
            label="Channel"
            htmlFor="template-channel"
            required
            error={form.formState.errors.channel?.message}
          >
            <Select
              id="template-channel"
              disabled={Boolean(editing)}
              {...form.register("channel")}
            >
              {channels.map((item) => (
                <option key={item}>{item}</option>
              ))}
            </Select>
          </Field>
          <Field
            label="Locale"
            htmlFor="template-locale"
            required
            hint="Use a BCP 47 locale such as en-NG"
            error={form.formState.errors.locale?.message}
          >
            <Input
              id="template-locale"
              disabled={Boolean(editing)}
              {...form.register("locale")}
            />
          </Field>
          <Field
            label="Provider reference"
            htmlFor="template-provider"
            hint="Approved WhatsApp template or registered SMS sender ID, where required"
          >
            <Input id="template-provider" {...form.register("providerRef")} />
          </Field>
          {watchedChannel === "EMAIL" ? (
            <Field
              className="sm:col-span-2"
              label="Subject"
              htmlFor="template-subject"
              required
              error={form.formState.errors.subject?.message}
            >
              <Input id="template-subject" {...form.register("subject")} />
            </Field>
          ) : null}
          <Field
            className="sm:col-span-2"
            label="Body"
            htmlFor="template-body"
            required
            hint="Variables use {{name}}. Validation and rendering are performed by the backend."
            error={form.formState.errors.body?.message}
          >
            <Textarea
              id="template-body"
              className="min-h-40 font-mono"
              {...form.register("body")}
              placeholder="Your shipment {{awb}} has been booked."
            />
          </Field>
          <div className="sm:col-span-2 rounded-md border bg-slate-50 p-3">
            <p className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-slate-600">
              <Braces aria-hidden className="h-4 w-4" /> Variables referenced in
              this draft
            </p>
            <div className="mt-2 flex flex-wrap gap-1">
              {draftVariables.length ? (
                [...new Set(draftVariables)].map((variable) => (
                  <Badge key={variable}>{`{{${variable}}}`}</Badge>
                ))
              ) : (
                <span className="text-sm text-slate-500">
                  No variables referenced.
                </span>
              )}
            </div>
          </div>
          {editing ? (
            <div className="sm:col-span-2 flex items-center justify-between rounded-md border p-3">
              <div>
                <strong className="block text-sm">Template active</strong>
                <span className="text-xs text-slate-500">
                  Inactive templates are not selected for new messages.
                </span>
              </div>
              <Switch
                checked={form.watch("isActive")}
                onCheckedChange={(checked) =>
                  form.setValue("isActive", checked, { shouldDirty: true })
                }
                label="Template active"
              />
            </div>
          ) : null}
          {create.error || update.error ? (
            <div className="sm:col-span-2">
              <ErrorState error={create.error || update.error} />
            </div>
          ) : null}
        </div>
      </Dialog>
      <TemplatePreviewDialog
        template={previewing}
        onClose={() => setPreviewing(undefined)}
      />
    </>
  );
}

function TemplatePreviewDialog({
  template,
  onClose,
}: {
  template?: NotificationTemplate;
  onClose: () => void;
}) {
  const variables = template?.variables ?? [];
  const [samples, setSamples] = useState<Record<string, string>>({});
  useEffect(
    () =>
      setSamples(
        Object.fromEntries(
          (template?.variables ?? []).map((variable) => [variable, ""]),
        ),
      ),
    [template],
  );
  const preview = useMutation({
    mutationFn: () =>
      apiRequest<NotificationPreviewResponse>(
        `/api/v1/notifications/templates/${template?.id}/preview`,
        { method: "POST", body: { variables: samples } },
      ),
  });
  return (
    <Dialog
      open={Boolean(template)}
      onOpenChange={(open) => !open && onClose()}
      title={`Preview · ${template?.name ?? "template"}`}
      description="The backend renders this preview with sample data and reports every missing variable."
      footer={
        <>
          <Button onClick={onClose}>Close</Button>
          <Button
            variant="primary"
            loading={preview.isPending}
            onClick={() => preview.mutate()}
          >
            <Eye aria-hidden className="h-4 w-4" /> Render preview
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {variables.length ? (
          <div className="grid gap-3 sm:grid-cols-2">
            {variables.map((variable) => (
              <Field
                key={variable}
                label={`{{${variable}}}`}
                htmlFor={`preview-${variable}`}
              >
                <Input
                  id={`preview-${variable}`}
                  value={samples[variable] ?? ""}
                  onChange={(event) =>
                    setSamples((current) => ({
                      ...current,
                      [variable]: event.target.value,
                    }))
                  }
                  placeholder={`Sample ${variable}`}
                />
              </Field>
            ))}
          </div>
        ) : (
          <InlineNotice tone="info" title="No sample data required">
            This template has no variables.
          </InlineNotice>
        )}
        {preview.data ? (
          <div className="rounded-lg border bg-slate-50 p-4" aria-live="polite">
            {preview.data.missingVariables?.length ? (
              <InlineNotice tone="warning" title="Missing variables">
                {preview.data.missingVariables
                  .map((item) => `{{${item}}}`)
                  .join(", ")}
              </InlineNotice>
            ) : (
              <InlineNotice tone="success" title="All variables resolved">
                The server rendered this template without missing values.
              </InlineNotice>
            )}
            {preview.data.subject ? (
              <>
                <p className="mt-4 text-xs font-semibold uppercase tracking-wide text-slate-500">
                  Subject
                </p>
                <p className="mt-1 font-semibold">{preview.data.subject}</p>
              </>
            ) : null}
            <p className="mt-4 text-xs font-semibold uppercase tracking-wide text-slate-500">
              Body
            </p>
            <p className="mt-1 whitespace-pre-wrap text-sm leading-6">
              {preview.data.body}
            </p>
          </div>
        ) : null}
        {preview.error ? <ErrorState error={preview.error} /> : null}
      </div>
    </Dialog>
  );
}

export function NotificationTriggersPage() {
  const query = useQuery({
    queryKey: ["notification-templates", "trigger-matrix"],
    queryFn: () =>
      apiRequest<NotificationTemplateListResponse>(
        "/api/v1/notifications/templates?limit=200&offset=0",
      ),
  });
  const templates = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Administration · Notifications"
        title="Event triggers"
        description="Coverage by backend shipment milestone. A trigger sends only when an active template exists for the recipient’s channel and locale."
      />
      <Panel>
        {query.isLoading ? (
          <LoadingState label="Loading trigger coverage" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : (
          <DataTable label="Notification event trigger coverage">
            <thead>
              <tr>
                <TableHead>Event</TableHead>
                {channels.map((channel) => (
                  <TableHead key={channel}>{titleCase(channel)}</TableHead>
                ))}
              </tr>
            </thead>
            <tbody>
              {notificationEvents.map((eventType) => (
                <tr key={eventType}>
                  <TableCell>
                    <strong className="font-mono text-xs">{eventType}</strong>
                  </TableCell>
                  {channels.map((channel) => {
                    const matching = templates.filter(
                      (template) =>
                        template.eventType === eventType &&
                        template.channel === channel &&
                        template.isActive,
                    );
                    return (
                      <TableCell key={channel}>
                        {matching.length ? (
                          <span className="inline-flex items-center gap-1.5 text-sm font-semibold text-emerald-800">
                            <CheckCircle2 aria-hidden className="h-4 w-4" />{" "}
                            {matching.length} active
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1.5 text-sm text-slate-500">
                            <XCircle aria-hidden className="h-4 w-4" /> No
                            template
                          </span>
                        )}
                      </TableCell>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </DataTable>
        )}
      </Panel>
      <InlineNotice tone="info" title="Milestone triggers are backend-owned">
        Bagging, manifesting, and other internal handling scans deliberately do
        not notify recipients. This screen shows template coverage; it does not
        change shipment transition rules.
      </InlineNotice>
    </>
  );
}

export function NotificationChannelsPage() {
  const channelQuery = useQuery({
    queryKey: ["notification-channels"],
    queryFn: () =>
      apiRequest<NotificationChannelListResponse>(
        "/api/v1/notifications/channels",
      ),
  });
  const healthQuery = useQuery({
    queryKey: ["notification-health", 24],
    queryFn: () =>
      apiRequest<NotificationHealthResponse>(
        "/api/v1/notifications/health?hours=24",
      ),
  });
  const error = channelQuery.error || healthQuery.error;
  return (
    <>
      <PageHeader
        eyebrow="Administration · Notifications"
        title="Channels and health"
        description="Compiled provider adapters and the last 24 hours of delivery outcomes. Channels are deployment configuration, not editable records."
      />
      {channelQuery.isLoading || healthQuery.isLoading ? (
        <LoadingState label="Loading notification channel health" />
      ) : error ? (
        <ErrorState
          error={error}
          retry={() => {
            void channelQuery.refetch();
            void healthQuery.refetch();
          }}
        />
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {channelQuery.data?.data?.map((item) => (
              <Panel key={item.channel} className="p-4">
                <div className="flex items-center justify-between">
                  <span className="grid h-9 w-9 place-items-center rounded-md bg-slate-100">
                    <ChannelIcon channel={item.channel} />
                  </span>
                  <Badge tone={item.configured ? "success" : "danger"}>
                    {item.configured ? "Configured" : "Not configured"}
                  </Badge>
                </div>
                <strong className="mt-4 block">
                  {titleCase(item.channel ?? "Channel")}
                </strong>
                <p className="mt-1 text-xs text-slate-500">
                  {item.configured
                    ? "Provider sender available"
                    : "Messages on this channel are suppressed"}
                </p>
              </Panel>
            ))}
          </div>
          <Panel className="mt-5">
            <PanelHeader
              title="Delivery outcomes"
              description={`Last ${healthQuery.data?.windowHours ?? 24} hours`}
            />
            <div className="grid gap-px bg-border sm:grid-cols-2 lg:grid-cols-4">
              {Object.entries(healthQuery.data?.byStatus ?? {}).map(
                ([status, count]) => (
                  <div key={status} className="bg-white p-4">
                    <StatusBadge status={status} />
                    <strong className="mt-2 block text-2xl tabular-nums">
                      {count}
                    </strong>
                  </div>
                ),
              )}
            </div>
          </Panel>
        </>
      )}
    </>
  );
}

export function NotificationDeliveriesPage() {
  const [status, setStatus] = useState("");
  const [channel, setChannel] = useState("");
  const [eventType, setEventType] = useState("");
  const [cursor, setCursor] = useState<number>();
  const query = useQuery({
    queryKey: ["notifications", status, channel, eventType, cursor],
    queryFn: () =>
      apiRequest<NotificationListResponse>(
        `/api/v1/notifications${queryString({ status, channel, eventType, cursor, limit: 50 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Administration · Notifications"
        title="Delivery attempts"
        description="What was sent or suppressed, to whom, through which channel, and why it failed."
      />
      <Panel>
        <FilterBar>
          <Select
            aria-label="Notification status"
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              setCursor(undefined);
            }}
          >
            <option value="">All statuses</option>
            <option>PENDING</option>
            <option>SENT</option>
            <option>DELIVERED</option>
            <option>FAILED</option>
            <option>DEAD_LETTER</option>
            <option>SUPPRESSED</option>
            <option>CANCELLED</option>
          </Select>
          <Select
            aria-label="Notification channel"
            value={channel}
            onChange={(event) => {
              setChannel(event.target.value);
              setCursor(undefined);
            }}
          >
            <option value="">All channels</option>
            {channels.map((item) => (
              <option key={item}>{item}</option>
            ))}
          </Select>
          <Input
            aria-label="Notification event"
            value={eventType}
            onChange={(event) => {
              setEventType(event.target.value);
              setCursor(undefined);
            }}
            placeholder="Event type"
          />
        </FilterBar>
        {query.isLoading ? (
          <LoadingState label="Loading notification attempts" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length ? (
          <>
            <DataTable label="Notification delivery attempts">
              <thead>
                <tr>
                  <TableHead>Event</TableHead>
                  <TableHead>Recipient</TableHead>
                  <TableHead>Channel</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Attempt</TableHead>
                  <TableHead>Time</TableHead>
                  <TableHead>Error summary</TableHead>
                  <TableHead>
                    <span className="sr-only">Open</span>
                  </TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((item) => (
                  <tr key={item.id}>
                    <TableCell>
                      <strong className="font-mono text-xs">
                        {item.eventType}
                      </strong>
                      {item.awb ? (
                        <span className="block font-mono text-xs text-slate-500">
                          {item.awb}
                        </span>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      {item.recipientName ??
                        titleCase(item.recipientType ?? "Recipient")}
                    </TableCell>
                    <TableCell>
                      <span className="inline-flex items-center gap-2">
                        <ChannelIcon channel={item.channel} />
                        {titleCase(item.channel ?? "")}
                      </span>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={item.status} />
                      {item.suppressedReason ? (
                        <span className="mt-1 block text-xs text-slate-500">
                          {titleCase(item.suppressedReason)}
                        </span>
                      ) : null}
                    </TableCell>
                    <TableCell className="font-mono tabular-nums">
                      {item.attemptCount ?? 0}
                    </TableCell>
                    <TableCell>{formatDateTime(item.createdAt)}</TableCell>
                    <TableCell>
                      <span className="line-clamp-2 max-w-64 text-xs">
                        {item.lastError ?? "—"}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Link
                        className="font-semibold text-primary hover:underline"
                        to={`/admin/notifications/deliveries/${item.id}`}
                      >
                        Inspect
                      </Link>
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <div className="flex justify-end border-t p-3">
              <Button
                disabled={query.data?.nextCursor == null}
                onClick={() => setCursor(query.data?.nextCursor)}
              >
                Next page
              </Button>
            </div>
          </>
        ) : (
          <EmptyState
            title="No notification attempts"
            description="No messages match the selected filters."
          />
        )}
      </Panel>
    </>
  );
}

export function NotificationDeliveryDetailPage() {
  const { notificationId = "" } = useParams();
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [confirmRetry, setConfirmRetry] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const query = useQuery({
    queryKey: ["notification", notificationId],
    queryFn: () =>
      apiRequest<NotificationDetail>(`/api/v1/notifications/${notificationId}`),
    enabled: Boolean(notificationId),
    retry: false,
  });
  const retry = useMutation({
    mutationFn: () =>
      apiRequest(`/api/v1/notifications/${notificationId}/retry`, {
        method: "POST",
      }),
    onSuccess: () => {
      setConfirmRetry(false);
      toast({ tone: "success", title: "Notification queued again" });
      void queryClient.invalidateQueries({
        queryKey: ["notification", notificationId],
      });
    },
  });
  const cancel = useMutation({
    mutationFn: () =>
      apiRequest(`/api/v1/notifications/${notificationId}/cancel`, {
        method: "POST",
        body: { reason: "Cancelled by notification administrator" },
      }),
    onSuccess: () => {
      setConfirmCancel(false);
      toast({ tone: "success", title: "Notification cancelled" });
      void queryClient.invalidateQueries({
        queryKey: ["notification", notificationId],
      });
    },
  });
  if (query.isLoading)
    return <LoadingState label="Loading notification evidence" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const item = query.data;
  if (!item)
    return (
      <EmptyState
        title="Notification not found"
        description="It may have been removed or belong to another organization."
      />
    );
  const canRetry =
    hasPermission("notification.retry") &&
    ["FAILED", "DEAD_LETTER"].includes(item.status ?? "");
  const canCancel =
    hasPermission("notification.retry") &&
    ["PENDING", "SENDING"].includes(item.status ?? "");
  return (
    <>
      <PageHeader
        eyebrow="Administration · Notifications"
        title={item.eventType ?? "Notification"}
        description={`${item.recipientName ?? item.recipientAddress ?? "Recipient"} · ${titleCase(item.channel ?? "")}`}
        actions={
          <>
            {canRetry ? (
              <Button onClick={() => setConfirmRetry(true)}>
                <RotateCcw aria-hidden className="h-4 w-4" /> Retry
              </Button>
            ) : null}
            {canCancel ? (
              <Button variant="danger" onClick={() => setConfirmCancel(true)}>
                Cancel unsent message
              </Button>
            ) : null}
          </>
        }
      />
      <div className="grid gap-5 xl:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
        <div className="space-y-5">
          <Panel>
            <PanelHeader title="Delivery state" />
            <dl className="grid gap-4 p-4 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-slate-500">Status</dt>
                <dd className="mt-1">
                  <StatusBadge status={item.status} />
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Attempts</dt>
                <dd className="mt-1 font-semibold">
                  {item.attemptCount ?? 0} of {item.maxRetries ?? "—"}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Created</dt>
                <dd>{formatDateTime(item.createdAt)}</dd>
              </div>
              <div>
                <dt className="text-slate-500">Next attempt</dt>
                <dd>{formatDateTime(item.nextAttemptAt)}</dd>
              </div>
              <div>
                <dt className="text-slate-500">Template</dt>
                <dd>{item.templateCode ?? "No matching template"}</dd>
              </div>
              <div>
                <dt className="text-slate-500">Locale</dt>
                <dd>{item.locale ?? "—"}</dd>
              </div>
            </dl>
          </Panel>
          <Panel>
            <PanelHeader
              title="Rendered message"
              description="The exact text composed for this recipient"
            />
            <div className="p-4">
              {item.subject ? (
                <>
                  <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">
                    Subject
                  </p>
                  <p className="mt-1 font-semibold">{item.subject}</p>
                </>
              ) : null}
              <p className="mt-4 whitespace-pre-wrap text-sm leading-6">
                {item.body}
              </p>
            </div>
          </Panel>
        </div>
        <Panel>
          <PanelHeader
            title="Provider attempts"
            description="Provider response, result, latency, and retryability for each try"
          />
          {item.attempts?.length ? (
            <DataTable label="Notification provider attempt history">
              <thead>
                <tr>
                  <TableHead>Attempt</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Time</TableHead>
                  <TableHead>Latency</TableHead>
                  <TableHead>Retryable</TableHead>
                  <TableHead>Error</TableHead>
                </tr>
              </thead>
              <tbody>
                {item.attempts.map((attempt) => (
                  <tr key={`${attempt.attemptNo}-${attempt.attemptedAt}`}>
                    <TableCell className="font-mono">
                      #{attempt.attemptNo}
                    </TableCell>
                    <TableCell>{attempt.provider ?? "Not reported"}</TableCell>
                    <TableCell>
                      <StatusBadge
                        status={attempt.outcome ?? attempt.providerStatus}
                      />
                    </TableCell>
                    <TableCell>{formatDateTime(attempt.attemptedAt)}</TableCell>
                    <TableCell className="font-mono tabular-nums">
                      {attempt.durationMs != null
                        ? `${attempt.durationMs} ms`
                        : "—"}
                    </TableCell>
                    <TableCell>
                      {attempt.retryable == null
                        ? "—"
                        : attempt.retryable
                          ? "Yes"
                          : "No"}
                    </TableCell>
                    <TableCell>
                      <span className="max-w-72 text-xs">
                        {attempt.errorMessage ?? "—"}
                      </span>
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
          ) : (
            <EmptyState
              title="No provider attempts"
              description={
                item.suppressedReason
                  ? `Message suppressed: ${titleCase(item.suppressedReason)}`
                  : "The sender has not attempted this message yet."
              }
            />
          )}
        </Panel>
      </div>
      <ConfirmAction
        open={confirmRetry}
        onOpenChange={setConfirmRetry}
        title="Retry this notification?"
        description="The backend will reset the attempt budget because this action assumes the delivery problem has been corrected."
        confirmLabel="Queue retry"
        loading={retry.isPending}
        onConfirm={() => retry.mutate()}
      >
        {retry.error ? <ErrorState error={retry.error} /> : null}
      </ConfirmAction>
      <ConfirmAction
        open={confirmCancel}
        onOpenChange={setConfirmCancel}
        title="Cancel this unsent notification?"
        description="The message will not be delivered. The cancellation remains visible in its audit state."
        confirmLabel="Cancel message"
        loading={cancel.isPending}
        onConfirm={() => cancel.mutate()}
      >
        {cancel.error ? <ErrorState error={cancel.error} /> : null}
      </ConfirmAction>
    </>
  );
}
