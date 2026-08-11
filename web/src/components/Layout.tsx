import { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

const navItems = [
  { path: '/', label: '대시보드', labelEn: 'Dashboard', icon: '◈' },
  { path: '/users', label: '사용자', labelEn: 'Users', icon: '◉' },
  { path: '/harnesses', label: '하네스', labelEn: 'Harnesses', icon: '⬡' },
  { path: '/projects', label: '프로젝트', labelEn: 'Projects', icon: '▣' },
  { path: '/repositories', label: '저장소', labelEn: 'Repositories', icon: '▤' },
  { path: '/sessions', label: '세션', labelEn: 'Sessions', icon: '◐' },
  { path: '/models', label: '모델', labelEn: 'Models', icon: '◆' },
  { path: '/endpoints', label: '엔드포인트', labelEn: 'Endpoints', icon: '◇' },
  { path: '/analytics', label: '분석', labelEn: 'Analytics', icon: '📊' },
  { path: '/communications', label: '커뮤니케이션', labelEn: 'Comms', icon: '💬' },
  { path: '/policy', label: '정책', labelEn: 'Policy', icon: '⚖' },
  { path: '/audit', label: '감사 로그', labelEn: 'Audit', icon: '☰' },
]

export default function Layout({ children }: { children: ReactNode }) {
  const { logout } = useAuth()
  const location = useLocation()

  return (
    <div className="flex h-screen bg-gray-50">
      {/* Sidebar */}
      <aside className="w-64 bg-gray-900 text-gray-300 flex flex-col">
        <div className="p-6 border-b border-gray-800">
          <h1 className="text-xl font-bold text-white">Patty Code</h1>
          <p className="text-sm text-gray-500">Control Plane</p>
        </div>
        <nav className="flex-1 overflow-y-auto py-4">
          {navItems.map((item) => (
            <Link
              key={item.path}
              to={item.path}
              className={`flex items-center gap-3 px-6 py-2.5 text-sm transition-colors ${
                location.pathname === item.path ||
                (item.path !== '/' && location.pathname.startsWith(item.path))
                  ? 'bg-patty-600 text-white border-r-2 border-patty-400'
                  : 'hover:bg-gray-800 hover:text-white'
              }`}
            >
              <span className="text-base">{item.icon}</span>
              <div>
                <div>{item.label}</div>
                <div className="text-xs text-gray-500">{item.labelEn}</div>
              </div>
            </Link>
          ))}
        </nav>
        <div className="p-4 border-t border-gray-800">
          <button
            onClick={logout}
            className="w-full btn-secondary text-sm"
          >
            로그아웃
          </button>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-y-auto">
        <div className="p-8">
          {children}
        </div>
      </main>
    </div>
  )
}
