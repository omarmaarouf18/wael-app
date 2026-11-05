import { connection } from "next/server";

import { LoginPageView } from "@/components/login-view";
import { placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("ar", "login");

export default async function Page() {
  await connection();
  return <LoginPageView lang="ar" />;
}
