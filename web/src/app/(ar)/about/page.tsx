import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("ar", "about");

export default function Page() {
  return <PlaceholderPage lang="ar" page="about" path="/about" />;
}
