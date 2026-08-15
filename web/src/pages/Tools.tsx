import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { FilterBar, useFilteredData, FilterConfig } from '../components/FilterBar'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['name', 'name_ko', 'tool_class'],
  searchPlaceholder: '도구명으로 검색...',
  dropdowns: [
    { key: 'category', label: '분류', options: [
      { value: 'read', label: 'Read' }, { value: 'write', label: 'Write' },
      { value: 'execute', label: 'Execute' }, { value: 'network', label: 'Network' },
    ]},
    { key: 'danger_level', label: '위험도', options: [
      { value: 'low', label: '낮음' }, { value: 'medium', label: '중간' },
      { value: 'high', label: '높음' }, { value: 'critical', label: '치명적' },
    ]},
  ],
}

export default function Tools() {
  const confirm = useConfirm()
  const [tools, setTools] = useState<any[]>([])
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState({
    name: '', name_ko: '', category: 'read', tool_class: '', danger_level: 'low', requires_approval: false,
  })
  const [approvals, setApprovals] = useState<any[]>([])

  const authHeaders = (): Record<string, string> => {
    const token = localStorage.getItem('pccp_token')
    return token ? { Authorization: `Bearer ${token}` } : {}
  }

  // fetch wrapper that surfaces HTTP errors instead of swallowing them
  const jsonFetch = async (url: string, options?: RequestInit) => {
    const res = await fetch(url, options)
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }))
      throw new Error(err.error || res.statusText)
    }
    return res.json().catch(() => ({}))
  }

  const load = () => {
    api.listTools().then(data => setTools(Array.isArray(data) ? data : []))
    api.listToolApprovals().then(data => setApprovals(Array.isArray(data) ? data : [])).catch(e => console.error('failed to load tool approvals:', e))
  }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(tools, filters, FILTER_CONFIG)

  const seed = async () => { try { await api.seedTools(); load() } catch (e: any) { showToast('시딩 실패: ' + (e.message || e)) } }

  const dangerBadge = (d: string) => {
    const m: Record<string,string> = { low: 'badge-green', medium: 'badge-blue', high: 'badge-yellow', critical: 'badge-red' }
    return m[d] || 'badge-gray'
  }
  const dangerLabel = (d: string) => {
    const m: Record<string,string> = { low: '낮음', medium: '중간', high: '높음', critical: '치명적' }
    return m[d] || d
  }
  const categoryLabel = (c: string) => {
    const m: Record<string,string> = { read: '읽기', write: '쓰기', execute: '실행', network: '네트워크', search: '검색', git: 'Git', test: '테스트' }
    return m[c] || c
  }

  const createOrUpdate = async () => {
    if (!form.name || !form.tool_class) { showToast('도구명과 클래스는 필수입니다'); return }
    try {
      if (editingId) {
        await jsonFetch(`/api/tools/${editingId}`, {
          method: 'PUT',
          headers: { ...authHeaders(), 'Content-Type': 'application/json' },
          body: JSON.stringify(form),
        })
      } else {
        await jsonFetch('/api/tools', {
          method: 'POST',
          headers: { ...authHeaders(), 'Content-Type': 'application/json' },
          body: JSON.stringify(form),
        })
      }
      setShowForm(false)
      setEditingId(null)
      setForm({ name: '', name_ko: '', category: 'read', tool_class: '', danger_level: 'low', requires_approval: false })
      load()
    } catch (e: any) { showToast('오류: ' + (e.message || e)) }
  }

  const startEdit = (t: any) => {
    setEditingId(t.id)
    setForm({
      name: t.name || '', name_ko: t.name_ko || '',
      category: t.category || 'read', tool_class: t.tool_class || '',
      danger_level: t.danger_level || 'low', requires_approval: t.requires_approval || false,
    })
    setShowForm(true)
  }

  const handleDelete = async (t: any) => {
    if (!await confirm({ title: '확인', message: `"${t.name}" 도구를 삭제하시겠습니까?`, danger: true })) return
    try {
      await jsonFetch(`/api/tools/${t.id}`, { method: 'DELETE', headers: authHeaders() })
      load()
    } catch (e: any) { showToast('삭제 실패: ' + (e.message || e)) }
  }

  const toggleApproval = async (t: any) => {
    try {
      await jsonFetch(`/api/tools/${t.id}`, {
        method: 'PUT',
        headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...t, requires_approval: !t.requires_approval }),
      })
      load()
    } catch (e: any) { showToast('변경 실패: ' + (e.message || e)) }
  }

  const stats = {
    total: tools.length,
    requiringApproval: tools.filter(t => t.requires_approval).length,
    highRisk: tools.filter(t => t.danger_level === 'high' || t.danger_level === 'critical').length,
    byCategory: tools.reduce((acc, t) => { acc[t.category] = (acc[t.category] || 0) + 1; return acc }, {} as Record<string, number>),
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <div>
          <h1 className="text-2xl font-bold">도구 관리 <span className="text-gray-400 text-lg font-normal">Tool Registry</span></h1>
          <p className="text-xs text-gray-400 mt-1">하네스가 실행할 수 있는 도구와 권한을 관리합니다 · Govern what operations the harness may perform</p>
        </div>
        <div className="flex gap-2">
          <button onClick={seed} className="btn-secondary text-sm">기본 도구 등록</button>
          <button onClick={() => { setShowForm(!showForm); setEditingId(null); setForm({ name: '', name_ko: '', category: 'read', tool_class: '', danger_level: 'low', requires_approval: false }) }} className="btn-primary text-sm">+ 도구 등록</button>
        </div>
      </div>

      {/* Register/Edit Form */}
      {showForm && (
        <div className="card mb-6">
          <h3 className="text-sm font-semibold mb-4">{editingId ? '도구 수정' : '새 도구 등록'}</h3>
          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className="label">도구명 · Name (필수)</label>
              <input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="file.read" disabled={!!editingId} />
            </div>
            <div>
              <label className="label">한글명 · Korean Name</label>
              <input className="input" value={form.name_ko} onChange={e => setForm({ ...form, name_ko: e.target.value })} placeholder="파일 읽기" />
            </div>
            <div>
              <label className="label">도구 클래스 · Tool Class (필수)</label>
              <input className="input" value={form.tool_class} onChange={e => setForm({ ...form, tool_class: e.target.value })} placeholder="read" />
            </div>
            <div>
              <label className="label">분류 · Category</label>
              <select className="input" value={form.category} onChange={e => setForm({ ...form, category: e.target.value })}>
                <option value="read">읽기 · Read</option>
                <option value="write">쓰기 · Write</option>
                <option value="execute">실행 · Execute</option>
                <option value="network">네트워크 · Network</option>
                <option value="git">Git</option>
                <option value="test">테스트 · Test</option>
                <option value="search">검색 · Search</option>
              </select>
            </div>
            <div>
              <label className="label">위험도 · Danger Level</label>
              <select className="input" value={form.danger_level} onChange={e => setForm({ ...form, danger_level: e.target.value })}>
                <option value="low">낮음 · Low</option>
                <option value="medium">중간 · Medium</option>
                <option value="high">높음 · High</option>
                <option value="critical">치명적 · Critical</option>
              </select>
            </div>
            <div className="flex items-end">
              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="checkbox" checked={form.requires_approval} onChange={e => setForm({ ...form, requires_approval: e.target.checked })} className="w-4 h-4" />
                승인 필요 · Requires Approval
              </label>
            </div>
          </div>
          <div className="flex gap-2 mt-4">
            <button onClick={createOrUpdate} className="btn-primary text-sm">{editingId ? '수정' : '등록'}</button>
            <button onClick={() => { setShowForm(false); setEditingId(null) }} className="btn-secondary text-sm">취소</button>
          </div>
        </div>
      )}

      {/* Stats */}
      <div className="grid grid-cols-4 gap-3 mb-4">
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold">{stats.total}</div>
          <div className="text-xs text-gray-500">전체 도구</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-yellow-600">{stats.requiringApproval}</div>
          <div className="text-xs text-gray-500">승인 필요</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-red-600">{stats.highRisk}</div>
          <div className="text-xs text-gray-500">고위험 도구</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-blue-600">{Object.keys(stats.byCategory).length}</div>
          <div className="text-xs text-gray-500">분류 수</div>
        </div>
      </div>

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="card">
        {filtered.length === 0 ? (
          <p className="text-gray-400 text-center py-8">등록된 도구가 없습니다</p>
        ) : (
          <table className="w-full overflow-x-auto block">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                <th className="pb-3">도구명</th>
                <th className="pb-3">한글명</th>
                <th className="pb-3">분류</th>
                <th className="pb-3">위험도</th>
                <th className="pb-3">승인</th>
                <th className="pb-3">작업</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map(t => (
                <tr key={t.id} className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30">
                  <td className="py-3 font-mono text-sm">{t.name}</td>
                  <td className="py-3 text-sm">{t.name_ko || '-'}</td>
                  <td className="py-3"><span className="badge-gray">{categoryLabel(t.category)}</span></td>
                  <td className="py-3"><span className={dangerBadge(t.danger_level)}>{dangerLabel(t.danger_level)}</span></td>
                  <td className="py-3">
                    <button onClick={() => toggleApproval(t)}>
                      {t.requires_approval ? <span className="badge-yellow cursor-pointer">✓ 승인필요</span> : <span className="text-gray-300 cursor-pointer">-</span>}
                    </button>
                  </td>
                  <td className="py-3">
                    <div className="flex gap-2">
                      <button onClick={() => startEdit(t)} className="text-xs text-blue-600 hover:underline">수정</button>
                      <button onClick={() => handleDelete(t)} className="text-xs text-red-600 hover:underline">삭제</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {approvals.length > 0 && (
        <div className="card mt-6">
          <h3 className="text-sm font-semibold mb-3">⏳ 대기 중인 승인 · Pending Approvals ({approvals.length})</h3>
          {approvals.map((a: any) => (
            <div key={a.id} className="flex items-center justify-between py-3 border-b border-gray-100 last:border-0">
              <div>
                <span className="text-sm font-medium">{a.tool_name || a.name || a.tool_id || 'Unknown'}</span>
                {a.reason && <span className="ml-2 text-xs text-gray-400">{a.reason}</span>}
              </div>
              <div className="flex gap-2">
                <button onClick={async () => { try { await jsonFetch(`/api/tools/approvals/${a.id}/decide`, { method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ decision: 'approved' }) }); load() } catch (e: any) { showToast('승인 실패: ' + (e.message || e)) } }} className="btn-sm btn-primary">승인</button>
                <button onClick={async () => { try { await jsonFetch(`/api/tools/approvals/${a.id}/decide`, { method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ decision: 'denied' }) }); load() } catch (e: any) { showToast('거부 실패: ' + (e.message || e)) } }} className="btn-sm btn-danger">거부</button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
