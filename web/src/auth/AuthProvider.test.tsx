import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { apiRequest, clearTokens } from "../api/client";
import { AuthProvider, useAuth } from "./AuthProvider";

vi.mock("../api/client", () => ({
  apiRequest: vi.fn(),
  clearTokens: vi.fn(),
  configureApiAuth: vi.fn(),
  hasRefreshToken: () => false,
  setTokens: vi.fn(),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});
function SessionControls() {
  const { login, logout, user } = useAuth();
  return (
    <>
      <span>{user?.fullName ?? "Signed out"}</span>
      <button onClick={() => void login("qa@example.test", "test-password")}>
        Log in
      </button>
      <button onClick={() => void logout().catch(() => undefined)}>
        Log out
      </button>
    </>
  );
}
it("clears cached account data on login and even when server logout fails", async () => {
  const user = userEvent.setup();
  const removeItem = vi.fn();
  vi.stubGlobal("localStorage", { removeItem });
  const client = new QueryClient();
  client.setQueryData(["customers"], { name: "Previous account" });
  vi.mocked(apiRequest).mockImplementation(async (path) => {
    await Promise.resolve();
    if (path === "/api/v1/auth/login")
      return { tokens: {}, user: { fullName: "Next account" } };
    throw new Error("Connection lost");
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AuthProvider>
          <SessionControls />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Log in" }));
  expect(await screen.findByText("Next account")).toBeVisible();
  expect(client.getQueryData(["customers"])).toBeUndefined();
  expect(removeItem).toHaveBeenCalledWith("courier.hub-facility");
  expect(removeItem).toHaveBeenCalledWith("courier.scan-facility");
  removeItem.mockClear();
  client.setQueryData(["invoices"], { amount: 100 });
  await user.click(screen.getByRole("button", { name: "Log out" }));
  await waitFor(() => expect(screen.getByText("Signed out")).toBeVisible());
  expect(client.getQueryData(["invoices"])).toBeUndefined();
  expect(clearTokens).toHaveBeenCalled();
  expect(removeItem).toHaveBeenCalledWith("courier.hub-facility");
  expect(removeItem).toHaveBeenCalledWith("courier.scan-facility");
});
