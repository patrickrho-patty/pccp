export function formatRelative(ts?: string, now: number = Date.now()): string {
  if (!ts) return '-'
  const d = new Date(ts)
  const diff = now - d.getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return '방금 전'
  if (mins < 60) return mins + '분 전'
  const hours = Math.floor(mins / 60)
  if (hours < 24) return hours + '시간 전'
  const days = Math.floor(hours / 24)
  if (days < 7) return days + '일 전'
  return d.toLocaleDateString('ko-KR')
}

// formatTenantTime renders an ISO instant in the tenant timezone with an
// explicit KST marker — the single labeled timestamp surfaces show
// (PAT-1496: never render a raw UTC slice beside a local time).
export function formatTenantTime(iso?: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleString('ko-KR', { timeZone: 'Asia/Seoul', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
