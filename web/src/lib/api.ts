// Typed client for the shared backend (api-gateway).
//
// STUB (website foundations, ADR-0014): no page calls this yet. W2 adds the
// public catalog endpoint, W3 web auth, W4 purchase requests; each brief
// adds its own typed calls here. Activation never happens in the browser.

import { API_ORIGIN } from "./config";

export type Lang = "ar" | "en";

/** Bilingual text as the academy API returns it (Arabic required). */
export interface LocalizedText {
  ar: string;
  en?: string;
}

/** Placeholder for the W2 public subject shape; not final. */
export interface PublicSubject {
  id: string;
  title: LocalizedText;
  description: LocalizedText;
  price: number | null;
  currency: "EGP";
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
  ) {
    super(`API ${status}: ${code}`);
    this.name = "ApiError";
  }
}

/**
 * GET a JSON resource from the API. Paths must be absolute API paths
 * ("/academy/..."); a full URL or a protocol-relative path is refused so a
 * caller can never send a request to another host.
 */
export async function apiGet<T>(path: string, init?: { signal?: AbortSignal }): Promise<T> {
  if (!path.startsWith("/") || path.startsWith("//")) {
    throw new Error(`apiGet: path must be an absolute API path, got ${JSON.stringify(path)}`);
  }
  const res = await fetch(`${API_ORIGIN}${path}`, {
    method: "GET",
    headers: { Accept: "application/json" },
    credentials: "omit",
    redirect: "error",
    signal: init?.signal,
  });
  if (!res.ok) {
    let code = "http_error";
    try {
      const body: unknown = await res.json();
      if (body && typeof body === "object" && "error" in body && typeof body.error === "string") {
        code = body.error;
      }
    } catch {
      // Non-JSON error body: keep the generic code.
    }
    throw new ApiError(res.status, code);
  }
  return (await res.json()) as T;
}
