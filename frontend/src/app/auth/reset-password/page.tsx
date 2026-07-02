import { ResetPasswordView } from "@/components/auth/reset-password-view";

type SearchParams = Promise<{ [key: string]: string | string[] | undefined }>;

export default async function ResetPasswordPage({ searchParams }: { searchParams: SearchParams }) {
  const value = (await searchParams).token;
  const token = Array.isArray(value) ? value[0] : value || "";
  return <ResetPasswordView token={token} />;
}
