import { useEffect, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { Modal, ModalFooter } from '../components/Modal'
import { GovernedActionModal } from '../components/GovernedActionModal'
import { COLOR_KO, INCIDENT_STATE_KO, COLOR_BG, effectiveColor, daySegmentColor, daySegmentLabel, buildNinetyDayBar } from '../publicStatusView'

type Comp = {
  id: string; name_ko: string; active: boolean
  measured_color: string; measured_ko: string
  effective_color: string; effective_ko: string
  override_color: string; override_reason: string; override_expires_at: string
  override_disagrees: boolean
  consecutive_failures: number; consecutive_successes: number
  last_observation_at: string; last_healthy_at: string
  registry_version: number
}
type Inc = {
  id: number; slug: string; title_ko: string; components: string
  state: string; state_ko: string; impact: string
  major: boolean; published: boolean
  last_update_at: string; next_update_due_at: string; update_overdue: boolean
}
type Rollup = {
  date_kst: string; availability_pct: number; impacted_seconds: number
  maintenance_seconds: number; no_data_seconds: number
}

export default function StatusCenter() {
  const [comps, setComps] = useState<Comp[]>([])
  const [incidents, setIncidents] = useState<Inc[]>([])
  const [rollups, setRollups] = useState<Record<string, { days: Rollup[]; uptime_90d_pct: number; rules_ko: string }>>({})
  const [snap, setSnap] = useState<any>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState({ title_ko: '', component: 'patty_code', impact: 'partial', major: false, maintenance: false })
  const [overrideFor, setOverrideFor] = useState<Comp | null>(null)
  const [ov, setOv] = useState({ color: 'orange', reason: '', expires_at: '', false_positive_ack: false })
  const [updateFor, setUpdateFor] = useState<Inc | null>(null)
  const [updateBody, setUpdateBody] = useState('')
  const [ingestOpen, setIngestOpen] = useState(false)
  const [obs, setObs] = useState({ component_id: 'patty_code', impact: 'severe', success: false, region: 'kr-1', window_seconds: 60 })

  const load = () => {
    api.psComponents().then((d: Comp[]) => {
      setComps(Array.isArray(d) ? d : [])
      for (const c of d) {
        api.psRollups(c.id).then((r: any) => setRollups((prev) => ({ ...prev, [c.id]: r }))).catch(() => {})
      }
    }).catch(() => {})
    api.psIncidents().then((d: Inc[]) => setIncidents(Array.isArray(d) ? d : [])).catch(() => {})
    fetch('/api/public/status').then((r) => r.json()).then(setSnap).catch(() => {})
  }
  useEffect(load, [])

  const publishSnapshot = () =>
    api.psPublishSnapshot().then(() => { showToast('공개 스냅샷을 서명 · 발행했습니다'); load() }).catch((e: any) => showToast(e.message || '발행 실패'))

  const rebuildRollups = (id: string) =>
    api.psRebuildRollups(id).then(() => { showToast('일별 가용성을 재계산했습니다'); load() }).catch((e: any) => showToast(e.message || '재계산 실패'))

  const createIncident = () =>
    api.psCreateIncident({ title_ko: form.title_ko, components: [form.component], impact: form.impact, major: form.major, maintenance: form.maintenance })
      .then(() => { setCreateOpen(false); showToast('알림 초안을 생성했습니다'); load() })
      .catch((e: any) => showToast(e.message || '생성 실패'))

  const transition = (inc: Inc, patch: any) =>
    api.psUpdateIncident(inc.id, patch).then(() => { showToast('알림 상태를 변경했습니다'); load() })
      .catch((e: any) => showToast(e.message || '변경 실패'))

  const postUpdate = () => {
    if (!updateFor) return
    api.psPostIncidentUpdate(updateFor.id, { body_ko: updateBody }).then(() => {
      setUpdateFor(null); setUpdateBody(''); showToast('한국어 업데이트를 게시했습니다'); load()
    }).catch((e: any) => showToast(e.message || '게시 실패'))
  }

  const applyOverride = () => {
    if (!overrideFor) return
    api.psOverride(overrideFor.id, ov).then(() => {
      setOverrideFor(null); showToast('상태 재정의를 적용했습니다'); load()
    }).catch((e: any) => showToast(e.message || '재정의 실패'))
  }

  const ingest = () =>
    api.psIngestObservations([obs]).then(() => { setIngestOpen(false); showToast('측정 샘플을 반영했습니다'); load() })
      .catch((e: any) => showToast(e.message || '반영 실패'))

  const stale = snap?.stale === true

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">공개 서비스 상태 <span className="text-xs text-gray-400 ml-2">PAT-1439 · status.patty.io 운영 콘솔</span></h1>
          <p className="text-sm text-gray-500 mt-1">측정 상태는 시스템이, 공개 문구는 사람이 한국어로 작성합니다. 공개 페이지는 마지막 유효 스냅샷을 계속 제공합니다.</p>
        </div>
        <div className="flex gap-2">
          <button className="btn-secondary text-sm" onClick={() => setIngestOpen(true)}>측정 샘플 투입</button>
          <button className="btn-secondary text-sm" onClick={() => setCreateOpen(true)}>알림 초안 작성</button>
          <button className="btn text-sm" onClick={publishSnapshot}>스냅샷 서명 · 발행</button>
        </div>
      </div>

      {/* Public preview: what anonymous visitors see right now */}
      <div className="card p-4">
        <div className="flex items-center justify-between mb-3">
          <h2 className="font-semibold">공개 페이지 미리보기 {stale && <span className="ml-2 text-xs px-2 py-0.5 rounded bg-gray-200 text-gray-700">데이터 만료 — 회색 표시 중</span>}</h2>
          {snap?.generated_at && <span className="text-xs text-gray-400">마지막 발행 {new Date(snap.generated_at).toLocaleString('ko-KR', { timeZone: 'Asia/Seoul' })} KST</span>}
        </div>
        {comps.filter((c) => c.active).map((c) => {
          const r = rollups[c.id]
          const eff = stale ? 'gray' : c.effective_color
          return (
            <div key={c.id} className="py-3 border-t first:border-t-0">
              <div className="flex items-center justify-between gap-4 flex-wrap">
                <div className="flex items-center gap-3">
                  <span className={`inline-block w-3 h-3 rounded-full ${COLOR_BG[eff] || COLOR_BG.gray}`} aria-hidden />
                  <span className="font-medium">{c.name_ko}</span>
                  <span className="text-sm text-gray-600">{stale ? COLOR_KO.gray : c.effective_ko}</span>
                  {c.override_disagrees && (
                    <span className="text-[11px] px-1.5 py-0.5 rounded bg-amber-100 text-amber-800" title={`측정 상태(${c.measured_ko})와 재정의가 다릅니다`}>
                      모니터링 이견: 측정 {c.measured_ko}
                    </span>
                  )}
                </div>
                <span className="text-sm text-gray-500">90일 가용성 {(r?.uptime_90d_pct ?? 0).toFixed(2)}%</span>
              </div>
              {/* 90-day bar: color + accessible per-day labels */}
              <div className="flex gap-[2px] mt-2" role="img" aria-label={`${c.name_ko} 90일 가용성 이력`}>
                {buildNinetyDayBar(r?.days ?? []).map((seg, i) => {
                  const day = r?.days?.find((d) => d.date_kst === seg.date_kst)
                  const color = day ? daySegmentColor(day) : 'gray'
                  return <span key={i} title={day ? daySegmentLabel(day) : '측정 데이터 없음'}
                    className={`h-6 flex-1 min-w-[2px] rounded-sm ${COLOR_BG[color]} opacity-90`} />
                })}
              </div>
              <div className="flex justify-between mt-1.5">
                <button className="text-[11px] text-gray-400 hover:text-gray-600" onClick={() => rebuildRollups(c.id)}>일별 롤업 재계산</button>
                {r?.rules_ko && <span className="text-[11px] text-gray-400 max-w-lg text-right">{r.rules_ko}</span>}
              </div>
            </div>
          )
        })}
      </div>

      {/* Incidents */}
      <div className="card">
        <div className="p-4 border-b flex items-center justify-between">
          <h2 className="font-semibold">공개 알림 · 한국어 수명주기</h2>
          <span className="text-[11px] text-gray-400">주요 장애: 최초 15분 내 업데이트 · 이후 30분 간격</span>
        </div>
        {incidents.length === 0 && <p className="p-4 text-sm text-gray-500">알림이 없습니다.</p>}
        {incidents.map((inc) => (
          <div key={inc.id} className="p-4 border-t flex items-start justify-between gap-4">
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium truncate">{inc.title_ko}</span>
                {inc.major && <span className="text-[11px] px-1.5 py-0.5 rounded bg-red-100 text-red-700">주요</span>}
                {inc.published ? <span className="text-[11px] px-1.5 py-0.5 rounded bg-emerald-100 text-emerald-700">공개</span>
                  : <span className="text-[11px] px-1.5 py-0.5 rounded bg-gray-100 text-gray-600">비공개 초안</span>}
                {inc.update_overdue && <span className="text-[11px] px-1.5 py-0.5 rounded bg-amber-100 text-amber-800">업데이트 지연</span>}
              </div>
              <div className="text-xs text-gray-500 mt-1">
                {inc.state_ko} · 대상 {inc.components}
                {inc.next_update_due_at && inc.state !== 'resolved' && ` · 다음 업데이트 기한 ${new Date(inc.next_update_due_at).toLocaleString('ko-KR', { timeZone: 'Asia/Seoul' })} KST`}
              </div>
            </div>
            <div className="flex gap-2 shrink-0">
              <button className="btn-secondary text-xs" onClick={() => { setUpdateFor(inc); setUpdateBody('') }}>한국어 업데이트</button>
              {!inc.published && <button className="btn-secondary text-xs" onClick={() => transition(inc, { publish: true })}>공개</button>}
              {inc.state !== 'mitigating' && inc.state !== 'resolved' && inc.state !== 'monitoring' && (
                <button className="btn-secondary text-xs" onClick={() => transition(inc, { state: 'mitigating' })}>원인 확인 · 조치 중으로</button>
              )}
              {inc.state !== 'monitoring' && inc.state !== 'resolved' && (
                <button className="btn-secondary text-xs" onClick={() => transition(inc, { state: 'monitoring' })}>안정성 확인 중으로</button>
              )}
              {inc.state !== 'resolved' && <button className="btn-secondary text-xs" onClick={() => transition(inc, { state: 'resolved' })}>정상화</button>}
              {!inc.major && <button className="btn-secondary text-xs" onClick={() => transition(inc, { major: true })}>주요 장애 지정</button>}
            </div>
          </div>
        ))}
      </div>

      {/* Component detail incl. overrides */}
      <div className="card">
        <div className="p-4 border-b"><h2 className="font-semibold">구성 요소 · 측정 상태 및 재정의</h2></div>
        {comps.map((c) => (
          <div key={c.id} className="p-4 border-t flex items-center justify-between gap-4">
            <div>
              <div className="flex items-center gap-2">
                <span className="font-medium">{c.name_ko}</span>
                <span className={`text-[11px] px-1.5 py-0.5 rounded ${c.active ? 'bg-emerald-100 text-emerald-700' : 'bg-gray-100 text-gray-500'}`}>{c.active ? '공개' : '비공개(출시 전)'}</span>
              </div>
              <div className="text-xs text-gray-500 mt-1">
                측정 {c.measured_ko} · 연속 실패 {c.consecutive_failures} / 연속 정상 {c.consecutive_successes}
                {c.last_observation_at && ` · 최근 측정 ${new Date(c.last_observation_at).toLocaleString('ko-KR', { timeZone: 'Asia/Seoul' })}`}
              </div>
              {c.override_color && (
                <div className="text-xs text-amber-700 mt-0.5">재정의 {COLOR_KO[c.override_color]}{c.override_expires_at ? ` · 만료 ${new Date(c.override_expires_at).toLocaleString('ko-KR', { timeZone: 'Asia/Seoul' })}` : ' · 만료 없음(악화)'}</div>
              )}
            </div>
            <div className="flex gap-2">
              <button className="btn-secondary text-xs" onClick={() => api.psActivateComponent(c.id, !c.active).then(load).catch((e: any) => showToast(e.message))}>
                {c.active ? '행 숨기기' : '행 공개'}
              </button>
              <button className="btn-secondary text-xs" onClick={() => { setOverrideFor(c); setOv({ color: 'orange', reason: '', expires_at: '', false_positive_ack: false }) }}>상태 재정의</button>
            </div>
          </div>
        ))}
      </div>

      {/* Create incident */}
      <Modal open={createOpen} title="공개 알림 초안 작성" onClose={() => setCreateOpen(false)} size="md"
        footer={<ModalFooter onCancel={() => setCreateOpen(false)} onConfirm={createIncident} confirmLabel="초안 생성" disabled={!form.title_ko.trim()} />}>
        <div className="space-y-3">
          <div><label className="label">한국어 제목</label>
            <input className="input" value={form.title_ko} onChange={(e) => setForm({ ...form, title_ko: e.target.value })} placeholder="예: 모델 응답 지연 발생" /></div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">대상 구성 요소</label>
              <select className="input" value={form.component} onChange={(e) => setForm({ ...form, component: e.target.value })}>
                {comps.map((c) => <option key={c.id} value={c.id}>{c.name_ko}</option>)}
              </select></div>
            <div><label className="label">측정 영향</label>
              <select className="input" value={form.impact} onChange={(e) => setForm({ ...form, impact: e.target.value })}>
                <option value="partial">일부</option><option value="severe">심각</option><option value="widespread">광범위</option>
              </select></div>
          </div>
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.major} onChange={(e) => setForm({ ...form, major: e.target.checked })} /> 주요 장애 (15분/30분 업데이트 기준 적용)</label>
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.maintenance} onChange={(e) => setForm({ ...form, maintenance: e.target.checked })} /> 점검 예정</label>
          <p className="text-xs text-gray-500">모든 공개 문구는 사람이 한국어로 작성합니다. 초안은 비공개이며, 공개 전에 검토하세요.</p>
        </div>
      </Modal>

      {/* Korean update composer */}
      <Modal open={!!updateFor} title={`한국어 업데이트 게시 · ${updateFor?.title_ko ?? ''}`} onClose={() => setUpdateFor(null)} size="md"
        footer={<ModalFooter onCancel={() => setUpdateFor(null)} onConfirm={postUpdate} confirmLabel="게시" disabled={!updateBody.trim()} />}>
        <div className="space-y-3">
          <div><label className="label">업데이트 내용 (한국어)</label>
            <textarea className="input h-28" value={updateBody} onChange={(e) => setUpdateBody(e.target.value)} placeholder="현재 상태와 조치 내용을 평어로 작성합니다" /></div>
          {updateFor?.update_overdue && <p className="text-xs text-red-600">업데이트 기한이 지났습니다. 즉시 게시하세요.</p>}
          <p className="text-xs text-gray-500">게시 후 주요 장애 수명주기 동안 다음 업데이트 기한이 30분 뒤로 갱신됩니다. 고객 정보, 인증 정보, 내부 구조는 포함할 수 없습니다.</p>
        </div>
      </Modal>

      {/* Observation ingest */}
      <Modal open={ingestOpen} title="측정 샘플 투입 (평가기 입력)" onClose={() => setIngestOpen(false)} size="sm"
        footer={<ModalFooter onCancel={() => setIngestOpen(false)} onConfirm={ingest} confirmLabel="투입" />}>
        <div className="space-y-3">
          <div><label className="label">구성 요소</label>
            <select className="input" value={obs.component_id} onChange={(e) => setObs({ ...obs, component_id: e.target.value })}>
              {comps.map((c) => <option key={c.id} value={c.id}>{c.name_ko}</option>)}
            </select></div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">여정 결과</label>
              <select className="input" value={obs.success ? 'ok' : 'fail'} onChange={(e) => setObs({ ...obs, success: e.target.value === 'ok' })}>
                <option value="ok">성공</option><option value="fail">실패</option>
              </select></div>
            <div><label className="label">영향 수준</label>
              <select className="input" value={obs.impact} onChange={(e) => setObs({ ...obs, impact: e.target.value })}>
                <option value="none">없음</option><option value="partial">일부</option><option value="severe">심각</option><option value="widespread">광범위</option>
              </select></div>
          </div>
          <p className="text-xs text-gray-500">지속 실패/회복 창(플래핑 방지)을 거쳐 측정 색상이 자동 변경됩니다. 최초다 악화 시 비공개 초안과 온콜 호출이 자동 생성됩니다.</p>
        </div>
      </Modal>

      {/* Override: governed action (reason required for healthier) */}
      {overrideFor && (() => {
        const oc = overrideFor
        return (
          <GovernedActionModal
            open
            title={`상태 재정의 · ${oc.name_ko}`}
            subtitle={oc.id}
            preview={
              <div className="space-y-2">
                <p className="text-sm">현재 측정 상태 <b>{oc.measured_ko}</b>, 공개 표시 <b>{oc.effective_ko}</b>.</p>
                <ul className="text-xs text-gray-600 list-disc pl-4 space-y-0.5">
                  <li>악화 재정의는 즉시 적용됩니다</li>
                  <li>개선 재정의는 사유와 만료 시각(7일 이내)이 필요합니다</li>
                  <li>측정 장애 상태에서 녹색 강제는 오탐 명시 확인이 필요하며, 콘솔에는 모니터링 이견이 계속 표시됩니다</li>
                </ul>
                <div className="space-y-3 pt-1">
                  <div><label className="label">재정의 색상</label>
                    <select className="input" value={ov.color} onChange={(e) => setOv({ ...ov, color: e.target.value })}>
                      {['green', 'yellow', 'orange', 'red', 'blue'].map((c) => <option key={c} value={c}>{COLOR_KO[c]}</option>)}
                    </select></div>
                  <div><label className="label">만료 시각 (개선 재정의 필수)</label>
                    <input type="datetime-local" className="input" value={ov.expires_at} onChange={(e) => setOv({ ...ov, expires_at: e.target.value })} /></div>
                  <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={ov.false_positive_ack} onChange={(e) => setOv({ ...ov, false_positive_ack: e.target.checked })} />
                    오탐임을 확인했습니다 (측정 장애 상태에서 녹색 강제 시 필요)</label>
                </div>
              </div>
            }
            confirmLabel="재정의 적용"
            reason={ov.reason}
            onReasonChange={(reason) => setOv({ ...ov, reason })}
            reasonPlaceholder="재정의 사유 (감사에 기록)"
            onCancel={() => setOverrideFor(null)}
            onConfirm={() => {
              const expires = ov.expires_at ? new Date(ov.expires_at).toISOString() : ''
              api.psOverride(oc.id, { ...ov, reason: ov.reason, expires_at: expires }).then(() => {
                setOverrideFor(null); showToast('상태 재정의를 적용했습니다'); load()
              }).catch((e: any) => showToast(e.message || '재정의 실패'))
            }}
          />
        )
      })()}
    </div>
  )
}
