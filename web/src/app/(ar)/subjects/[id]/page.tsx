import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("ar", "subject");

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <PlaceholderPage lang="ar" page="subject" path={`/subjects/${encodeURIComponent(id)}`} />;
}
