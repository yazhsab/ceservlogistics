import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import {
  Checkbox,
  ErrorState,
  Field,
  Input,
  StatusBadge,
  Switch,
} from "./ui";

describe("shared state components", () => {
  it("presents status as text rather than color alone", () => {
    render(<StatusBadge status="OUT_FOR_DELIVERY" />);
    expect(screen.getByText("Out For Delivery")).toBeVisible();
  });

  it("shows actionable API support context", () => {
    const error = Object.assign(new Error("No route is configured."), {
      requestId: "req_test_01",
    });
    render(<ErrorState error={error} retry={() => undefined} />);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "No route is configured.",
    );
    expect(screen.getByText("Request req_test_01")).toBeVisible();
    expect(screen.getByRole("button", { name: "Try again" })).toBeEnabled();
  });

  it("keeps boolean controls named and keyboard operable", async () => {
    const user = userEvent.setup();
    render(
      <>
        <Checkbox aria-label="Select row" />
        <Switch label="Remote area" />
      </>,
    );
    await user.click(screen.getByRole("checkbox", { name: "Select row" }));
    await user.click(screen.getByRole("switch", { name: "Remote area" }));
    expect(screen.getByRole("checkbox", { name: "Select row" })).toBeChecked();
    expect(screen.getByRole("switch", { name: "Remote area" })).toBeChecked();
  });

  it("associates validation errors and required state with form controls", () => {
    render(
      <Field
        label="Account reference"
        htmlFor="reference"
        required
        error="Enter an account reference."
      >
        <div className="relative">
          <Input id="reference" />
        </div>
      </Field>,
    );
    const input = screen.getByRole("textbox", { name: /Account reference/ });
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAttribute("aria-required", "true");
    expect(input).toHaveAccessibleDescription("Enter an account reference.");
  });
});
