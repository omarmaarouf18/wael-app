// Unit tests for the web API client (src/lib/api.ts).
// Run: npm test (node --test; Node 24 strips the TypeScript types).

import assert from "node:assert/strict";
import { afterEach, beforeEach, test } from "node:test";

import {
  ApiError,
  apiFetch,
  clearAccessToken,
  createPurchaseRequest,
  getAccessToken,
  getMyRequestById,
  getMyRequests,
  getPaymentMethods,
  resendSignupOTP,
  setAccessToken,
} from "../src/lib/api.ts";

test("ApiError stores status and code", () => {
  const err = new ApiError(401, "unauthorized", "custom message");
  assert.equal(err.status, 401);
  assert.equal(err.code, "unauthorized");
  assert.equal(err.message, "custom message");
  assert.equal(err.name, "ApiError");

  const errDefault = new ApiError(403, "forbidden");
  assert.equal(errDefault.message, "API 403: forbidden");
});

test("in-memory access token management", () => {
  clearAccessToken();
  assert.equal(getAccessToken(), null);

  setAccessToken("test-token-123");
  assert.equal(getAccessToken(), "test-token-123");

  clearAccessToken();
  assert.equal(getAccessToken(), null);
});

test("refuses paths that are not absolute or are protocol-relative", async () => {
  await assert.rejects(
    async () => {
      await apiFetch("relative/path");
    },
    { message: /must be an absolute API path/ },
  );

  await assert.rejects(
    async () => {
      await apiFetch("//evil.com/path");
    },
    { message: /must be an absolute API path/ },
  );
});

// Mock fetch for request shape testing
const originalFetch = globalThis.fetch;

beforeEach(() => {
  clearAccessToken();
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  clearAccessToken();
});

test("apiFetch sets X-Client: web and credentials: include", async () => {
  let capturedUrl = "";
  let capturedInit: RequestInit | undefined;

  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    capturedUrl = String(url);
    capturedInit = init;
    return new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  const res = await apiFetch<{ ok: boolean }>("/api/v1/test");
  assert.deepEqual(res, { ok: true });
  assert.ok(capturedUrl.endsWith("/api/v1/test"));
  assert.equal(capturedInit?.credentials, "include");

  const headers = capturedInit?.headers as Record<string, string>;
  assert.equal(headers["X-Client"], "web");
  assert.equal(headers["Accept"], "application/json");
  assert.equal(headers["Authorization"], undefined);
});

test("apiFetch attaches Bearer token when token is set", async () => {
  setAccessToken("session-token-xyz");

  let capturedInit: RequestInit | undefined;
  globalThis.fetch = async (_url: string | URL | Request, init?: RequestInit) => {
    capturedInit = init;
    return new Response(JSON.stringify({ status: "ok" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  await apiFetch("/api/v1/auth/me");
  const headers = capturedInit?.headers as Record<string, string>;
  assert.equal(headers["Authorization"], "Bearer session-token-xyz");
});

test("apiFetch parses ApiError body and code", async () => {
  globalThis.fetch = async () => {
    return new Response(JSON.stringify({ error: "account_locked", message: "Locked" }), {
      status: 429,
      headers: { "Content-Type": "application/json" },
    });
  };

  await assert.rejects(
    async () => {
      await apiFetch("/api/v1/auth/login", { body: { email: "a@b.com" } });
    },
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 429);
      assert.equal(err.code, "account_locked");
      return true;
    },
  );
});

test("getPaymentMethods calls GET /api/v1/academy/payment-methods", async () => {
  let capturedUrl = "";
  globalThis.fetch = async (url: string | URL | Request) => {
    capturedUrl = String(url);
    return new Response(JSON.stringify({
      items: [{ kind: "instapay", label: "My InstaPay", number_or_address: "user@instapay" }],
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  const res = await getPaymentMethods();
  assert.ok(capturedUrl.endsWith("/api/v1/academy/payment-methods"));
  assert.equal(res.items.length, 1);
  assert.equal(res.items[0]?.kind, "instapay");
});

test("createPurchaseRequest sends POST with json payload", async () => {
  let capturedUrl = "";
  let capturedBody = "";
  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    capturedUrl = String(url);
    capturedBody = String(init?.body);
    return new Response(JSON.stringify({
      id: "req-123",
      subject_id: "subj-abc",
      status: "pending",
      source: "web",
      payment_method: "wallet",
      transfer_reference: "REF12345",
      sender_number: "01012345678",
      amount: 150,
      created_at: "2026-11-05T10:00:00Z",
    }), {
      status: 201,
      headers: { "Content-Type": "application/json" },
    });
  };

  const res = await createPurchaseRequest("subj-abc", {
    payment_method: "wallet",
    transfer_reference: "REF12345",
    sender_number: "01012345678",
    amount: 150,
  });

  assert.ok(capturedUrl.endsWith("/api/v1/academy/subjects/subj-abc/purchase-request"));
  assert.equal(res.id, "req-123");
  assert.equal(res.status, "pending");
  assert.deepEqual(JSON.parse(capturedBody), {
    payment_method: "wallet",
    transfer_reference: "REF12345",
    sender_number: "01012345678",
    amount: 150,
  });
});

test("getMyRequests calls GET /api/v1/academy/me/requests", async () => {
  let capturedUrl = "";
  globalThis.fetch = async (url: string | URL | Request) => {
    capturedUrl = String(url);
    return new Response(JSON.stringify({
      items: [
        {
          id: "req-1",
          subject_id: "subj-1",
          subject_title_ar: "مادة",
          subject_title_en: "Subject",
          status: "pending",
          source: "web",
          created_at: "2026-11-05T10:00:00Z",
          amount: 150,
        },
      ],
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  const res = await getMyRequests();
  assert.ok(capturedUrl.endsWith("/api/v1/academy/me/requests"));
  assert.equal(res.items.length, 1);
  assert.equal(res.items[0]?.id, "req-1");
});

test("getMyRequestById calls GET /api/v1/academy/me/requests/{id}", async () => {
  let capturedUrl = "";
  globalThis.fetch = async (url: string | URL | Request) => {
    capturedUrl = String(url);
    return new Response(JSON.stringify({
      id: "req-42",
      subject_id: "subj-1",
      subject_title_ar: "مادة",
      subject_title_en: "Subject",
      status: "accepted",
      source: "web",
      created_at: "2026-11-05T10:00:00Z",
      amount: 150,
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  const res = await getMyRequestById("req-42");
  assert.ok(capturedUrl.endsWith("/api/v1/academy/me/requests/req-42"));
  assert.equal(res.id, "req-42");
  assert.equal(res.status, "accepted");
});

test("resendSignupOTP pins exact payload with only email and no pending_id", async () => {
  let capturedUrl = "";
  let capturedBody = "";
  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    capturedUrl = String(url);
    capturedBody = String(init?.body);
    return new Response(JSON.stringify({ status: "ok" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  await resendSignupOTP({ email: "student@example.com" });
  assert.ok(capturedUrl.endsWith("/api/v1/auth/signup/resend"));
  assert.equal(capturedBody, JSON.stringify({ email: "student@example.com" }));
  const parsed = JSON.parse(capturedBody);
  assert.deepEqual(parsed, { email: "student@example.com" });
  assert.equal(Object.prototype.hasOwnProperty.call(parsed, "pending_id"), false);
});

