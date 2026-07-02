import { VerifyEmailView } from "@/components/auth/verify-email-view";

type SearchParams = Promise<{ [key: string]: string | string[] | undefined }>;

export default async function VerifyEmailPage({ searchParams }: { searchParams: SearchParams }) {
  const value = (await searchParams).token;
  const token = Array.isArray(value) ? value[0] : value || "";
  return <VerifyEmailView token={token} />;
}
