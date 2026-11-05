import { connection } from "next/server";

import { ForgotPasswordPageView } from "@/components/forgot-password-view";
import { placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("ar", "forgotPassword");

export default async function Page() {
  await connection();
  return <ForgotPasswordPageView lang="ar" />;
}
