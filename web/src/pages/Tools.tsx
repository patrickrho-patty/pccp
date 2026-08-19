import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { showToast } from '../components/Toast'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import {
  assessAllowlistImpact, assessGateChange, effectiveAllowlist,
  summarizeRisk, isStaleBase,
} from '../toolGovernance'
import { approvalView, rankApprovals } from '../approvalView'

// Tools page (web/14 plan): the Tool Registry — governance metadata
// that the relay enforces on the live path (A). Includes the approvals
// queue (B), MCP cross-link (C), classification presets + wizard (D),
// seed feedback (UX2), per-project allowlist (feature 7).
// PAT-1509: approval-gate and allowlist changes go through a draft diff
// + impact preview with reason/confirmation instead of silent toggles.

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
  // PAT-1509 governed-change state
  const [detailTool, setDetailTool] = useState<any | null>(null)
  // 프로젝트별 허용 목록 캐시 — 페이지 로드 시 한 번만 조회 (도구 상세 모달이 재사용)
  const [projectAllowlists, setProjectAllowlists] = useState<Record<string, any[] | null> | null>(null)
  const [gateTarget, setGateTarget] = useState<any | null>(null)
  const [gateReason, setGateReason] = useState('')
  const [gateConfirm, setGateConfirm] = useState(false)
  const [savedNames, setSavedNames] = useState<string[]>([]) // allowlist base snapshot (diff 기준)
  const [allowPreview, setAllowPreview] = useState<any | null>(null)
  const [allowReason, setAllowReason] = useState('')
  const [allowConfirm, setAllowConfirm] = useState(false)

  const load = () => {
    api.listTools().then((d: any[]) => {
      const list = Array.isArray(d) ? d : []
      setTools(list)
      setAllToolNames(list.map((t: any) => t.name))
    }).catch(() => {})
    api.toolApprovals().then((d: any[]) => setApprovals(Array.isArray(d) ? d : [])).catch(() => {})
    api.toolPresets().then(setPresets).catch(() => {})
    api.listProjects().then((d: any[]) => {
      const list = Array.isArray(d) ? d : []
      setProjects(list)
      // 모든 프로젝트의 허용 목록을 로드 시 한 번에 가져와 캐시한다.
      Promise.all(list.map((p: any) =>
        api.getProjectToolAllowlist(p.id)
          .then((rows: any[]) => [p.id, Array.isArray(rows) ? rows : []] as [string, any[] | null])
          .catch(() => [p.id, null] as [string, any[] | null])
      )).then(entries => setProjectAllowlists(Object.fromEntries(entries)))
    }).catch(() => {})
  }
  useEffect(() => { load() }, [])

  const loadAllowlist = (projectId: string) => {
    if (!projectId) { setAllowlist([]); setSavedNames([]); return }
    api.getProjectToolAllowlist(projectId).then((d: any[]) => {
      const rows = Array.isArray(d) ? d : []
      setAllowlist(rows)
      setSavedNames(rows.map((r: any) => r.tool_name))
      // 저장 후에는 전체 reload 없이 이 프로젝트의 캐시 항목만 갱신한다.
      setProjectAllowlists(prev => (prev ? { ...prev, [projectId]: rows } : prev))
    }).catch(() => { setAllowlist([]); setSavedNames([]) })
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

  // 승인 게이트 변경은 즉시 토글이 아니라 diff/사유/확인을 거친다 (PAT-1509).
  const openGate = (t: any) => { setGateTarget(t); setGateReason(''); setGateConfirm(false) }

  const confirmGate = async () => {
    if (!gateTarget) return
    const change = assessGateChange(gateTarget, !gateTarget.requires_approval)
    if (!gateReason.trim()) { showToast('변경 사유를 입력하세요 (감사 기록에 남습니다)', 'error'); return }
    if (change.highRisk && !gateConfirm) { showToast('고위험 도구의 게이트 해제는 확인 체크가 필요합니다', 'error'); return }
    try {
      await api.updateTool(gateTarget.id, { requires_approval: change.to, reason: gateReason.trim() })
      showToast('승인 정책 변경 완료 — 감사 기록되며 다음 호출부터 적용됩니다', 'success')
      setGateTarget(null)
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  // 도구 상세: 한글 capability/위험 근거/임대 클래스/다이제스트 + 프로젝트별 허용 상태.
  // 허용 상태는 로드 시 캐시한 projectAllowlists를 읽는다 — 모달을 열 때마다 재조회하지 않는다.
  const openDetail = (t: any) => setDetailTool(t)

  const decide = async (a: any, decision: string) => {
    try {
      await api.decideToolApproval(a.id, decision, 'admin')
      showToast(decision === 'approved' ? '승인 완료' : '거절 완료', 'success')
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const draftNames = useMemo(
    () => allToolNames.filter(n => allowlist.some((r: any) => r.tool_name === n)),
    [allToolNames, allowlist])

  // 허용 목록 저장은 초안 diff + 영향 미리보기 모달을 먼저 연다 (PAT-1509).
  const openAllowPreview = () => {
    if (!selectedProject) { showToast('프로젝트를 선택하세요', 'error'); return }
    const impact = assessAllowlistImpact(savedNames, draftNames, tools)
    if (!impact.hasChanges) { showToast('변경 사항이 없습니다', 'info'); return }
    setAllowReason(''); setAllowConfirm(false)
    setAllowPreview(impact)
  }

  const confirmAllowSave = async () => {
    if (!allowPreview) return
    if (!allowReason.trim()) { showToast('변경 사유를 입력하세요 (감사 기록에 남습니다)', 'error'); return }
    if (allowPreview.weakening && !allowConfirm) { showToast('보호가 약화되는 변경은 확인 체크가 필요합니다', 'error'); return }
    try {
      // 동시 편집 감지: 확인 시점에 재조회해 diff 기준 스냅샷과 비교한다.
      const latest = await api.getProjectToolAllowlist(selectedProject)
      const latestNames = (Array.isArray(latest) ? latest : []).map((r: any) => r.tool_name)
      if (isStaleBase(savedNames, latestNames)) {
        showToast('다른 관리자가 목록을 변경했습니다 — 최신 상태를 다시 불러옵니다', 'error')
        setAllowPreview(null)
        loadAllowlist(selectedProject)
        return
      }
      await api.setProjectToolAllowlist(selectedProject, draftNames, allowReason.trim())
      showToast('허용 목록 저장 완료 — 감사 기록되며 다음 도구 호출부터 적용됩니다', 'success')
      setAllowPreview(null)
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

  // 도구 상세 모달의 프로젝트별 허용 상태 — 캐시가 아직이면 null (조회 중 표시)
  const detailProjects = !detailTool || projectAllowlists === null ? null
    : projects.map((p: any) => ({ project: p, rows: projectAllowlists[p.id] ?? null }))

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">도구 레지스트리 · Tools</h2>
          <p className="text-[11px] text-gray-400">
            등록된 도구만 하네스가 호출할 수 있습니다 — 릴레이가 요청 시점에 레지스트리·임대·프로젝트 허용 목록을 강제합니다 (§17.1).
          </p>
        </div>
        <div className="flex gap-2 shrink-0 flex-wrap">
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
                    <button className="text-xs font-semibold truncate hover:text-blue-600" onClick={() => openDetail(t)}
                      title="상세 보기 — capability·위험·임대·프로젝트 허용 상태">
                      {t.name_ko || t.name} <span className="text-gray-400 font-mono font-normal">({t.name})</span>
                    </button>
                    <div className="text-[10px] text-gray-400">{t.category} · class {t.tool_class}</div>
                  </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <span className={`text-[10px] px-2 py-0.5 rounded-full border ${DANGER_BADGE[t.danger_level] || ''}`}
                    title={DANGER_HELP[t.danger_level] || ''}>
                    {DANGER_KO[t.danger_level] || t.danger_level}
                  </span>
                  <button className={`text-[10px] px-2 py-0.5 rounded-full border ${t.requires_approval ? 'bg-amber-50 text-amber-700 border-amber-200' : 'bg-gray-100 text-gray-500 border-gray-200'}`}
                    onClick={() => openGate(t)} title="승인 정책 변경 — diff·사유·확인 후 감사 기록됨">
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
          <div className="flex items-center justify-between">
            <p className="text-[11px] text-gray-500">대기 중 승인 — 긴급순: 만료 · 위험도 · 대기 시간</p>
            <span className="text-[10px] text-gray-400">총 {approvals.length}건</span>
          </div>
          {approvals.length === 0 && <p className="text-[11px] text-gray-400">대기 중 승인 요청 없음</p>}
          {rankApprovals(approvals).map((a: any) => {
            const v = approvalView(a)
            return (
              <div key={a.id} className="border rounded-lg p-2 flex items-center justify-between gap-2 text-[11px]">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-semibold">{v.title}</span>
                    <span className={`text-[9px] px-1.5 py-0.5 rounded-full border ${v.expired ? 'bg-red-50 text-red-700 border-red-200' : 'bg-gray-100 text-gray-500 border-gray-200'}`}>
                      {v.expired ? '만료' : `대기 ${v.ageLabel}`}
                    </span>
                    {v.expiresLabel && <span className="text-[9px] text-gray-400">{v.expiresLabel}</span>}
                  </div>
                  <div className="text-[10px] text-gray-400 mt-0.5">
                    요청자 {v.requestedBy}{v.harnessId ? ` · 하네스 ${v.harnessId}` : ''}{v.sessionTitle ? ` · 세션 ${v.sessionTitle}` : ''} · {(a.created_at || '').slice(0, 16)}
                  </div>
                </div>
                <div className="flex gap-1 shrink-0">
                  <button className="text-[10px] px-2 py-1 rounded bg-green-50 text-green-600" onClick={() => decide(a, 'approved')}>승인</button>
                  <button className="text-[10px] px-2 py-1 rounded bg-red-50 text-red-600" onClick={() => decide(a, 'denied')}>거절</button>
                </div>
              </div>
            )
          })}
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
              {/* 현재 유효 정책 배너 — 로컬 체크박스 초안이 아니라 저장된 상태 기준 */}
              {(() => {
                const eff = effectiveAllowlist(savedNames, tools)
                return (
                  <div className={`text-[11px] px-3 py-2 rounded-lg border ${eff.mode === 'unset' ? 'bg-yellow-50 text-yellow-800 border-yellow-200' : 'bg-blue-50 text-blue-800 border-blue-200'}`}>
                    <span className="font-semibold">현재 유효 정책:</span> {eff.label}
                    {eff.unknown.length > 0 && (
                      <span className="block text-red-600 mt-0.5">레지스트리에 없는 도구가 목록에 남아 있습니다: {eff.unknown.join(', ')}</span>
                    )}
                  </div>
                )
              })()}
              {/* 초안 위험 요약 */}
              <div className="text-[10px] text-gray-500">
                선택 {draftNames.length}개 — {Object.entries(summarizeRisk(draftNames, tools)).map(([k, v]) =>
                  `${DANGER_KO[k] || '미등록'} ${v}`).join(' · ') || '없음'}
              </div>
              <div className="space-y-1">
                {tools.map((t: any) => (
                  <label key={t.name} className="flex items-center gap-2 text-xs">
                    <input type="checkbox" checked={allowlist.some((r: any) => r.tool_name === t.name)}
                      onChange={() => toggleAllowlistTool(t.name)} />
                    <span className="font-mono">{t.name}</span>
                    <span className="text-gray-500">{t.name_ko || ''}</span>
                    <span className={`text-[10px] px-1.5 rounded-full border ${DANGER_BADGE[t.danger_level] || ''}`}
                      title={DANGER_HELP[t.danger_level] || ''}>
                      {DANGER_KO[t.danger_level] || t.danger_level}
                    </span>
                    {t.requires_approval && <span className="text-[10px] text-amber-600">승인 필요</span>}
                  </label>
                ))}
              </div>
              <button className="btn-sm btn-primary" onClick={openAllowPreview}>허용 목록 저장 (영향 검토)</button>
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

      {/* 도구 상세 (PAT-1509) — 한글 capability/효과, 위험 근거, 필요 임대 클래스, 상태, 다이제스트, 프로젝트별 허용 상태 */}
      <Modal open={!!detailTool} title={`도구 상세 — ${detailTool?.name_ko || detailTool?.name || ''}`}
        subtitle={detailTool?.name} onClose={() => setDetailTool(null)} size="lg">
        {detailTool && (
          <div className="space-y-3 text-xs">
            <div className="grid grid-cols-2 gap-2">
              <div><span className="text-gray-400">카테고리</span><div>{detailTool.category}</div></div>
              <div><span className="text-gray-400">필요 임대 클래스</span><div className="font-mono">{detailTool.tool_class}</div></div>
              <div>
                <span className="text-gray-400">위험도</span>
                <div className="flex items-center gap-2">
                  <span className={`text-[10px] px-2 py-0.5 rounded-full border ${DANGER_BADGE[detailTool.danger_level] || ''}`}>
                    {DANGER_KO[detailTool.danger_level] || detailTool.danger_level}
                  </span>
                  <span className="text-[10px] text-gray-500">{DANGER_HELP[detailTool.danger_level] || ''}</span>
                </div>
              </div>
              <div><span className="text-gray-400">승인 정책</span><div>{detailTool.requires_approval ? '호출 시 리뷰어 승인 필요' : '자동 허용'}</div></div>
              <div><span className="text-gray-400">상태</span><div>{detailTool.status === 'active' ? '활성' : detailTool.status}</div></div>
              <div><span className="text-gray-400">무결성 다이제스트</span><div className="font-mono break-all">{detailTool.signature || '미고정 (런타임 다이제스트 검증 없음)'}</div></div>
            </div>
            <div>
              <div className="text-gray-400 mb-1">프로젝트별 허용 상태</div>
              {detailProjects === null && <p className="text-[11px] text-gray-400">조회 중...</p>}
              {detailProjects !== null && detailProjects.length === 0 && <p className="text-[11px] text-gray-400">프로젝트 없음</p>}
              <div className="space-y-1">
                {(detailProjects || []).map((r: any) => {
                  const onList = r.rows !== null && r.rows.some((x: any) => x.tool_name === detailTool.name)
                  const state = r.rows === null ? '조회 실패 (권한 제한/오프라인)'
                    : r.rows.length === 0 ? '허용 목록 미설정 — 기본 허용'
                    : onList ? '허용 목록에 포함' : '허용 목록에 없음 — 차단'
                  return (
                    <div key={r.project.id} className="flex items-center justify-between border rounded-lg px-2 py-1 text-[11px]">
                      <span>{r.project.name_ko || r.project.name}</span>
                      <span className={r.rows === null ? 'text-red-600' : onList || r.rows.length === 0 ? 'text-green-600' : 'text-gray-500'}>{state}</span>
                    </div>
                  )
                })}
              </div>
            </div>
          </div>
        )}
      </Modal>

      {/* 승인 정책 변경 — 초안 diff + 영향 확인 (PAT-1509) */}
      <Modal open={!!gateTarget} title="승인 정책 변경 — 영향 확인" subtitle={gateTarget?.name}
        onClose={() => setGateTarget(null)}
        footer={<ModalFooter onCancel={() => setGateTarget(null)} onConfirm={confirmGate}
          confirmLabel="변경 적용" danger={!!gateTarget && assessGateChange(gateTarget, !gateTarget.requires_approval).weakening} />}>
        {gateTarget && (() => {
          const change = assessGateChange(gateTarget, !gateTarget.requires_approval)
          return (
            <div className="space-y-3 text-xs">
              <div className="border rounded-lg p-3 space-y-1">
                <div className="text-[10px] text-gray-400 font-semibold">변경 diff</div>
                <div>현재: <span className="font-semibold">{change.from ? '승인 필요' : '자동 허용'}</span> → 변경 후: <span className="font-semibold">{change.to ? '승인 필요' : '자동 허용'}</span></div>
              </div>
              {change.weakening && (
                <div className={`text-[11px] px-3 py-2 rounded-lg border ${change.highRisk ? 'bg-red-50 text-red-700 border-red-200' : 'bg-yellow-50 text-yellow-800 border-yellow-200'}`}>
                  {change.highRisk
                    ? '보호 약화 경고: 이 게이트는 기본 정책 수단(심층 방어)입니다. 해제해도 high/critical 도구는 서버에서 항상 리뷰어 승인을 요구하므로 리뷰 없이 즉시 호출되지는 않습니다.'
                    : '보호 약화: 승인 없이 호출 가능해집니다.'}
                </div>
              )}
              <div>
                <label className="text-[10px] text-gray-500">변경 사유 (필수 — 감사 기록에 남습니다)</label>
                <input className="input text-xs w-full" value={gateReason} onChange={e => setGateReason(e.target.value)}
                  placeholder="예: 긴급 장애 대응 / 보안 검토 완료" />
              </div>
              {change.highRisk && (
                <label className="flex items-center gap-2 text-xs text-gray-700">
                  <input type="checkbox" checked={gateConfirm} onChange={e => setGateConfirm(e.target.checked)} />
                  고위험 도구의 승인 게이트 해제 영향을 확인했습니다
                </label>
              )}
            </div>
          )
        })()}
      </Modal>

      {/* 허용 목록 저장 — 초안 diff + 영향 미리보기 (PAT-1509) */}
      <Modal open={!!allowPreview} title="허용 목록 변경 — 영향 미리보기"
        subtitle={projects.find((p: any) => p.id === selectedProject)?.name_ko || projects.find((p: any) => p.id === selectedProject)?.name}
        onClose={() => setAllowPreview(null)}
        footer={<ModalFooter onCancel={() => setAllowPreview(null)} onConfirm={confirmAllowSave}
          confirmLabel="저장" danger={!!allowPreview?.weakening} />}>
        {allowPreview && (
          <div className="space-y-3 text-xs">
            {allowPreview.becomesUnset && (
              <div className="text-[11px] px-3 py-2 rounded-lg border bg-red-50 text-red-700 border-red-200">
                보호 약화 경고: 목록이 비워지면 "미설정"으로 되돌아가 등록된 모든 도구가 허용됩니다.
              </div>
            )}
            {allowPreview.addedHighRisk.length > 0 && (
              <div className="text-[11px] px-3 py-2 rounded-lg border bg-red-50 text-red-700 border-red-200">
                고위험 capability 추가: {allowPreview.addedHighRisk.join(', ')} — high/critical 도구가 이 프로젝트에서 호출 가능해집니다.
              </div>
            )}
            {allowPreview.unknown.length > 0 && (
              <div className="text-[11px] px-3 py-2 rounded-lg border bg-yellow-50 text-yellow-800 border-yellow-200">
                레지스트리에 없는 도구가 포함되어 있습니다: {allowPreview.unknown.join(', ')}
              </div>
            )}
            <div className="border rounded-lg p-3 space-y-1">
              <div className="text-[10px] text-gray-400 font-semibold">변경 diff (현재 저장본 기준)</div>
              <div>추가 ({allowPreview.diff.added.length}): {allowPreview.diff.added.join(', ') || '없음'}</div>
              <div>삭제 ({allowPreview.diff.removed.length}): {allowPreview.diff.removed.join(', ') || '없음'}
                {allowPreview.removedGated.length > 0 && <span className="text-amber-600"> — 승인 게이트 도구 포함: {allowPreview.removedGated.join(', ')}</span>}
              </div>
              <div>유지 ({allowPreview.diff.kept.length})</div>
            </div>
            <p className="text-[10px] text-gray-400">
              전파: 저장 즉시 감사 기록되며, 실행 중인 세션/하네스는 다음 도구 호출 시점부터 새 목록이 강제됩니다 (릴레이 요청 시점 검사).
            </p>
            <div>
              <label className="text-[10px] text-gray-500">변경 사유 (필수 — 감사 기록에 남습니다)</label>
              <input className="input text-xs w-full" value={allowReason} onChange={e => setAllowReason(e.target.value)}
                placeholder="예: 보안 검토 완료 / 프로젝트 범위 조정" />
            </div>
            {allowPreview.weakening && (
              <label className="flex items-center gap-2 text-xs text-gray-700">
                <input type="checkbox" checked={allowConfirm} onChange={e => setAllowConfirm(e.target.checked)} />
                보호가 약화되는 변경의 영향을 확인했습니다
              </label>
            )}
          </div>
        )}
      </Modal>
    </div>
  )
}
