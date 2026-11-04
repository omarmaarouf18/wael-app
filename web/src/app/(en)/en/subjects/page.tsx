import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("en", "subjects");

export default function Page() {
  return <PlaceholderPage lang="en" page="subjects" path="/subjects" />;
}
