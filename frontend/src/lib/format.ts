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

const messageTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

const weekdayFormatter = new Intl.DateTimeFormat("zh-CN", {
  weekday: "short",
});

const millisecondsPerDay = 24 * 60 * 60 * 1000;

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

function localCalendarDay(date: Date) {
  return Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()) / millisecondsPerDay;
}

function formatMessageDateTime(
  value?: string | number | Date | null,
  now: Date = new Date(),
  fallback = "-",
) {
  if (!value) return fallback;
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return fallback;

  const dayDifference = localCalendarDay(now) - localCalendarDay(date);
  const time = messageTimeFormatter.format(date);
  if (dayDifference === 0) return `今天 ${time}`;
  if (dayDifference === 1) return `昨天 ${time}`;
  if (dayDifference === 2) return `前天 ${time}`;
  if (dayDifference >= 3 && dayDifference <= 6) {
    return `${weekdayFormatter.format(date)} ${time}`;
  }
  return fullDateTimeFormatter.format(date);
}

export { formatDateTime, formatMessageDateTime };
