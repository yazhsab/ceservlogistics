import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  Ban,
  Cable,
  CheckCircle2,
  CirclePause,
  Clock3,
  KeyRound,
  Plus,
  RotateCcw,
  Webhook,
} from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { Link, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type APIKey,
  type APIKeyListResponse,
  type APIKeyUsage,
  type CreatedWebhookEndpoint,
  type IssueAPIKeyRequest,
  type IssuedAPIKey,
  type PartnerScope,
  type ReplayWebhookResponse,
  type WebhookAttemptListResponse,
  type WebhookDeliveryDetail,
  type WebhookDeliveryPage,
  type WebhookEndpoint,
  type WebhookEndpointListResponse,
  type WebhookEventListResponse,
  type WebhookEventType,
  type WebhookSubscriptionListResponse,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import {
  HttpStatus,
  IntegrationStatus,
  OneTimeSecretDialog,
  ScopeList,
} from "../components/integrations";
import { useToast } from "../components/ToastProvider";
import {
  Badge,
  Button,
  Checkbox,
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
  Pagination,
  Panel,
  PanelHeader,
  Select,
  TableCell,
  TableHead,
  Textarea,
} from "../components/ui";
import { formatDateTime, titleCase } from "../lib/utils";

const countFormatter = new Intl.NumberFormat("en-NG");

function formatCount(value?: number | null) {
  return countFormatter.format(value ?? 0);
}

function safeHttpsUrl(value?: string | null) {
  if (!value) return undefined;
  try {
    const parsed = new URL(value);
    return parsed.protocol === "https:" ? parsed : undefined;
  } catch {
    return undefined;
  }
}

const partnerScopes = [
  "serviceability:read",
  "pricing:read",
  "shipment:create",
  "shipment:read",
  "shipment:cancel",
  "tracking:read",
  "pickup:create",
  "pickup:read",
  "label:read",
  "pod:read",
  "webhook:manage",
] satisfies PartnerScope[];

const keySchema = z.object({
  name: z.string().trim().min(2).max(120),
  scopes: z.array(z.string()),
  allowedCidrs: z.string(),
  expiresAt: z.string(),
  rateLimit: z
    .string()
    .refine(
      (value) => !value || (/^\d+$/.test(value) && Number(value) >= 1),
      "Use a positive requests-per-minute value",
    ),
});
type KeyForm = z.infer<typeof keySchema>;

function splitLines(value: string) {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function APIKeysPage() {
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [status, setStatus] = useState("");
  const [offset, setOffset] = useState(0);
  const [createOpen, setCreateOpen] = useState(false);
  const [issued, setIssued] = useState<IssuedAPIKey>();
  const [usageKey, setUsageKey] = useState<APIKey>();
  const [suspendAction, setSuspendAction] = useState<{
    key: APIKey;
    suspend: boolean;
  }>();
  const [revokeKey, setRevokeKey] = useState<APIKey>();
  const [reason, setReason] = useState("");
  const form = useForm<KeyForm>({
    resolver: zodResolver(keySchema),
    defaultValues: {
      name: "",
      scopes: [],
      allowedCidrs: "",
      expiresAt: "",
      rateLimit: "",
    },
  });
  const query = useQuery({
    queryKey: ["api-keys", status, offset],
    queryFn: () =>
      apiRequest<APIKeyListResponse>(
        `/api/v1/api-keys${queryString({ status, limit: 50, offset })}`,
      ),
  });
  const create = useMutation({
    mutationFn: (values: KeyForm) => {
      const body: IssueAPIKeyRequest = {
        name: values.name,
        scopes: values.scopes as PartnerScope[],
        allowedCidrs: splitLines(values.allowedCidrs),
        expiresAt: values.expiresAt
          ? new Date(values.expiresAt).toISOString()
          : undefined,
        rateLimitPerMinute: values.rateLimit
          ? Number(values.rateLimit)
          : undefined,
      };
      return apiRequest<IssuedAPIKey>("/api/v1/api-keys", {
        method: "POST",
        body,
      });
    },
    onSuccess: (result) => {
      setCreateOpen(false);
      setIssued(result);
      form.reset();
      void queryClient.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });
  const suspend = useMutation({
    mutationFn: (action: { key: APIKey; suspend: boolean }) =>
      apiRequest<APIKey>(`/api/v1/api-keys/${action.key.id}/suspend`, {
        method: "POST",
        body: { suspend: action.suspend, reason: reason || undefined },
      }),
    onSuccess: (result) => {
      setSuspendAction(undefined);
      setReason("");
      toast({
        tone: "success",
        title:
          result.status === "SUSPENDED"
            ? "API key suspended"
            : "API key resumed",
        description: result.name,
      });
      void queryClient.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });
  const revoke = useMutation({
    mutationFn: () =>
      apiRequest<APIKey>(`/api/v1/api-keys/${revokeKey?.id}/revoke`, {
        method: "POST",
        body: { reason },
      }),
    onSuccess: (result) => {
      setRevokeKey(undefined);
      setReason("");
      toast({
        tone: "success",
        title: "API key permanently revoked",
        description: result.name,
      });
      void queryClient.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });
  const rows = query.data?.data ?? [];
  const selectedScopes = form.watch("scopes");
  return (
    <>
      <PageHeader
        eyebrow="Administration · Integrations"
        title="API credentials"
        description="Issue scoped partner credentials, review use, pause suspected leaks, and permanently revoke compromised keys. Secrets are never recoverable."
        actions={
          hasPermission("apikey.manage") ? (
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Issue credential
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <FilterBar>
          <Select
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              setOffset(0);
            }}
            aria-label="API credential status"
          >
            <option value="">All states</option>
            <option>ACTIVE</option>
            <option>SUSPENDED</option>
            <option>REVOKED</option>
            <option>EXPIRED</option>
          </Select>
        </FilterBar>
        {query.isLoading ? (
          <LoadingState label="Loading API credentials" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length ? (
          <>
            <DataTable label="API credentials">
              <thead>
                <tr>
                  <TableHead>Name and key</TableHead>
                  <TableHead>Scopes</TableHead>
                  <TableHead>Network</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead>Last used</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="text-right">Requests</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((key) => (
                  <tr key={key.id}>
                    <TableCell>
                      <strong className="block">{key.name}</strong>
                      <span className="font-mono text-xs text-slate-500">
                        {key.keyId} · {key.secretHint}…
                      </span>
                    </TableCell>
                    <TableCell>
                      <ScopeList scopes={key.scopes} />
                    </TableCell>
                    <TableCell>
                      {key.allowedCidrs?.length ? (
                        <span className="font-mono text-xs">
                          {key.allowedCidrs.join(", ")}
                        </span>
                      ) : (
                        <span className="text-xs text-slate-500">
                          Any source
                        </span>
                      )}
                    </TableCell>
                    <TableCell>{formatDateTime(key.createdAt)}</TableCell>
                    <TableCell>{formatDateTime(key.lastUsedAt)}</TableCell>
                    <TableCell>{formatDateTime(key.expiresAt)}</TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      {formatCount(key.requestCount)}
                    </TableCell>
                    <TableCell>
                      <IntegrationStatus status={key.status} />
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-1">
                        <Button size="sm" onClick={() => setUsageKey(key)}>
                          <Activity aria-hidden className="h-4 w-4" /> Usage
                        </Button>
                        {hasPermission("apikey.manage") &&
                        key.status === "ACTIVE" ? (
                          <Button
                            size="sm"
                            onClick={() =>
                              setSuspendAction({ key, suspend: true })
                            }
                          >
                            <CirclePause aria-hidden className="h-4 w-4" />{" "}
                            Suspend
                          </Button>
                        ) : null}
                        {hasPermission("apikey.manage") &&
                        key.status === "SUSPENDED" ? (
                          <Button
                            size="sm"
                            onClick={() =>
                              setSuspendAction({ key, suspend: false })
                            }
                          >
                            <CheckCircle2 aria-hidden className="h-4 w-4" />{" "}
                            Resume
                          </Button>
                        ) : null}
                        {hasPermission("apikey.manage") &&
                        !["REVOKED", "EXPIRED"].includes(key.status ?? "") ? (
                          <Button
                            size="sm"
                            variant="danger"
                            onClick={() => setRevokeKey(key)}
                          >
                            <Ban aria-hidden className="h-4 w-4" /> Revoke
                          </Button>
                        ) : null}
                      </div>
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <Pagination
              page={Math.floor(offset / 50) + 1}
              totalPages={Math.max(
                1,
                Math.ceil(
                  (query.data?.pagination?.totalItems ?? rows.length) / 50,
                ),
              )}
              onPageChange={(page) => setOffset((page - 1) * 50)}
              label={`${query.data?.pagination?.totalItems ?? rows.length} credentials`}
            />
          </>
        ) : (
          <EmptyState
            icon={KeyRound}
            title="No API credentials"
            description="Issue a credential only when a partner integration is ready to store it securely."
            action={
              hasPermission("apikey.manage") ? (
                <Button onClick={() => setCreateOpen(true)}>
                  Issue credential
                </Button>
              ) : undefined
            }
          />
        )}
      </Panel>
      <Dialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        title="Issue API credential"
        description="Start with the smallest scope set. A credential with no scopes cannot call partner operations."
        footer={
          <>
            <Button onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={create.isPending}
              onClick={() =>
                void form.handleSubmit((values) => create.mutate(values))()
              }
            >
              Issue once
            </Button>
          </>
        }
      >
        <div className="space-y-5">
          <Field
            label="Credential name"
            htmlFor="api-key-name"
            required
            error={form.formState.errors.name?.message}
          >
            <Input
              id="api-key-name"
              {...form.register("name")}
              placeholder="Warehouse management production"
            />
          </Field>
          <fieldset>
            <legend className="text-sm font-semibold text-slate-800">
              Access scopes
            </legend>
            <p className="mt-1 text-xs text-slate-500">
              Scopes are partner permissions, not employee role permissions.
            </p>
            <div className="mt-3 grid gap-2 sm:grid-cols-2">
              {partnerScopes.map((scope) => (
                <label
                  key={scope}
                  className="flex items-center gap-2 rounded-md border p-2.5 text-xs font-mono hover:bg-slate-50"
                >
                  <Checkbox
                    aria-label={`Grant ${scope}`}
                    checked={selectedScopes.includes(scope)}
                    onCheckedChange={(checked) =>
                      form.setValue(
                        "scopes",
                        checked
                          ? [...selectedScopes, scope]
                          : selectedScopes.filter((item) => item !== scope),
                        { shouldDirty: true },
                      )
                    }
                  />
                  {scope}
                </label>
              ))}
            </div>
          </fieldset>
          <Field
            label="Allowed CIDRs"
            htmlFor="api-key-cidrs"
            hint="Optional · one CIDR per line. Empty allows any source IP."
          >
            <Textarea
              id="api-key-cidrs"
              {...form.register("allowedCidrs")}
              placeholder="203.0.113.0/24"
            />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Expires" htmlFor="api-key-expires" hint="Optional">
              <Input
                id="api-key-expires"
                type="datetime-local"
                {...form.register("expiresAt")}
              />
            </Field>
            <Field
              label="Rate limit / minute"
              htmlFor="api-key-rate"
              hint="Optional · organization default otherwise"
              error={form.formState.errors.rateLimit?.message}
            >
              <Input
                id="api-key-rate"
                inputMode="numeric"
                {...form.register("rateLimit")}
              />
            </Field>
          </div>
          {create.error ? <ErrorState error={create.error} /> : null}
        </div>
      </Dialog>
      <OneTimeSecretDialog
        open={Boolean(issued)}
        onOpenChange={(open) => !open && setIssued(undefined)}
        title="API credential issued"
        label="Authorization token"
        secret={issued?.token}
        warning={issued?.warning}
      >
        <ScopeList scopes={issued?.key?.scopes} />
      </OneTimeSecretDialog>
      <APIKeyUsageDialog
        keyRecord={usageKey}
        onOpenChange={(open) => !open && setUsageKey(undefined)}
      />
      <ConfirmAction
        open={Boolean(suspendAction)}
        onOpenChange={(open) => !open && setSuspendAction(undefined)}
        title={
          suspendAction?.suspend
            ? "Suspend this API key?"
            : "Resume this API key?"
        }
        description={
          suspendAction?.suspend
            ? "Partner calls will stop immediately. Suspension is reversible."
            : "Partner calls will resume with the existing secret and scope set."
        }
        confirmLabel={suspendAction?.suspend ? "Suspend key" : "Resume key"}
        loading={suspend.isPending}
        onConfirm={() => suspendAction && suspend.mutate(suspendAction)}
      >
        <Field
          label="Reason"
          htmlFor="key-suspend-reason"
          hint="Recommended for the security audit trail"
        >
          <Textarea
            id="key-suspend-reason"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </Field>
        {suspend.error ? <ErrorState error={suspend.error} /> : null}
      </ConfirmAction>
      <ConfirmAction
        open={Boolean(revokeKey)}
        onOpenChange={(open) => !open && setRevokeKey(undefined)}
        title="Permanently revoke this API key?"
        description="Revocation cannot be undone. The partner must receive a newly issued credential to reconnect."
        confirmLabel="Revoke permanently"
        loading={revoke.isPending}
        confirmDisabled={reason.trim().length < 3}
        onConfirm={() => revoke.mutate()}
      >
        <Field
          label="Revocation reason"
          htmlFor="key-revoke-reason"
          required
          hint="Backend requires at least 3 characters"
        >
          <Textarea
            id="key-revoke-reason"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </Field>
        {revoke.error ? <ErrorState error={revoke.error} /> : null}
      </ConfirmAction>
    </>
  );
}

function APIKeyUsageDialog({
  keyRecord,
  onOpenChange,
}: {
  keyRecord?: APIKey;
  onOpenChange: (open: boolean) => void;
}) {
  const query = useQuery({
    queryKey: ["api-key-usage", keyRecord?.id],
    queryFn: () =>
      apiRequest<APIKeyUsage>(
        `/api/v1/api-keys/${keyRecord?.id}/usage?hours=24`,
      ),
    enabled: Boolean(keyRecord?.id),
  });
  return (
    <Dialog
      open={Boolean(keyRecord)}
      onOpenChange={onOpenChange}
      title={`${keyRecord?.name ?? "API key"} usage`}
      description="Last 24 hours. Route and error codes are safe operational metadata; secrets are never returned."
    >
      {query.isLoading ? (
        <LoadingState label="Loading credential use" />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : (
        <div className="space-y-4">
          <div className="grid grid-cols-3 gap-3">
            {[
              ["Requests", formatCount(query.data?.requestCount)],
              ["Errors", formatCount(query.data?.errorCount)],
              ["Average", `${query.data?.avgDurationMs ?? 0} ms`],
            ].map(([label, value]) => (
              <div
                key={String(label)}
                className="rounded-md border bg-slate-50 p-3"
              >
                <strong className="block text-xl tabular-nums">{value}</strong>
                <span className="text-xs text-slate-500">{label}</span>
              </div>
            ))}
          </div>
          {query.data?.recent?.length ? (
            <div className="overflow-x-auto">
              <table
                className="w-full table-fixed text-left text-sm"
                aria-label="Recent API calls"
              >
                <thead>
                  <tr>
                    <TableHead className="w-1/4">Time</TableHead>
                    <TableHead className="w-1/2">Request</TableHead>
                    <TableHead className="w-1/4">Result</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {query.data.recent.map((call, index) => (
                    <tr key={`${call.occurredAt}-${index}`}>
                      <TableCell>
                        {formatDateTime(call.occurredAt)}
                        <span className="mt-1 block text-xs text-slate-500">
                          {call.durationMs ?? 0} ms
                        </span>
                      </TableCell>
                      <TableCell>
                        <span className="break-all font-mono text-xs">
                          {call.method} {call.route}
                        </span>
                      </TableCell>
                      <TableCell>
                        <HttpStatus code={call.statusCode} />
                        {call.errorCode ? (
                          <Badge className="mt-1 block w-fit" tone="danger">
                            {call.errorCode}
                          </Badge>
                        ) : null}
                      </TableCell>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState
              title="No calls in this window"
              description="The credential has not sent traffic during the selected period."
            />
          )}
        </div>
      )}
    </Dialog>
  );
}

const webhookSchema = z.object({
  name: z.string().trim().min(2).max(120),
  url: z.string().url().startsWith("https://", "Webhook URLs must use HTTPS"),
  events: z.array(z.string()).min(1, "Select at least one event"),
});
type WebhookForm = z.infer<typeof webhookSchema>;

export function WebhookEndpointsPage() {
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [createOpen, setCreateOpen] = useState(false);
  const [offset, setOffset] = useState(0);
  const [created, setCreated] = useState<CreatedWebhookEndpoint>();
  const [configure, setConfigure] = useState<WebhookEndpoint>();
  const [statusAction, setStatusAction] = useState<{
    endpoint: WebhookEndpoint;
    status: "ACTIVE" | "PAUSED" | "DISABLED";
  }>();
  const form = useForm<WebhookForm>({
    resolver: zodResolver(webhookSchema),
    defaultValues: { name: "", url: "", events: [] },
  });
  const endpointQuery = useQuery({
    queryKey: ["webhook-endpoints", offset],
    queryFn: () =>
      apiRequest<WebhookEndpointListResponse>(
        `/api/v1/webhooks/endpoints?limit=50&offset=${offset}`,
      ),
  });
  const eventQuery = useQuery({
    queryKey: ["webhook-events"],
    queryFn: () =>
      apiRequest<WebhookEventListResponse>("/api/v1/webhooks/events"),
  });
  const create = useMutation({
    mutationFn: (values: WebhookForm) =>
      apiRequest<CreatedWebhookEndpoint>("/api/v1/webhooks/endpoints", {
        method: "POST",
        body: {
          name: values.name,
          url: values.url,
          events: values.events as WebhookEventType[],
        },
      }),
    onSuccess: (result) => {
      setCreateOpen(false);
      setCreated(result);
      form.reset();
      void queryClient.invalidateQueries({ queryKey: ["webhook-endpoints"] });
    },
  });
  const statusMutation = useMutation({
    mutationFn: (action: {
      endpoint: WebhookEndpoint;
      status: "ACTIVE" | "PAUSED" | "DISABLED";
    }) =>
      apiRequest<WebhookEndpoint>(
        `/api/v1/webhooks/endpoints/${action.endpoint.id}/status`,
        {
          method: "POST",
          body: { status: action.status },
        },
      ),
    onSuccess: (result) => {
      setStatusAction(undefined);
      toast({
        tone: "success",
        title: `Webhook ${titleCase(result.status ?? "updated")}`,
        description: result.name,
      });
      void queryClient.invalidateQueries({ queryKey: ["webhook-endpoints"] });
    },
  });
  const events = eventQuery.data?.data ?? [];
  const selectedEvents = form.watch("events");
  const rows = endpointQuery.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Administration · Integrations"
        title="Webhook endpoints"
        description="Send customer-safe business events to HTTPS endpoints. Signing secrets are shown once and delivery is always asynchronous."
        actions={
          hasPermission("webhook.manage") ? (
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Register endpoint
            </Button>
          ) : undefined
        }
      />
      <InlineNotice tone="info" title="At-least-once delivery">
        Consumers must deduplicate on Webhook-Id and verify the timestamped HMAC
        signature against the raw request body.
      </InlineNotice>
      <Panel className="mt-4">
        {endpointQuery.isLoading ? (
          <LoadingState label="Loading webhook endpoints" />
        ) : endpointQuery.error ? (
          <ErrorState
            error={endpointQuery.error}
            retry={() => void endpointQuery.refetch()}
          />
        ) : rows.length ? (
          <>
            <DataTable label="Webhook endpoints">
              <thead>
                <tr>
                  <TableHead>Endpoint</TableHead>
                  <TableHead>URL</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Failures</TableHead>
                  <TableHead>Last success</TableHead>
                  <TableHead>Last failure</TableHead>
                  <TableHead>Policy</TableHead>
                  <TableHead>
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((endpoint) => {
                  const trustedUrl = safeHttpsUrl(endpoint.url);
                  return (
                    <tr key={endpoint.id}>
                      <TableCell>
                        <strong>{endpoint.name}</strong>
                        <span className="block max-w-52 break-all font-mono text-xs text-slate-500">
                          {endpoint.id}
                        </span>
                      </TableCell>
                      <TableCell>
                        {trustedUrl ? (
                          <a
                            className="block max-w-48 text-xs font-medium text-primary hover:underline"
                            href={trustedUrl.toString()}
                            target="_blank"
                            rel="noreferrer"
                          >
                            <span className="block">{trustedUrl.host}</span>
                            <span className="block break-all text-[11px] font-normal text-slate-500">
                              {trustedUrl.pathname}
                              {trustedUrl.search}
                            </span>
                          </a>
                        ) : (
                          <span className="break-all text-xs text-danger">
                            Invalid endpoint URL: {endpoint.url}
                          </span>
                        )}
                      </TableCell>
                      <TableCell>
                        <IntegrationStatus status={endpoint.status} />
                        {endpoint.disabledReason ? (
                          <span className="mt-1 block max-w-xs text-xs text-danger">
                            {endpoint.disabledReason}
                          </span>
                        ) : null}
                      </TableCell>
                      <TableCell className="font-mono tabular-nums">
                        {endpoint.consecutiveFailures ?? 0} / 20
                      </TableCell>
                      <TableCell>
                        {formatDateTime(endpoint.lastSuccessAt)}
                      </TableCell>
                      <TableCell>
                        {formatDateTime(endpoint.lastFailureAt)}
                      </TableCell>
                      <TableCell>
                        {endpoint.maxAttempts ?? 0} attempts ·{" "}
                        {endpoint.timeoutSeconds ?? 0}s
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button
                            size="sm"
                            onClick={() => setConfigure(endpoint)}
                          >
                            <Cable aria-hidden className="h-4 w-4" /> Events
                          </Button>
                          {hasPermission("webhook.manage") &&
                          endpoint.status === "ACTIVE" ? (
                            <Button
                              size="sm"
                              onClick={() =>
                                setStatusAction({ endpoint, status: "PAUSED" })
                              }
                            >
                              Pause
                            </Button>
                          ) : null}
                          {hasPermission("webhook.manage") &&
                          endpoint.status === "PAUSED" ? (
                            <Button
                              size="sm"
                              onClick={() =>
                                setStatusAction({ endpoint, status: "ACTIVE" })
                              }
                            >
                              Resume
                            </Button>
                          ) : null}
                          {hasPermission("webhook.manage") &&
                          endpoint.status !== "DISABLED" ? (
                            <Button
                              size="sm"
                              variant="danger"
                              onClick={() =>
                                setStatusAction({
                                  endpoint,
                                  status: "DISABLED",
                                })
                              }
                            >
                              Disable
                            </Button>
                          ) : null}
                        </div>
                      </TableCell>
                    </tr>
                  );
                })}
              </tbody>
            </DataTable>
            <Pagination
              page={Math.floor(offset / 50) + 1}
              totalPages={Math.max(
                1,
                Math.ceil(
                  (endpointQuery.data?.pagination?.totalItems ?? rows.length) /
                    50,
                ),
              )}
              onPageChange={(page) => setOffset((page - 1) * 50)}
              label={`${endpointQuery.data?.pagination?.totalItems ?? rows.length} endpoints`}
            />
          </>
        ) : (
          <EmptyState
            icon={Webhook}
            title="No webhook endpoints"
            description="Register an HTTPS endpoint when the receiving service is ready to store its signing secret."
          />
        )}
      </Panel>
      <Dialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        title="Register webhook endpoint"
        description="The platform never contacts this URL during setup. Events are queued and delivered by workers after business transactions commit."
        footer={
          <>
            <Button onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={create.isPending}
              onClick={() =>
                void form.handleSubmit((values) => create.mutate(values))()
              }
            >
              Register endpoint
            </Button>
          </>
        }
      >
        <div className="space-y-5">
          <Field
            label="Endpoint name"
            htmlFor="webhook-name"
            required
            error={form.formState.errors.name?.message}
          >
            <Input
              id="webhook-name"
              {...form.register("name")}
              placeholder="Order platform production"
            />
          </Field>
          <Field
            label="HTTPS URL"
            htmlFor="webhook-url"
            required
            error={form.formState.errors.url?.message}
          >
            <Input
              id="webhook-url"
              type="url"
              {...form.register("url")}
              placeholder="https://partner.example.com/ceserve/events"
            />
          </Field>
          <fieldset>
            <legend className="text-sm font-semibold">Business events</legend>
            <div className="mt-3 grid gap-2 sm:grid-cols-2">
              {events.map((eventType) => (
                <label
                  key={eventType}
                  className="flex items-center gap-2 rounded-md border p-2.5 text-xs font-mono hover:bg-slate-50"
                >
                  <Checkbox
                    aria-label={`Subscribe to ${eventType}`}
                    checked={selectedEvents.includes(eventType)}
                    onCheckedChange={(checked) =>
                      form.setValue(
                        "events",
                        checked
                          ? [...selectedEvents, eventType]
                          : selectedEvents.filter((item) => item !== eventType),
                        { shouldDirty: true, shouldValidate: true },
                      )
                    }
                  />
                  {eventType}
                </label>
              ))}
            </div>
            {form.formState.errors.events?.message ? (
              <p role="alert" className="mt-2 text-xs text-danger">
                {form.formState.errors.events.message}
              </p>
            ) : null}
          </fieldset>
          {eventQuery.error ? (
            <ErrorState
              error={eventQuery.error}
              retry={() => void eventQuery.refetch()}
            />
          ) : null}
          {create.error ? <ErrorState error={create.error} /> : null}
        </div>
      </Dialog>
      <OneTimeSecretDialog
        open={Boolean(created)}
        onOpenChange={(open) => !open && setCreated(undefined)}
        title="Webhook endpoint registered"
        label="Signing secret"
        secret={created?.signingSecret}
        warning={created?.warning}
      >
        <dl className="grid gap-2 rounded-md bg-slate-50 p-3 text-xs sm:grid-cols-2">
          <div>
            <dt className="text-slate-500">Signature header</dt>
            <dd className="font-mono font-semibold">
              {created?.signatureHeader?.header}
            </dd>
          </div>
          <div>
            <dt className="text-slate-500">Timestamp header</dt>
            <dd className="font-mono font-semibold">
              {created?.signatureHeader?.timestamp}
            </dd>
          </div>
          <div className="sm:col-span-2">
            <dt className="text-slate-500">Scheme</dt>
            <dd className="break-all font-mono font-semibold">
              {created?.signatureHeader?.scheme}
            </dd>
          </div>
        </dl>
      </OneTimeSecretDialog>
      <WebhookSubscriptionsDialog
        endpoint={configure}
        events={events}
        onOpenChange={(open) => !open && setConfigure(undefined)}
      />
      <ConfirmAction
        open={Boolean(statusAction)}
        onOpenChange={(open) => !open && setStatusAction(undefined)}
        title={`${titleCase(statusAction?.status ?? "Change")} this webhook endpoint?`}
        description={
          statusAction?.status === "ACTIVE"
            ? "Delivery will resume and the consecutive-failure counter will be cleared."
            : statusAction?.status === "PAUSED"
              ? "New deliveries remain queued until this endpoint is resumed."
              : "Delivery will stop for this endpoint. Its historical delivery evidence remains available."
        }
        confirmLabel={
          statusAction?.status === "ACTIVE"
            ? "Resume endpoint"
            : statusAction?.status === "PAUSED"
              ? "Pause endpoint"
              : "Disable endpoint"
        }
        loading={statusMutation.isPending}
        onConfirm={() => statusAction && statusMutation.mutate(statusAction)}
      >
        {statusMutation.error ? (
          <ErrorState error={statusMutation.error} />
        ) : null}
      </ConfirmAction>
    </>
  );
}

function WebhookSubscriptionsDialog({
  endpoint,
  events,
  onOpenChange,
}: {
  endpoint?: WebhookEndpoint;
  events: WebhookEventType[];
  onOpenChange: (open: boolean) => void;
}) {
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["webhook-subscriptions", endpoint?.id],
    queryFn: () =>
      apiRequest<WebhookSubscriptionListResponse>(
        `/api/v1/webhooks/endpoints/${endpoint?.id}/subscriptions`,
      ),
    enabled: Boolean(endpoint?.id),
  });
  const mutation = useMutation({
    mutationFn: ({
      eventType,
      active,
    }: {
      eventType: WebhookEventType;
      active: boolean;
    }) =>
      active
        ? apiRequest(
            `/api/v1/webhooks/endpoints/${endpoint?.id}/subscriptions`,
            { method: "POST", body: { eventType } },
          )
        : apiRequest(
            `/api/v1/webhooks/endpoints/${endpoint?.id}/subscriptions/${encodeURIComponent(eventType)}`,
            { method: "DELETE" },
          ),
    onSuccess: () =>
      void queryClient.invalidateQueries({
        queryKey: ["webhook-subscriptions", endpoint?.id],
      }),
  });
  const active = new Set(
    (query.data?.data ?? [])
      .filter((item) => item.isActive)
      .map((item) => item.eventType),
  );
  return (
    <Dialog
      open={Boolean(endpoint)}
      onOpenChange={onOpenChange}
      title={`${endpoint?.name ?? "Endpoint"} events`}
      description="Internal handling steps are intentionally not published to partners."
    >
      {query.isLoading ? (
        <LoadingState label="Loading event subscriptions" />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : (
        <div className="grid gap-2 sm:grid-cols-2">
          {events.map((eventType) => (
            <label
              key={eventType}
              className="flex items-center gap-2 rounded-md border p-3 text-xs font-mono"
            >
              <Checkbox
                aria-label={`${hasPermission("webhook.manage") ? "Enable" : "Subscription status for"} ${eventType}`}
                checked={active.has(eventType)}
                disabled={!hasPermission("webhook.manage")}
                onCheckedChange={
                  hasPermission("webhook.manage")
                    ? (checked) =>
                        mutation.mutate({ eventType, active: checked })
                    : undefined
                }
              />
              <span className="flex-1">{eventType}</span>
              {!hasPermission("webhook.manage") ? (
                <Badge tone="neutral">Read only</Badge>
              ) : null}
            </label>
          ))}
        </div>
      )}
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function WebhookDeliveriesPage() {
  const [status, setStatus] = useState("");
  const [eventType, setEventType] = useState("");
  const [endpointId, setEndpointId] = useState("");
  const [cursorStack, setCursorStack] = useState<Array<number | undefined>>([
    undefined,
  ]);
  const cursor = cursorStack.at(-1);
  const query = useQuery({
    queryKey: ["webhook-deliveries", status, eventType, endpointId, cursor],
    queryFn: () =>
      apiRequest<WebhookDeliveryPage>(
        `/api/v1/webhooks/deliveries${queryString({ status, eventType, endpointId, cursor, limit: 50 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  const resetCursor = () => setCursorStack([undefined]);
  return (
    <>
      <PageHeader
        eyebrow="Administration · Integrations"
        title="Webhook deliveries"
        description="Inspect every asynchronous delivery and attempt. Failed evidence remains immutable; replay creates a new delivery."
      />
      <Panel>
        <FilterBar>
          <Select
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              resetCursor();
            }}
            aria-label="Webhook delivery status"
          >
            <option value="">All states</option>
            <option>PENDING</option>
            <option>SENDING</option>
            <option>DELIVERED</option>
            <option>FAILED</option>
            <option>DEAD_LETTER</option>
            <option>CANCELLED</option>
          </Select>
          <Input
            value={eventType}
            onChange={(event) => {
              setEventType(event.target.value);
              resetCursor();
            }}
            placeholder="Event type"
            aria-label="Webhook event type"
          />
          <Input
            value={endpointId}
            onChange={(event) => {
              setEndpointId(event.target.value);
              resetCursor();
            }}
            placeholder="Endpoint ID"
            aria-label="Webhook endpoint ID"
          />
        </FilterBar>
        {query.isLoading ? (
          <LoadingState label="Loading webhook deliveries" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length ? (
          <>
            <DataTable label="Webhook deliveries">
              <thead>
                <tr>
                  <TableHead>Event</TableHead>
                  <TableHead>Endpoint</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Attempt</TableHead>
                  <TableHead>HTTP</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead>Next attempt</TableHead>
                  <TableHead>Error summary</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((delivery) => (
                  <tr key={delivery.id}>
                    <TableCell>
                      <Link
                        className="font-mono text-xs font-semibold text-primary hover:underline"
                        to={`/admin/integrations/webhooks/deliveries/${delivery.id}`}
                      >
                        {delivery.eventType}
                      </Link>
                      <span className="block font-mono text-[11px] text-slate-500">
                        {delivery.eventId}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-xs">
                        {delivery.endpoint}
                      </span>
                    </TableCell>
                    <TableCell>
                      <IntegrationStatus status={delivery.status} />
                    </TableCell>
                    <TableCell className="font-mono tabular-nums">
                      {delivery.attemptCount ?? 0}
                    </TableCell>
                    <TableCell>
                      <HttpStatus code={delivery.lastStatusCode} />
                    </TableCell>
                    <TableCell>{formatDateTime(delivery.createdAt)}</TableCell>
                    <TableCell>
                      {formatDateTime(delivery.nextAttemptAt)}
                    </TableCell>
                    <TableCell>
                      <span className="block max-w-xs truncate text-xs text-danger">
                        {delivery.lastError || "—"}
                      </span>
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <div className="flex items-center justify-between border-t p-3">
              <span className="text-xs text-slate-500">
                Cursor page {cursorStack.length} · {rows.length} deliveries
              </span>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  disabled={cursorStack.length === 1}
                  onClick={() => setCursorStack((items) => items.slice(0, -1))}
                >
                  Previous
                </Button>
                <Button
                  size="sm"
                  disabled={query.data?.nextCursor == null}
                  onClick={() =>
                    query.data?.nextCursor != null &&
                    setCursorStack((items) => [
                      ...items,
                      query.data?.nextCursor,
                    ])
                  }
                >
                  Next
                </Button>
              </div>
            </div>
          </>
        ) : (
          <EmptyState
            icon={Cable}
            title="No webhook deliveries"
            description="No delivery matches these filters."
          />
        )}
      </Panel>
    </>
  );
}

export function WebhookDeliveryDetailPage() {
  const { deliveryId = "" } = useParams();
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [replayOpen, setReplayOpen] = useState(false);
  const query = useQuery({
    queryKey: ["webhook-delivery", deliveryId],
    queryFn: () =>
      apiRequest<WebhookDeliveryDetail>(
        `/api/v1/webhooks/deliveries/${deliveryId}`,
      ),
    enabled: Boolean(deliveryId),
  });
  const attempts = useQuery({
    queryKey: ["webhook-attempts", deliveryId],
    queryFn: () =>
      apiRequest<WebhookAttemptListResponse>(
        `/api/v1/webhooks/deliveries/${deliveryId}/attempts`,
      ),
    enabled: Boolean(deliveryId),
  });
  const replay = useMutation({
    mutationFn: () =>
      apiRequest<ReplayWebhookResponse>(
        `/api/v1/webhooks/deliveries/${deliveryId}/replay`,
        { method: "POST" },
      ),
    onSuccess: (result) => {
      setReplayOpen(false);
      toast({
        tone: "success",
        title: "Replay queued",
        description: `${result.id ?? "New delivery"} preserves event ${result.eventId ?? "identity"}.`,
      });
      void queryClient.invalidateQueries({ queryKey: ["webhook-deliveries"] });
    },
  });
  if (query.isLoading) return <LoadingState label="Loading webhook delivery" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const delivery = query.data;
  return (
    <>
      <PageHeader
        eyebrow="Administration · Webhook delivery"
        title={delivery?.eventType ?? "Webhook delivery"}
        description={`${delivery?.endpoint ?? "Unknown endpoint"} · Webhook-Id ${delivery?.eventId ?? "—"}`}
        actions={
          <>
            <IntegrationStatus status={delivery?.status} />
            {hasPermission("webhook.replay") &&
            delivery?.status !== "SENDING" ? (
              <Button variant="primary" onClick={() => setReplayOpen(true)}>
                <RotateCcw aria-hidden className="h-4 w-4" /> Replay delivery
              </Button>
            ) : null}
          </>
        }
      />
      <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,0.75fr)_minmax(0,1.25fr)]">
        <div className="min-w-0 space-y-4">
          <Panel>
            <PanelHeader
              title="Delivery evidence"
              description="The original remains immutable even when replayed."
            />
            <dl className="grid gap-4 p-4 sm:grid-cols-2">
              {[
                ["Target URL", delivery?.url],
                ["Created", formatDateTime(delivery?.createdAt)],
                ["Delivered", formatDateTime(delivery?.deliveredAt)],
                ["Next attempt", formatDateTime(delivery?.nextAttemptAt)],
                ["Attempts", delivery?.attemptCount ?? 0],
                [
                  "Last HTTP",
                  delivery?.lastStatusCode
                    ? `HTTP ${delivery.lastStatusCode}`
                    : "No response",
                ],
              ].map(([label, value]) => (
                <div key={String(label)}>
                  <dt className="text-xs uppercase tracking-wide text-slate-500">
                    {label}
                  </dt>
                  <dd className="mt-1 break-all text-sm font-semibold">
                    {value}
                  </dd>
                </div>
              ))}
            </dl>
            {delivery?.lastError ? (
              <InlineNotice tone="danger" title="Last error">
                {delivery.lastError}
              </InlineNotice>
            ) : null}
          </Panel>
          <Panel>
            <PanelHeader
              title="Signed payload"
              description="Displayed as data, never executed as HTML."
            />
            <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-all p-4 font-mono text-xs text-slate-700">
              {JSON.stringify(delivery?.payload ?? {}, null, 2)}
            </pre>
          </Panel>
        </div>
        <Panel className="min-w-0">
          <PanelHeader
            title="Attempt history"
            description="Provider response, latency, and errors for every try."
          />
          {attempts.isLoading ? (
            <LoadingState label="Loading attempt history" />
          ) : attempts.error ? (
            <ErrorState
              error={attempts.error}
              retry={() => void attempts.refetch()}
            />
          ) : attempts.data?.data?.length ? (
            <DataTable label="Webhook attempt history">
              <thead>
                <tr>
                  <TableHead>Attempt</TableHead>
                  <TableHead>Time</TableHead>
                  <TableHead>HTTP</TableHead>
                  <TableHead>Latency</TableHead>
                  <TableHead>Error or response</TableHead>
                </tr>
              </thead>
              <tbody>
                {attempts.data.data.map((attempt) => (
                  <tr key={`${attempt.attemptNo}-${attempt.attemptedAt}`}>
                    <TableCell className="font-mono font-semibold">
                      #{attempt.attemptNo}
                    </TableCell>
                    <TableCell>{formatDateTime(attempt.attemptedAt)}</TableCell>
                    <TableCell>
                      <HttpStatus code={attempt.statusCode} />
                    </TableCell>
                    <TableCell>
                      {attempt.durationMs != null
                        ? `${attempt.durationMs} ms`
                        : "—"}
                    </TableCell>
                    <TableCell>
                      <span
                        className={
                          attempt.errorMessage
                            ? "text-danger"
                            : "text-slate-600"
                        }
                      >
                        {attempt.errorMessage ??
                          attempt.responseBody ??
                          "No body"}
                      </span>
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
          ) : (
            <EmptyState
              icon={Clock3}
              title="No attempts yet"
              description="The delivery is queued but no worker attempt has been recorded."
            />
          )}
        </Panel>
      </div>
      <ConfirmAction
        open={replayOpen}
        onOpenChange={setReplayOpen}
        title="Queue a webhook replay?"
        description="A new delivery will be created with the exact same payload and Webhook-Id. The original delivery and attempts remain unchanged."
        confirmLabel="Queue replay"
        loading={replay.isPending}
        onConfirm={() => replay.mutate()}
      >
        <InlineNotice
          tone="warning"
          title="Consumer deduplication still applies"
        >
          The receiver may correctly ignore this replay if it already processed
          the event ID.
        </InlineNotice>
        {replay.error ? <ErrorState error={replay.error} /> : null}
      </ConfirmAction>
    </>
  );
}
