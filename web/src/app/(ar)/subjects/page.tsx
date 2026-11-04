import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("ar", "subjects");

export default function Page() {
  return <PlaceholderPage lang="ar" page="subjects" path="/subjects" />;
}
