import { HomePage, homeMetadata } from "@/components/home-page";

export const metadata = homeMetadata("ar");

// Static, refreshed with the public catalog every 300 s
// (CATALOG_REVALIDATE_SECONDS in src/lib/catalog.ts).
export const revalidate = 300;

export default function Page() {
  return <HomePage lang="ar" />;
}
