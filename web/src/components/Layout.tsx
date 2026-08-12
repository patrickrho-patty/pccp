import { ReactNode } from 'react'
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
      { path: '/sessions', label: '세션', labelEn: 'Sessions', icon: '◐' },
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
      { path: '/fleet', label: '플릿', labelEn: 'Fleet', icon: '⛶' },
      { path: '/communications', label: '커뮤니케이션', labelEn: 'Comms', icon: '💬' },
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
    title: '감사',
    titleEn: 'Audit',
    items: [
      { path: '/audit', label: '감사 로그', labelEn: 'Audit Log', icon: '☰' },
    ]
  },
]

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
        <div className="p-6 max-w-[1600px] mx-auto">
          {children}
        </div>
      </main>
    </div>
  )
}
