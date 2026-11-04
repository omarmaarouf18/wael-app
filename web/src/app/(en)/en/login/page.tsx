import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("en", "login");

export default function Page() {
  return <PlaceholderPage lang="en" page="login" path="/login" />;
}
