const relative = new Intl.RelativeTimeFormat("ko", { numeric: "auto" });
const absolute = new Intl.DateTimeFormat("ko", { dateStyle: "medium", timeStyle: "medium" });

const STEPS: Array<[Intl.RelativeTimeFormatUnit, number]> = [
  ["second", 60],
  ["minute", 60],
  ["hour", 24],
  ["day", 30],
  ["month", 12],
];

/** "3분 전" style label; empty for missing or unparsable timestamps. */
export function formatRelative(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return "";
  const time = Date.parse(iso);
  if (Number.isNaN(time)) return "";
  let value = (time - now) / 1000;
  for (const [unit, size] of STEPS) {
    if (Math.abs(value) < size) return relative.format(Math.round(value), unit);
    value /= size;
  }
  return relative.format(Math.round(value), "year");
}

export function formatAbsolute(iso: string | null | undefined): string {
  if (!iso) return "";
  const time = Date.parse(iso);
  return Number.isNaN(time) ? "" : absolute.format(time);
}

export const ACTION_LABELS: Record<string, string> = {
  "secret.create": "생성",
  "secret.update": "수정",
  "secret.delete": "삭제",
  "secret.grants": "권한 변경",
  "secret.reveal": "값 보기",
  "secret.read": "에이전트 조회",
  "agent.enroll": "에이전트 연결",
  "agent.unenroll": "연결 해제",
};

export function actionLabel(action: string): string {
  return ACTION_LABELS[action] ?? action;
}

export function resultLabel(result: string): string {
  if (result === "ok") return "성공";
  if (result === "denied") return "거부";
  return result;
}
