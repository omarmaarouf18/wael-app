import { PlaceholderPage, placeholderMetadata } from "@/components/placeholder-page";

export const metadata = placeholderMetadata("en", "subject");

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <PlaceholderPage lang="en" page="subject" path={`/subjects/${encodeURIComponent(id)}`} />;
}
