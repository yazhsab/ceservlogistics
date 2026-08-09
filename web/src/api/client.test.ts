import { afterEach, describe, expect, it, vi } from "vitest";
import { apiRequest, clearTokens } from "./client";

afterEach(() => {
  clearTokens(false);
  vi.unstubAllGlobals();
});

describe("API request security boundary", () => {
  it("rejects protocol-relative and non-relative request targets", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      apiRequest("//attacker.example/collect", { auth: false }),
    ).rejects.toThrow("Refused an unsafe API request URL");
    await expect(
      apiRequest("https://attacker.example/collect", { auth: false }),
    ).rejects.toThrow("Refused an unsafe API request URL");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("restores an access token before the first authenticated request", async () => {
    sessionStorage.setItem("courier.refresh-token", "refresh-before-reload");
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            accessToken: "restored-access",
            refreshToken: "rotated-refresh",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ id: "usr_01" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(apiRequest<{ id: string }>("/api/v1/auth/me")).resolves.toEqual(
      { id: "usr_01" },
    );
    expect(fetchMock).toHaveBeenCalledTimes(2);
    const request = fetchMock.mock.calls[1]?.[1] as RequestInit;
    expect(new Headers(request.headers).get("Authorization")).toBe(
      "Bearer restored-access",
    );
  });

  it("provides actionable fallback copy for an unreadable rate-limit response", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("", { status: 429 })),
    );

    await expect(
      apiRequest("/api/v1/shipments", { auth: false }),
    ).rejects.toMatchObject({
      status: 429,
      message: "Too many requests were sent. Wait a moment before trying again.",
    });
  });
});
