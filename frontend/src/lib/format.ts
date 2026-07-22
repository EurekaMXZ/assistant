const fullDateTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
});

const shortDateTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
});

interface FormatDateTimeOptions {
  fallback?: string;
  includeYear?: boolean;
}

function formatDateTime(
  value?: string | number | Date | null,
  { fallback = "-", includeYear = true }: FormatDateTimeOptions = {},
) {
  if (!value) return fallback;
  return (includeYear ? fullDateTimeFormatter : shortDateTimeFormatter).format(new Date(value));
}

export { formatDateTime };
