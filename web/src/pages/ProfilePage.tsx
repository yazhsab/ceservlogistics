import { useQuery } from "@tanstack/react-query";
import { CircleUserRound, KeyRound, Laptop, ShieldCheck } from "lucide-react";
import { Link } from "react-router-dom";
import type { components } from "../api/schema";
import { apiRequest } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import {
  Badge,
  ErrorState,
  LoadingState,
  PageHeader,
  Panel,
  PanelHeader,
  StatusBadge,
} from "../components/ui";
import { formatDateTime } from "../lib/utils";

type SessionSummary = components["schemas"]["SessionSummary"];

export default function ProfilePage() {
  const { user } = useAuth();
  const sessions = useQuery({
    queryKey: ["auth", "sessions"],
    queryFn: () =>
      apiRequest<{ data?: SessionSummary[] }>("/api/v1/auth/sessions"),
  });
  return (
    <>
      <PageHeader
        eyebrow="Account"
        title="My profile"
        description="Your identity, access scope, and active secure sessions."
        actions={
          <Link
            to="/change-password"
            className="inline-flex min-h-10 items-center gap-2 rounded-md border border-border bg-white px-3.5 text-sm font-semibold hover:bg-muted"
          >
            <KeyRound aria-hidden className="h-4 w-4" /> Change password
          </Link>
        }
      />
      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(360px,.7fr)]">
        <Panel>
          <PanelHeader title="Account details" />
          <dl className="grid gap-px bg-border sm:grid-cols-2">
            <Detail
              label="Full name"
              value={user?.fullName}
              icon={CircleUserRound}
            />
            <Detail label="Email" value={user?.email} />
            <Detail
              label="Status"
              value={<StatusBadge status={user?.status} />}
            />
            <Detail
              label="Last sign-in"
              value={formatDateTime(
                user?.lastLoginAt,
                user?.organization?.timezone,
              )}
            />
            <Detail label="Organization" value={user?.organization?.name} />
            <Detail
              label="Organization code"
              value={user?.organization?.code}
            />
          </dl>
        </Panel>
        <Panel>
          <PanelHeader
            title="Access scope"
            description="Authorization is enforced by the server."
          />
          <div className="p-4">
            <div className="flex items-start gap-3 rounded-md bg-emerald-50 p-3">
              <ShieldCheck
                aria-hidden
                className="mt-0.5 h-5 w-5 text-primary"
              />
              <div>
                <p className="text-sm font-semibold text-emerald-950">
                  {user?.hasOrganizationWideAccess
                    ? "Organization-wide access"
                    : `${user?.operatingUnitIds?.length ?? 0} operating unit${user?.operatingUnitIds?.length === 1 ? "" : "s"}`}
                </p>
                <p className="mt-1 text-xs text-emerald-800">
                  {user?.permissions?.length ?? 0} effective permissions across{" "}
                  {user?.roles?.length ?? 0} role assignments.
                </p>
              </div>
            </div>
            <div className="mt-4 flex flex-wrap gap-1.5">
              {user?.roles?.map((role) => (
                <Badge key={role} tone="primary">
                  {role.replaceAll("_", " ")}
                </Badge>
              ))}
            </div>
          </div>
        </Panel>
        <Panel className="xl:col-span-2">
          <PanelHeader
            title="Active sessions"
            description="No token or credential material is shown."
          />
          {sessions.isLoading ? (
            <LoadingState label="Loading sessions" />
          ) : sessions.error ? (
            <ErrorState
              error={sessions.error}
              retry={() => void sessions.refetch()}
            />
          ) : (
            <div className="divide-y divide-border">
              {sessions.data?.data?.map((session) => (
                <div
                  key={session.id}
                  className="flex flex-wrap items-center gap-4 px-4 py-3"
                >
                  <Laptop aria-hidden className="h-5 w-5 text-slate-400" />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-slate-800">
                      {session.userAgent ?? "Unknown device"}
                    </p>
                    <p className="mt-0.5 text-xs text-slate-500">
                      {session.clientIp} · Last used{" "}
                      {formatDateTime(session.lastUsedAt)}
                    </p>
                  </div>
                  {session.current ? (
                    <Badge tone="success">Current session</Badge>
                  ) : (
                    <Badge>Expires {formatDateTime(session.expiresAt)}</Badge>
                  )}
                </div>
              )) ?? (
                <p className="p-6 text-center text-sm text-slate-500">
                  No sessions were returned.
                </p>
              )}
            </div>
          )}
        </Panel>
      </div>
    </>
  );
}

function Detail({
  label,
  value,
  icon: Icon,
}: {
  label: string;
  value?: React.ReactNode;
  icon?: typeof CircleUserRound;
}) {
  return (
    <div className="bg-white p-4">
      <dt className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-slate-500">
        {Icon ? <Icon aria-hidden className="h-3.5 w-3.5" /> : null}
        {label}
      </dt>
      <dd className="mt-1.5 text-sm font-medium text-slate-900">
        {value || "—"}
      </dd>
    </div>
  );
}
