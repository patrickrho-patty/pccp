import { useEffect, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { Modal, ModalFooter } from '../components/Modal'

type Occ = {
  id: number; intended_at: string; state: string; attempts: number
  result_summary_ko: string; cost_tokens: number; deny_reason: string
}
type Sched = {
  id: number; state: string; revision: number
  task_spec: any; trigger: any; next_occurrence_at: string; timezone: string
  occurrences: Occ[]
}
type Cap = {
  capability_id: string; kind: string; display_ko: string
  state: string; cloud_executable: boolean; version: string
}

const SCHED_STATE_KO: Record<string, string> = {
  draft: '초안', active: '활성', paused: '일시중지', authorization_required: '재인증 필요',
  restricted: '제한', completed: '완료', revoked: '철회됨', deleted: '삭제됨',
}
const OCC_STATE_KO: Record<string, string> = {
  pending: '대기', admitted: '승인됨', running: '실행 중', waiting_for_authorization: '재인증 대기',
  succeeded: '성공', failed: '실패', denied: '거부됨', expired: '만료', cancelled: '취소됨', coalesced: '통합됨',
}
const CAP_STATE_KO: Record<string, string> = {
  available: '사용 가능', authorization_required: '연결 필요', insufficient_scope: '범위 부족',
  expired: '만료', revoked: '철회됨', local_only: '로컬 전용', prohibited: '금지됨', unavailable: '사용 불가',
}

export default function Schedules() {
  const [schedules, setSchedules] = useState<Sched[]>([])
  const [caps, setCaps] = useState<Cap[]>([])
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState({ objective: '', kind: 'once', at: '', expr: '0 8 * * 1-5', timezone: 'Asia/Seoul' })

  const load = () => {
    api.csSchedules().then((d: Sched[]) => setSchedules(Array.isArray(d) ? d : [])).catch(() => {})
    api.csCapabilities().then((d: Cap[]) => setCaps(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(() => { load() }, [])

  const create = () => {
    const trigger = form.kind === 'once'
      ? { kind: 'once', at: new Date(form.at).toISOString(), timezone: form.timezone }
      : { kind: 'cron', expr: form.expr, timezone: form.timezone }
    api.csCreate({
      task_spec: { objective: form.objective, success_criteria: '사용자 요청 충족', delivery: 'account' },
      context_snapshot: { frozen: true },
      trigger, timezone: form.timezone,
    }).then((r: any) => {
      setCreateOpen(false); showToast(r.ack_ko || '일정을 등록했습니다'); load()
    }).catch((e: any) => showToast(e.message))
  }

  const mutate = (id: number, action: string) =>
    api.csMutate(id, action).then(() => { showToast('일정 상태를 변경했습니다'); load() }).catch((e: any) => showToast(e.message))

  const sweep = () =>
    api.csDispatch().then((r: any) => { showToast(`승인 ${r.admitted} · 통합 ${r.coalesced} · 24h 초과 만료 ${r.expired_older_than_24h}`); load() })
      .catch((e: any) => showToast(e.message))

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">클라우드 일정 <span className="text-xs text-gray-400 ml-2">PAT-1437 · 퍼블릭 전용 · 하네스 오프라인 실행</span></h1>
          <p className="text-sm text-gray-500 mt-1">등록 시점의 작업 명세와 문맥이 고정되어 Patty 서버에서 실행됩니다. 이후 대화 내용은 자동 반영되지 않습니다.</p>
        </div>
        <div className="flex gap-2">
          <button className="btn-secondary text-sm" onClick={sweep}>발생 디스패치</button>
          <button className="btn text-sm" onClick={() => setCreateOpen(true)}>일정 등록</button>
        </div>
      </div>

      {/* Schedules */}
      {schedules.map((sc) => (
        <div key={sc.id} className="card">
          <div className="p-4 border-b flex items-start justify-between gap-4">
            <div>
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium">{sc.task_spec?.objective}</span>
                <span className={`text-[11px] px-1.5 py-0.5 rounded ${sc.state === 'active' ? 'bg-emerald-100 text-emerald-700' : 'bg-gray-100'}`}>{SCHED_STATE_KO[sc.state] || sc.state}</span>
                <span className="text-[11px] text-gray-400">개정 {sc.revision}</span>
              </div>
              <div className="text-xs text-gray-500 mt-1">
                {sc.trigger?.kind === 'cron' ? `반복 ${sc.trigger.expr}` : '1회'}
                {` · ${sc.timezone}`}
                {sc.next_occurrence_at && ` · 다음 ${new Date(sc.next_occurrence_at).toLocaleString('ko-KR', { timeZone: sc.timezone })}`}
              </div>
            </div>
            <div className="flex gap-2 shrink-0">
              {sc.state === 'active' && <button className="btn-secondary text-xs" onClick={() => mutate(sc.id, 'pause')}>일시중지</button>}
              {sc.state === 'paused' && <button className="btn-secondary text-xs" onClick={() => mutate(sc.id, 'resume')}>재개</button>}
              {sc.state !== 'revoked' && sc.state !== 'deleted' && (
                <button className="btn-secondary text-xs" onClick={() => mutate(sc.id, 'revoke')}>철회</button>
              )}
              {sc.state !== 'deleted' && <button className="btn-secondary text-xs" onClick={() => mutate(sc.id, 'delete')}>삭제</button>}
            </div>
          </div>
          {sc.occurrences?.length > 0 && (
            <div className="max-h-40 overflow-auto text-xs">
              {sc.occurrences.map((o) => (
                <div key={o.id} className="px-4 py-2 border-t flex items-center gap-3">
                  <span className="text-gray-500">{new Date(o.intended_at).toLocaleString('ko-KR')}</span>
                  <span className={`px-1.5 py-0.5 rounded ${o.state === 'succeeded' ? 'bg-emerald-100 text-emerald-700' : o.state === 'denied' || o.state === 'failed' ? 'bg-red-100 text-red-700' : 'bg-gray-100'}`}>
                    {OCC_STATE_KO[o.state] || o.state}
                  </span>
                  {o.attempts > 0 && <span className="text-gray-400">시도 {o.attempts}/3</span>}
                  {o.result_summary_ko && <span className="text-gray-600 truncate">{o.result_summary_ko}</span>}
                  {o.deny_reason && <span className="text-red-500">{o.deny_reason}</span>}
                </div>
              ))}
            </div>
          )}
        </div>
      ))}
      {schedules.length === 0 && <div className="card p-6 text-sm text-gray-500 text-center">일정이 없습니다.</div>}

      {/* Capabilities (metadata only — never credentials) */}
      <div className="card">
        <div className="p-4 border-b"><h2 className="font-semibold">계정 역량 (메타데이터만 표시 — 자격증명 비공개)</h2></div>
        {caps.map((c) => (
          <div key={c.capability_id} className="p-3 border-t flex items-center justify-between text-xs">
            <div className="flex items-center gap-2">
              <span className="font-medium">{c.display_ko || c.capability_id}</span>
              <span className="font-mono text-gray-400">{c.capability_id}</span>
              <span className="px-1.5 py-0.5 rounded bg-gray-100">{c.kind}</span>
              <span className="text-gray-400">v{c.version}</span>
            </div>
            <div className="flex items-center gap-2">
              {!c.cloud_executable && <span className="text-gray-400">클라우드 실행 불가 (로컬 전용)</span>}
              <span className={`px-1.5 py-0.5 rounded ${c.state === 'available' ? 'bg-emerald-100 text-emerald-700' : 'bg-amber-100 text-amber-800'}`}>
                {CAP_STATE_KO[c.state] || c.state}
              </span>
              {c.state !== 'available' && c.cloud_executable && (
                <button className="btn-secondary text-[11px]" onClick={() =>
                  api.csConnect(c.capability_id).then((r: any) => { showToast(r.note_ko || '연결 흐름을 시작했습니다'); load() }).catch((e: any) => showToast(e.message))
                }>연결</button>
              )}
            </div>
          </div>
        ))}
        {caps.length === 0 && <p className="p-4 text-sm text-gray-500">역량이 없습니다.</p>}
      </div>

      {/* Create */}
      <Modal open={createOpen} title="클라우드 일정 등록" onClose={() => setCreateOpen(false)} size="md"
        footer={<ModalFooter onCancel={() => setCreateOpen(false)} onConfirm={create} confirmLabel="등록"
          disabled={!form.objective.trim() || (form.kind === 'once' ? !form.at : !form.expr.trim())} />}>
        <div className="space-y-3">
          <div><label className="label">작업 목표 (한국어) *</label>
            <textarea className="input h-20" value={form.objective} onChange={(e) => setForm({ ...form, objective: e.target.value })}
              placeholder="예: 매일 아침 배포 상태를 점검하고 요약을 전달해줘" /></div>
          <div className="grid grid-cols-3 gap-3">
            <div><label className="label">반복</label>
              <select className="input" value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
                <option value="once">1회</option><option value="cron">반복 (cron)</option>
              </select></div>
            <div><label className="label">{form.kind === 'once' ? '실행 시각 *' : 'cron 식 *'}</label>
              {form.kind === 'once'
                ? <input type="datetime-local" className="input" value={form.at} onChange={(e) => setForm({ ...form, at: e.target.value })} />
                : <input className="input font-mono" value={form.expr} onChange={(e) => setForm({ ...form, expr: e.target.value })} placeholder="0 8 * * 1-5" />}</div>
            <div><label className="label">시간대</label>
              <input className="input" value={form.timezone} onChange={(e) => setForm({ ...form, timezone: e.target.value })} /></div>
          </div>
          <p className="text-xs text-gray-500">
            등록 시점의 대화 문맥이 스냅샷으로 고정됩니다. 로컬 파일 · 로컬 전용 MCP · 기기 쿠키는 사용할 수 없으며, 연결된 클라우드 역량만 사용됩니다.
            결과적 조치는 등록 시 명시적으로 좁힐 때만 무인 실행됩니다. 금지 용도(자격증명 탈취 · 피싱 · 스팸 · 멀웨어 · 불법 감시 등)는 등록이 거부됩니다.
          </p>
        </div>
      </Modal>
    </div>
  )
}
