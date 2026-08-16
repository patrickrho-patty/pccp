import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { showToast } from '../components/Toast'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'

// Tools page (web/14 plan): the Tool Registry — governance metadata
// that the relay enforces on the live path (A). Includes the approvals
// queue (B), MCP cross-link (C), classification presets + wizard (D),
// seed feedback (UX2), per-project allowlist (feature 7).

const DANGER_KO: Record<string, string> = { low: '낮음', medium: '중간', high: '높음', critical: '심각' }
const DANGER_BADGE: Record<string, string> = {
  low: 'bg-green-50 text-green-700 border-green-200',
  medium: 'bg-yellow-50 text-yellow-700 border-yellow-200',
  high: 'bg-orange-50 text-orange-700 border-orange-200',
  critical: 'bg-red-50 text-red-700 border-red-200',
}
const DANGER_HELP: Record<string, string> = {
  low: '읽기 전용 — 기본 허용 가능',
  medium: '수정 가능 — 감사 기록됨',
  high: '삭제/실행 — 승인 필요 권장',
  critical: '인프라/네트워크 — 항상 승인 필요',
}

const TABS = [
  { id: 'registry', label: '도구 레지스트리' },
  { id: 'approvals', label: '승인 대기' },
  { id: 'allowlist', label: '프로젝트 허용 목록' },
]

export default function Tools() {
  const { favorites, sortPinnedFirst } = useFavorites('tools')
  const [tab, setTab] = useState('registry')
  const [tools, setTools] = useState<any[]>([])
  const [approvals, setApprovals] = useState<any[]>([])
  const [presets, setPresets] = useState<any>(null)
  const [projects, setProjects] = useState<any[]>([])
  const [selectedProject, setSelectedProject] = useState('')
  const [allowlist, setAllowlist] = useState<any[]>([])
  const [allToolNames, setAllToolNames] = useState<string[]>([])
  const [formOpen, setFormOpen] = useState(false)
  const [form, setForm] = useState({ name: '', name_ko: '', category: 'read', tool_class: 'read', danger_level: 'low', requires_approval: false })
  const [filterCat, setFilterCat] = useState('')
  const [filterDanger, setFilterDanger] = useState('')

  const load = () => {
    api.listTools().then((d: any[]) => {
      const list = Array.isArray(d) ? d : []
      setTools(list)
      setAllToolNames(list.map((t: any) => t.name))
    }).catch(() => {})
    api.toolApprovals().then((d: any[]) => setApprovals(Array.isArray(d) ? d : [])).catch(() => {})
    api.toolPresets().then(setPresets).catch(() => {})
    api.listProjects().then((d: any[]) => setProjects(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(() => { load() }, [])

  const loadAllowlist = (projectId: string) => {
    if (!projectId) { setAllowlist([]); return }
    api.getProjectToolAllowlist(projectId).then((d: any[]) => setAllowlist(Array.isArray(d) ? d : [])).catch(() => setAllowlist([]))
  }

  const seed = async () => {
    try {
      const res = await api.seedTools()
      showToast(res.added > 0 ? `기본 도구 ${res.added}개 등록 완료` : '이미 모두 등록되어 있습니다 (0개 추가)', 'info')
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const register = async () => {
    if (!form.name.trim()) {
      showToast('도구 이름이 필요합니다', 'error')
      return
    }
    try {
      await api.registerTool(form)
      showToast('도구 등록 완료 — 릴레이가 요청 시점에 강제합니다', 'success')
      setFormOpen(false)
      setForm({ name: '', name_ko: '', category: 'read', tool_class: 'read', danger_level: 'low', requires_approval: false })
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const toggleApproval = async (t: any) => {
    try {
      await api.updateTool(t.id, { requires_approval: !t.requires_approval })
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const decide = async (a: any, decision: string) => {
    try {
      await api.decideToolApproval(a.id, decision, 'admin')
      showToast(decision === 'approved' ? '승인 완료' : '거절 완료', 'success')
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const saveAllowlist = async () => {
    if (!selectedProject) {
      showToast('프로젝트를 선택하세요', 'error')
      return
    }
    const names = allToolNames.filter(n => allowlist.some((r: any) => r.tool_name === n))
    try {
      await api.setProjectToolAllowlist(selectedProject, names)
      showToast('허용 목록 저장 완료', 'success')
      loadAllowlist(selectedProject)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const toggleAllowlistTool = (name: string) => {
    setAllowlist(prev => {
      const exists = prev.some((r: any) => r.tool_name === name)
      if (exists) return prev.filter((r: any) => r.tool_name !== name)
      return [...prev, { tool_name: name }]
    })
  }

  const filtered = tools.filter(t =>
    (!filterCat || t.category === filterCat) && (!filterDanger || t.danger_level === filterDanger))
  const sorted = sortPinnedFirst(filtered, t => t.id)

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">도구 레지스트리 · Tools</h2>
          <p className="text-[11px] text-gray-400">
            등록된 도구만 하네스가 호출할 수 있습니다 — 릴레이가 요청 시점에 레지스트리·임대·프로젝트 허용 목록을 강제합니다 (§17.1).
          </p>
        </div>
        <div className="flex gap-2">
          <button className="btn-sm btn-secondary" onClick={seed}>기본 도구 시드</button>
          <Link className="btn-sm btn-secondary" to="/enterprise-features">MCP 거버넌스</Link>
          <button className="btn-sm btn-primary" onClick={() => setFormOpen(true)}>+ 도구 등록</button>
        </div>
      </div>

      <div className="flex gap-1 border-b border-gray-200">
        {TABS.map(t => (
          <button key={t.id} onClick={() => setTab(t.id)}
            className={`px-3 py-2 text-xs ${tab === t.id ? 'border-b-2 border-blue-600 text-blue-600 font-semibold' : 'text-gray-500'}`}>
            {t.label}{t.id === 'approvals' && approvals.length > 0 ? ` (${approvals.length})` : ''}
          </button>
        ))}
      </div>

      {tab === 'registry' && (
        <>
          <div className="flex gap-2 flex-wrap">
            <select className="input text-xs w-32" value={filterCat} onChange={e => setFilterCat(e.target.value)}>
              <option value="">전체 카테고리</option>
              {(presets?.categories || []).map((c: any) => <option key={c.value} value={c.value}>{c.label_ko}</option>)}
            </select>
            <select className="input text-xs w-32" value={filterDanger} onChange={e => setFilterDanger(e.target.value)}>
              <option value="">전체 위험도</option>
              {Object.entries(DANGER_KO).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select>
          </div>
          <div className="space-y-2">
            {sorted.length === 0 && <EmptyState icon="🧰" title="도구가 없습니다"
              message="기본 도구를 시드하거나 커스텀 도구를 등록하세요." action={{ label: '기본 도구 시드', onClick: seed }} />}
            {sorted.map((t: any) => (
              <div key={t.id} className="card p-3 flex items-center justify-between gap-2">
                <div className="flex items-center gap-2 min-w-0">
                  <FavoriteStar entity="tools" id={t.id} />
                  <div className="min-w-0">
                    <div className="text-xs font-semibold truncate">{t.name_ko || t.name} <span className="text-gray-400 font-mono font-normal">({t.name})</span></div>
                    <div className="text-[10px] text-gray-400">{t.category} · class {t.tool_class}</div>
                  </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <span className={`text-[10px] px-2 py-0.5 rounded-full border ${DANGER_BADGE[t.danger_level] || ''}`}
                    title={DANGER_HELP[t.danger_level] || ''}>
                    {DANGER_KO[t.danger_level] || t.danger_level}
                  </span>
                  <button className={`text-[10px] px-2 py-0.5 rounded-full border ${t.requires_approval ? 'bg-amber-50 text-amber-700 border-amber-200' : 'bg-gray-100 text-gray-500 border-gray-200'}`}
                    onClick={() => toggleApproval(t)} title="승인 필요 토글 (감사 기록됨)">
                    {t.requires_approval ? '승인 필요' : '자동 허용'}
                  </button>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      {tab === 'approvals' && (
        <div className="card p-4 space-y-2">
          {approvals.length === 0 && <p className="text-[11px] text-gray-400">대기 중 승인 요청 없음</p>}
          {approvals.map((a: any) => (
            <div key={a.id} className="border rounded-lg p-2 flex items-center justify-between gap-2 text-[11px]">
              <div>
                <span className="font-semibold">{a.approval_type}</span>
                <span className="text-gray-400 ml-2">세션 {a.session_id?.slice(0, 12) || '—'}</span>
                <div className="text-[10px] text-gray-400">요청자 {a.requested_by || '—'} · {(a.created_at || '').slice(0, 16)}</div>
              </div>
              <div className="flex gap-1">
                <button className="text-[10px] px-2 py-1 rounded bg-green-50 text-green-600" onClick={() => decide(a, 'approved')}>승인</button>
                <button className="text-[10px] px-2 py-1 rounded bg-red-50 text-red-600" onClick={() => decide(a, 'rejected')}>거절</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {tab === 'allowlist' && (
        <div className="card p-4 space-y-3">
          <p className="text-[11px] text-gray-500">프로젝트별 도구 허용 목록 — 허용 목록이 설정된 프로젝트는 목록에 없는 도구 호출이 차단됩니다.</p>
          <select className="input text-xs w-64" value={selectedProject}
            onChange={e => { setSelectedProject(e.target.value); loadAllowlist(e.target.value) }}>
            <option value="">프로젝트 선택...</option>
            {projects.map((p: any) => <option key={p.id} value={p.id}>{p.name_ko || p.name}</option>)}
          </select>
          {selectedProject && (
            <>
              <div className="space-y-1">
                {allToolNames.map(name => (
                  <label key={name} className="flex items-center gap-2 text-xs">
                    <input type="checkbox" checked={allowlist.some((r: any) => r.tool_name === name)}
                      onChange={() => toggleAllowlistTool(name)} />
                    <span className="font-mono">{name}</span>
                  </label>
                ))}
              </div>
              <button className="btn-sm btn-primary" onClick={saveAllowlist}>허용 목록 저장</button>
            </>
          )}
        </div>
      )}

      <Modal open={formOpen} title="도구 등록 (커스텀 마법사)" onClose={() => setFormOpen(false)}
        footer={<ModalFooter onCancel={() => setFormOpen(false)} onConfirm={register} confirmLabel="등록" />}>
        <div className="space-y-2">
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">이름 (식별자, 예: git.commit)</label>
              <input className="input text-xs w-full" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">한글 이름</label>
              <input className="input text-xs w-full" value={form.name_ko} onChange={e => setForm({ ...form, name_ko: e.target.value })} />
            </div>
          </div>
          <div>
            <label className="text-[10px] text-gray-500">카테고리</label>
            <select className="input text-xs w-full" value={form.category} onChange={e => setForm({ ...form, category: e.target.value })}>
              {(presets?.categories || []).map((c: any) => <option key={c.value} value={c.value}>{c.label_ko} — {c.description}</option>)}
            </select>
          </div>
          <div>
            <label className="text-[10px] text-gray-500">도구 클래스 (임대에서 허용되는 클래스)</label>
            <select className="input text-xs w-full" value={form.tool_class} onChange={e => setForm({ ...form, tool_class: e.target.value })}>
              {(presets?.tool_classes || []).map((c: any) => <option key={c.value} value={c.value}>{c.label_ko} — {c.description}</option>)}
            </select>
          </div>
          <div>
            <label className="text-[10px] text-gray-500">위험도</label>
            <select className="input text-xs w-full" value={form.danger_level} onChange={e => setForm({ ...form, danger_level: e.target.value })}>
              {(presets?.danger_levels || []).map((c: any) => <option key={c.value} value={c.value}>{c.label_ko} — {c.description}</option>)}
            </select>
          </div>
          <label className="flex items-center gap-2 text-xs text-gray-600">
            <input type="checkbox" checked={form.requires_approval} onChange={e => setForm({ ...form, requires_approval: e.target.checked })} />
            호출 시 리뷰어 승인 필요
          </label>
        </div>
      </Modal>
    </div>
  )
}
