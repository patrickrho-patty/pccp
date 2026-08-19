import { useState, useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'

export default function GlobalSearch() {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<any[]>([])
  const [showResults, setShowResults] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

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
    const headers = { Authorization: `Bearer ${sessionStorage.getItem('pccp_token') || ''}` }
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
    <div className="relative w-full max-w-2xl mx-auto">
      <input
        ref={inputRef}
        aria-label="전역 검색 — 사용자, 하네스, 세션, 프로젝트 검색"
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
