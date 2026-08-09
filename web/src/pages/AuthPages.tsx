import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowLeft, LockKeyhole, Mail, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import {
  Link,
  Navigate,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import { z } from "zod";
import { ApiError, apiRequest, type UserProfile } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { Button, Field, InlineNotice, Input } from "../components/ui";

const loginSchema = z.object({
  email: z.email("Enter a valid email address."),
  password: z.string().min(1, "Enter your password."),
});
const emailSchema = z.object({
  email: z.email("Enter a valid email address."),
});
const passwordSchema = z
  .object({
    newPassword: z.string().min(12, "Use at least 12 characters.").max(256),
    confirmPassword: z.string(),
  })
  .refine((value) => value.newPassword === value.confirmPassword, {
    path: ["confirmPassword"],
    message: "Passwords do not match.",
  });
type LoginValues = z.infer<typeof loginSchema>;
type EmailValues = z.infer<typeof emailSchema>;
type PasswordValues = z.infer<typeof passwordSchema>;

function signedInHome(profile?: UserProfile) {
  if (profile?.portal?.isCustomerUser) return "/portal/customer";
  if (profile?.portal?.franchise) return "/portal/franchise";
  return "/shipments";
}

function AuthFrame({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <main className="grid min-h-screen bg-white lg:grid-cols-[minmax(420px,0.82fr)_1.18fr]">
      <section className="flex min-h-screen items-center justify-center px-6 py-10">
        <div className="w-full max-w-[410px]">
          <Link
            to="/login"
            className="mb-10 inline-flex items-center gap-3"
          >
            <span className="grid h-10 w-10 place-items-center rounded-md bg-primary text-sm font-black text-white">
              CS
            </span>
            <span>
              <strong className="block text-sm tracking-[0.08em] text-slate-950">
                CESERVE
              </strong>
              <span className="block text-[10px] uppercase tracking-[0.18em] text-slate-500">
                Courier Operating System
              </span>
            </span>
          </Link>
          <h1 className="text-2xl font-bold tracking-tight text-slate-950">
            {title}
          </h1>
          <p className="mt-2 text-sm leading-6 text-slate-600">{description}</p>
          <div className="mt-7">{children}</div>
          <p className="mt-10 text-xs text-slate-600">
            Protected enterprise access · Activity is recorded for security and
            support.
          </p>
        </div>
      </section>
      <aside className="relative hidden overflow-hidden bg-[#123f36] p-12 text-white lg:flex lg:flex-col lg:justify-between">
        <div
          className="absolute inset-0 opacity-25"
          style={{
            backgroundImage:
              "linear-gradient(rgba(255,255,255,.08) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,.08) 1px, transparent 1px)",
            backgroundSize: "48px 48px",
          }}
        />
        <div className="relative">
          <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[#d8f25a]">
            National operations, one system
          </p>
          <h2 className="mt-4 max-w-xl text-4xl font-semibold leading-tight">
            Move every shipment with clarity, speed, and control.
          </h2>
          <p className="mt-5 max-w-lg text-base leading-7 text-emerald-50/75">
            A secure operating workspace for bookings, routing, pricing,
            customers, and the network behind every delivery.
          </p>
        </div>
        <div className="relative grid max-w-2xl grid-cols-3 gap-px overflow-hidden rounded-lg bg-white/15">
          <div className="bg-[#123f36]/80 p-5">
            <strong className="block text-2xl">Fast</strong>
            <span className="mt-1 block text-xs text-emerald-100/70">
              Keyboard-first booking
            </span>
          </div>
          <div className="bg-[#123f36]/80 p-5">
            <strong className="block text-2xl">Clear</strong>
            <span className="mt-1 block text-xs text-emerald-100/70">
              Explainable pricing
            </span>
          </div>
          <div className="bg-[#123f36]/80 p-5">
            <strong className="block text-2xl">Secure</strong>
            <span className="mt-1 block text-xs text-emerald-100/70">
              Permission-aware access
            </span>
          </div>
        </div>
      </aside>
    </main>
  );
}

export function LoginPage() {
  const { login, user } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [serverError, setServerError] = useState("");
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: "", password: "" },
  });
  if (user) return <Navigate to={signedInHome(user)} replace />;
  const submit = async (values: LoginValues) => {
    setServerError("");
    try {
      const profile = await login(values.email, values.password);
      void navigate(
        profile.mustChangePassword
          ? "/change-password"
          : ((location.state as { from?: string } | null)?.from ??
              signedInHome(profile)),
        { replace: true },
      );
    } catch (error) {
      const code = error instanceof ApiError ? error.code : "";
      setServerError(
        code === "INVALID_CREDENTIALS"
          ? "Email or password is incorrect."
          : code === "ACCOUNT_LOCKED"
            ? "Too many attempts. Try again in a few minutes."
            : code === "ACCOUNT_INACTIVE"
              ? "Your account is inactive. Contact your administrator."
              : error instanceof Error
                ? error.message
                : "Sign in could not be completed.",
      );
    }
  };
  return (
    <AuthFrame
      title="Welcome back"
      description="Sign in to your organization’s courier operations workspace."
    >
      <form
        className="space-y-5"
        onSubmit={(event) => void handleSubmit(submit)(event)}
        noValidate
      >
        {serverError ? (
          <InlineNotice tone="danger" title="Unable to sign in">
            {serverError}
          </InlineNotice>
        ) : null}
        <Field
          label="Work email"
          htmlFor="email"
          required
          error={errors.email?.message}
        >
          <div className="relative">
            <Mail
              aria-hidden
              className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400"
            />
            <Input
              id="email"
              autoComplete="username"
              autoFocus
              className="pl-9"
              {...register("email")}
            />
          </div>
        </Field>
        <Field
          label="Password"
          htmlFor="password"
          required
          error={errors.password?.message}
        >
          <div className="relative">
            <LockKeyhole
              aria-hidden
              className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400"
            />
            <Input
              id="password"
              type="password"
              autoComplete="current-password"
              className="pl-9"
              {...register("password")}
            />
          </div>
        </Field>
        <div className="flex justify-end">
          <Link
            to="/forgot-password"
            className="text-sm font-semibold text-primary hover:underline"
          >
            Forgot password?
          </Link>
        </div>
        <Button
          type="submit"
          variant="primary"
          size="lg"
          loading={isSubmitting}
          className="w-full"
        >
          Sign in securely
        </Button>
      </form>
    </AuthFrame>
  );
}

export function ForgotPasswordPage() {
  const [sent, setSent] = useState(false);
  const [serverError, setServerError] = useState("");
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<EmailValues>({ resolver: zodResolver(emailSchema) });
  const submit = async (values: EmailValues) => {
    try {
      await apiRequest("/api/v1/auth/forgot-password", {
        method: "POST",
        body: values,
        auth: false,
      });
      setSent(true);
    } catch (error) {
      setServerError(
        error instanceof Error
          ? error.message
          : "The request could not be completed.",
      );
    }
  };
  return (
    <AuthFrame
      title="Reset your password"
      description="We’ll send reset instructions if the email belongs to an account."
    >
      {sent ? (
        <div>
          <InlineNotice tone="success" title="Check your inbox">
            If an account exists for that email, reset instructions have been
            sent.
          </InlineNotice>
          <Link
            to="/login"
            className="mt-6 inline-flex items-center gap-2 text-sm font-semibold text-primary"
          >
            <ArrowLeft aria-hidden className="h-4 w-4" /> Back to sign in
          </Link>
        </div>
      ) : (
        <form
          className="space-y-5"
          onSubmit={(event) => void handleSubmit(submit)(event)}
        >
          {serverError ? (
            <InlineNotice tone="danger" title="Request failed">
              {serverError}
            </InlineNotice>
          ) : null}
          <Field
            label="Work email"
            htmlFor="email"
            required
            error={errors.email?.message}
          >
            <Input
              id="email"
              autoFocus
              autoComplete="email"
              {...register("email")}
            />
          </Field>
          <Button
            type="submit"
            variant="primary"
            size="lg"
            loading={isSubmitting}
            className="w-full"
          >
            Send reset instructions
          </Button>
          <Link
            to="/login"
            className="flex items-center justify-center gap-2 text-sm font-semibold text-slate-600"
          >
            <ArrowLeft aria-hidden className="h-4 w-4" /> Back to sign in
          </Link>
        </form>
      )}
    </AuthFrame>
  );
}

export function ResetPasswordPage() {
  const [params] = useSearchParams();
  const [done, setDone] = useState(false);
  const [serverError, setServerError] = useState("");
  const [token] = useState(() => params.get("token") ?? "");
  useEffect(() => {
    if (!token || !window.location.search) return;
    window.history.replaceState(
      window.history.state,
      "",
      `${window.location.pathname}${window.location.hash}`,
    );
  }, [token]);
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<PasswordValues>({ resolver: zodResolver(passwordSchema) });
  const submit = async (values: PasswordValues) => {
    try {
      await apiRequest("/api/v1/auth/reset-password", {
        method: "POST",
        body: { token, newPassword: values.newPassword },
        auth: false,
      });
      setDone(true);
    } catch (error) {
      setServerError(
        error instanceof Error
          ? error.message
          : "The password could not be reset.",
      );
    }
  };
  return (
    <AuthFrame
      title="Choose a new password"
      description="Use a unique password with at least 12 characters."
    >
      {done ? (
        <div>
          <InlineNotice tone="success" title="Password changed">
            Your existing sessions have been closed. Sign in with your new
            password.
          </InlineNotice>
          <Link
            to="/login"
            className="mt-6 inline-flex items-center gap-2 text-sm font-semibold text-primary"
          >
            <ShieldCheck aria-hidden className="h-4 w-4" /> Continue to sign in
          </Link>
        </div>
      ) : !token ? (
        <InlineNotice tone="danger" title="Reset link is incomplete">
          Request a new password reset link and try again.
        </InlineNotice>
      ) : (
        <form
          className="space-y-5"
          onSubmit={(event) => void handleSubmit(submit)(event)}
        >
          {serverError ? (
            <InlineNotice tone="danger" title="Unable to reset password">
              {serverError}
            </InlineNotice>
          ) : null}
          <Field
            label="New password"
            htmlFor="newPassword"
            required
            error={errors.newPassword?.message}
            hint="At least 12 characters."
          >
            <Input
              id="newPassword"
              type="password"
              autoComplete="new-password"
              {...register("newPassword")}
            />
          </Field>
          <Field
            label="Confirm password"
            htmlFor="confirmPassword"
            required
            error={errors.confirmPassword?.message}
          >
            <Input
              id="confirmPassword"
              type="password"
              autoComplete="new-password"
              {...register("confirmPassword")}
            />
          </Field>
          <Button
            type="submit"
            variant="primary"
            size="lg"
            loading={isSubmitting}
            className="w-full"
          >
            Set new password
          </Button>
        </form>
      )}
    </AuthFrame>
  );
}

export function ChangePasswordPage() {
  const { user, refreshProfile } = useAuth();
  const navigate = useNavigate();
  const [serverError, setServerError] = useState("");
  const schema = passwordSchema.and(
    z.object({
      currentPassword: z.string().min(1, "Enter your current password."),
    }),
  );
  type Values = z.infer<typeof schema>;
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<Values>({ resolver: zodResolver(schema) });
  const submit = async (values: Values) => {
    try {
      await apiRequest("/api/v1/auth/change-password", {
        method: "POST",
        body: {
          currentPassword: values.currentPassword,
          newPassword: values.newPassword,
        },
      });
      await refreshProfile();
      void navigate("/shipments", { replace: true });
    } catch (error) {
      setServerError(
        error instanceof Error
          ? error.message
          : "The password could not be changed.",
      );
    }
  };
  return (
    <AuthFrame
      title={
        user?.mustChangePassword
          ? "Change required password"
          : "Change your password"
      }
      description={
        user?.mustChangePassword
          ? "Your administrator requires a new password before you continue."
          : "Changing your password closes every other active session."
      }
    >
      <form
        className="space-y-5"
        onSubmit={(event) => void handleSubmit(submit)(event)}
      >
        {serverError ? (
          <InlineNotice tone="danger" title="Unable to change password">
            {serverError}
          </InlineNotice>
        ) : null}
        <Field
          label="Current password"
          htmlFor="currentPassword"
          required
          error={errors.currentPassword?.message}
        >
          <Input
            id="currentPassword"
            type="password"
            autoComplete="current-password"
            {...register("currentPassword")}
          />
        </Field>
        <Field
          label="New password"
          htmlFor="newPassword"
          required
          error={errors.newPassword?.message}
        >
          <Input
            id="newPassword"
            type="password"
            autoComplete="new-password"
            {...register("newPassword")}
          />
        </Field>
        <Field
          label="Confirm password"
          htmlFor="confirmPassword"
          required
          error={errors.confirmPassword?.message}
        >
          <Input
            id="confirmPassword"
            type="password"
            autoComplete="new-password"
            {...register("confirmPassword")}
          />
        </Field>
        <Button
          type="submit"
          variant="primary"
          size="lg"
          loading={isSubmitting}
          className="w-full"
        >
          Change password
        </Button>
      </form>
    </AuthFrame>
  );
}
