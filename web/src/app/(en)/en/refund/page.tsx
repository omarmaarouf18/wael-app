import { RefundPage, refundMetadata } from "@/components/refund-page";

export const metadata = refundMetadata("en");

export default function Page() {
  return <RefundPage lang="en" />;
}
