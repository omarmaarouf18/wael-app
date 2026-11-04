import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("en", "home");

export default function Page() {
  return <PlaceholderPage lang="en" page="home" path="/" />;
}
