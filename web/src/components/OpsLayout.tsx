import { ReactNode, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

type NavItem = { path: string; label: string; labelEn: string; icon: string }

const navSections = [
  {
    title: '서비스 현황',
    titleEn: 'Service Overview',
    items: [
      { path: '/', label: '대시보드', labelEn: 'Command Center', icon: '◈' },
      { path: '/sre', label: 'SRE 운영', labelEn: 'SRE Console', icon: '📡' },
      { path: '/fleet', label: '플릿 관리', labelEn: 'Fleet', icon: '⛶' },
      { path: '/live', label: '실시간 뷰', labelEn: 'Live Wall', icon: '🔴' },
    ]
  },
  {
    title: '계정',
    titleEn: 'Accounts',
    items: [
      { path: '/portal', label: '계정 포털', labelEn: 'Account Portal', icon: '👤' },
    ]
  },
  {
    title: '모델',
    titleEn: 'Models',
    items: [
      { path: '/catalog', label: '카탈로그', labelEn: 'Catalog', icon: '🗂' },
      { path: '/models', label: '모델 패키지', labelEn: 'Packages', icon: '◆' },
      { path: '/endpoints', label: '엔드포인트', labelEn: 'Endpoints', icon: '◇' },
    ]
  },
  {
    title: '리스크',
    titleEn: 'Risk',
    items: [
      { path: '/security', label: '보안', labelEn: 'Security', icon: '🛡' },
    ]
  },
  {
    title: '시스템',
    titleEn: 'System',
    items: [
      { path: '/audit', label: '감사 로그', labelEn: 'Audit Log', icon: '☰' },
    ]
  },
]

export default function OpsLayout({ children }: { children: ReactNode }) {
  const { logout, email, setProfile } = useAuth()
  const location = useLocation()

  return (
    <div className="flex h-screen bg-gray-50">
      <aside className="w-56 bg-gray-900 text-gray-300 flex flex-col overflow-hidden border-r border-gray-800">
        <div className="px-4 py-4 border-b border-gray-800 flex-shrink-0">
          <div className="flex items-center gap-2">
            <div className="w-7 h-7 rounded bg-red-600 flex items-center justify-center text-white font-bold text-sm">P</div>
            <div>
              <div className="text-sm font-semibold text-white leading-tight">Patty Code</div>
              <div className="text-[10px] text-red-400 leading-tight">Operations Console</div>
            </div>
          </div>
        </div>

        <nav className="flex-1 overflow-y-auto py-2 sidebar-scroll">
          {navSections.map((section) => (
            <div key={section.titleEn} className="mb-1">
              <div className="px-4 py-1.5 text-[10px] font-semibold text-gray-500 uppercase tracking-wider">
                {section.title}
              </div>
              {section.items.map((item: NavItem) => {
                const isActive = location.pathname === item.path ||
                  (item.path !== '/' && location.pathname.startsWith(item.path))
                return (
                  <Link key={item.path} to={item.path}
                    className={`flex items-center gap-2.5 px-4 py-1.5 text-xs transition-colors ${
                      isActive ? 'bg-red-600/20 text-red-300 border-l-2 border-red-400'
                        : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200 border-l-2 border-transparent'
                    }`}>
                    <span className="text-sm leading-none">{item.icon}</span>
                    <span>{item.label}</span>
                  </Link>
                )
              })}
            </div>
          ))}
        </nav>

        <div className="px-4 py-3 border-t border-gray-800 flex-shrink-0 space-y-2">
          <select
            className="w-full bg-gray-800 text-gray-300 text-xs rounded px-2 py-1 border border-gray-700"
            value="patty_ops"
            onChange={(e) => setProfile(e.target.value as any)}
          >
            <option value="patty_ops">📡 Patty Ops</option>
            <option value="customer">🏢 Customer Console</option>
            <option value="portal">👤 Account Portal</option>
          </select>
          <div className="text-[10px] text-gray-600">{email}</div>
          <button onClick={logout} className="text-xs text-gray-500 hover:text-gray-300">로그아웃</button>
        </div>
      </aside>

      <main className="flex-1 overflow-y-auto main-scroll">
        <div className="p-6 max-w-[1600px] mx-auto">{children}</div>
      </main>
    </div>
  )
}
