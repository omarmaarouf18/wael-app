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

  constructor(status: number, code: string, message?: string) {
    super(message || `API ${status}: ${code}`);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
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
    try {
      const body: unknown = await res.json();
      if (body && typeof body === "object") {
        if ("error" in body && typeof body.error === "string") {
          code = body.error;
        }
        if ("message" in body && typeof body.message === "string") {
          message = body.message;
        }
      }
    } catch {
      // Non-JSON error body: keep the generic code.
    }
    throw new ApiError(res.status, code, message);
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

export async function refresh(): Promise<AuthTokens> {
  const res = await apiPost<AuthTokens>("/api/v1/auth/refresh", {}, { skipAuth: true });
  setAccessToken(res.access_token);
  return res;
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

export async function getMyEntitlements(): Promise<{ subject_ids: string[] }> {
  return apiGet<{ subject_ids: string[] }>("/api/v1/academy/me/entitlements");
}
