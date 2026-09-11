/**
 * 格式化工具。
 *
 * 后台到处是时间戳与计数，格式不统一会让表格读起来很累。此处把「怎么显示」
 * 集中到一处，各页面不再各写各的。
 */

/** 相对时间。列表里「3 分钟前」比「2026-09-11 14:32:07」有用得多。 */
export function relativeTime(value: string | undefined | null): string {
  if (!value) {
    return "—";
  }
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) {
    return "—";
  }

  const diff = Date.now() - time.getTime();
  const abs = Math.abs(diff);
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;

  if (abs < minute) {
    return diff >= 0 ? "刚刚" : "即将";
  }
  if (abs < hour) {
    const n = Math.round(abs / minute);
    return diff >= 0 ? `${n} 分钟前` : `${n} 分钟后`;
  }
  if (abs < day) {
    const n = Math.round(abs / hour);
    return diff >= 0 ? `${n} 小时前` : `${n} 小时后`;
  }
  if (abs < 30 * day) {
    const n = Math.round(abs / day);
    return diff >= 0 ? `${n} 天前` : `${n} 天后`;
  }
  return absoluteDate(value);
}

/** 绝对日期，用于 title 属性与详情页 —— 相对时间不提供精确信息。 */
export function absoluteDate(value: string | undefined | null): string {
  if (!value) {
    return "—";
  }
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) {
    return "—";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(time);
}

/** 文件体积。附件列表用。 */
export function fileSize(bytes: number | undefined | null): string {
  if (bytes === undefined || bytes === null) {
    return "—";
  }
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}

/** 千分位计数。 */
export function count(value: number | undefined | null): string {
  if (value === undefined || value === null) {
    return "—";
  }
  return new Intl.NumberFormat("zh-CN").format(value);
}

/** 把 ISO 时间转成 `<input type="datetime-local">` 需要的本地时刻串。 */
export function toLocalInput(value: string | undefined | null): string {
  if (!value) {
    return "";
  }
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) {
    return "";
  }
  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    `${time.getFullYear()}-${pad(time.getMonth() + 1)}-${pad(time.getDate())}` +
    `T${pad(time.getHours())}:${pad(time.getMinutes())}`
  );
}

/** `<input type="datetime-local">` 的本地时刻串转回 ISO。空串返回 undefined。 */
export function fromLocalInput(value: string): string | undefined {
  if (!value) {
    return undefined;
  }
  const time = new Date(value);
  return Number.isNaN(time.getTime()) ? undefined : time.toISOString();
}
