import { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

const navItems = [
  { path: '/', label: '내 계정', labelEn: 'My Account', icon: '👤' },
  { path: '/portal', label: '구독 관리', labelEn: 'Subscription', icon: '💳' },
  { path: '/harnesses', label: '내 기기', labelEn: 'My Devices', icon: '💻' },
]

export default function PortalLayout({ children }: { children: ReactNode }) {
  const { logout, email, setProfile } = useAuth()
  const location = useLocation()

  return (
    <div className="flex h-screen bg-gray-50">
      <aside className="w-56 bg-gray-900 text-gray-300 flex flex-col overflow-hidden border-r border-gray-800">
        <div className="px-4 py-4 border-b border-gray-800 flex-shrink-0">
          <div className="flex items-center gap-2">
            <div className="w-7 h-7 rounded bg-green-600 flex items-center justify-center text-white font-bold text-sm">P</div>
            <div>
              <div className="text-sm font-semibold text-white leading-tight">Patty Code</div>
              <div className="text-[10px] text-green-400 leading-tight">Account Portal</div>
            </div>
          </div>
        </div>

        <nav className="flex-1 overflow-y-auto py-2">
          {navItems.map((item) => {
            const isActive = location.pathname === item.path
            return (
              <Link key={item.path} to={item.path}
                className={`flex items-center gap-2.5 px-4 py-2 text-sm transition-colors ${
                  isActive ? 'bg-green-600/20 text-green-300 border-l-2 border-green-400'
                    : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200 border-l-2 border-transparent'
                }`}>
                <span className="text-base leading-none">{item.icon}</span>
                <span>{item.label}</span>
              </Link>
            )
          })}
        </nav>

        <div className="px-4 py-3 border-t border-gray-800 flex-shrink-0 space-y-2">
          <div className="text-xs text-gray-400">{email}</div>
          <button onClick={logout} className="text-xs text-gray-500 hover:text-gray-300">로그아웃</button>
        </div>
      </aside>

      <main className="flex-1 overflow-y-auto main-scroll">
        <div className="p-6 max-w-3xl mx-auto">{children}</div>
      </main>
    </div>
  )
}
