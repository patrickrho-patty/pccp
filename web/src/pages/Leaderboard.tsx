import { useState, useEffect, useMemo } from 'react'
import { api } from '../api'
import { Modal, ModalFooter } from '../components/Modal'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'

// Leaderboard (PAT-1440) — evidence-backed workforce scorecard. Four scored
// properties (accepted delivery, first-pass quality, security/governance
// adherence, delivery efficiency) with tenant weights within enforced bounds,
// cohort categories with minimum evidence, rubric versioning frozen at period
// start, and human only reviews — never an automatic employment action.

type Row = {
  id: string
  period_id: string
  subject_id: string
  cohort?: string
  delivery_score: number
  quality_score: number
  security_score: number
  efficiency_score: number
  overall_score: number
  rank: number
  percentile?: number
  evidence_count: number
  accepted_outcomes: number
  governed_actions: number
  confirmed_violations: number
  confidence: string
  explanation?: string
  state?: string
}

type Period = {
  id: string
  name?: string
  name_ko?: string
  period_type: string
  start_at: string
  end_at: string
  status: string
}

type Rubric = {
  id: string
  name?: string
  version: number
  weight_delivery: number
  weight_quality: number
  weight_security: number
  weight_efficiency: number
  status?: string
}

export default function Leaderboard() {
  const confirm = useConfirm()
  const [periods, setPeriods] = useState<Period[]>([])
  const [periodId, setPeriodId] = useState('')
  const [rows, setRows] = useState<Row[]>([])
  const [rubrics, setRubrics] = useState<Rubric[]>([])
  const [cohortFilter, setCohortFilter] = useState('')
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(true)
  const [generating, setGenerating] = useState(false)
  // Modals
  const [rubricModal, setRubricModal] = useState(false)
  const [rubricForm, setRubricForm] = useState({ name: '', name_ko: '', weight_delivery: 30, weight_quality: 25, weight_security: 30, weight_efficiency: 15 })
  const [periodModal, setPeriodModal] = useState(false)
  const [periodForm, setPeriodForm] = useState({ name_ko: '', period_type: 'rolling_90d', start_at: '', end_at: '' })
  const [objectiveModal, setObjectiveModal] = useState(false)
  const [objectiveForm, setObjectiveForm] = useState({ objective_id: '', work_type: 'feature', size_band: 'medium', owner_id: '', status: 'accepted', accepted_at: new Date().toISOString().slice(0, 10) })
  const [rowModal, setRowModal] = useState<Row | null>(null)
  const [reviewForm, setReviewForm] = useState({ decision: 'retain', rationale: '' })
  const [correctionForm, setCorrectionForm] = useState({ kind: 'dispute', reason: '' })

  const loadPeriods = () => {
    api.listLeaderboardPeriods().then(setPeriods).catch(() => {})
  }
  useEffect(() => { loadPeriods(); api.listLeaderboardRubrics().then(setRubrics).catch(() => {}) }, [])

  const loadRows = (pid = periodId) => {
    if (!pid) return
    setLoading(true)
    const params = new URLSearchParams({ period_id: pid })
    if (cohortFilter) params.set('cohort', cohortFilter)
    if (search) params.set('subject_id', search)
    api.listLeaderboard(`?${params.toString()}`).then((data) => {
      setRows(Array.isArray(data) ? data : [])
      setLoading(false)
    }).catch(() => setLoading(false))
  }
  useEffect(() => { loadRows() }, [periodId])

  const cohorts = useMemo(() => Array.from(new Set(rows.map(r => r.cohort || 'org'))), [rows])

  const generate = async () => {
    if (!periodId) return
    setGenerating(true)
    try {
      const out = await api.generateLeaderboard(periodId)
      showToast(`산출 완료 — ${out.generated ?? 0}명`, 'success')
      loadRows()
    } catch (e: any) { showToast(e.message || '산출 실패', 'error') }
    finally { setGenerating(false) }
  }

  const freeze = async () => {
    if (!periodId) return
    if (!await confirm({ title: '기간 동결', message: '이 기간을 동결하면 공식 점수가 불변화됩니다. 계속할까요?', danger: true })) return
    try {
      await api.freezeLeaderboard(periodId)
      showToast('동결됨 (점수 불변)', 'success')
      loadPeriods()
    } catch (e: any) { showToast(e.message || '동결 실패', 'error') }
  }

  const saveRubric = async () => {
    const sum = rubricForm.weight_delivery + rubricForm.weight_quality + rubricForm.weight_security + rubricForm.weight_efficiency
    if (sum !== 100) { showToast('가중치 합계가 100이어야 합니다', 'error'); return }
    if (rubricForm.weight_quality < 20 || rubricForm.weight_security < 20) { showToast('품질·보안은 각각 20% 이상', 'error'); return }
    try {
      await api.saveLeaderboardRubric(rubricForm)
      showToast('룹릭 저장됨 — 새 버전', 'success')
      setRubricModal(false)
      api.listLeaderboardRubrics().then(setRubrics).catch(() => {})
    } catch (e: any) { showToast(e.message || '저장 실패', 'error') }
  }

  const submitReview = async () => {
    if (!rowModal) return
    try {
      await api.submitLeaderboardReview({ period_id: rowModal.period_id, subject_id: rowModal.subject_id, ...reviewForm })
      showToast('검토 기록됨 (자동 승진 권고 아님)', 'success')
      setRowModal(null)
    } catch (e: any) { showToast(e.message || '실패', 'error') }
  }

  const submitCorrection = async () => {
    if (!rowModal) return
    try {
      await api.submitLeaderboardCorrection({ period_id: rowModal.period_id, subject_id: rowModal.subject_id, ...correctionForm })
      showToast('신청 접수됨', 'success')
      setCorrectionForm({ kind: 'dispute', reason: '' })
    } catch (e: any) { showToast(e.message || '실패', 'error') }
  }

  const confBadge = (c: string) => c === 'insufficient'
    ? <span className="text-[10px] px-2 py-0.5 rounded-full border bg-gray-100 text-gray-500 border-gray-200">근거 부족</span>
    : <span className="text-[10px] px-2 py-0.5 rounded-full border bg-green-50 text-green-700 border-green-200">검증</span>

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
        <div>
          <h1 className="text-xl font-semibold">워크 인텔리전스 리더보드</h1>
          <p className="text-xs text-gray-500 mt-1">
            근거 기반 역량 점수. 4개 속성(수용 배달·1차 품질·보안 준수·전달 효율)은 항상 검증 가능한 감사/증거에서 산출되며,
            원시 라인·세시녀·토큰 수는 점수를 직접 높이지 않습니다. 순위는 후보군 내 동급 비교이며 자동 인사 조치는 없습니다.
          </p>
        </div>
        <div className="flex gap-2">
          <button className="btn-sm btn-secondary" onClick={() => setPeriodModal(true)}>+ 기간</button>
          <button className="btn-sm btn-secondary" onClick={() => setRubricModal(true)}>룹릭</button>
          <button className="btn-sm btn-secondary" onClick={() => setObjectiveModal(true)}>+ 목표</button>
          <button className="btn-sm btn-primary" onClick={generate} disabled={generating}>{generating ? '산출 중…' : '⚙ 점수 산출'}</button>
        </div>
      </div>

      <div className="flex gap-2 mb-3 flex-wrap">
        <select className="input max-w-[240px] text-xs" value={periodId} onChange={e => setPeriodId(e.target.value)}>
          <option value="">리뷰 기간 선택</option>
          {periods.map(p => <option key={p.id} value={p.id}>{p.name_ko || p.name || p.id} · {p.status} · {p.period_type}</option>)}
        </select>
        <select className="input max-w-[180px] text-xs" value={cohortFilter} onChange={e => { setCohortFilter(e.target.value); loadRows(periodId) }}>
          <option value="">전체 코호트</option>
          {cohorts.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <input className="input max-w-[180px] text-xs" placeholder="subject_id 검색" value={search} onChange={e => setSearch(e.target.value)} />
        <button className="btn-sm btn-secondary" onClick={() => loadRows()}>조회</button>
        {periods.find(p => p.id === periodId)?.status === 'running' && (
          <button className="btn-sm btn-danger ml-auto" onClick={freeze}>동결</button>
        )}
      </div>

      {rubrics.length > 0 && (
        <div className="flex gap-2 mb-3 flex-wrap text-[10px]">
          {rubrics.slice(0, 3).map(r => (
            <span key={r.id} className="px-2 py-0.5 rounded-full border border-gray-200 text-gray-600">
              룹릭 v{r.version} · 수용 {r.weight_delivery}% 품질 {r.weight_quality}% 보안 {r.weight_security}% 효율 {r.weight_efficiency}%
            </span>
          ))}
        </div>
      )}

      <div className="card overflow-x-auto">
        {loading ? <p className="text-sm text-gray-400 p-4">불러오는 중…</p> : rows.length === 0 ? (
          <p className="text-sm text-gray-400 p-4">기간을 선택하고 점수를 산출하세요. (5개 이상 수용 결과 + 20회 이상 거버넌스 액션 미만은 ‘근거 부족’으로 순위에서 제외됩니다)</p>
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-gray-500 border-b border-gray-100">
                <th className="py-2 px-2">순위</th>
                <th className="py-2 px-2">사용자</th>
                <th className="py-2 px-2">코호트</th>
                <th className="py-2 px-2 text-right">수용</th>
                <th className="py-2 px-2 text-right">품질</th>
                <th className="py-2 px-2 text-right">보안</th>
                <th className="py-2 px-2 text-right">효율</th>
                <th className="py-2 px-2 text-right">종합</th>
                <th className="py-2 px-2">증거/확신</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(r => (
                <tr key={r.id} className="border-b border-gray-50 hover:bg-gray-50/50 cursor-pointer" onClick={() => setRowModal(r)}>
                  <td className="py-2 px-2 font-medium">{r.confidence === 'insufficient' ? '-' : r.rank || '-'}</td>
                  <td className="py-2 px-2 font-mono">{r.subject_id}</td>
                  <td className="py-2 px-2 text-gray-500">{r.cohort || 'org'}</td>
                  <td className="py-2 px-2 text-right">{r.confidence === 'insufficient' ? '-' : Math.round(r.delivery_score)}</td>
                  <td className="py-2 px-2 text-right">{r.confidence === 'insufficient' ? '-' : Math.round(r.quality_score)}</td>
                  <td className="py-2 px-2 text-right">{r.confidence === 'insufficient' ? '-' : Math.round(r.security_score)}</td>
                  <td className="py-2 px-2 text-right">{r.confidence === 'insufficient' ? '-' : Math.round(r.efficiency_score)}</td>
                  <td className="py-2 px-2 text-right font-semibold">{r.confidence === 'insufficient' ? '-' : Math.round(r.overall_score)}</td>
                  <td className="py-2 px-2">
                    <div className="flex items-center gap-1">
                      {confBadge(r.confidence)}
                      <span className="text-gray-400">{r.evidence_count}건</span>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {rubricModal && (
        <Modal title="룹릭 편집 (버전 새로 생성)" onClose={() => setRubricModal(false)}>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">이름</label><input className="input text-xs" value={rubricForm.name} onChange={e => setRubricForm({ ...rubricForm, name: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">한국어 이름</label><input className="input text-xs" value={rubricForm.name_ko} onChange={e => setRubricForm({ ...rubricForm, name_ko: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">수용 배달 %</label><input type="number" className="input text-xs" value={rubricForm.weight_delivery} onChange={e => setRubricForm({ ...rubricForm, weight_delivery: +e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">1차 품질 % (최소 20)</label><input type="number" className="input text-xs" value={rubricForm.weight_quality} onChange={e => setRubricForm({ ...rubricForm, weight_quality: +e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">보안 준수 % (최소 20)</label><input type="number" className="input text-xs" value={rubricForm.weight_security} onChange={e => setRubricForm({ ...rubricForm, weight_security: +e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">전달 효율 %</label><input type="number" className="input text-xs" value={rubricForm.weight_efficiency} onChange={e => setRubricForm({ ...rubricForm, weight_efficiency: +e.target.value })} /></div>
            </div>
            <p className="text-[10px] text-gray-400">합계 100% · 품질/보안 각 20% 이상 · 단일 속성 40% 이하 (Patty 상한). 기간 시작 시 동결되며 변경은 신규 버전으로만 적용됩니다.</p>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setRubricModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={saveRubric}>저장</button>
          </ModalFooter>
        </Modal>
      )}

      {periodModal && (
        <Modal title="새 리뷰 기간" onClose={() => setPeriodModal(false)}>
          <div className="space-y-3">
            <div><label className="text-xs text-gray-500">이름 (한국어)</label><input className="input text-xs" value={periodForm.name_ko} onChange={e => setPeriodForm({ ...periodForm, name_ko: e.target.value })} /></div>
            <div>
              <label className="text-xs text-gray-500">유형</label>
              <select className="input text-xs" value={periodForm.period_type} onChange={e => setPeriodForm({ ...periodForm, period_type: e.target.value })}>
                <option value="rolling_90d">최근 90일</option>
                <option value="fixed_quarter">고정 분기</option>
              </select>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">시작</label><input type="date" className="input text-xs" value={periodForm.start_at} onChange={e => setPeriodForm({ ...periodForm, start_at: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">종료</label><input type="date" className="input text-xs" value={periodForm.end_at} onChange={e => setPeriodForm({ ...periodForm, end_at: e.target.value })} /></div>
            </div>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setPeriodModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={async () => {
              if (!periodForm.start_at || !periodForm.end_at) { showToast('시작/종료 필요', 'error'); return }
              try {
                await api.createLeaderboardPeriod({ ...periodForm, name: periodForm.name_ko, start_at: periodForm.start_at + 'T00:00:00Z', end_at: periodForm.end_at + 'T23:59:59Z' })
                showToast('기간 생성됨', 'success')
                setPeriodModal(false)
                loadPeriods()
              } catch (e: any) { showToast(e.message || '실패', 'error') }
            }}>생성</button>
          </ModalFooter>
        </Modal>
      )}

      {objectiveModal && (
        <Modal title="목표 등록 (사전 선언)" onClose={() => setObjectiveModal(false)}>
          <div className="space-y-3 max-h-[70vh] overflow-y-auto">
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">목표 ID</label><input className="input text-xs font-mono" value={objectiveForm.objective_id} onChange={e => setObjectiveForm({ ...objectiveForm, objective_id: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">담당자 ID</label><input className="input text-xs font-mono" value={objectiveForm.owner_id} onChange={e => setObjectiveForm({ ...objectiveForm, owner_id: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">작업 유형</label>
                <select className="input text-xs" value={objectiveForm.work_type} onChange={e => setObjectiveForm({ ...objectiveForm, work_type: e.target.value })}>
                  <option value="defect">결함</option><option value="feature">기능</option><option value="refactor">리팩터링</option><option value="security">보안</option><option value="documentation">문서</option>
                </select></div>
              <div><label className="text-xs text-gray-500">크기</label>
                <select className="input text-xs" value={objectiveForm.size_band} onChange={e => setObjectiveForm({ ...objectiveForm, size_band: e.target.value })}>
                  <option value="small">소</option><option value="medium">중</option><option value="large">대</option>
                </select></div>
              <div><label className="text-xs text-gray-500">상태</label>
                <select className="input text-xs" value={objectiveForm.status} onChange={e => setObjectiveForm({ ...objectiveForm, status: e.target.value })}>
                  <option value="open">진행</option><option value="accepted">수용</option><option value="rejected">거부</option><option value="reverted">되돌림</option>
                </select></div>
              <div><label className="text-xs text-gray-500">수용일</label><input type="date" className="input text-xs" value={objectiveForm.accepted_at} onChange={e => setObjectiveForm({ ...objectiveForm, accepted_at: e.target.value })} /></div>
            </div>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setObjectiveModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={async () => {
              if (!objectiveForm.objective_id || !objectiveForm.owner_id) { showToast('목표/담당자 ID 필요', 'error'); return }
              try {
                await api.saveLeaderboardObjective({ ...objectiveForm, accepted_at: objectiveForm.accepted_at + 'T12:00:00Z', started_at: objectiveForm.accepted_at + 'T00:00:00Z' })
                showToast('목표 등록됨', 'success')
                setObjectiveModal(false)
              } catch (e: any) { showToast(e.message || '실패', 'error') }
            }}>등록</button>
          </ModalFooter>
        </Modal>
      )}

      {rowModal && (
        <Modal title={`점수 상세 — ${rowModal.subject_id}`} onClose={() => setRowModal(null)}>
          <div className="space-y-3 max-h-[65vh] overflow-y-auto">
            <div className="grid grid-cols-2 gap-2 text-xs">
              <div className="border rounded p-2 bg-blue-50/40"><div className="text-gray-500">수용 배달</div><div className="text-lg font-semibold">{rowModal.confidence === 'insufficient' ? '-' : Math.round(rowModal.delivery_score)}</div></div>
              <div className="border rounded p-2 bg-green-50/40"><div className="text-gray-500">1차 품질</div><div className="text-lg font-semibold">{rowModal.confidence === 'insufficient' ? '-' : Math.round(rowModal.quality_score)}</div></div>
              <div className="border rounded p-2 bg-red-50/40"><div className="text-gray-500">보안 준수</div><div className="text-lg font-semibold">{rowModal.confidence === 'insufficient' ? '-' : Math.round(rowModal.security_score)}</div></div>
              <div className="border rounded p-2 bg-gray-50"><div className="text-gray-500">전달 효율</div><div className="text-lg font-semibold">{rowModal.confidence === 'insufficient' ? '-' : Math.round(rowModal.efficiency_score)}</div></div>
            </div>
            <div className="text-xs text-gray-600 border rounded p-2 bg-gray-50 whitespace-pre-wrap">{rowModal.explanation || '—'}</div>
            <div className="flex gap-2 text-[10px] text-gray-500">
              <span>수용 {rowModal.accepted_outcomes}</span>·<span>거버넌스 액션 {rowModal.governed_actions}</span>·<span>확정 위반 {rowModal.confirmed_violations}</span>·<span>증거 {rowModal.evidence_count}</span>
            </div>

            <h3 className="text-xs font-medium border-t pt-2">인간 검토 기록 (점수와 분리)</h3>
            <div className="grid grid-cols-2 gap-2">
              <select className="input text-xs" value={reviewForm.decision} onChange={e => setReviewForm({ ...reviewForm, decision: e.target.value })}>
                <option value="retain">유지</option>
                <option value="promotion_review">승진 검토 참고</option>
                <option value="documented">문서화</option>
              </select>
              <input className="input text-xs" placeholder="독립 검토 근거" value={reviewForm.rationale} onChange={e => setReviewForm({ ...reviewForm, rationale: e.target.value })} />
            </div>
            <button className="btn-sm btn-primary w-full" onClick={submitReview}>검토 기록 (자동 승진 권고 아님)</button>

            <h3 className="text-xs font-medium border-t pt-2">정정 / 이의 제기</h3>
            <div className="grid grid-cols-2 gap-2">
              <select className="input text-xs" value={correctionForm.kind} onChange={e => setCorrectionForm({ ...correctionForm, kind: e.target.value })}>
                <option value="dispute">이의 제기</option>
                <option value="correction">정정</option>
                <option value="appeal">항소</option>
              </select>
              <input className="input text-xs" placeholder="사유 / 증거 참조" value={correctionForm.reason} onChange={e => setCorrectionForm({ ...correctionForm, reason: e.target.value })} />
            </div>
            <button className="btn-sm btn-secondary w-full" onClick={submitCorrection}>신청 접수</button>
          </div>
        </Modal>
      )}
    </div>
  )
}
