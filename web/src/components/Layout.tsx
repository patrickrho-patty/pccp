import { ReactNode, useState, useEffect, useRef } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

type NavItem = {
  path: string
  label: string
  labelEn: string
  icon: string
}

type NavSection = {
  title: string
  titleEn: string
  items: NavItem[]
}

const navSections: NavSection[] = [
  {
    title: '개요',
    titleEn: 'Overview',
    items: [
      { path: '/', label: '대시보드', labelEn: 'Dashboard', icon: '◈' },
    ]
  },
  {
    title: '조직',
    titleEn: 'Organization',
    items: [
      { path: '/users', label: '사용자', labelEn: 'Users', icon: '◉' },
      { path: '/harnesses', label: '하네스', labelEn: 'Harnesses', icon: '⬡' },
      { path: '/projects', label: '프로젝트', labelEn: 'Projects', icon: '▣' },
      { path: '/repositories', label: '저장소', labelEn: 'Repositories', icon: '▤' },
    ]
  },
  {
    title: 'AI 세션',
    titleEn: 'AI Sessions',
    items: [
      { path: '/live', label: '실시간 뷰', labelEn: 'Live Wall', icon: '🔴' },
      { path: '/sessions', label: '세션', labelEn: 'Sessions', icon: '◐' },
      { path: '/fleet', label: '플릿 관리', labelEn: 'Fleet', icon: '⛶' },
    ]
  },
  {
    title: '모델',
    titleEn: 'Models',
    items: [
      { path: '/models', label: '모델 패키지', labelEn: 'Packages', icon: '◆' },
      { path: '/catalog', label: '카탈로그', labelEn: 'Catalog', icon: '🗂' },
      { path: '/endpoints', label: '엔드포인트', labelEn: 'Endpoints', icon: '◇' },
    ]
  },
  {
    title: '거버넌스',
    titleEn: 'Governance',
    items: [
      { path: '/policy', label: '정책', labelEn: 'Policy', icon: '⚖' },
      { path: '/security', label: '보안', labelEn: 'Security', icon: '🛡' },
      { path: '/compliance', label: '컴플라이언스', labelEn: 'Compliance', icon: '📋' },
      { path: '/tools', label: '도구', labelEn: 'Tools', icon: '🔧' },
    ]
  },
  {
    title: '운영',
    titleEn: 'Operations',
    items: [
      { path: '/analytics', label: '분석', labelEn: 'Analytics', icon: '📊' },
      { path: '/communications', label: '커뮤니케이션', labelEn: 'Comms', icon: '💬' },
      { path: '/sandboxes', label: '샌드박스', labelEn: 'Sandboxes', icon: '📦' },
    ]
  },
  {
    title: '퍼블릭 클라우드',
    titleEn: 'Public Cloud',
    items: [
      { path: '/portal', label: '계정 포털', labelEn: 'Portal', icon: '👤' },
      { path: '/sre', label: 'SRE 운영', labelEn: 'SRE', icon: '📡' },
    ]
  },
  {
    title: '프로바이던스',
    titleEn: 'Provenance',
    items: [
      { path: '/explorer', label: '코드 탐색기', labelEn: 'Explorer', icon: '🔬' },
      { path: '/provenance', label: '프로바이던스', labelEn: 'Provenance', icon: '🔗' },
    ]
  },
  {
    title: '감사',
    titleEn: 'Audit',
    items: [
      { path: '/audit', label: '감사 로그', labelEn: 'Audit Log', icon: '☰' },
    ]
  },
]

function GlobalSearch() {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<any[]>([])
  const [showResults, setShowResults] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  // ⌘K / Ctrl+K focuses the global search (Plan A8 — command palette)
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        inputRef.current?.focus()
        inputRef.current?.select()
      }
      if (e.key === 'Escape') inputRef.current?.blur()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  const search = async (q: string) => {
    setQuery(q)
    if (q.length < 2) { setResults([]); return }
    const headers = { Authorization: `Bearer ${localStorage.getItem('pccp_token') || ''}` }
    try {
      const [users, harnesses, sessions, models, repos] = await Promise.all([
        fetch('/api/users', { headers }).then(r => r.json()).catch(() => []),
        fetch('/api/harnesses', { headers }).then(r => r.json()).catch(() => []),
        fetch('/api/sessions', { headers }).then(r => r.json()).catch(() => []),
        fetch('/api/models', { headers }).then(r => r.json()).catch(() => []),
        fetch('/api/repositories', { headers }).then(r => r.json()).catch(() => []),
      ])
      const ql = q.toLowerCase()
      const matches: any[] = []
      ;(Array.isArray(users) ? users : []).filter((u: any) =>
        (u.name_ko || '').toLowerCase().includes(ql) || (u.email || '').toLowerCase().includes(ql)
      ).slice(0, 3).forEach((u: any) => matches.push({ type: '사용자', label: u.name_ko || u.name, sub: u.email, path: `/users/${u.id}` }))
      ;(Array.isArray(harnesses) ? harnesses : []).filter((h: any) =>
        (h.harness_id || '').toLowerCase().includes(ql)
      ).slice(0, 3).forEach((h: any) => matches.push({ type: '하네스', label: h.harness_id?.slice(0, 25), sub: h.status, path: `/harnesses/${h.id}` }))
      ;(Array.isArray(sessions) ? sessions : []).filter((s: any) =>
        (s.title || s.session_id || '').toLowerCase().includes(ql)
      ).slice(0, 3).forEach((s: any) => matches.push({ type: '세션', label: s.title || '제목 없음', sub: s.session_id?.slice(0, 20), path: '/sessions' }))
      ;(Array.isArray(models) ? models : []).filter((m: any) =>
        (m.display_name || m.model_id || '').toLowerCase().includes(ql)
      ).slice(0, 3).forEach((m: any) => matches.push({ type: '모델', label: m.display_name || m.model_id, sub: m.engine_type, path: '/models' }))
      ;(Array.isArray(repos) ? repos : []).filter((r: any) =>
        (r.name || '').toLowerCase().includes(ql)
      ).slice(0, 3).forEach((r: any) => matches.push({ type: '저장소', label: r.name, sub: r.scm_provider, path: `/repositories/${r.id}` }))
      setResults(matches)
    } catch {}
  }

  return (
    <div className="relative max-w-2xl mx-auto">
      <input
        ref={inputRef}
        className="w-full px-3 py-1.5 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:outline-none focus:border-blue-400 focus:bg-white transition-colors"
        placeholder="🔍 전역 검색 (⌘K) · Search users, harnesses, sessions..."
        value={query}
        onChange={e => search(e.target.value)}
        onFocus={() => setShowResults(true)}
        onBlur={() => setTimeout(() => setShowResults(false), 200)}
      />
      {showResults && results.length > 0 && (
        <div className="absolute top-full left-0 right-0 mt-1 bg-white border border-gray-200 rounded-lg shadow-lg z-30 max-h-80 overflow-y-auto">
          {results.map((r, i) => (
            <Link key={i} to={r.path} className="flex items-center gap-3 px-3 py-2 hover:bg-blue-50 border-b border-gray-50 last:border-0">
              <span className="text-[10px] font-semibold text-gray-400 w-12">{r.type}</span>
              <div className="flex-1">
                <div className="text-sm font-medium">{r.label}</div>
                {r.sub && <div className="text-xs text-gray-400">{r.sub}</div>}
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}

export default function Layout({ children }: { children: ReactNode }) {
  const { logout } = useAuth()
  const location = useLocation()

  return (
    <div className="flex h-screen bg-gray-50">
      {/* Enterprise Sidebar */}
      <aside className="w-56 bg-gray-900 text-gray-300 flex flex-col overflow-hidden border-r border-gray-800">
        {/* Logo */}
        <div className="px-4 py-4 border-b border-gray-800 flex-shrink-0">
          <div className="flex items-center gap-2">
            <div className="w-7 h-7 rounded bg-blue-600 flex items-center justify-center text-white font-bold text-sm">P</div>
            <div>
              <div className="text-sm font-semibold text-white leading-tight">Patty Code</div>
              <div className="text-[10px] text-gray-500 leading-tight">Control Plane</div>
            </div>
          </div>
        </div>

        {/* Nav sections */}
        <nav className="flex-1 overflow-y-auto py-2 sidebar-scroll">
          {navSections.map((section) => (
            <div key={section.titleEn} className="mb-1">
              <div className="px-4 py-1.5 text-[10px] font-semibold text-gray-500 uppercase tracking-wider">
                {section.title}
              </div>
              {section.items.map((item) => {
                const isActive = location.pathname === item.path ||
                  (item.path !== '/' && location.pathname.startsWith(item.path))
                return (
                  <Link
                    key={item.path}
                    to={item.path}
                    className={`flex items-center gap-2.5 px-4 py-1.5 text-xs transition-colors ${
                      isActive
                        ? 'bg-blue-600/20 text-blue-300 border-l-2 border-blue-400'
                        : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200 border-l-2 border-transparent'
                    }`}
                  >
                    <span className="text-sm leading-none">{item.icon}</span>
                    <span>{item.label}</span>
                  </Link>
                )
              })}
            </div>
          ))}
        </nav>

        {/* Footer */}
        <div className="px-4 py-3 border-t border-gray-800 flex-shrink-0">
          <button onClick={logout} className="text-xs text-gray-500 hover:text-gray-300">
            로그아웃 · Logout
          </button>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-y-auto main-scroll">
        {/* Global Search Bar */}
        <div className="bg-white border-b border-gray-200 px-6 py-2 sticky top-0 z-20">
          <GlobalSearch />
        </div>
        <div className="p-6 max-w-[1600px] mx-auto">
          {children}
        </div>
      </main>
    </div>
  )
}
