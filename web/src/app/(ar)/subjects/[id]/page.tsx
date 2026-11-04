import type { Metadata } from "next";

import { SubjectPage, subjectMetadata, subjectStaticParams } from "@/components/catalog-pages";

// Prebuilt for every subject in the catalog and refreshed every 300 s
// (CATALOG_REVALIDATE_SECONDS); a subject published after the build is
// rendered on its first request and cached the same way. All from the one
// catalog call (src/lib/catalog.ts).
export const revalidate = 300;

export function generateStaticParams() {
  return subjectStaticParams();
}

export async function generateMetadata({ params }: { params: Promise<{ id: string }> }): Promise<Metadata> {
  const { id } = await params;
  return subjectMetadata("ar", id);
}

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <SubjectPage lang="ar" id={id} />;
}
