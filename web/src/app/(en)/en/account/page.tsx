import { connection } from "next/server";

import { AccountPageView } from "@/components/account-view";
import { placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("en", "account");

export default async function Page() {
  await connection();
  return <AccountPageView lang="en" />;
}
