import { Component, type ErrorInfo, type ReactNode } from "react";
import { AlertTriangle } from "lucide-react";
import { Button } from "./ui";

export class AppErrorBoundary extends Component<
  { children: ReactNode },
  { error?: Error }
> {
  state: { error?: Error } = {};

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    if (import.meta.env.DEV)
      console.error("Frontend render failure", error, info);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <main className="grid min-h-screen place-items-center bg-background p-6">
        <section className="max-w-lg rounded-lg border border-border bg-white p-8 text-center shadow-panel">
          <span className="mx-auto grid h-12 w-12 place-items-center rounded-full bg-red-50 text-danger">
            <AlertTriangle aria-hidden className="h-6 w-6" />
          </span>
          <h1 className="mt-4 text-xl font-bold">This view could not open</h1>
          <p className="mt-2 text-sm text-slate-600">
            Your work may still be saved. Reload this view; if it happens again,
            share the time and page with support.
          </p>
          <Button
            variant="primary"
            className="mt-5"
            onClick={() => window.location.reload()}
          >
            Reload view
          </Button>
        </section>
      </main>
    );
  }
}
