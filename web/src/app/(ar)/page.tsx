import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("ar", "home");

export default function Page() {
  return <PlaceholderPage lang="ar" page="home" path="/" />;
}
