import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import {
  IntegrationStatus,
  OneTimeSecretDialog,
  ScopeList,
} from "./integrations";

describe("integration safety components", () => {
  it("states empty access scopes explicitly", () => {
    render(<ScopeList scopes={[]} />);
    expect(screen.getByText("No access scopes")).toBeInTheDocument();
  });

  it("renders webhook failure state as text", () => {
    render(<IntegrationStatus status="DEAD_LETTER" />);
    expect(screen.getByText("Dead Letter")).toBeInTheDocument();
  });

  it("warns that a secret is one-time and copies only on user action", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    render(
      <OneTimeSecretDialog
        open
        onOpenChange={() => undefined}
        title="Credential issued"
        secret="apk_test.secret-value"
        label="Authorization token"
      />,
    );
    expect(
      screen.getByText("Copy this now; it will not be shown again."),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Authorization token")).toHaveValue(
      "apk_test.secret-value",
    );
    expect(writeText).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Copy" }));
    expect(writeText).toHaveBeenCalledWith("apk_test.secret-value");
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
  });
});
