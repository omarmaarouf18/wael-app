import { connection } from "next/server";

import { ForgotPasswordPageView } from "@/components/forgot-password-view";
import { placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("en", "forgotPassword");

export default async function Page() {
  await connection();
  return <ForgotPasswordPageView lang="en" />;
}
