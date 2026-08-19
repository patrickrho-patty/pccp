export function formatRelative(ts?: string, now: number = Date.now()): string {
	if (!ts) return '-'
	const d = new Date(ts)
	if (isNaN(d.getTime())) return '-'
	const diff = now - d.getTime()
	if (diff < -60_000) return `서버 시간보다 ${Math.ceil(Math.abs(diff) / 60000)}분 이후`
	const mins = Math.floor(diff / 60000)
  if (mins < 1) return '방금 전'
  if (mins < 60) return mins + '분 전'
  const hours = Math.floor(mins / 60)
  if (hours < 24) return hours + '시간 전'
  const days = Math.floor(hours / 24)
  if (days < 7) return days + '일 전'
  return d.toLocaleDateString('ko-KR')
}

// formatShortTime renders an ISO instant as a short, timezone-labeled local
// timestamp. Single canonical short formatter — do not hand-roll
// `slice(0,16).replace('T',' ')` (it silently strips timezone info).
export function formatShortTime(iso?: string | null, timeZone: string = 'Asia/Seoul'): string {
	if (!iso) return '-'
	const d = new Date(iso)
	if (isNaN(d.getTime())) return '-'
	try {
		return `${d.toLocaleString('ko-KR', { timeZone, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })} (${timeZone})`
	} catch {
		return `${d.toLocaleString('ko-KR', { timeZone: 'Asia/Seoul', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })} (Asia/Seoul)`
	}
}

// formatTenantTime renders an ISO instant with the tenant's explicit IANA
// timezone label (PAT-1496: never show an unlabeled local timestamp).
export function formatTenantTime(iso?: string, timeZone: string = 'Asia/Seoul'): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return '-'
	try {
		return `${d.toLocaleString('ko-KR', { timeZone, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })} (${timeZone})`
	} catch {
		return `${d.toLocaleString('ko-KR', { timeZone: 'Asia/Seoul', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })} (Asia/Seoul)`
	}
}
