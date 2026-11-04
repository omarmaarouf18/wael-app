import { connection } from "next/server";

import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("ar", "signup");

// A page with a form (W3): rendered per request so Next.js applies the CSP
// nonce from src/proxy.ts. The fixed pages are static (ADR-0014 decision 7).
export default async function Page() {
  await connection();
  return <PlaceholderPage lang="ar" page="signup" path="/signup" />;
}
