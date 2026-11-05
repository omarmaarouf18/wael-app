import { connection } from "next/server";

import { placeholderMetadata } from "@/components/placeholder-page";
import { SignupPageView } from "@/components/signup-view";

export const metadata = placeholderMetadata("ar", "signup");

export default async function Page() {
  await connection();
  return <SignupPageView lang="ar" />;
}
