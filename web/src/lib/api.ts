// Typed client for the shared backend (api-gateway).
//
// W2 added the public catalog endpoint, W3 adds web auth, W4 adds purchase
// requests. Activation never happens in the browser.

import { API_ORIGIN } from "./config.ts";

export type Lang = "ar" | "en";

/** Bilingual text as the academy API returns it (Arabic required). */
export interface LocalizedText {
  ar: string;
  en?: string;
}

export interface UserProfile {
  id: string;
  email: string;
  role: string;
  full_name: string;
  phone: string | null;
  email_verified: boolean;
}

export interface AuthTokens {
  access_token: string;
  expires_in: number;
}

export interface SignupResponse {
  id: string;
  email: string;
  role: string;
  full_name: string;
  phone: string | null;
  pending_id: string;
  dev_otp?: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly messageAr?: string;
  readonly messageEn?: string;
  readonly subjectId?: string;
  readonly retryAfter?: number;

  constructor(
    status: number,
    code: string,
    message?: string,
    opts?: { messageAr?: string; messageEn?: string; subjectId?: string; retryAfter?: number },
  ) {
    super(message || `API ${status}: ${code}`);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.messageAr = opts?.messageAr;
    this.messageEn = opts?.messageEn;
    this.subjectId = opts?.subjectId;
    this.retryAfter = opts?.retryAfter;
  }
}

let inMemoryAccessToken: string | null = null;

export function getAccessToken(): string | null {
  return inMemoryAccessToken;
}

export function setAccessToken(token: string | null): void {
  inMemoryAccessToken = token;
}

export function clearAccessToken(): void {
  inMemoryAccessToken = null;
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  skipAuth?: boolean;
}

export async function apiFetch<T>(path: string, options: RequestOptions = {}): Promise<T> {
  if (!path.startsWith("/") || path.startsWith("//")) {
    throw new Error(`apiFetch: path must be an absolute API path, got ${JSON.stringify(path)}`);
  }
  const headers: Record<string, string> = {
    Accept: "application/json",
    "X-Client": "web",
    ...(options.headers || {}),
  };
  if (options.body !== undefined && !headers["Content-Type"]) {
    headers["Content-Type"] = "application/json";
  }
  if (!options.skipAuth && inMemoryAccessToken && !headers["Authorization"]) {
    headers["Authorization"] = `Bearer ${inMemoryAccessToken}`;
  }

  const res = await fetch(`${API_ORIGIN}${path}`, {
    method: options.method || (options.body ? "POST" : "GET"),
    headers,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    credentials: "include",
    redirect: "error",
    signal: options.signal,
  });

  if (!res.ok) {
    let code = "http_error";
    let message = "";
    let messageAr: string | undefined;
    let messageEn: string | undefined;
    let subjectId: string | undefined;
    const retryAfterHeader = res.headers.get("Retry-After");
    const retryAfter = retryAfterHeader ? parseInt(retryAfterHeader, 10) : undefined;
    try {
      const body: unknown = await res.json();
      if (body && typeof body === "object") {
        const b = body as Record<string, unknown>;
        if (typeof b.error === "string") {
          code = b.error;
        } else if (typeof b.code === "string") {
          code = b.code;
        }
        if (typeof b.message === "string") {
          message = b.message;
        }
        if (typeof b.message_ar === "string") {
          messageAr = b.message_ar;
        }
        if (typeof b.message_en === "string") {
          messageEn = b.message_en;
        }
        if (typeof b.subject_id === "string") {
          subjectId = b.subject_id;
        }
      }
    } catch {
      // Non-JSON error body: keep the generic code.
    }
    throw new ApiError(res.status, code, message, {
      messageAr,
      messageEn,
      subjectId,
      retryAfter: Number.isNaN(retryAfter) ? undefined : retryAfter,
    });
  }

  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

export async function apiGet<T>(path: string, init?: { signal?: AbortSignal; skipAuth?: boolean }): Promise<T> {
  return apiFetch<T>(path, { method: "GET", signal: init?.signal, skipAuth: init?.skipAuth });
}

export async function apiPost<T>(
  path: string,
  body?: unknown,
  init?: { signal?: AbortSignal; skipAuth?: boolean },
): Promise<T> {
  return apiFetch<T>(path, { method: "POST", body, signal: init?.signal, skipAuth: init?.skipAuth });
}

export async function apiPatch<T>(
  path: string,
  body?: unknown,
  init?: { signal?: AbortSignal; skipAuth?: boolean },
): Promise<T> {
  return apiFetch<T>(path, { method: "PATCH", body, signal: init?.signal, skipAuth: init?.skipAuth });
}

export async function apiDelete<T>(
  path: string,
  init?: { signal?: AbortSignal; skipAuth?: boolean },
): Promise<T> {
  return apiFetch<T>(path, { method: "DELETE", signal: init?.signal, skipAuth: init?.skipAuth });
}

// Auth API client calls

export async function login(data: { email: string; password: string }): Promise<AuthTokens> {
  const res = await apiPost<AuthTokens>("/api/v1/auth/login", data, { skipAuth: true });
  setAccessToken(res.access_token);
  return res;
}

export async function signup(data: {
  full_name: string;
  email: string;
  password: string;
  phone?: string;
}): Promise<SignupResponse> {
  return apiPost<SignupResponse>("/api/v1/auth/signup", data, { skipAuth: true });
}

export async function verifyOTP(data: {
  email: string;
  code: string;
  pending_id: string;
}): Promise<AuthTokens> {
  const res = await apiPost<AuthTokens>("/api/v1/auth/verify-otp", data, { skipAuth: true });
  setAccessToken(res.access_token);
  return res;
}

export async function resendSignupOTP(data: {
  email?: string;
  pending_id: string;
}): Promise<{ dev_otp?: string }> {
  return apiPost<{ dev_otp?: string }>("/api/v1/auth/signup/resend", data, { skipAuth: true });
}

let inFlightRefresh: Promise<AuthTokens> | null = null;

export async function refresh(): Promise<AuthTokens> {
  if (inFlightRefresh) {
    return inFlightRefresh;
  }
  inFlightRefresh = (async () => {
    try {
      const res = await apiPost<AuthTokens>("/api/v1/auth/refresh", {}, { skipAuth: true });
      setAccessToken(res.access_token);
      return res;
    } finally {
      inFlightRefresh = null;
    }
  })();
  return inFlightRefresh;
}

export async function logout(): Promise<void> {
  try {
    await apiPost("/api/v1/auth/logout", {});
  } finally {
    clearAccessToken();
  }
}

export async function requestPasswordReset(email: string): Promise<{ status: string; dev_otp?: string }> {
  return apiPost<{ status: string; dev_otp?: string }>("/api/v1/auth/reset/request", { email }, { skipAuth: true });
}

export async function verifyPasswordReset(email: string, code: string): Promise<{ reset_token: string }> {
  return apiPost<{ reset_token: string }>("/api/v1/auth/reset/verify", { email, code }, { skipAuth: true });
}

export async function confirmPasswordReset(data: {
  reset_token: string;
  new_password: string;
}): Promise<{ status: string }> {
  return apiPost<{ status: string }>("/api/v1/auth/reset/confirm", data, { skipAuth: true });
}

export async function getMe(): Promise<UserProfile> {
  return apiGet<UserProfile>("/api/v1/auth/me");
}

export interface AuthInitResult {
  isLoggedIn: boolean;
  token: string | null;
  user: UserProfile | null;
}

/**
 * Initializes client auth state on initial mount or full page reload.
 * Attempts to silently exchange the HttpOnly cookie for an in-memory access token.
 * If successful, also fetches the user profile. If the refresh token is missing,
 * expired, or invalid, fails silently without throwing, reporting logged out.
 */
export async function initAuthSession(): Promise<AuthInitResult> {
  try {
    const res = await refresh();
    let me: UserProfile | null = null;
    try {
      me = await getMe();
    } catch {
      // Non-fatal if profile fetch fails
    }
    return {
      isLoggedIn: true,
      token: res.access_token,
      user: me,
    };
  } catch {
    clearAccessToken();
    return {
      isLoggedIn: false,
      token: null,
      user: null,
    };
  }
}

export async function getMyEntitlements(): Promise<{ subject_ids: string[] }> {
  return apiGet<{ subject_ids: string[] }>("/api/v1/academy/me/entitlements");
}

export interface PaymentMethod {
  kind: "wallet" | "instapay";
  label: string;
  number_or_address: string;
}

export interface StudentRequestItem {
  id: string;
  subject_id: string;
  subject_title_ar: string;
  subject_title_en: string;
  status: "pending" | "accepted" | "rejected";
  source: string;
  created_at: string;
  decided_at?: string;
  reject_reason?: string;
  amount: number;
}

export interface PurchaseRequestResponse {
  id: string;
  subject_id: string;
  status: string;
  source: string;
  payment_method: string;
  transfer_reference: string;
  sender_number: string;
  amount: number;
  created_at: string;
}

export async function getPaymentMethods(): Promise<{ items: PaymentMethod[] }> {
  return apiGet<{ items: PaymentMethod[] }>("/api/v1/academy/payment-methods");
}

export async function createPurchaseRequest(
  subjectId: string,
  data: {
    payment_method: "wallet" | "instapay";
    transfer_reference: string;
    sender_number: string;
    amount: number;
  },
): Promise<PurchaseRequestResponse> {
  return apiPost<PurchaseRequestResponse>(`/api/v1/academy/subjects/${encodeURIComponent(subjectId)}/purchase-request`, data);
}

export async function getMyRequests(): Promise<{ items: StudentRequestItem[] }> {
  return apiGet<{ items: StudentRequestItem[] }>("/api/v1/academy/me/requests");
}

export async function getMyRequestById(id: string): Promise<StudentRequestItem> {
  return apiGet<StudentRequestItem>(`/api/v1/academy/me/requests/${encodeURIComponent(id)}`);
}

// Account Management (Part B)

export interface ProfileUpdateResponse {
  full_name: string;
  phone: string;
}

export async function updateProfile(data: {
  full_name?: string;
  phone?: string;
  current_password: string;
}): Promise<ProfileUpdateResponse> {
  return apiPatch<ProfileUpdateResponse>("/api/v1/auth/me", data);
}

export async function changePassword(data: {
  current_password: string;
  new_password: string;
}): Promise<void> {
  await apiPost("/api/v1/auth/password/change", data);
}

export interface DeviceSession {
  sid: string;
  device_label: string;
  created_at: string;
  last_used_at: string;
  current: boolean;
}

export async function getSessions(): Promise<{ sessions: DeviceSession[] }> {
  return apiGet<{ sessions: DeviceSession[] }>("/api/v1/auth/sessions");
}

export async function deleteSession(sid: string): Promise<void> {
  await apiDelete(`/api/v1/auth/sessions/${encodeURIComponent(sid)}`);
}

export interface NotificationItem {
  id: string;
  title: string;
  title_ar: string;
  body: string;
  body_ar: string;
  created_at: string;
  read: boolean;
  type?: string;
  target_route?: string;
  subject_id?: string;
}

export async function getNotifications(params?: {
  page?: number;
  limit?: number;
}): Promise<{ notifications: NotificationItem[] }> {
  const page = params?.page ?? 1;
  const limit = params?.limit ?? 20;
  return apiGet<{ notifications: NotificationItem[] }>(`/api/v1/notifications/list?page=${page}&limit=${limit}`);
}

export async function markNotificationRead(id: string): Promise<void> {
  await apiPost("/api/v1/notifications/read", { id });
}

export async function requestAccountDeletion(data: {
  current_password: string;
  confirm?: string;
}): Promise<{ deletion_date: string }> {
  return apiPost<{ deletion_date: string }>("/api/v1/auth/account/delete", {
    current_password: data.current_password,
    confirm: data.confirm ?? "حذف",
  });
}

// Orders API (Part C - Agent 1 Contract docs/contracts/orders-api.md)

export interface CreateOrderPayload {
  subject_ids: string[];
  payment_method: "wallet" | "instapay";
  transfer_reference: string;
  sender_number: string;
  amount_paid: number;
}

export interface OrderItem {
  request_id: string;
  subject_id: string;
  title_ar: string;
  title_en?: string;
  level_name_ar?: string;
  price: number;
  status: "pending" | "accepted" | "rejected";
  decided_at?: string;
  reject_reason?: string;
}

export interface OrderResponse {
  id: string;
  source: "web" | "app";
  status: "open" | "closed";
  created_at: string;
  decided_at?: string;
  payment_method?: "wallet" | "instapay";
  transfer_reference?: string;
  sender_number?: string;
  amount_paid?: number;
  total: number;
  amount_mismatch: boolean;
  items: OrderItem[];
}

export async function createOrder(data: CreateOrderPayload): Promise<OrderResponse> {
  return apiPost<OrderResponse>("/api/v1/academy/orders", data);
}

export async function getMyOrders(): Promise<{ items: OrderResponse[] }> {
  return apiGet<{ items: OrderResponse[] }>("/api/v1/academy/me/orders");
}

export async function getMyOrderById(id: string): Promise<OrderResponse> {
  return apiGet<OrderResponse>(`/api/v1/academy/me/orders/${encodeURIComponent(id)}`);
}


