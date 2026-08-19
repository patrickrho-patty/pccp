// PAT-1449: harness release campaign view model — canonical Korean
// state labels and cohort/preview helpers mirrored from the Go domain.

export const HV_STATES = [
  'supported', 'update_available', 'update_required_grace', 'restricted',
  'revoked', 'updating', 'verifying', 'rollback_required', 'repair_required', 'unknown_or_tampered',
] as const

export const HV_STATE_KO: Record<string, string> = {
  supported: '정상 지원',
  update_available: '업데이트 가능',
  update_required_grace: '업데이트 필요 (유예 중)',
  restricted: '제한 모드',
  revoked: '폐기된 릴리스',
  updating: '설치 진행 중',
  verifying: '설치 검증 중',
  rollback_required: '롤백 필요',
  repair_required: '복구 필요',
  unknown_or_tampered: '검증 불가 (변조 의심)',
}

export const HV_STATE_TONE: Record<string, string> = {
  supported: 'bg-emerald-100 text-emerald-700',
  update_available: 'bg-sky-100 text-sky-700',
  update_required_grace: 'bg-amber-100 text-amber-800',
  restricted: 'bg-orange-100 text-orange-700',
  revoked: 'bg-red-100 text-red-700',
  updating: 'bg-sky-100 text-sky-700',
  verifying: 'bg-sky-100 text-sky-700',
  rollback_required: 'bg-amber-100 text-amber-800',
  repair_required: 'bg-orange-100 text-orange-700',
  unknown_or_tampered: 'bg-gray-200 text-gray-700',
}

export const HV_CAMPAIGN_STATE_KO: Record<string, string> = {
  draft: '초안', active: '활성', paused: '일시중지', cancelled: '취소됨', completed: '완료', rolled_back: '롤백됨',
}

export const RING_KO: Record<string, string> = { canary: '카나리', beta: '베타', stable: '안정' }

// Parse check mirrored for form validation: canonical semver, optional v.
export function isValidVersion(s: string): boolean {
  return /^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+.+)?$/.test(s.trim())
}

// Deadline urgency for the grace banner.
export function deadlineTone(deadline?: string, now: Date = new Date()): 'none' | 'soon' | 'past' {
  if (!deadline) return 'none'
  const d = new Date(deadline)
  if (Number.isNaN(d.getTime())) return 'none'
  if (now > d) return 'past'
  if (d.getTime() - now.getTime() < 24 * 3600 * 1000) return 'soon'
  return 'none'
}
