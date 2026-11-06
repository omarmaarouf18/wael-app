// Unit tests for Part B (Account) and Part C (Orders) API client methods.
// Run: npm test

import assert from "node:assert/strict";
import { afterEach, beforeEach, test } from "node:test";

import {
  ApiError,
  changePassword,
  clearAccessToken,
  createOrder,
  deleteSession,
  getMyOrderById,
  getMyOrders,
  getNotifications,
  getSessions,
  markNotificationRead,
  requestAccountDeletion,
  setAccessToken,
  updateProfile,
} from "../src/lib/api.ts";

const originalFetch = globalThis.fetch;

beforeEach(() => {
  clearAccessToken();
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  clearAccessToken();
});

test("updateProfile sends PATCH /api/v1/auth/me with credentials and token", async () => {
  setAccessToken("test-user-token");
  let called = false;

  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    called = true;
    assert.equal(String(url), "https://api.elmetracademy.app/api/v1/auth/me");
    assert.equal(init?.method, "PATCH");
    assert.equal(init?.credentials, "include");
    const headers = init?.headers as Record<string, string>;
    assert.equal(headers["X-Client"], "web");
    assert.equal(headers["Authorization"], "Bearer test-user-token");
    assert.equal(headers["Content-Type"], "application/json");

    const body = JSON.parse(String(init?.body));
    assert.equal(body.full_name, "Updated Name");
    assert.equal(body.phone, "01012345678");
    assert.equal(body.current_password, "secret123");

    return new Response(JSON.stringify({ full_name: "Updated Name", phone: "01012345678" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  const res = await updateProfile({
    full_name: "Updated Name",
    phone: "01012345678",
    current_password: "secret123",
  });
  assert.equal(called, true);
  assert.equal(res.full_name, "Updated Name");
  assert.equal(res.phone, "01012345678");
});

test("changePassword sends POST /api/v1/auth/password/change", async () => {
  setAccessToken("test-user-token");
  let called = false;

  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    called = true;
    assert.equal(String(url), "https://api.elmetracademy.app/api/v1/auth/password/change");
    assert.equal(init?.method, "POST");
    const body = JSON.parse(String(init?.body));
    assert.equal(body.current_password, "old-password-123");
    assert.equal(body.new_password, "new-password-456");

    return new Response(JSON.stringify({ status: "ok" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  await changePassword({
    current_password: "old-password-123",
    new_password: "new-password-456",
  });
  assert.equal(called, true);
});

test("getSessions and deleteSession for devices management", async () => {
  setAccessToken("test-user-token");
  let listCalled = false;
  let deleteCalled = false;

  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    const urlStr = String(url);
    if (urlStr.endsWith("/api/v1/auth/sessions") && init?.method === "GET") {
      listCalled = true;
      return new Response(
        JSON.stringify({
          sessions: [
            {
              sid: "sess-1",
              device_label: "Chrome on macOS",
              created_at: "2026-11-05T10:00:00Z",
              last_used_at: "2026-11-05T12:00:00Z",
              current: true,
            },
            {
              sid: "sess-2",
              device_label: "iPhone 13",
              created_at: "2026-11-04T10:00:00Z",
              last_used_at: "2026-11-05T11:00:00Z",
              current: false,
            },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }

    if (urlStr.endsWith("/api/v1/auth/sessions/sess-2") && init?.method === "DELETE") {
      deleteCalled = true;
      return new Response(JSON.stringify({ status: "ok" }), { status: 200 });
    }

    return new Response(JSON.stringify({ error: "not_found" }), { status: 404 });
  };

  const res = await getSessions();
  assert.equal(listCalled, true);
  assert.equal(res.sessions.length, 2);
  assert.equal(res.sessions[0]?.sid, "sess-1");
  assert.equal(res.sessions[0]?.current, true);

  await deleteSession("sess-2");
  assert.equal(deleteCalled, true);
});

test("notifications API getNotifications and markNotificationRead", async () => {
  setAccessToken("test-user-token");
  let listCalled = false;
  let readCalled = false;

  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    const urlStr = String(url);
    if (urlStr.includes("/api/v1/notifications/list") && init?.method === "GET") {
      listCalled = true;
      return new Response(
        JSON.stringify({
          notifications: [
            {
              id: "notif-1",
              title: "Order update",
              title_ar: "تحديث الطلب",
              body: "Your order was approved",
              body_ar: "تمت الموافقة على طلبك",
              created_at: "2026-11-05T12:00:00Z",
              read: false,
            },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }

    if (urlStr.endsWith("/api/v1/notifications/read") && init?.method === "POST") {
      readCalled = true;
      const body = JSON.parse(String(init?.body));
      assert.equal(body.id, "notif-1");
      return new Response(JSON.stringify({ status: "ok" }), { status: 200 });
    }

    return new Response(JSON.stringify({ error: "not_found" }), { status: 404 });
  };

  const listRes = await getNotifications();
  assert.equal(listCalled, true);
  assert.equal(listRes.notifications.length, 1);
  assert.equal(listRes.notifications[0]?.id, "notif-1");

  await markNotificationRead("notif-1");
  assert.equal(readCalled, true);
});

test("requestAccountDeletion sends POST /api/v1/auth/account/delete", async () => {
  setAccessToken("test-user-token");
  let called = false;

  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    called = true;
    assert.equal(String(url), "https://api.elmetracademy.app/api/v1/auth/account/delete");
    assert.equal(init?.method, "POST");
    const body = JSON.parse(String(init?.body));
    assert.equal(body.current_password, "secret123");
    assert.equal(body.confirm, "حذف");

    return new Response(JSON.stringify({ deletion_date: "2026-12-05T10:00:00Z" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  const res = await requestAccountDeletion({ current_password: "secret123", confirm: "حذف" });
  assert.equal(called, true);
  assert.equal(res.deletion_date, "2026-12-05T10:00:00Z");
});

test("createOrder sends POST /api/v1/academy/orders and parses response", async () => {
  setAccessToken("test-user-token");
  let called = false;

  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    called = true;
    assert.equal(String(url), "https://api.elmetracademy.app/api/v1/academy/orders");
    assert.equal(init?.method, "POST");
    const body = JSON.parse(String(init?.body));
    assert.deepEqual(body.subject_ids, ["subj-1", "subj-2"]);
    assert.equal(body.payment_method, "wallet");
    assert.equal(body.transfer_reference, "TXN-12345");
    assert.equal(body.sender_number, "01098765432");
    assert.equal(body.amount_paid, 300);

    return new Response(
      JSON.stringify({
        id: "ord-test-1",
        source: "web",
        status: "open",
        created_at: "2026-11-05T10:00:00Z",
        total: 300,
        amount_mismatch: false,
        items: [
          {
            request_id: "req-1",
            subject_id: "subj-1",
            title_ar: "مادة 1",
            price: 150,
            status: "pending",
          },
          {
            request_id: "req-2",
            subject_id: "subj-2",
            title_ar: "مادة 2",
            price: 150,
            status: "pending",
          },
        ],
      }),
      { status: 201, headers: { "Content-Type": "application/json" } },
    );
  };

  const res = await createOrder({
    subject_ids: ["subj-1", "subj-2"],
    payment_method: "wallet",
    transfer_reference: "TXN-12345",
    sender_number: "01098765432",
    amount_paid: 300,
  });

  assert.equal(called, true);
  assert.equal(res.id, "ord-test-1");
  assert.equal(res.status, "open");
  assert.equal(res.items.length, 2);
});

test("getMyOrders and getMyOrderById parse orders list and item details", async () => {
  setAccessToken("test-user-token");
  let listCalled = false;
  let singleCalled = false;

  globalThis.fetch = async (url: string | URL | Request, init?: RequestInit) => {
    const urlStr = String(url);
    if (urlStr.endsWith("/api/v1/academy/me/orders") && init?.method === "GET") {
      listCalled = true;
      return new Response(
        JSON.stringify({
          items: [
            {
              id: "ord-1",
              source: "web",
              status: "closed",
              created_at: "2026-11-05T10:00:00Z",
              total: 200,
              amount_mismatch: false,
              items: [],
            },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }

    if (urlStr.endsWith("/api/v1/academy/me/orders/ord-1") && init?.method === "GET") {
      singleCalled = true;
      return new Response(
        JSON.stringify({
          id: "ord-1",
          source: "web",
          status: "closed",
          created_at: "2026-11-05T10:00:00Z",
          total: 200,
          amount_mismatch: false,
          items: [],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }

    return new Response(JSON.stringify({ error: "not_found" }), { status: 404 });
  };

  const listRes = await getMyOrders();
  assert.equal(listCalled, true);
  assert.equal(listRes.items.length, 1);
  assert.equal(listRes.items[0]?.id, "ord-1");

  const singleRes = await getMyOrderById("ord-1");
  assert.equal(singleCalled, true);
  assert.equal(singleRes.id, "ord-1");
});

test("ApiError parses bilingual messages, subject_id, and Retry-After header", async () => {
  globalThis.fetch = async () => {
    return new Response(
      JSON.stringify({
        error: "already_owned",
        code: "already_owned",
        message_ar: "أنت مشترك في هذه المادة بالفعل",
        message_en: "You already own this subject",
        subject_id: "subj-101",
      }),
      {
        status: 409,
        headers: {
          "Content-Type": "application/json",
          "Retry-After": "60",
        },
      },
    );
  };

  await assert.rejects(
    async () => {
      await createOrder({
        subject_ids: ["subj-101"],
        payment_method: "instapay",
        transfer_reference: "REF123",
        sender_number: "01000000000",
        amount_paid: 100,
      });
    },
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 409);
      assert.equal(err.code, "already_owned");
      assert.equal(err.subjectId, "subj-101");
      assert.equal(err.messageAr, "أنت مشترك في هذه المادة بالفعل");
      assert.equal(err.messageEn, "You already own this subject");
      assert.equal(err.retryAfter, 60);
      return true;
    },
  );
});
