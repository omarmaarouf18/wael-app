import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("en", "terms");

export default function Page() {
  return <PlaceholderPage lang="en" page="terms" path="/terms" />;
}
