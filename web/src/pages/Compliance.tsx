import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { Modal, ModalFooter } from '../components/Modal'
import { showToast } from '../components/Toast'
import {
  taskState, dueAgeLabel, evidenceSourceKo, evidenceFreshnessLabel,
  groupAssessmentRuns, parseControlResults,
} from '../complianceView'

// Compliance page (web/08 plan): REAL backend-fed assessment. No more
// hardcoded "compliant" badges — every status comes from the evidence
// engine (assessControlState) which reads actual PCCP state. Self-
// assessment disclaimer is shown: certification is the customer's
// process (§41 guardrail).

const STATUS_BADGE: Record<string, string> = {
  compliant: 'bg-green-50 text-green-700 border-green-200',
  partially_compliant: 'bg-yellow-50 text-yellow-700 border-yellow-200',
  gap: 'bg-red-50 text-red-700 border-red-200',
  partial: 'bg-yellow-50 text-yellow-700 border-yellow-200',
  not_applicable: 'bg-gray-100 text-gray-500 border-gray-200',
}
const STATUS_KO: Record<string, string> = {
  compliant: '준수', partially_compliant: '부분 준수', gap: '갭', partial: '부분', not_applicable: '해당 없음',
}

export default function Compliance() {
  const [meta, setMeta] = useState<any[]>([])
  const [selected, setSelected] = useState('')
  const [scope, setScope] = useState('SaaS')
  const [level, setLevel] = useState('')
  const [assessment, setAssessment] = useState<any>(null)
  const [history, setHistory] = useState<any[]>([])
  const [evidence, setEvidence] = useState<any[]>([])
  const [remediations, setRemediations] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [filter, setFilter] = useState('')
  const [evidenceOpen, setEvidenceOpen] = useState<any>(null)
  const [evidenceDetail, setEvidenceDetail] = useState<any>(null)
  const [snapshotOpen, setSnapshotOpen] = useState<any>(null) // PAT-1504 immutable assessment snapshot
  const [evForm, setEvForm] = useState({ control_id: '', title: '', description: '', source: 'manual', reference: '' })
  const [taskOpen, setTaskOpen] = useState<any>(null)
  const [taskDetail, setTaskDetail] = useState<any>(null)
  const [taskForm, setTaskForm] = useState({ owner: '', due_date: '', sla: '30d', notes: '' })
  const [bulkOwner, setBulkOwner] = useState('')
  // PAT-1484: dashboard KPI "진행 중 컴플라이언스 개선 과제" deep-links here
  // as /compliance?tab=remediation&status=unresolved so the page shows only
  // unresolved remediations (status != done), matching the KPI's count via
  // the shared backend scope contract.
  const [searchParams, setSearchParams] = useSearchParams()
  const remedFilter = searchParams.get('status') || ''
  const remediationScope = remedFilter === 'unresolved' ? ((t: any) => t.status !== 'done') : remedFilter ? ((t: any) => t.status === remedFilter) : null
  const remediationScopeLabel = remedFilter === 'unresolved' ? '진행 중 과제' : remedFilter ? `상태 ${remedFilter}` : ''
  const sidebarTasks = remediationScope ? (remediations || []).filter(remediationScope) : (remediations || [])

  const loadMeta = () => {
    api.complianceMeta().then(d => {
      const list = Array.isArray(d) ? d : []
      setMeta(list)
      if (list.length && !selected) setSelected(list[0].certification)
      if (list.length && !level) setLevel((list[0].levels?.[0] as any)?.value || '')
    }).catch(() => {})
  }
  useEffect(() => { loadMeta() }, [])

  const currentMeta = meta.find(m => m.certification === selected)

  const loadSide = (cert: string) => {
    if (!cert) return
    api.complianceEvidence(cert).then(d => setEvidence(Array.isArray(d) ? d : [])).catch(() => setEvidence([]))
    api.complianceRemediations(cert).then(d => setRemediations(Array.isArray(d) ? d : [])).catch(() => setRemediations([]))
    api.complianceHistory().then(d => setHistory(Array.isArray(d) ? d : [])).catch(() => setHistory([]))
  }
  useEffect(() => { loadSide(selected) }, [selected])

  const assess = async () => {
    if (!selected) return
    setLoading(true)
    try {
      const res = await api.complianceAssess(selected, scope, level)
      setAssessment(res)
      showToast('자체 평가 완료', 'success')
      loadSide(selected)
    } catch (e: any) {
      showToast(e?.message || '평가 실패', 'error')
    } finally {
      setLoading(false)
    }
  }

  const downloadExport = async (format: string) => {
    // Authenticated fetch + blob download (the endpoint sits behind the
    // admin JWT middleware, so a plain window.open would 401).
    const token = sessionStorage.getItem('pccp_token')
    try {
      const resp = await fetch(`/api/compliance/export?certification=${encodeURIComponent(selected)}&format=${format}`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      if (!resp.ok) throw new Error('export failed')
      const blob = await resp.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `${selected}-compliance-matrix.${format === 'csv' ? 'csv' : 'json'}`
      a.click()
      URL.revokeObjectURL(url)
    } catch (e: any) {
      showToast(e?.message || '내보내기 실패', 'error')
    }
  }

  const addEvidence = async () => {
    if (!evForm.control_id) {
      showToast('통제 항목을 선택하세요', 'error')
      return
    }
    try {
      await api.complianceEvidenceAdd(selected, evForm.control_id, evForm.title, evForm.description, evForm.source, evForm.reference)
      showToast('증거 추가 완료', 'success')
      setEvidenceOpen(null)
      setEvForm({ control_id: '', title: '', description: '', source: 'manual', reference: '' })
      loadSide(selected)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const addTask = async () => {
    if (!taskOpen?.control_id) return
    try {
      await api.complianceRemediationAdd(selected, taskOpen.control_id, taskForm.owner, taskForm.due_date, taskForm.sla, taskForm.notes)
      showToast('개선 과제 등록 완료', 'success')
      setTaskOpen(null)
      setTaskForm({ owner: '', due_date: '', sla: '30d', notes: '' })
      loadSide(selected)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const bulkRemediate = async () => {
    try {
      const res = await api.complianceBulkRemediate(selected, bulkOwner)
      showToast(`갭 → 과제 일괄 전환 (${res.created}건)`, 'success')
      setBulkOwner('')
      loadSide(selected)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const updateTask = async (id: string, status: string) => {
    try {
      await api.complianceRemediationUpdate(id, status, '', '', '')
      loadSide(selected)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const results = (assessment?.control_results || []) as any[]
  const filtered = filter ? results.filter((r: any) => r.status === filter) : results
  const counts = {
    compliant: results.filter((r: any) => r.status === 'compliant').length,
    partial: results.filter((r: any) => r.status === 'partial').length,
    gap: results.filter((r: any) => r.status === 'gap').length,
  }
  const evByControl: Record<string, number> = {}
  evidence.forEach(e => { evByControl[e.control_id] = (evByControl[e.control_id] || 0) + 1 })
  const taskByControl: Record<string, string> = {}
  remediations.forEach(t => { taskByControl[t.control_id] = t.status })

  // PAT-1504: group repeated identical assessment runs into one row each,
  // with a change summary vs the previous distinct run, and a drillable
  // immutable snapshot (parseControlResults reads the persisted ResultsJSON).
  const { grouped: groupedHistory, changedControls: groupedChanges } = groupAssessmentRuns(history)

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">컴플라이언스 · Compliance</h2>
          <p className="text-[11px] text-gray-400">
            자체 평가(self-assessment)입니다 — 인증은 고객의 절차이며, 이 페이지는 맵·증거·과제를 제공합니다 (§41).
          </p>
        </div>
        <div className="flex gap-2 flex-wrap items-center">
          <select className="input text-xs" value={selected} onChange={e => { setSelected(e.target.value); setAssessment(null); setLevel((meta.find(m => m.certification === e.target.value)?.levels?.[0] as any)?.value || '') }}>
            {meta.map(m => <option key={m.certification} value={m.certification}>{m.name_ko}</option>)}
          </select>
          <select className="input text-xs" value={scope} onChange={e => setScope(e.target.value)}>
            {(currentMeta?.scopes || ['SaaS']).map((s: string) => <option key={s} value={s}>{s}</option>)}
          </select>
          <select className="input text-xs" value={level} onChange={e => setLevel(e.target.value)}>
            {(currentMeta?.levels || []).map((l: any) => <option key={l.value} value={l.value}>{l.label_ko || l.label}</option>)}
          </select>
          <button className="btn-sm btn-primary" onClick={assess} disabled={loading}>
            {loading ? '평가 중...' : '자체 평가 실행'}
          </button>
        </div>
      </div>

      {assessment && (
        <>
          <div className="grid grid-cols-3 gap-3">
            <button className="card p-3 text-center hover:border-green-300" onClick={() => setFilter(filter === 'compliant' ? '' : 'compliant')}>
              <div className="text-lg font-bold text-green-600">{counts.compliant}</div>
              <div className="text-[10px] text-gray-400">준수</div>
            </button>
            <button className="card p-3 text-center hover:border-yellow-300" onClick={() => setFilter(filter === 'partial' ? '' : 'partial')}>
              <div className="text-lg font-bold text-yellow-600">{counts.partial}</div>
              <div className="text-[10px] text-gray-400">부분</div>
            </button>
            <button className="card p-3 text-center hover:border-red-300" onClick={() => setFilter(filter === 'gap' ? '' : 'gap')}>
              <div className="text-lg font-bold text-red-600">{counts.gap}</div>
              <div className="text-[10px] text-gray-400">갭</div>
            </button>
          </div>

          <div className="card p-4 space-y-2">
            <div className="flex items-center justify-between flex-wrap gap-2">
              <h3 className="text-xs font-bold">통제 결과 ({filtered.length}/{results.length})</h3>
              <div className="flex gap-2 shrink-0 flex-wrap">
                <input className="input text-xs w-48" placeholder="일괄 과제 담당자"
                  value={bulkOwner} onChange={e => setBulkOwner(e.target.value)} />
                <button className="btn-sm btn-secondary" onClick={bulkRemediate}>갭 → 과제 일괄</button>
                <button className="btn-sm btn-secondary" onClick={() => downloadExport('csv')}>CSV 내보내기</button>
                <button className="btn-sm btn-secondary" onClick={() => downloadExport('json')}>JSON 패키지</button>
              </div>
            </div>
            {filtered.map((r: any) => (
              <div key={r.control_id} className="border rounded-lg p-2 flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="text-xs font-semibold">{r.control_id}</div>
                  <div className="text-[11px] text-gray-500 truncate">{r.gap_description_ko || r.gap_description || ''}</div>
                  <div className="text-[10px] text-gray-400">{r.evidence}</div>
                </div>
                <div className="flex items-center gap-1 shrink-0">
                  <span className="text-[10px] px-1 rounded bg-gray-100 text-gray-500">{evByControl[r.control_id] || 0} 증거</span>
                  {taskByControl[r.control_id] && (
                    <button className="text-[10px] px-1 rounded bg-blue-50 text-blue-600" onClick={() => updateTask(remediations.find(t => t.control_id === r.control_id)?.id, taskByControl[r.control_id] === 'done' ? 'open' : 'done')}>
                      과제 {taskByControl[r.control_id]}
                    </button>
                  )}
                  <button className="text-[10px] px-1 rounded hover:bg-gray-100" onClick={() => { setEvidenceOpen(r); setEvForm({ control_id: r.control_id, title: '', description: '', source: 'manual', reference: '' }) }}>+증거</button>
                  {r.status !== 'compliant' && (
                    <button className="text-[10px] px-1 rounded hover:bg-amber-50 text-amber-600" onClick={() => { setTaskOpen(r); setTaskForm({ owner: '', due_date: '', sla: '30d', notes: '' }) }}>+과제</button>
                  )}
                  <span className={`text-[10px] px-2 py-0.5 rounded-full border ${STATUS_BADGE[r.status] || ''}`}>{STATUS_KO[r.status] || r.status}</span>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      {!assessment && (
        <div className="card p-8 text-center text-xs text-gray-400">
          인증 대상·범위·등급을 선택하고 자체 평가를 실행하세요. 결과는 실제 PCCP 상태(사용자/규칙/감사 이벤트/보안 발견)에서 산출됩니다.
        </div>
      )}

      {/* Evidence vault (C1) — traceable workspace */}
      <div className="card p-4">
        <h3 className="text-xs font-bold mb-2">증거 보관소 · Evidence Vault ({evidence.length})</h3>
        {evidence.length === 0 ? <p className="text-[11px] text-gray-400">등록된 증거 없음</p> : (
          <div className="space-y-1">
            {evidence.slice(0, 20).map((e: any) => (
              <button key={e.id} onClick={() => setEvidenceDetail(e)} className="w-full flex justify-between text-[11px] border-b border-gray-50 py-1 hover:bg-blue-50/50 text-left">
                <span className="text-blue-600 hover:underline">{e.control_id} — {e.title} <span className="text-gray-400">({e.source})</span> · <span className="text-gray-500">{e.reference || '참조 없음'}</span></span>
                <span className="text-gray-400">{(e.collected_at || '').slice(0, 10)} · 상세 →</span>
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Remediation tracking (C2) — PAT-1484: honors the dashboard KPI
          deep link /compliance?tab=remediation&status=unresolved by showing
          only the scoped remediations, with a visible count and clear. */}
      <div className="card p-4">
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-xs font-bold">개선 과제 · Remediation ({sidebarTasks.length})</h3>
          {remediationScope && (
            <button
              onClick={() => setSearchParams({})}
              className="text-[11px] text-gray-500 hover:text-gray-700 hover:underline"
              aria-label="개선 과제 필터 초기화">
              {remediationScopeLabel} · {sidebarTasks.length}건 · 필터 초기화 ✕
            </button>
          )}
        </div>
        {sidebarTasks.length === 0 ? <p className="text-[11px] text-gray-400">{remediationScope ? '해당 범위의 등록된 과제 없음' : '등록된 과제 없음'}</p> : (
          <div className="space-y-1">
            {sidebarTasks.map((t: any) => (
              <button key={t.id} onClick={() => setTaskDetail(t)} className="w-full flex justify-between items-center text-[11px] border-b border-gray-50 py-1 hover:bg-amber-50/50 text-left">
                <span className="text-gray-700 font-medium">{t.control_id} — {t.owner || '담당자 미정'}</span>
                <span className="text-gray-400">{t.sla || ''} · {t.due_date || ''}</span>
                <span className={`text-[10px] px-1.5 py-0.5 rounded border ${t.status === 'done' ? 'bg-green-50 text-green-700 border-green-200' : t.status === 'in_progress' ? 'bg-yellow-50 text-yellow-700 border-yellow-200' : 'bg-gray-50 text-gray-600 border-gray-200'}`}>{t.status}</span>
                <span className="text-blue-600 ml-2">상세 →</span>
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Assessment history (C3) — grouped and drillable */}
      {history.length > 0 && (
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">평가 이력 · History ({history.length})</h3>
          <p className="text-[10px] text-gray-400 mb-2">동일 결과의 반복 평가는 하나로 묶습니다(×N). 각 행을 펼치면 변경된 통제와 갭을 확인하고, 클릭하면 해당 시점의 불변 스냅샷을 엽니다.</p>
          <div className="space-y-1">
            {groupedHistory.map((g: any) => {
              const changed = groupedChanges[g.id] || []
              return (
                <button key={g.id} onClick={() => setSnapshotOpen(g)} className="w-full text-[11px] border-b border-gray-50 py-1 hover:bg-gray-50 text-left px-1">
                  <div className="flex justify-between w-full">
                    <span className="text-gray-700">{g.scope}/{g.level}<span className="text-gray-400"> · {g.assessedAt ? g.assessedAt.slice(0, 16).replace('T', ' ') : ''}</span>{g.count > 1 && <span className="text-[9px] px-1.5 ml-1 py-0.5 rounded-full bg-gray-100 text-gray-500">× {g.count}</span>}</span>
                    <span className={g.overallStatus === 'compliant' ? 'text-green-600' : g.overallStatus === 'gap' ? 'text-red-600' : 'text-yellow-600'}>{g.overallStatus} ({g.openGaps} 갭) · 스냅샷 →</span>
                  </div>
                  {changed.length > 0 && (
                    <div className="mt-0.5 text-[10px] text-amber-600">변경: {changed.join(', ')}</div>
                  )}
                </button>
              )
            })}
          </div>
        </div>
      )}

      {/* Evidence modal (C1) */}
      <Modal open={!!evidenceOpen} title={`증거 등록 — ${evidenceOpen?.control_id || ''}`}
        onClose={() => setEvidenceOpen(null)}
        footer={<ModalFooter onCancel={() => setEvidenceOpen(null)} onConfirm={addEvidence} confirmLabel="등록" />}>
        <div className="space-y-2">
          <div>
            <label className="text-[10px] text-gray-500">제목</label>
            <input className="input text-xs w-full" value={evForm.title} onChange={e => setEvForm({ ...evForm, title: e.target.value })} />
          </div>
          <div>
            <label className="text-[10px] text-gray-500">설명</label>
            <textarea className="input text-xs w-full" rows={2} value={evForm.description} onChange={e => setEvForm({ ...evForm, description: e.target.value })} />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">출처</label>
              <select className="input text-xs w-full" value={evForm.source} onChange={e => setEvForm({ ...evForm, source: e.target.value })}>
                <option value="manual">manual</option>
                <option value="audit">audit</option>
                <option value="provenance">provenance</option>
                <option value="security">security</option>
                <option value="attestation">attestation</option>
              </select>
            </div>
            <div>
              <label className="text-[10px] text-gray-500">참조 (API 경로/파일)</label>
              <input className="input text-xs w-full" value={evForm.reference} onChange={e => setEvForm({ ...evForm, reference: e.target.value })} />
            </div>
          </div>
        </div>
      </Modal>

      {/* Task modal (C2) */}
      <Modal open={!!taskOpen} title={`개선 과제 등록 — ${taskOpen?.control_id || ''}`}
        onClose={() => setTaskOpen(null)}
        footer={<ModalFooter onCancel={() => setTaskOpen(null)} onConfirm={addTask} confirmLabel="등록" />}>
        <div className="space-y-2">
          <div>
            <label className="text-[10px] text-gray-500">담당자</label>
            <input className="input text-xs w-full" value={taskForm.owner} onChange={e => setTaskForm({ ...taskForm, owner: e.target.value })} />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">기한</label>
              <input className="input text-xs w-full" type="date" value={taskForm.due_date} onChange={e => setTaskForm({ ...taskForm, due_date: e.target.value })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">SLA</label>
              <select className="input text-xs w-full" value={taskForm.sla} onChange={e => setTaskForm({ ...taskForm, sla: e.target.value })}>
                <option value="30d">30일</option>
                <option value="60d">60일</option>
                <option value="90d">90일</option>
              </select>
            </div>
          </div>
          <div>
            <label className="text-[10px] text-gray-500">비고</label>
            <textarea className="input text-xs w-full" rows={2} value={taskForm.notes} onChange={e => setTaskForm({ ...taskForm, notes: e.target.value })} />
          </div>
        </div>
      </Modal>

      {/* Evidence detail drawer */}
      <Modal open={!!evidenceDetail} title={`증거 상세 — ${evidenceDetail?.control_id || ''}`} onClose={() => setEvidenceDetail(null)} footer={<ModalFooter onCancel={() => setEvidenceDetail(null)} onConfirm={() => setEvidenceDetail(null)} confirmLabel="닫기" />}>
        {evidenceDetail && (
          <div className="space-y-2 text-xs">
            <div><span className="text-gray-500">제목:</span> {evidenceDetail.title}</div>
            <div><span className="text-gray-500">통제:</span> {evidenceDetail.control_id}</div>
            <div><span className="text-gray-500">출처:</span> {evidenceSourceKo(evidenceDetail.source)} <span className="text-gray-400">· {evidenceDetail.reference || '참조 없음'}</span></div>
            <div><span className="text-gray-500">수집일:</span> {(evidenceDetail.collected_at || '').slice(0, 16)} <span className="text-gray-400">({evidenceFreshnessLabel(evidenceDetail.collected_at)})</span></div>
            <div><span className="text-gray-500">설명:</span> {evidenceDetail.description || '설명 없음'}</div>
            <div className="text-[10px] text-gray-400">증거 ID: {evidenceDetail.id} · 증거는 감사 로그와 연결되어 추적됩니다.</div>
          </div>
        )}
      </Modal>

      {/* Remediation detail drawer */}
      <Modal open={!!taskDetail} title={`개선 과제 상세 — ${taskDetail?.control_id || ''}`} onClose={() => setTaskDetail(null)} footer={<ModalFooter onCancel={() => setTaskDetail(null)} onConfirm={() => setTaskDetail(null)} confirmLabel="닫기" />}>
        {taskDetail && (() => {
          const st = taskState(taskDetail.status)
          return (
            <div className="space-y-2 text-xs">
              <div><span className="text-gray-500">통제:</span> {taskDetail.control_id}</div>
              <div><span className="text-gray-500">담당자:</span> {taskDetail.owner || '미정'}</div>
              <div><span className="text-gray-500">현재 상태:</span> <span className={`px-1.5 py-0.5 rounded border ${st.color}`}>{st.icon} {st.labelKo}</span> <span className="text-gray-400">· 다음 단계: {st.nextActionKo}</span></div>
              <div><span className="text-gray-500">기한:</span> {taskDetail.due_date || '-'} · SLA {taskDetail.sla || '-'} {dueAgeLabel(taskDetail.due_date) ? <span className="text-red-500">({dueAgeLabel(taskDetail.due_date)})</span> : ''}</div>
              <div><span className="text-gray-500">비고:</span> {taskDetail.notes || '비고 없음'}</div>
              <div className="flex items-center gap-2 mt-2">
                <span className="text-gray-400 text-[10px]">상태 변경:</span>
                <select className="input text-xs" value={taskDetail.status} onChange={e => { updateTask(taskDetail.id, e.target.value); setTaskDetail({ ...taskDetail, status: e.target.value }) }}>
                  <option value="open">미착수</option>
                  <option value="in_progress">진행 중</option>
                  <option value="done">완료</option>
                </select>
              </div>
              <div className="text-[10px] text-gray-400">과제 ID: {taskDetail.id} · 대시보드 카운트와 동일한 필터 계약을 사용합니다.</div>
            </div>
          )
        })()}
      </Modal>
      {/* Assessment snapshot (PAT-1504): immutable result snapshot */}
      <Modal open={!!snapshotOpen} title={`평가 스냅샷 — ${snapshotOpen?.scope || ''}/${snapshotOpen?.level || ''} (×${snapshotOpen?.count || 1})`} onClose={() => setSnapshotOpen(null)} size="lg"
        footer={<ModalFooter onCancel={() => setSnapshotOpen(null)} onConfirm={() => setSnapshotOpen(null)} confirmLabel="닫기" />}>
        {snapshotOpen && (() => {
          const controls = Object.entries(snapshotOpen.results || {})
          return (
            <div className="space-y-2 text-xs">
              <div className="flex justify-between text-[11px] text-gray-500">
                <span>평가 시각: {snapshotOpen.assessedAt ? snapshotOpen.assessedAt.slice(0, 16).replace('T', ' ') : '—'}</span>
                <span>종합 {snapshotOpen.overallStatus} · 갭 {snapshotOpen.openGaps}</span>
              </div>
              {controls.length === 0 && <p className="text-[11px] text-gray-400">스냅샷에 통제 결과가 없습니다</p>}
              {controls.map(([cid, rows]) => {
                const first = rows[0] || {}
                return (
                  <div key={cid} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
                    <div className="min-w-0">
                      <span className="text-gray-700 font-medium">{cid}</span>
                      <span className="text-gray-400 ml-1 block truncate">{first.gap_description_ko || first.gap_description || '상세 설명 없음'}</span>
                    </div>
                    <span className={`text-[10px] px-2 py-0.5 rounded-full border ${STATUS_KO[first.status] ? STATUS_BADGE[first.status] || '' : 'bg-gray-100 text-gray-500 border-gray-200'}`}>{STATUS_KO[first.status] || first.status}</span>
                  </div>
                )
              })}
              <p className="text-[10px] text-gray-400">이 스냅샷은 평가 시점의 불변 결과입니다. (ResultsJSON)</p>
            </div>
          )
        })()}
      </Modal>
    </div>
  )
}
