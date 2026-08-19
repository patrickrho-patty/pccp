// glossary.ts — PAT-1519: one Korean-first terminology + state-label system.
//
// The console used to mix Korean labels, English product copy, raw database
// enums (active, pending, normal, pre_approved, open/in_progress/done, ...),
// and internal event keys. This module is the CANONICAL registry: every
// entity / lifecycle state / risk-severity / decision / evidence / action /
// outcome label resolves here (composing the per-domain modules where they
// already exist), with a safe Korean "알 수 없는 상태" fallback plus an
// optional telemetry hook for the missing mapping, and Korean-first
// date/number/byte/token/duration/currency formatters.
//
// Pages must call these helpers — never independently title-case or translate
// raw backend values. Raw values stay visible in technical/evidence detail.

export interface GlossaryTerm {
  ko: string      // Korean primary label
  en?: string     // optional secondary technical label
  explain?: string // plain-language meaning + next action
}

// ---- Canonical entity names ------------------------------------------------

import { sessionStatusMeta } from './sessionState.ts'

export const ENTITY_KO: Record<string, string> = {
  user: '사용자', harness: '하네스', session: '세션', project: '프로젝트',
  repository: '저장소', model: '모델', endpoint: '엔드포인트',
  finding: '보안 발견', audit_event: '감사 이벤트', policy_epoch: '정책 버전',
  policy_rule: '정책 규칙', tool: '도구', approval: '승인 요청',
  compliance_evidence: '준수 증거', remediation: '개선 과제', broadcast: '안내 방송',
  file_transfer: '파일 전송', conversation: '대화', sandbox: '샌드박스',
}

// ---- Lifecycle states (aggregating per-domain modules) ---------------------
const LIFECYCLE_KO: Record<string, GlossaryTerm> = {
  active: { ko: '활성', en: 'active', explain: '정상 작동 중' },
  pending: { ko: '대기', en: 'pending', explain: '승인/등록 대기 중' },
  enrolled: { ko: '등록됨', en: 'enrolled', explain: '자격이 발급됨' },
  idle: { ko: '유휴', en: 'idle', explain: '실행 중이지만 활성 세션 없음' },
  paused: { ko: '일시정지', en: 'paused', explain: '일시 중지됨' },
  closed: { ko: '종료', en: 'closed', explain: '정상 종료' },
  terminated: { ko: '강제종료', en: 'terminated', explain: '비정상/강제 종료' },
  suspended: { ko: '정지', en: 'suspended', explain: '사용이 중지됨' },
  offboarded: { ko: '퇴사', en: 'offboarded', explain: '계정이 정리됨' },
  quarantined: { ko: '격리', en: 'quarantined', explain: '격리됨 — 조사 필요' },
  revoked: { ko: '해지', en: 'revoked', explain: '자격이 해지됨' },
  pre_approved: { ko: '사전 승인', en: 'pre_approved', explain: '사전 승인된 등록' },
  stable: { ko: '안정', en: 'stable', explain: '안정 상태' },
  licensed: { ko: '라이선스', en: 'licensed', explain: '라이선스 보유' },
  elevated: { ko: '주의', en: 'elevated', explain: '리스크 상승 — 검토 필요' },
  normal: { ko: '정상', en: 'normal', explain: '정상 상태' },
  available: { ko: '사용 가능', en: 'available', explain: '이용 가능' },
  ready: { ko: '준비됨', en: 'ready', explain: '사용 준비 완료' },
  uploading: { ko: '업로드 중', en: 'uploading' },
  scanning: { ko: '검사 중', en: 'scanning' },
  delivered: { ko: '전달됨', en: 'delivered' },
  completed: { ko: '완료', en: 'completed' },
  rejected: { ko: '거부', en: 'rejected' },
  expired: { ko: '만료', en: 'expired' },
}

// ---- Risk / severity -------------------------------------------------------

export const SEVERITY_KO: Record<string, GlossaryTerm> = {
  info: { ko: '정보', en: 'info' },
  low: { ko: '낮음', en: 'low' },
  medium: { ko: '중간', en: 'medium' },
  high: { ko: '높음', en: 'high' },
  critical: { ko: '심각', en: 'critical', explain: '즉시 조치 필요' },
}

// ---- Decisions / outcomes / compliance -------------------------------------

export const DECISION_KO: Record<string, GlossaryTerm> = {
  allowed: { ko: '허용', en: 'allowed' },
  approved: { ko: '승인', en: 'approved' },
  denied: { ko: '거부', en: 'denied', explain: '요청이 차단됨' },
  rejected: { ko: '거부', en: 'rejected' },
  success: { ko: '성공', en: 'success' },
  failed: { ko: '실패', en: 'failed' },
  failure: { ko: '실패', en: 'failure' },
  pending: { ko: '대기', en: 'pending' },
  warning: { ko: '경고', en: 'warning' },
  compliant: { ko: '준수', en: 'compliant' },
  partially_compliant: { ko: '부분 준수', en: 'partially_compliant' },
  gap: { ko: '갭', en: 'gap', explain: '미충족 통제' },
  not_applicable: { ko: '해당 없음', en: 'not_applicable' },
  open: { ko: '미착수', en: 'open', explain: '작업 시작 전' },
  in_progress: { ko: '진행 중', en: 'in_progress' },
  done: { ko: '완료', en: 'done' },
  online: { ko: '접속 중', en: 'online' },
  away: { ko: '자리 비움', en: 'away' },
  busy: { ko: '업무 중', en: 'busy' },
  offline: { ko: '오프라인', en: 'offline' },
}

// ---- Actions / evidence ----------------------------------------------------

export const ACTION_KO: Record<string, GlossaryTerm> = {
  created: { ko: '생성', en: 'created' },
  updated: { ko: '갱신', en: 'updated' },
  deleted: { ko: '삭제', en: 'deleted' },
  enrolled: { ko: '등록', en: 'enrolled' },
  revoked: { ko: '해제', en: 'revoked' },
  approved: { ko: '승인', en: 'approved' },
  assessed: { ko: '평가', en: 'assessed' },
  published: { ko: '게시', en: 'published' },
  quarantined: { ko: '격리', en: 'quarantined' },
  executed: { ko: '실행', en: 'executed' },
  file_write: { ko: '파일 작성', en: 'file_write' },
  ai_inference: { ko: 'AI 추론', en: 'ai_inference' },
  tool_use: { ko: '도구 실행', en: 'tool_use' },
  exchange_open: { ko: '교환 시작', en: 'exchange_open' },
  exchange_close: { ko: '교환 종료', en: 'exchange_close' },
}

export const EVIDENCE_KO: Record<string, GlossaryTerm> = {
  receipt: { ko: '수신 증명', en: 'receipt' },
  provenance: { ko: '프로비넌스', en: 'provenance' },
  attestation: { ko: '증명', en: 'attestation' },
  changeset: { ko: '변경셋', en: 'changeset' },
  digest: { ko: '해시 검증', en: 'digest' },
  audit_chain: { ko: '감사 체인', en: 'audit_chain' },
}

// ---- Telemetry + safe fallback ---------------------------------------------

const UNKNOWN_KO = '알 수 없는 상태'

/** Optional hook so missing mappings emit telemetry (and tests can stub it). */
let telemetryRelay: (field: string, raw: string) => void = () => {}
export function setGlossaryTelemetry(fn: (field: string, raw: string) => void) { telemetryRelay = fn }
export function _resetGlossaryTelemetryForTests() { telemetryRelay = () => {} }

/** Safe label lookup with unknown fallback + telemetry. */
export function glossaryLabel(field: string, raw: string | undefined): GlossaryTerm {
  const value = raw ?? ''
  const termMaps: Record<string, Record<string, GlossaryTerm>> = {
    lifecycle: LIFECYCLE_KO, severity: SEVERITY_KO, decision: DECISION_KO,
    action: ACTION_KO, evidence: EVIDENCE_KO,
  }
  const strMap: Record<string, Record<string, string>> = { entity: ENTITY_KO }
  if (termMaps[field]?.[value]) return termMaps[field][value]
  if (strMap[field]?.[value]) return { ko: strMap[field][value], en: value }
  if (value) telemetryRelay(field, value)
  return { ko: UNKNOWN_KO, en: value || undefined }
}

/** Convenience: plain Korean label with fallback (no term object needed). */
export function koLabel(field: string, raw: string | undefined): string {
  return glossaryLabel(field, raw).ko
}

/** Compose a session lifecycle from its canonical module when known. */
export function sessionLifecycleLabel(status: string): string {
  try { return sessionStatusMeta(status).ko } catch { return glossaryLabel('lifecycle', status).ko }
}

// ---- Person term -----------------------------------------------------------

/** PAT-1519: 사용자 is the general term; 개발자 only for an explicit role. */
export function personTerm(roleOrType?: string): string {
  const t = (roleOrType || '').toLowerCase()
  return t === 'developer' || t === 'dev' ? '개발자' : '사용자'
}

// ---- Korean-first formatters -----------------------------------------------

export function formatDurationKo(seconds?: number): string {
  if (typeof seconds !== 'number' || Number.isNaN(seconds)) return '—'
  const s = Math.max(0, Math.floor(seconds))
  if (s < 60) return `${s}초`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}분`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}시간`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d}일`
  return `${Math.floor(d / 30)}개월`
}

export function formatBytesKo(bytes?: number): string {
  if (typeof bytes !== 'number' || Number.isNaN(bytes)) return '—'
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = bytes / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(1)} ${units[i]}`
}

export function formatTokensKo(n?: number): string {
  if (typeof n !== 'number' || Number.isNaN(n)) return '—'
  return `${n.toLocaleString('ko-KR')} 토큰`
}

export function formatCurrencyKo(amount: number, currency = 'KRW'): string {
  if (Number.isNaN(amount)) return '—'
  if (currency === 'KRW') return `${Math.round(amount).toLocaleString('ko-KR')}원`
  if (currency === 'USD') return `$${amount.toFixed(2)}`
  return `${amount} ${currency}`
}

/** Absolute date/time with explicit timezone context. */
export function formatDateTimeKo(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString('ko-KR', { timeZoneName: 'short' })
}
