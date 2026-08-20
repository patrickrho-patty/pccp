import { ReactNode, useEffect, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { api } from '../api'
import { CommandPalette } from './CommandPalette'
import { navCountsFromDashboard, navQueueFor, navSeverityTint } from '../navQueues'

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
      { path: '/skills', label: '스킬', labelEn: 'Skills', icon: '🧩' },
      { path: '/prompts', label: '시스템 프롬프트', labelEn: 'System Prompts', icon: '📝' },
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
      { path: '/leaderboard', label: '리더보드', labelEn: 'Leaderboard', icon: '🏆' },
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

// Collapsible sub-menus (00 A2) — sections expand/collapse with a
// buttery height transition; the collapsed set persists per operator.
const COLLAPSE_KEY = 'pccp_nav_collapsed'

function useCollapsedSections() {
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  useEffect(() => {
    try {
      const raw = localStorage.getItem(COLLAPSE_KEY)
      setCollapsed(new Set(raw ? JSON.parse(raw) : []))
    } catch { setCollapsed(new Set()) }
  }, [])
  const toggle = (key: string) => {
    setCollapsed(prev => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      localStorage.setItem(COLLAPSE_KEY, JSON.stringify([...next]))
      return next
    })
  }
  return { collapsed, toggle }
}

// Theme + density toggles (00 A9) — persisted; density compacts the
// card/table rhythm for dense-data operators.
function useThemePrefs() {
  const [theme, setTheme] = useState<'light' | 'dark'>(() =>
    (localStorage.getItem('pccp_theme') as 'light' | 'dark') || 'light')
  const [density, setDensity] = useState<'comfortable' | 'compact'>(() =>
    (localStorage.getItem('pccp_density') as 'comfortable' | 'compact') || 'comfortable')

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    localStorage.setItem('pccp_theme', theme)
  }, [theme])
  useEffect(() => {
    document.documentElement.setAttribute('data-density', density)
    localStorage.setItem('pccp_density', density)
  }, [density])

  return { theme, setTheme, density, setDensity }
}

export default function Layout({ children }: { children: ReactNode }) {
  const { logout } = useAuth()
  const location = useLocation()
  const { collapsed, toggle } = useCollapsedSections()
  const { theme, setTheme, density, setDensity } = useThemePrefs()
  const [paletteOpen, setPaletteOpen] = useState(false)
  const searchInputRef = useRef<HTMLInputElement>(null)

  // PAT-1518: actionable nav counts — only for queues with an exact-scoped
  // destination, resolved through the canonical dashboard metric contract
  // (PAT-1487/1488) so the badge and its destination list always reconcile.
  const [navCounts, setNavCounts] = useState<Record<string, number>>({})
  useEffect(() => {
    let active = true
    api.dashboard().then((d: any) => {
      if (!active || !d) return
      setNavCounts(navCountsFromDashboard(d))
    }).catch(() => {})
    return () => { active = false }
  }, [])

  // ⌘K / Ctrl+K toggles the command palette; "/" focuses search when
  // not typing in an input (00 A8/A13).
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPaletteOpen(o => !o)
        return
      }
      if (e.key === '/' && !(e.metaKey || e.ctrlKey)) {
        const tag = (e.target as HTMLElement)?.tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
        e.preventDefault()
        setPaletteOpen(true)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  const sectionHasActive = (section: NavSection) =>
    section.items.some(item =>
      location.pathname === item.path ||
      (item.path !== '/' && location.pathname.startsWith(item.path))
    )

  return (
    <div className="flex h-screen bg-gray-50">
      {/* Enterprise Sidebar */}
      <aside className="enterprise-sidebar w-56 bg-gray-900 text-gray-300 flex flex-col overflow-hidden border-r border-gray-800">
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

        {/* Nav sections — collapsible sub-menus */}
        <nav className="flex-1 overflow-y-auto py-2 sidebar-scroll">
          {navSections.map((section) => {
            const isCollapsed = collapsed.has(section.titleEn)
            return (
              <div key={section.titleEn} className="mb-0.5">
                <button
                  onClick={() => toggle(section.titleEn)}
                  className={`w-full flex items-center justify-between px-4 py-1.5 text-[10px] font-semibold uppercase tracking-wider transition-colors ${
                    sectionHasActive(section) ? 'text-blue-300' : 'text-gray-500 hover:text-gray-300'
                  }`}
                >
                  <span>{section.title}</span>
                  <span className={`text-[8px] transition-transform duration-200 ${isCollapsed ? '' : 'rotate-90'}`}>▶</span>
                </button>
                <div className={`nav-submenu overflow-hidden ${isCollapsed ? 'max-h-0' : 'max-h-96'}`}>
                  {section.items.map((item) => {
                    const isActive = location.pathname === item.path ||
                      (item.path !== '/' && location.pathname.startsWith(item.path))
                    const q = navQueueFor(item.path)
                    const count = q ? (navCounts[item.path] || 0) : 0
                    return (
                      <Link
                        key={item.path}
                        to={count > 0 && q ? q.href : item.path}
                        className={`flex items-center gap-2.5 px-4 py-1.5 text-xs transition-colors ${
                          isActive
                            ? 'bg-blue-600/20 text-blue-300 border-l-2 border-blue-400'
                            : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200 border-l-2 border-transparent'
                        }`}
                      >
                        <span className="text-sm leading-none">{item.icon}</span>
                        <span className="flex-1">{item.label}</span>
                        {count > 0 && q && (
                          <span className={`${navSeverityTint(q.severity)} text-[9px] px-1.5 py-0.5 rounded-full text-white`}
                            aria-label={`${item.label} ${count}건`}>
                            {count}
                          </span>
                        )}
                      </Link>
                    )
                  })}
                </div>
              </div>
            )
          })}
        </nav>

        {/* Footer */}
        <div className="px-4 py-3 border-t border-gray-800 flex-shrink-0">
          <button onClick={logout} className="text-xs text-gray-500 hover:text-gray-300">
            로그아웃 · Logout
          </button>
        </div>
      </aside>

      {/* Main content */}
      <main className="main-content flex-1 overflow-y-auto main-scroll">
        {/* Top bar: search + theme/density toggles */}
        <div className="bg-white border-b border-gray-200 px-6 py-2 sticky top-0 z-20 flex items-center gap-3">
          <input
            ref={searchInputRef}
            readOnly
            onFocus={e => { e.currentTarget.blur(); setPaletteOpen(true) }}
            onClick={() => setPaletteOpen(true)}
            className="w-full max-w-2xl px-3 py-1.5 text-sm bg-gray-50 border border-gray-200 rounded-lg cursor-pointer hover:border-blue-300 transition-colors"
            placeholder="🔍 검색 또는 페이지 이동 · Press ⌘K or /"
          />
          <div className="flex items-center gap-1 ml-auto">
            <button
              onClick={() => setDensity(density === 'compact' ? 'comfortable' : 'compact')}
              title="밀도 전환 · Density"
              className="text-xs px-2 py-1 rounded border border-gray-200 text-gray-500 hover:bg-gray-50 transition-colors"
            >
              {density === 'compact' ? '≡ 조밀' : '☰ 넓게'}
            </button>
            <button
              onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')}
              title="테마 전환 · Theme"
              className="text-xs px-2 py-1 rounded border border-gray-200 text-gray-500 hover:bg-gray-50 transition-colors"
            >
              {theme === 'light' ? '🌙 다크' : '☀️ 라이트'}
            </button>
          </div>
        </div>
        {/* Page-enter motion (00 A3) — re-animates on route change */}
        <div key={location.pathname} className="p-6 max-w-[1600px] mx-auto page-enter">
          {children}
        </div>
      </main>

      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
    </div>
  )
}
