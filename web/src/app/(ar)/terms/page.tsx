import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("ar", "terms");

export default function Page() {
  return <PlaceholderPage lang="ar" page="terms" path="/terms" />;
}
