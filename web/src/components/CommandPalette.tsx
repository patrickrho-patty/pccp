import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

// CommandPalette (00-cross-cutting A8/A11/A13) — ⌘K / Ctrl+K opens a
// modal command palette: jump to pages, run page actions, and search
// entities through the unified /api/search service. Arrow keys + Enter
// navigate, Esc closes, "/" focuses the inline search field.

type PaletteItem = {
  id: string
  group: string
  label: string
  sub?: string
  path?: string
  action?: () => void
  icon?: string
}

const PAGE_ITEMS: PaletteItem[] = [
  { id: 'dash', group: '페이지 · Pages', label: '대시보드', icon: '◈', path: '/' },
  { id: 'users', group: '페이지 · Pages', label: '사용자 · Users', icon: '◉', path: '/users' },
  { id: 'harnesses', group: '페이지 · Pages', label: '하네스 · Harnesses', icon: '⬡', path: '/harnesses' },
  { id: 'projects', group: '페이지 · Pages', label: '프로젝트 · Projects', icon: '▣', path: '/projects' },
  { id: 'repos', group: '페이지 · Pages', label: '저장소 · Repositories', icon: '▤', path: '/repositories' },
  { id: 'sessions', group: '페이지 · Pages', label: '세션 · Sessions', icon: '◐', path: '/sessions' },
  { id: 'live', group: '페이지 · Pages', label: '실시간 뷰 · Live', icon: '🔴', path: '/live' },
  { id: 'policy', group: '페이지 · Pages', label: '정책 · Policy', icon: '⚖', path: '/policy' },
  { id: 'security', group: '페이지 · Pages', label: '보안 · Security', icon: '🛡', path: '/security' },
  { id: 'compliance', group: '페이지 · Pages', label: '컴플라이언스 · Compliance', icon: '📋', path: '/compliance' },
  { id: 'analytics', group: '페이지 · Pages', label: '분석 · Analytics', icon: '📊', path: '/analytics' },
  { id: 'audit', group: '페이지 · Pages', label: '감사 · Audit', icon: '☰', path: '/audit' },
]

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [query, setQuery] = useState('')
  const [entityResults, setEntityResults] = useState<PaletteItem[]>([])
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()

  // Rebuild on open; focus the input.
  useEffect(() => {
    if (open) {
      setQuery('')
      setEntityResults([])
      setActive(0)
      setTimeout(() => inputRef.current?.focus(), 10)
    }
  }, [open])

  // Unified entity search (A11) — backend /api/search.
  useEffect(() => {
    if (!open) return
    if (query.trim().length < 2) { setEntityResults([]); return }
    let cancelled = false
    fetch(`/api/search?q=${encodeURIComponent(query)}`, {
      headers: { Authorization: `Bearer ${localStorage.getItem('pccp_token') || ''}` },
    })
      .then(r => (r.ok ? r.json() : []))
      .then((results: any[]) => {
        if (cancelled) return
        const items: PaletteItem[] = (Array.isArray(results) ? results : []).map((r: any, i: number) => ({
          id: `entity-${i}`,
          group: '엔티티 검색 · Entities',
          label: r.label,
          sub: r.sub,
          path: r.path,
          icon: r.type_icon || '🔎',
          action: r.action ? () => { if (r.action.type === 'chat') { window.location.href = r.action.href } } : undefined,
        }))
        setEntityResults(items)
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [query, open])

  const filteredPages = PAGE_ITEMS.filter(p =>
    !query.trim() || p.label.toLowerCase().includes(query.toLowerCase())
  )
  const items = [...filteredPages, ...entityResults]
  const clampedActive = Math.min(active, Math.max(items.length - 1, 0))

  const activate = (item: PaletteItem) => {
    onClose()
    if (item.action) { item.action(); return }
    if (item.path) navigate(item.path)
  }

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (!open) return
      if (e.key === 'ArrowDown') { e.preventDefault(); setActive(a => Math.min(a + 1, items.length - 1)) }
      else if (e.key === 'ArrowUp') { e.preventDefault(); setActive(a => Math.max(a - 1, 0)) }
      else if (e.key === 'Enter') {
        e.preventDefault()
        const item = items[clampedActive]
        if (item) activate(item)
      } else if (e.key === 'Escape') { e.preventDefault(); onClose() }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, items, clampedActive])

  useEffect(() => {
    listRef.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [clampedActive])

  if (!open) return null
  return (
    <div className="fixed inset-0 bg-black/40 z-50 flex items-start justify-center pt-[12vh] p-4 animate-fadeIn" onClick={onClose}>
      <div className="bg-white rounded-xl shadow-2xl w-full max-w-xl border border-gray-200 animate-scaleIn" onClick={e => e.stopPropagation()}>
        <div className="flex items-center gap-2 px-4 py-3 border-b border-gray-100">
          <span className="text-gray-400">🔍</span>
          <input
            ref={inputRef}
            className="flex-1 outline-none text-sm"
            placeholder="페이지 이동 또는 엔티티 검색... · Type to search"
            value={query}
            onChange={e => { setQuery(e.target.value); setActive(0) }}
          />
          <kbd className="text-[10px] text-gray-400 border border-gray-200 rounded px-1.5 py-0.5">ESC</kbd>
        </div>
        <div ref={listRef} className="max-h-80 overflow-y-auto py-2">
          {items.length === 0 && (
            <p className="text-center text-xs text-gray-400 py-6">결과 없음 · No matches — 계속 입력하세요</p>
          )}
          {items.map((item, i) => (
            <button
              key={item.id}
              data-active={i === clampedActive}
              onClick={() => activate(item)}
              className={`w-full flex items-center gap-3 px-4 py-2 text-left ${i === clampedActive ? 'bg-blue-50' : 'hover:bg-gray-50'}`}
            >
              <span className="text-sm w-5 text-center">{item.icon || '·'}</span>
              <div className="flex-1 min-w-0">
                <div className="text-sm font-medium truncate">{item.label}</div>
                {item.sub && <div className="text-xs text-gray-400 truncate">{item.sub}</div>}
              </div>
              <span className="text-[10px] text-gray-300 flex-shrink-0">{item.group}</span>
            </button>
          ))}
        </div>
        <div className="px-4 py-2 border-t border-gray-100 flex gap-3 text-[10px] text-gray-400">
          <span><kbd className="border border-gray-200 rounded px-1">↑↓</kbd> 이동</span>
          <span><kbd className="border border-gray-200 rounded px-1">Enter</kbd> 열기</span>
          <span><kbd className="border border-gray-200 rounded px-1">⌘K</kbd> 토글</span>
        </div>
      </div>
    </div>
  )
}
