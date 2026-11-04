import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("en", "about");

export default function Page() {
  return <PlaceholderPage lang="en" page="about" path="/about" />;
}
