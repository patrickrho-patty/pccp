// complianceView.ts — PAT-1504: traceable compliance workspace presentation.
//
// Compliance evidence, remediations, and assessment runs share one coherent
// presentation so an admin can move from an assessment result to the exact
// control, evidence, gap, owner, deadline, and later verification — without
// decoding raw workflow state from row text. Module-only formatting (no
// business inference in components) mirrors responsibility.ts/evidenceView.ts.

export interface TaskState {
  id: string
  labelKo: string
  nextActionKo: string
  color: string // tailwind badge token (icon+text primary, color secondary)
  icon: string
}

/** Canonical remediation task states (open / in_progress / done) with
 *  current state + a recommended next action (PAT-1504: "show current state
 *  only, and the next action"). Extend as statuses evolve; keep this as the
 *  single label registry the page and any summary consume. */
const TASK_STATE: Record<string, TaskState> = {
  open: { id: 'open', labelKo: '미착수', nextActionKo: '담당자 배정 후 시작', color: 'bg-gray-100 text-gray-600 border-gray-200', icon: '◻' },
  in_progress: { id: 'in_progress', labelKo: '진행 중', nextActionKo: '완료 검증 수행', color: 'bg-blue-50 text-blue-700 border-blue-200', icon: '◐' },
  done: { id: 'done', labelKo: '완료', nextActionKo: '재평가로 검증', color: 'bg-green-50 text-green-700 border-green-200', icon: '◉' },
}

export function taskState(s: string): TaskState {
  return TASK_STATE[s] || { id: s, labelKo: s || '미심판', nextActionKo: '', color: 'bg-gray-100 text-gray-500 border-gray-200', icon: '◌' }
}

/** Human due-date age label (overdue / due-soon / recent), secondary text. */
export function dueAgeLabel(dueDate?: string): string | undefined {
  if (!dueDate) return undefined
  const day = Math.floor((new Date(dueDate + 'T00:00:00').getTime() - Date.now()) / 86400000)
  if (day < 0) return `기한 초과 ${Math.abs(day)}일`
  if (day === 0) return '오늘 마감'
  if (day <= 7) return `${day}일 내`
  return `마감 D-${day}`
}

/** Evidence source → human Korean label (manual/audit/provenance/...). */
const EVIDENCE_SOURCE_KO: Record<string, string> = {
  manual: '수동 등록', audit: '감사', provenance: '프로비넌스', security: '보안', attestation: '증명',
}

export function evidenceSourceKo(source?: string): string {
  return (source && EVIDENCE_SOURCE_KO[source]) || source || '기록'
}

/** Evidence freshness label — age of the collected_at timestamp. */
export function evidenceFreshnessLabel(collectedAt?: string): string {
  if (!collectedAt) return '시각 미기록'
  const t = new Date(collectedAt).getTime()
  if (Number.isNaN(t)) return '시각 미기록'
  const days = Math.floor((Date.now() - t) / 86400000)
  if (days < 0) return '미래 시각'
  if (days === 0) return '오늘 수집'
  if (days < 30) return `${days}일 전 수집`
  return `${Math.floor(days / 30)}개월 전`
}

export interface AssessmentRun {
  id: string
  assessedAt: string
  scope: string
  level: string
  overallStatus: string
  openGaps: number
  results: Record<string, { status: string; gap_description_ko?: string; gap_description?: string }[]>
  count: number // how many repeated identical runs this group represents
}

/** Parse the ResultsJSON snapshot into control_id → result rows. */
export function parseControlResults(raw: string | undefined, fallback: any[] = []): Record<string, { status: string; gap_description_ko?: string; gap_description?: string }[]> {
  if (!raw) return {}
  try {
    const arr = typeof raw === 'string' ? JSON.parse(raw) : raw
    if (!Array.isArray(arr)) return {}
    const byControl: Record<string, { status: string; gap_description_ko?: string; gap_description?: string }[]> = {}
    for (const r of arr) {
      if (!r?.control_id) continue
      byControl[r.control_id] = byControl[r.control_id] || []
      byControl[r.control_id].push({ status: r.status, gap_description_ko: r.gap_description_ko, gap_description: r.gap_description })
    }
    return byControl
  } catch { return {} }
}

/** Group repeated identical assessment runs (same cert/scope/level + status
 *  summary) into one row, keeping the newest snapshot as the drill target.
 *  Returns groups with a change summary vs the previous distinct run. */
export function groupAssessmentRuns(runs: any[]): { grouped: AssessmentRun[]; changedControls: Record<string, string[]> } {
  const seen: Record<string, AssessmentRun> = {}
  const order: string[] = []
  for (const r of runs || []) {
    const resultsHash = typeof r.results === 'string' ? String(r.results).slice(0, 512) : JSON.stringify(r.results || '').slice(0, 512)
    const key = [r.certification || '', r.scope || '', r.level || '', r.overall_status || '', resultsHash].join('|')
    const existing = seen[key]
    if (existing) {
      existing.count = (existing.count || 1) + 1
      // Keep newest snapshot as drill target
      if (String(r.assessed_at || '') > String(existing.assessedAt || '')) {
        existing.id = r.id
        existing.assessedAt = r.assessed_at
        existing.results = parseControlResults(r.results)
      }
      continue
    }
    const parsed = parseControlResults(r.results)
    const grouped: AssessmentRun = {
      id: r.id, assessedAt: r.assessed_at, scope: r.scope, level: r.level,
      overallStatus: r.overall_status, openGaps: r.open_gaps, results: parsed, count: 1,
    }
    seen[key] = grouped
    order.push(key)
  }
  const grouped = order.map(k => seen[k])
  // Change summary: control status diffs between consecutive distinct runs.
  const changedControls: Record<string, string[]> = {}
  for (let i = 1; i < grouped.length; i++) {
    const prev = grouped[i - 1]
    const cur = grouped[i]
    const keys = new Set([...Object.keys(prev.results), ...Object.keys(cur.results)])
    const diffs: string[] = []
    for (const c of keys) {
      const a = prev.results[c]?.[0]?.status
      const b = cur.results[c]?.[0]?.status
      if (a !== b) diffs.push(`${c}: ${a || '없음'} → ${b || '없음'}`)
    }
    if (diffs.length) changedControls[cur.id] = diffs
  }
  return { grouped, changedControls }
}
