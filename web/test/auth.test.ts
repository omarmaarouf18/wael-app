// Unit tests for web auth flow and session persistence.
// Tests:
// - Concurrent refresh deduplication (one in flight)
// - Login -> reload -> still logged in
// - Logout -> reload -> logged out
// - Expired cookie -> logged out without an error screen
// Run: npm test (node --test)

import assert from "node:assert/strict";
import { afterEach, beforeEach, test } from "node:test";

import {
  ApiError,
  clearAccessToken,
  getAccessToken,
  login,
  logout,
  refresh,
  setAccessToken,
} from "../src/lib/api.ts";

const originalFetch = globalThis.fetch;

beforeEach(() => {
  clearAccessToken();
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  clearAccessToken();
});

test("concurrent refresh calls are deduplicated to a single in-flight fetch", async () => {
  let fetchCallCount = 0;

  globalThis.fetch = async (url: string | URL | Request) => {
    const urlStr = String(url);
    if (urlStr.endsWith("/api/v1/auth/refresh")) {
      fetchCallCount++;
      // Simulate network latency so concurrent calls overlap
      await new Promise((resolve) => setTimeout(resolve, 20));
      return new Response(
        JSON.stringify({
          access_token: "rotated-access-token-123",
          expires_in: 86400,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      );
    }
    return new Response(JSON.stringify({}), { status: 404 });
  };

  // Launch 5 concurrent refresh calls
  const promises = [refresh(), refresh(), refresh(), refresh(), refresh()];
  const results = await Promise.all(promises);

  // Exactly 1 network request was made
  assert.equal(fetchCallCount, 1, "expected exactly 1 fetch call for concurrent refreshes");

  // All 5 promises resolved with the exact same rotated token
  for (const res of results) {
    assert.equal(res.access_token, "rotated-access-token-123");
  }
  assert.equal(getAccessToken(), "rotated-access-token-123");

  // A subsequent refresh call after completion triggers a new network request
  await refresh();
  assert.equal(fetchCallCount, 2, "expected a new fetch call after the previous finished");
});

test("login -> reload -> still logged in", async () => {
  let refreshCalled = false;

  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    const urlStr = String(url);
    if (urlStr.endsWith("/api/v1/auth/login")) {
      return new Response(
        JSON.stringify({
          access_token: "initial-login-token",
          expires_in: 86400,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      );
    }
    if (urlStr.endsWith("/api/v1/auth/refresh")) {
      refreshCalled = true;
      assert.equal(init?.credentials, "include");
      const headers = init?.headers as Record<string, string>;
      assert.equal(headers["X-Client"], "web");
      return new Response(
        JSON.stringify({
          access_token: "restored-reloaded-token",
          expires_in: 86400,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      );
    }
    return new Response(JSON.stringify({}), { status: 404 });
  };

  // 1. User logs in
  const loginRes = await login({ email: "student@example.com", password: "Password123!" });
  assert.equal(loginRes.access_token, "initial-login-token");
  assert.equal(getAccessToken(), "initial-login-token");

  // 2. User reloads or navigates to a new page (browser JS context wiped)
  clearAccessToken();
  assert.equal(getAccessToken(), null, "in-memory access token wiped on reload");

  // 3. New page loads -> AuthProvider calls refresh() with credentials: "include"
  const refreshRes = await refresh();
  assert.equal(refreshCalled, true);
  assert.equal(refreshRes.access_token, "restored-reloaded-token");
  assert.equal(getAccessToken(), "restored-reloaded-token", "user is still logged in after reload");
});

test("logout -> reload -> logged out", async () => {
  let logoutCalled = false;
  let refreshCalled = false;

  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    const urlStr = String(url);
    if (urlStr.endsWith("/api/v1/auth/logout")) {
      logoutCalled = true;
      assert.equal(init?.credentials, "include");
      return new Response(null, { status: 204 });
    }
    if (urlStr.endsWith("/api/v1/auth/refresh")) {
      refreshCalled = true;
      // Cookie was cleared by logout on the server; refresh returns 401
      return new Response(
        JSON.stringify({
          code: "invalid_token",
          error: "invalid refresh token",
        }),
        {
          status: 401,
          headers: { "Content-Type": "application/json" },
        },
      );
    }
    return new Response(JSON.stringify({}), { status: 404 });
  };

  // 1. Logged-in user has access token
  setAccessToken("active-session-token");
  assert.equal(getAccessToken(), "active-session-token");

  // 2. User clicks logout
  await logout();
  assert.equal(logoutCalled, true);
  assert.equal(getAccessToken(), null, "token cleared on logout");

  // 3. User reloads -> page on mount calls refresh()
  await assert.rejects(
    async () => {
      await refresh();
    },
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 401);
      assert.equal(err.code, "invalid refresh token");
      return true;
    },
  );

  assert.equal(refreshCalled, true);
  assert.equal(getAccessToken(), null, "user remains logged out after reload");
});

test("expired cookie -> logged out without an error screen", async () => {
  globalThis.fetch = async (url: string | URL | Request) => {
    const urlStr = String(url);
    if (urlStr.endsWith("/api/v1/auth/refresh")) {
      return new Response(
        JSON.stringify({
          code: "session_expired",
          error: "refresh token expired",
        }),
        {
          status: 401,
          headers: { "Content-Type": "application/json" },
        },
      );
    }
    return new Response(JSON.stringify({}), { status: 404 });
  };

  // Simulating AuthProvider on-mount refresh handler:
  // Must catch 401 silently, clear any stale state, and report logged out without throwing.
  let isLoggedIn = true;
  let hasErrorScreen = false;

  try {
    const res = await refresh();
    setAccessToken(res.access_token);
    isLoggedIn = true;
  } catch (err: unknown) {
    if (err instanceof ApiError && err.status === 401) {
      // 401 means logged out, silently
      clearAccessToken();
      isLoggedIn = false;
      hasErrorScreen = false;
    } else {
      hasErrorScreen = true;
    }
  }

  assert.equal(isLoggedIn, false, "user should be treated as logged out");
  assert.equal(hasErrorScreen, false, "no error screen should be shown for expired cookie");
  assert.equal(getAccessToken(), null);
});
