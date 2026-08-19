// PAT-1439: public status page view model — colors, Korean labels,
// lifecycle states, and 90-day bar rendering data. Korean is the
// authoritative public language; machines own color, humans own wording.

export const PS_COLORS = ['green', 'yellow', 'orange', 'red', 'blue', 'gray'] as const
export type PSColor = (typeof PS_COLORS)[number]

export const COLOR_KO: Record<string, string> = {
  green: '정상 운영',
  yellow: '일부 기능 영향 · 확인 중',
  orange: '서비스 이용 불안정 · 확인 중',
  red: '서비스 이용이 어려운 상태 · 확인 중',
  blue: '점검 예정 / 점검 진행 중',
  gray: '상태 확인 중 / 측정 정보 부족',
}

export const COLOR_RANK: Record<string, number> = {
  gray: 0, green: 1, blue: 2, yellow: 3, orange: 4, red: 5,
}

export const COLOR_BG: Record<string, string> = {
  green: 'bg-emerald-500', yellow: 'bg-amber-400', orange: 'bg-orange-500',
  red: 'bg-red-600', blue: 'bg-sky-500', gray: 'bg-gray-400',
}

export const INCIDENT_STATE_KO: Record<string, string> = {
  investigating: '확인 중',
  mitigating: '원인 확인 및 조치 중',
  monitoring: '안정성 확인 중',
  resolved: '정상화',
  maintenance_scheduled: '점검 예정',
  maintenance_in_progress: '점검 진행 중',
}

// Effective override color: an unexpired override wins, else measured.
export function effectiveColor(
  c: { measured_color?: string; override_color?: string; override_expires_at?: string },
  now: Date = new Date(),
): string {
  if (c.override_color) {
    if (!c.override_expires_at) return c.override_color
    const exp = new Date(c.override_expires_at)
    if (!Number.isNaN(exp.getTime()) && now < exp) return c.override_color
  }
  return c.measured_color || 'gray'
}

// Daily segment color for the 90-day bar (availability thresholds pair
// with text, never color alone — a11y per PAT-1439/1517).
export function daySegmentColor(day: { availability_pct: number; no_data_seconds?: number; maintenance_seconds?: number }): string {
  if (day.no_data_seconds !== undefined && day.no_data_seconds >= 86400) return 'gray'
  if ((day.maintenance_seconds ?? 0) > 0 && (day.availability_pct ?? 100) >= 99.95) return 'blue'
  if ((day.availability_pct ?? 0) >= 99.95) return 'green'
  if ((day.availability_pct ?? 0) >= 95) return 'yellow'
  if ((day.availability_pct ?? 0) >= 80) return 'orange'
  return 'red'
}

// A11y label for one 90-day segment (KST date, availability, downtime).
export function daySegmentLabel(day: { date_kst: string; availability_pct: number; impacted_seconds: number; maintenance_seconds?: number; no_data_seconds?: number }): string {
  const parts = [`${day.date_kst} (KST)`, `가용성 ${day.availability_pct.toFixed(2)}%`]
  if (day.impacted_seconds > 0) parts.push(`영향 ${Math.round(day.impacted_seconds / 60)}분`)
  if ((day.maintenance_seconds ?? 0) > 0) parts.push(`점검 ${Math.round(day.maintenance_seconds! / 60)}분`)
  if ((day.no_data_seconds ?? 0) > 0) parts.push('측정 없음 구간 있음')
  return parts.join(' · ')
}

// Pad the 90-day bar to exactly 90 segments (oldest first); missing days
// render as no-data gray.
export function buildNinetyDayBar(days: Array<{ date_kst: string }>, now: Date = new Date()): Array<{ date_kst: string | null }> {
  const byDate = new Map(days.map((d) => [d.date_kst, d]))
  const out: Array<{ date_kst: string | null }> = []
  const kstNow = new Date(now.getTime() + 9 * 3600 * 1000)
  for (let i = 89; i >= 0; i--) {
    const d = new Date(kstNow)
    d.setUTCDate(d.getUTCDate() - i)
    const key = d.toISOString().slice(0, 10)
    out.push(byDate.has(key) ? { date_kst: key } : { date_kst: null })
  }
  return out
}
