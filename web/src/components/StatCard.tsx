import { Link } from 'react-router-dom'

// StatCard (00-cross-cutting A5) — every dashboard/count is a
// drill-down. `to` deep-links to the filtered list; without `to` the
// value renders as a plain stat. `sub` shows a context line (e.g.
// "+3 today" or "미해결").
const ACCENTS: Record<string, string> = {
  blue: 'text-blue-600',
  green: 'text-green-600',
  red: 'text-red-600',
  orange: 'text-orange-600',
  yellow: 'text-yellow-600',
  purple: 'text-purple-600',
  gray: 'text-gray-600',
}

export function StatCard({ label, value, accent = 'gray', to, sub, query }: {
  label: string
  value: React.ReactNode
  accent?: keyof typeof ACCENTS | string
  to?: string
  sub?: string
  query?: string
}) {
  const inner = (
    <div className="stat-card text-center hover:border-gray-300 hover:bg-gray-50 transition-colors cursor-default h-full flex flex-col justify-center">
      <div className={`stat-value ${ACCENTS[accent] || accent}`}>{value}</div>
      <div className="stat-label">{label}</div>
      {sub && <div className="text-[10px] text-gray-400 mt-0.5">{sub}</div>}
    </div>
  )
  if (!to) return inner
  return <Link to={query ? `${to}${query}` : to} className="block h-full">{inner}</Link>
}
