import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Pencil,
  Plus,
  SearchX,
  ShieldCheck,
  Trash2,
  UserRoundCog,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { Link, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type OperatingUnitListResponse,
  type Permission,
  type Role,
  type User,
  type UserListResponse,
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
  Textarea,
} from "../components/ui";
import { formatDateTime, titleCase } from "../lib/utils";

interface RoleListResponse {
  data?: Role[];
}
interface PermissionListResponse {
  data?: Permission[];
}

const createUserSchema = z.object({
  fullName: z.string().min(2, "Enter the user’s full name.").max(160),
  email: z.email("Enter a valid email address."),
  phone: z.string().optional(),
  password: z.string().min(12, "Use at least 12 characters."),
  roleCode: z.string().min(1, "Choose a role."),
  operatingUnitId: z.string().optional(),
  mustChangePassword: z.boolean(),
});
type CreateUserValues = z.infer<typeof createUserSchema>;

export function UsersPage() {
  const { hasPermission } = useAuth();
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [roleCode, setRoleCode] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const query = useQuery({
    queryKey: ["users", { page, search, status, roleCode }],
    queryFn: () =>
      apiRequest<UserListResponse>(
        `/api/v1/users${queryString({ page, limit: 25, search, status, roleCode })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = query.data?.data ?? [];
  const pagination = query.data?.pagination;
  return (
    <>
      <PageHeader
        eyebrow="Administration"
        title="Users"
        description="Manage people, account state, role assignments, and operating-unit scope."
        actions={
          hasPermission("user.create") ? (
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Add user
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="flex flex-wrap gap-3 border-b border-border p-4">
          <SearchInput
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(1);
            }}
            placeholder="Search name or email"
            className="min-w-[240px] flex-1"
            aria-label="Search users"
          />
          <Select
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              setPage(1);
            }}
            className="w-44"
            aria-label="Filter by status"
          >
            <option value="">All statuses</option>
            <option>ACTIVE</option>
            <option>INACTIVE</option>
            <option>LOCKED</option>
          </Select>
          <Input
            value={roleCode}
            onChange={(event) => {
              setRoleCode(event.target.value.toUpperCase());
              setPage(1);
            }}
            placeholder="Role code"
            className="w-44"
            aria-label="Filter by role code"
          />
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading users" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={SearchX}
            title="No users found"
            description="Adjust the filters or add a user if you have permission."
          />
        ) : (
          <>
            <DataTable label="Users">
              <thead>
                <tr>
                  <TableHead>User</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Roles</TableHead>
                  <TableHead>Operating unit</TableHead>
                  <TableHead>Last login</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((user) => (
                  <tr key={user.id} className="hover:bg-slate-50">
                    <TableCell>
                      <Link
                        to={`/admin/users/${user.id}`}
                        className="font-semibold text-primary hover:underline"
                      >
                        {user.fullName}
                      </Link>
                      <span className="mt-0.5 block text-xs text-slate-500">
                        {user.email}
                      </span>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={user.status} />
                    </TableCell>
                    <TableCell>
                      <div className="flex max-w-xs flex-wrap gap-1">
                        {user.roles?.length ? (
                          user.roles.map((role) => (
                            <Badge
                              key={`${role.roleCode}-${role.operatingUnit?.id ?? "org"}`}
                            >
                              {titleCase(
                                role.roleName ?? role.roleCode ?? "Role",
                              )}
                            </Badge>
                          ))
                        ) : (
                          <span className="text-xs text-slate-400">
                            {user.roles
                              ? "No roles"
                              : "Open user to view roles"}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      {user.roles === undefined
                        ? "View user details"
                        : !user.roles.length
                          ? "No scope assigned"
                          : user.roles
                              ?.map((role) => role.operatingUnit?.code)
                              .filter(Boolean)
                              .join(", ") || "Organization"}
                    </TableCell>
                    <TableCell>{formatDateTime(user.lastLoginAt)}</TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <Pagination
              page={pagination?.page ?? page}
              totalPages={pagination?.totalPages ?? 1}
              onPageChange={setPage}
              label={`${pagination?.totalItems ?? rows.length} users`}
            />
          </>
        )}
      </Panel>
      <CreateUserDialog open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

function CreateUserDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { toast } = useToast();
  const client = useQueryClient();
  const roles = useQuery({
    queryKey: ["roles"],
    queryFn: () => apiRequest<RoleListResponse>("/api/v1/roles"),
    enabled: open,
  });
  const units = useQuery({
    queryKey: ["operating-units", "picker"],
    queryFn: () =>
      apiRequest<OperatingUnitListResponse>(
        "/api/v1/network/operating-units?limit=100",
      ),
    enabled: open,
  });
  const {
    register,
    handleSubmit,
    reset,
    watch,
    formState: { errors },
  } = useForm<CreateUserValues>({
    resolver: zodResolver(createUserSchema),
    defaultValues: {
      mustChangePassword: true,
      roleCode: "",
      operatingUnitId: "",
    },
  });
  const selectedRole = roles.data?.data?.find(
    (role) => role.code === watch("roleCode"),
  );
  const mutation = useMutation({
    mutationFn: (values: CreateUserValues) =>
      apiRequest<User>("/api/v1/users", {
        method: "POST",
        body: {
          fullName: values.fullName,
          email: values.email,
          phone: values.phone || undefined,
          password: values.password,
          mustChangePassword: values.mustChangePassword,
          roles: [
            {
              roleCode: values.roleCode,
              ...(values.operatingUnitId
                ? { operatingUnitId: values.operatingUnitId }
                : {}),
            },
          ],
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["users"] });
      toast({
        tone: "success",
        title: "User created",
        description: "The account and initial role assignment are ready.",
      });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add user"
      description="Create an account with an initial role and access scope."
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
            Create user
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Full name"
          htmlFor="fullName"
          required
          error={errors.fullName?.message}
        >
          <Input id="fullName" autoFocus {...register("fullName")} />
        </Field>
        <Field
          label="Email"
          htmlFor="email"
          required
          error={errors.email?.message}
        >
          <Input id="email" type="email" {...register("email")} />
        </Field>
        <Field label="Phone" htmlFor="phone" error={errors.phone?.message}>
          <Input id="phone" inputMode="tel" {...register("phone")} />
        </Field>
        <Field
          label="Temporary password"
          htmlFor="password"
          required
          error={errors.password?.message}
          hint="At least 12 characters."
        >
          <Input
            id="password"
            type="password"
            autoComplete="new-password"
            {...register("password")}
          />
        </Field>
        <Field
          label="Initial role"
          htmlFor="roleCode"
          required
          error={errors.roleCode?.message}
        >
          <Select id="roleCode" {...register("roleCode")}>
            <option value="">Choose role</option>
            {roles.data?.data?.map((role) => (
              <option key={role.id} value={role.code}>
                {role.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field
          label="Operating unit"
          htmlFor="operatingUnitId"
          required={Boolean(selectedRole?.scopeRequired)}
          error={errors.operatingUnitId?.message}
          hint={
            selectedRole?.scopeRequired
              ? "This role requires a facility scope."
              : "Optional for organization-wide roles."
          }
        >
          <Select id="operatingUnitId" {...register("operatingUnitId")}>
            <option value="">Organization-wide</option>
            {units.data?.data?.map((unit) => (
              <option key={unit.id} value={unit.id}>
                {unit.code} — {unit.name}
              </option>
            ))}
          </Select>
        </Field>
        <label className="flex items-start gap-3 sm:col-span-2">
          <input
            type="checkbox"
            className="mt-1 h-4 w-4 accent-emerald-800"
            {...register("mustChangePassword")}
          />
          <span>
            <span className="block text-sm font-medium">
              Require password change
            </span>
            <span className="block text-xs text-slate-500">
              The user must choose a new password before entering the app.
            </span>
          </span>
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

export function UserDetailPage() {
  const { userId = "" } = useParams();
  const { hasPermission } = useAuth();
  const { toast } = useToast();
  const client = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [statusOpen, setStatusOpen] = useState(false);
  const [roleOpen, setRoleOpen] = useState(false);
  const [statusReason, setStatusReason] = useState("");
  const query = useQuery({
    queryKey: ["user", userId],
    queryFn: () => apiRequest<User>(`/api/v1/users/${userId}`),
  });
  const user = query.data;
  const statusMutation = useMutation({
    mutationFn: () =>
      apiRequest<User>(`/api/v1/users/${userId}/status`, {
        method: "POST",
        body: {
          status: user?.status === "ACTIVE" ? "INACTIVE" : "ACTIVE",
          reason: statusReason || undefined,
        },
      }),
    onSuccess: (updated) => {
      client.setQueryData(["user", userId], updated);
      void client.invalidateQueries({ queryKey: ["users"] });
      toast({
        tone: "success",
        title:
          updated.status === "ACTIVE" ? "User activated" : "User deactivated",
      });
      setStatusOpen(false);
      setStatusReason("");
    },
  });
  const revokeMutation = useMutation({
    mutationFn: (grant: { roleCode: string; operatingUnitId?: string }) =>
      apiRequest<void>(`/api/v1/users/${userId}/roles`, {
        method: "DELETE",
        body: grant,
      }),
    onSuccess: () => {
      void query.refetch();
      void client.invalidateQueries({ queryKey: ["users"] });
      toast({ tone: "success", title: "Role assignment removed" });
    },
  });
  if (query.isLoading) return <LoadingState label="Loading user" />;
  if (query.error || !user)
    return (
      <ErrorState
        error={query.error ?? new Error("User not found")}
        retry={() => void query.refetch()}
      />
    );
  return (
    <>
      <PageHeader
        eyebrow="Administration / Users"
        title={user.fullName ?? "User"}
        description={user.email}
        actions={
          <>
            {hasPermission("user.update") ? (
              <Button onClick={() => setEditOpen(true)}>
                <Pencil aria-hidden className="h-4 w-4" /> Edit profile
              </Button>
            ) : null}
            {hasPermission("user.assign_role") ? (
              <Button onClick={() => setRoleOpen(true)}>
                <UserRoundCog aria-hidden className="h-4 w-4" /> Grant role
              </Button>
            ) : null}
            {hasPermission("user.deactivate") ? (
              <Button
                variant={user.status === "ACTIVE" ? "danger" : "primary"}
                onClick={() => setStatusOpen(true)}
              >
                {user.status === "ACTIVE" ? "Deactivate" : "Activate"}
              </Button>
            ) : null}
          </>
        }
      />
      <div className="grid gap-5 lg:grid-cols-3">
        <Panel className="lg:col-span-2">
          <div className="grid gap-px bg-border sm:grid-cols-2">
            <Detail label="Status">
              <StatusBadge status={user.status} />
            </Detail>
            <Detail label="Phone">{user.phone || "—"}</Detail>
            <Detail label="Last login">
              {formatDateTime(user.lastLoginAt)}
            </Detail>
            <Detail label="Created">{formatDateTime(user.createdAt)}</Detail>
          </div>
        </Panel>
        <Panel>
          <div className="p-4">
            <h2 className="text-sm font-semibold">Role assignments</h2>
            <div className="mt-3 space-y-2">
              {user.roles?.map((role) => (
                <div
                  key={`${role.roleCode}-${role.operatingUnit?.id ?? "org"}`}
                  className="rounded-md border p-3"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <p className="text-sm font-semibold">{role.roleName}</p>
                      <p className="mt-1 text-xs text-slate-500">
                        {role.operatingUnit
                          ? `${role.operatingUnit.code} · ${role.operatingUnit.name}`
                          : "Organization-wide"}
                      </p>
                    </div>
                    {hasPermission("user.assign_role") && role.roleCode ? (
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={`Remove ${role.roleName ?? role.roleCode}`}
                        loading={revokeMutation.isPending}
                        onClick={() =>
                          revokeMutation.mutate({
                            roleCode: role.roleCode!,
                            operatingUnitId: role.operatingUnit?.id,
                          })
                        }
                      >
                        <Trash2 aria-hidden className="h-4 w-4 text-danger" />
                      </Button>
                    ) : null}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </Panel>
      </div>
      <EditUserDialog user={user} open={editOpen} onOpenChange={setEditOpen} />
      <GrantRoleDialog
        userId={userId}
        open={roleOpen}
        onOpenChange={setRoleOpen}
        onGranted={() => void query.refetch()}
      />
      <ConfirmAction
        open={statusOpen}
        onOpenChange={setStatusOpen}
        title={user.status === "ACTIVE" ? "Deactivate user?" : "Activate user?"}
        description={
          user.status === "ACTIVE"
            ? "All active sessions will be revoked immediately. This does not delete audit history."
            : "The user will be allowed to sign in again with their existing role assignments."
        }
        confirmLabel={
          user.status === "ACTIVE" ? "Deactivate user" : "Activate user"
        }
        loading={statusMutation.isPending}
        onConfirm={() => statusMutation.mutate()}
      >
        <Field
          label="Reason"
          htmlFor="userStatusReason"
          hint="Recorded for administrator context."
        >
          <Textarea
            id="userStatusReason"
            value={statusReason}
            onChange={(event) => setStatusReason(event.target.value)}
          />
        </Field>
        {statusMutation.error ? (
          <p role="alert" className="mt-3 text-sm text-danger">
            {statusMutation.error.message}
          </p>
        ) : null}
      </ConfirmAction>
    </>
  );
}

const editUserSchema = z.object({
  fullName: z.string().min(2).max(160),
  email: z.email(),
  phone: z.string().max(30).optional(),
});
type EditUserValues = z.infer<typeof editUserSchema>;

function EditUserDialog({
  user,
  open,
  onOpenChange,
}: {
  user: User;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<EditUserValues>({
    resolver: zodResolver(editUserSchema),
    values: {
      fullName: user.fullName ?? "",
      email: user.email ?? "",
      phone: user.phone ?? "",
    },
  });
  const mutation = useMutation({
    mutationFn: (values: EditUserValues) =>
      apiRequest<User>(`/api/v1/users/${user.id}`, {
        method: "PATCH",
        body: { ...values, phone: values.phone || undefined },
      }),
    onSuccess: (updated) => {
      client.setQueryData(["user", user.id], updated);
      void client.invalidateQueries({ queryKey: ["users"] });
      toast({ tone: "success", title: "User profile updated" });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Edit user profile"
      description="Account state and access assignments are managed separately for clarity."
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
            Save profile
          </Button>
        </>
      }
    >
      <form className="grid gap-4" onSubmit={(event) => event.preventDefault()}>
        <Field
          label="Full name"
          htmlFor="editUserName"
          required
          error={errors.fullName?.message}
        >
          <Input id="editUserName" {...register("fullName")} />
        </Field>
        <Field
          label="Email"
          htmlFor="editUserEmail"
          required
          error={errors.email?.message}
        >
          <Input id="editUserEmail" type="email" {...register("email")} />
        </Field>
        <Field
          label="Phone"
          htmlFor="editUserPhone"
          error={errors.phone?.message}
        >
          <Input id="editUserPhone" {...register("phone")} />
        </Field>
        {mutation.error ? (
          <p role="alert" className="text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

const grantRoleSchema = z.object({
  roleCode: z.string().min(1, "Choose a role."),
  operatingUnitId: z.string().optional(),
});
type GrantRoleValues = z.infer<typeof grantRoleSchema>;

function GrantRoleDialog({
  userId,
  open,
  onOpenChange,
  onGranted,
}: {
  userId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onGranted: () => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const roles = useQuery({
    queryKey: ["roles"],
    queryFn: () => apiRequest<RoleListResponse>("/api/v1/roles"),
    enabled: open,
  });
  const units = useQuery({
    queryKey: ["operating-units", "picker"],
    queryFn: () =>
      apiRequest<OperatingUnitListResponse>(
        "/api/v1/network/operating-units?limit=100",
      ),
    enabled: open,
  });
  const {
    register,
    handleSubmit,
    watch,
    reset,
    formState: { errors },
  } = useForm<GrantRoleValues>({
    resolver: zodResolver(grantRoleSchema),
    defaultValues: { roleCode: "", operatingUnitId: "" },
  });
  const selectedRole = roles.data?.data?.find(
    (role) => role.code === watch("roleCode"),
  );
  const mutation = useMutation({
    mutationFn: (values: GrantRoleValues) => {
      if (selectedRole?.scopeRequired && !values.operatingUnitId)
        throw new Error("This role requires an operating unit scope.");
      return apiRequest(`/api/v1/users/${userId}/roles`, {
        method: "POST",
        body: {
          roleCode: values.roleCode,
          operatingUnitId: values.operatingUnitId || undefined,
        },
      });
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["users"] });
      onGranted();
      toast({ tone: "success", title: "Role granted" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Grant role"
      description="Scope a role to one operating unit when the role requires it."
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
            Grant role
          </Button>
        </>
      }
    >
      <form className="grid gap-4" onSubmit={(event) => event.preventDefault()}>
        <Field
          label="Role"
          htmlFor="grantRole"
          required
          error={errors.roleCode?.message}
        >
          <Select id="grantRole" {...register("roleCode")}>
            <option value="">Choose role</option>
            {roles.data?.data?.map((role) => (
              <option key={role.id} value={role.code}>
                {role.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field
          label="Operating unit"
          htmlFor="grantUnit"
          required={Boolean(selectedRole?.scopeRequired)}
          hint={
            selectedRole?.scopeRequired
              ? "Required for this role."
              : "Leave blank for organization scope."
          }
        >
          <Select id="grantUnit" {...register("operatingUnitId")}>
            <option value="">Organization-wide</option>
            {units.data?.data?.map((unit) => (
              <option key={unit.id} value={unit.id}>
                {unit.code} — {unit.name}
              </option>
            ))}
          </Select>
        </Field>
        {mutation.error ? (
          <p role="alert" className="text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

export function RolesPage() {
  const { hasPermission } = useAuth();
  const { toast } = useToast();
  const client = useQueryClient();
  const [selected, setSelected] = useState<Role>();
  const [createOpen, setCreateOpen] = useState(false);
  const [permissionDraft, setPermissionDraft] = useState<{
    roleId: string;
    permissions: string[];
  }>();
  const roles = useQuery({
    queryKey: ["roles"],
    queryFn: () => apiRequest<RoleListResponse>("/api/v1/roles"),
  });
  const permissions = useQuery({
    queryKey: ["permissions"],
    queryFn: () => apiRequest<PermissionListResponse>("/api/v1/permissions"),
  });
  const grouped = useMemo(
    () =>
      Object.entries(
        (permissions.data?.data ?? []).reduce<Record<string, Permission[]>>(
          (acc, permission) => {
            const key = permission.module ?? "Other";
            (acc[key] ??= []).push(permission);
            return acc;
          },
          {},
        ),
      ),
    [permissions.data],
  );
  const summary = selected ?? roles.data?.data?.[0];
  const roleDetail = useQuery({
    queryKey: ["role", summary?.id],
    queryFn: () => apiRequest<Role>(`/api/v1/roles/${summary?.id}`),
    enabled: Boolean(summary?.id),
  });
  const role = roleDetail.data;
  const draftPermissions =
    permissionDraft?.roleId === role?.id
      ? (permissionDraft?.permissions ?? [])
      : (role?.permissions ?? []);
  const updatePermissions = useMutation({
    mutationFn: (values: { id: string; permissions: string[] }) =>
      apiRequest<Role>(`/api/v1/roles/${values.id}/permissions`, {
        method: "PUT",
        body: { permissions: values.permissions },
      }),
    onSuccess: (updated) => {
      client.setQueryData(["role", updated.id], updated);
      setPermissionDraft(undefined);
      void client.invalidateQueries({ queryKey: ["roles"] });
      toast({ tone: "success", title: "Role permissions updated" });
    },
  });
  if (roles.isLoading || permissions.isLoading)
    return <LoadingState label="Loading access catalogue" />;
  if (roles.error || permissions.error)
    return (
      <ErrorState
        error={roles.error ?? permissions.error}
        retry={() => {
          void roles.refetch();
          void permissions.refetch();
        }}
      />
    );
  return (
    <>
      <PageHeader
        eyebrow="Administration"
        title="Roles & permissions"
        description="Review role scope and the effective permission matrix. System roles are immutable."
        actions={
          hasPermission("role.create") ? (
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Add custom role
            </Button>
          ) : undefined
        }
      />
      <div className="grid gap-5 xl:grid-cols-[320px_minmax(0,1fr)]">
        <Panel>
          <div className="border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold">Roles</h2>
          </div>
          <div className="p-2">
            {roles.data?.data?.map((item) => (
              <button
                key={item.id}
                onClick={() => setSelected(item)}
                className={`mb-1 w-full rounded-md px-3 py-2.5 text-left ${summary?.id === item.id ? "bg-emerald-50 text-emerald-950" : "hover:bg-muted"}`}
              >
                <span className="flex items-center justify-between gap-2">
                  <strong className="text-sm">{item.name}</strong>
                  {item.isSystem ? (
                    <Badge>System</Badge>
                  ) : (
                    <Badge tone="primary">Custom</Badge>
                  )}
                </span>
                <span className="mt-1 block text-xs text-slate-500">
                  {item.permissionCount ?? 0} permissions ·{" "}
                  {item.scopeRequired ? "Unit-scoped" : "Organization scope"}
                </span>
              </button>
            ))}
          </div>
        </Panel>
        <Panel>
          <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-4 py-3">
            <div>
              <h2 className="text-sm font-semibold">
                {role?.name ?? "Select a role"}
              </h2>
              <p className="mt-0.5 text-xs text-slate-500">
                {role?.description}
              </p>
            </div>
            {role?.isSystem ? (
              <Badge>
                <ShieldCheck aria-hidden className="h-3 w-3" /> Managed system
                role
              </Badge>
            ) : hasPermission("role.update") ? (
              <Button
                size="sm"
                variant="primary"
                loading={updatePermissions.isPending}
                disabled={!role?.id || roleDetail.isFetching}
                onClick={() => {
                  if (role?.id)
                    updatePermissions.mutate({
                      id: role.id,
                      permissions: draftPermissions,
                    });
                }}
              >
                Save permissions
              </Button>
            ) : null}
          </div>
          {roleDetail.isLoading ? (
            <LoadingState label="Loading role permissions" />
          ) : roleDetail.error ? (
            <ErrorState
              error={roleDetail.error}
              retry={() => void roleDetail.refetch()}
            />
          ) : role ? (
            <div className="overflow-x-auto">
              <table
                className="w-full min-w-[700px] text-sm"
                aria-label={`Permission matrix for ${role.name}`}
              >
                <thead>
                  <tr>
                    <TableHead>Module</TableHead>
                    <TableHead>Permission</TableHead>
                    <TableHead>Description</TableHead>
                    <TableHead className="text-center">Granted</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {grouped.flatMap(([module, items]) =>
                    items.map((permission, index) => (
                      <tr key={permission.code}>
                        <TableCell>
                          {index === 0 ? (
                            <span className="font-semibold">{module}</span>
                          ) : null}
                        </TableCell>
                        <TableCell>
                          <code className="text-xs">{permission.code}</code>
                        </TableCell>
                        <TableCell className="max-w-lg text-xs text-slate-600">
                          {permission.description}
                        </TableCell>
                        <TableCell className="text-center">
                          {!role.isSystem && hasPermission("role.update") ? (
                            <input
                              type="checkbox"
                              className="h-4 w-4 accent-emerald-800"
                              aria-label={`${permission.code} permission`}
                              checked={draftPermissions.includes(
                                permission.code ?? "",
                              )}
                              onChange={(event) => {
                                const code = permission.code ?? "";
                                if (role.id)
                                  setPermissionDraft({
                                    roleId: role.id,
                                    permissions: event.target.checked
                                      ? [...draftPermissions, code]
                                      : draftPermissions.filter(
                                          (item) => item !== code,
                                        ),
                                  });
                              }}
                            />
                          ) : (
                            <span
                              className={`inline-grid h-6 w-6 place-items-center rounded-full ${role.permissions?.includes(permission.code ?? "") ? "bg-emerald-100 text-emerald-800" : "bg-slate-100 text-slate-400"}`}
                              aria-label={
                                role.permissions?.includes(
                                  permission.code ?? "",
                                )
                                  ? "Granted"
                                  : "Not granted"
                              }
                            >
                              {role.permissions?.includes(permission.code ?? "")
                                ? "✓"
                                : "—"}
                            </span>
                          )}
                        </TableCell>
                      </tr>
                    )),
                  )}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState
              icon={ShieldCheck}
              title="Select a role"
              description="Choose a role to review its permissions."
            />
          )}
        </Panel>
      </div>
      {updatePermissions.error ? (
        <ErrorState error={updatePermissions.error} />
      ) : null}
      <CreateRoleDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        permissions={permissions.data?.data ?? []}
      />
    </>
  );
}

const createRoleSchema = z.object({
  code: z.string().regex(/^[A-Z0-9][A-Z0-9_-]{1,31}$/),
  name: z.string().min(2).max(120),
  description: z.string().max(500).optional(),
  scopeRequired: z.boolean(),
  permissions: z.array(z.string()).min(1, "Select at least one permission."),
});
type CreateRoleValues = z.infer<typeof createRoleSchema>;

function CreateRoleDialog({
  open,
  onOpenChange,
  permissions,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  permissions: Permission[];
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<CreateRoleValues>({
    resolver: zodResolver(createRoleSchema),
    defaultValues: { scopeRequired: false, permissions: [] },
  });
  const mutation = useMutation({
    mutationFn: (values: CreateRoleValues) =>
      apiRequest<Role>("/api/v1/roles", {
        method: "POST",
        body: {
          ...values,
          code: values.code.toUpperCase(),
          description: values.description || undefined,
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["roles"] });
      toast({ tone: "success", title: "Custom role created" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create custom role"
      description="Only permissions already held by your account can be granted."
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
            Create role
          </Button>
        </>
      }
    >
      <form className="grid gap-4" onSubmit={(event) => event.preventDefault()}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Role code"
            htmlFor="customRoleCode"
            required
            error={errors.code?.message}
          >
            <Input
              id="customRoleCode"
              className="uppercase"
              {...register("code")}
            />
          </Field>
          <Field
            label="Role name"
            htmlFor="customRoleName"
            required
            error={errors.name?.message}
          >
            <Input id="customRoleName" {...register("name")} />
          </Field>
        </div>
        <Field
          label="Description"
          htmlFor="customRoleDescription"
          error={errors.description?.message}
        >
          <Textarea id="customRoleDescription" {...register("description")} />
        </Field>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="h-4 w-4 accent-emerald-800"
            {...register("scopeRequired")}
          />
          Require an operating-unit scope
        </label>
        <fieldset className="max-h-64 overflow-y-auto rounded-md border border-border p-3">
          <legend className="px-1 text-sm font-semibold">Permissions</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            {permissions.map((permission) => (
              <label
                key={permission.code}
                className="flex items-start gap-2 text-xs"
              >
                <input
                  type="checkbox"
                  value={permission.code}
                  className="mt-0.5 h-4 w-4 accent-emerald-800"
                  {...register("permissions")}
                />
                <span>
                  <strong className="block font-mono">{permission.code}</strong>
                  <span className="text-slate-500">
                    {permission.description}
                  </span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>
        {errors.permissions ? (
          <p role="alert" className="text-xs text-danger">
            {errors.permissions.message}
          </p>
        ) : null}
        {mutation.error ? (
          <p role="alert" className="text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

function Detail({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="bg-white p-4">
      <p className="text-xs font-medium uppercase tracking-wide text-slate-500">
        {label}
      </p>
      <div className="mt-1.5 text-sm text-slate-900">{children}</div>
    </div>
  );
}
