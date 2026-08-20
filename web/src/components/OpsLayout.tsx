import { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import GlobalSearch from './GlobalSearch'

type NavItem = { path: string; label: string; labelEn: string; icon: string }

const navSections = [
  {
    title: '서비스 현황',
    titleEn: 'Service Overview',
    items: [
      { path: '/', label: '서비스 대시보드', labelEn: 'Service Dashboard', icon: '◈' },
      { path: '/sre', label: 'SRE 운영', labelEn: 'SRE Console', icon: '📡' },
      { path: '/analytics', label: '사용량 분석', labelEn: 'Usage Analytics', icon: '📊' },
    ]
  },
  {
    title: '계정 및 구독',
    titleEn: 'Accounts & Subscriptions',
    items: [
      { path: '/accounts', label: '구독자 관리', labelEn: 'Subscribers', icon: '👥' },
      { path: '/fleet', label: '하네스 플릿', labelEn: 'Harness Fleet', icon: '⛶' },
      { path: '/sessions', label: '세션 현황', labelEn: 'Sessions', icon: '◐' },
    ]
  },
  {
    title: '모델 인프라',
    titleEn: 'Model Infrastructure',
    items: [
      { path: '/models', label: '모델 인프라', labelEn: 'Model Infra', icon: '◆' },
    ]
  },
  {
    title: '리스크 및 보안',
    titleEn: 'Risk & Security',
    items: [
      { path: '/security', label: '플랫폼 보안', labelEn: 'Platform Security', icon: '🛡' },
      { path: '/sre/qos', label: 'GPU 대기열', labelEn: 'GPU QoS', icon: '📶' },
    ]
  },
  {
    title: '엔터프라이즈 이동 관리',
    titleEn: 'Enterprise Migration',
    items: [
      { path: '/sso', label: 'SSO 마이그레이션', labelEn: 'SSO Migration', icon: '🔐' },
      { path: '/reference', label: '레퍼런스', labelEn: 'Reference', icon: '📚' },
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
            aria-label="콘솔 전환 — 운영 콘솔 / 기업·정부 콘솔 / 계정 포털"
            className="w-full bg-gray-800 text-gray-300 text-xs rounded px-2 py-1 border border-gray-700"
            value="patty_ops"
            onChange={(e) => setProfile(e.target.value as any)}
          >
            <option value="patty_ops">📡 Patty Ops Console</option>
            <option value="customer">🏢 Enterprise/Govt Console</option>
            <option value="portal">👤 Account Portal</option>
          </select>
          <div className="text-[10px] text-gray-600">{email}</div>
          <button onClick={logout} className="text-xs text-gray-500 hover:text-gray-300">로그아웃 · Logout</button>
        </div>
      </aside>

      <main className="main-content flex-1 overflow-y-auto main-scroll">
        <div className="p-6 max-w-[1600px] mx-auto">
          <div className="mb-4"><GlobalSearch /></div>
          {children}
        </div>
      </main>
    </div>
  )
}
