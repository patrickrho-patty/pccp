export function formatRelative(ts?: string): string {
  if (!ts) return '-'
  const d = new Date(ts)
  const diff = Date.now() - d.getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return '방금 전'
  if (mins < 60) return mins + '분 전'
  const hours = Math.floor(mins / 60)
  if (hours < 24) return hours + '시간 전'
  const days = Math.floor(hours / 24)
  if (days < 7) return days + '일 전'
  return d.toLocaleDateString('ko-KR')
}
