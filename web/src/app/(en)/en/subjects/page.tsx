import { SubjectsPage } from "@/components/catalog-pages";
import { placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("en", "subjects");

// Static, refreshed from the public catalog every 300 s
// (CATALOG_REVALIDATE_SECONDS in src/lib/catalog.ts).
export const revalidate = 300;

export default function Page() {
  return <SubjectsPage lang="en" />;
}
