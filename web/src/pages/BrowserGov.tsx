import { useEffect, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'

type Policy = { version: number; policy: any; signature?: string; foundations_ko?: string[] }
type Task = {
  id: number; task_id: string; user_id: string; harness_id: string; session_id: string
  tabs_json: string; goal_ko: string; lease_id: string; policy_version: number
  state: string; outcome: string; created_at: string
}
type Timeline = {
  task: Task
  events: Array<{ id: number; action: string; risk_class: string; target_summary: string; origin: string; result: string; occurred_at: string; effect_op_id: string }>
}

const STATE_KO: Record<string, string> = {
  active: '활성', waiting_approval: '승인 대기 (일시정지)', completed: '완료',
  cancelled: '취소됨', failed: '실패', taken_over: '사용자 개입 전환',
}
const APSTATE_KO: Record<string, string> = {
  pending: '대기', approved: '승인됨', denied: '거부됨', expired: '만료', used: '사용됨',
}
const RISK_KO: Record<string, string> = {
  read_only: '읽기 전용', reversible: '가역', high_impact: '고영향', mandatory_takeover: '개입 필수',
}

export default function BrowserGov() {
  const [policy, setPolicy] = useState<Policy | null>(null)
  const [draft, setDraft] = useState('')
  const [tasks, setTasks] = useState<Task[]>([])
  const [timeline, setTimeline] = useState<Timeline | null>(null)

  const load = () => {
    api.bgPolicy().then((p: Policy) => { setPolicy(p); setDraft(JSON.stringify(p.policy, null, 2)) }).catch(() => {})
    api.bgTasks().then((d: Task[]) => setTasks(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(load, [])

  const publish = () => {
    try { JSON.parse(draft) } catch { showToast('정책 JSON이 올바르지 않습니다'); return }
    api.bgPutPolicy(draft).then(() => { showToast('새 정책 버전을 서명·발행했습니다'); load() })
      .catch((e: any) => showToast(e.message))
  }

  const openTimeline = (taskId: string) =>
    api.bgTaskTimeline(taskId).then(setTimeline).catch((e: any) => showToast(e.message))

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">브라우저 거버넌스 <span className="text-xs text-gray-400 ml-2">PAT-1448 · 관리 정책 · 과업 · 승인 · 증거</span></h1>
          <p className="text-sm text-gray-500 mt-1">터미널이 명령·승인 권위이고 브라우저는 실행 표면입니다. 모델은 대상·권한을 스스로 확장할 수 없으며, 비밀번호·결제·CAPTCHA·MFA는 사용자 개입 없이는 수행되지 않습니다.</p>
        </div>
        <button className="btn text-sm" onClick={publish}>정책 버전 서명 · 발행</button>
      </div>

      {/* Foundations */}
      {policy?.foundations_ko && (
        <div className="card p-3 flex flex-wrap gap-1.5 items-center">
          <span className="text-xs text-gray-500 mr-1">비활성화 불가 기반:</span>
          {policy.foundations_ko.map((f) => (
            <span key={f} className="text-[11px] px-1.5 py-0.5 rounded bg-gray-100">{f}</span>
          ))}
        </div>
      )}

      <div className="grid grid-cols-2 gap-4">
        {/* Policy editor */}
        <div className="card">
          <div className="p-3 border-b flex items-center justify-between">
            <h2 className="font-semibold text-sm">관리 정책 v{policy?.version ?? 0}</h2>
            {policy?.signature && <span className="text-[11px] text-gray-400 font-mono">서명 {policy.signature.slice(0, 16)}…</span>}
          </div>
          <textarea className="w-full h-72 p-3 font-mono text-[11px] border-0 focus:ring-0 rounded-b-lg"
            value={draft} onChange={(e) => setDraft(e.target.value)} spellCheck={false} />
          <p className="px-3 pb-2 text-[11px] text-gray-400">대상은 https만 허용되며, 개입 필수 동작(password_entry·payment_entry·captcha·mfa·identity_verification)은 정책으로 완화할 수 없습니다.</p>
        </div>

        {/* Approvals pending — derived from waiting tasks */}
        <div className="card">
          <div className="p-3 border-b"><h2 className="font-semibold text-sm">승인 대기 과업</h2></div>
          {tasks.filter((t) => t.state === 'waiting_approval').map((t) => (
            <div key={t.task_id} className="p-3 border-t">
              <div className="text-sm font-medium">{t.goal_ko}</div>
              <div className="text-[11px] text-gray-500 mt-0.5">과업 {t.task_id.slice(0, 14)}… · 정책 v{t.policy_version}</div>
              <button className="btn-secondary text-xs mt-2"
                onClick={() => openTimeline(t.task_id)}>증거 타임라인 열기</button>
            </div>
          ))}
          {tasks.filter((t) => t.state === 'waiting_approval').length === 0 &&
            <p className="p-3 text-sm text-gray-500">승인 대기 중인 과업이 없습니다.</p>}
          <p className="px-3 py-2 text-[11px] text-gray-400 border-t">승인 결정는 하네스 터미널에서 이루어집니다 (터미널 = 승인 권위). 이 콘솔은 관리·감시 전용입니다.</p>
        </div>
      </div>

      {/* Tasks */}
      <div className="card">
        <div className="p-3 border-b"><h2 className="font-semibold text-sm">위임 과업</h2></div>
        {tasks.length === 0 && <p className="p-3 text-sm text-gray-500">과업이 없습니다.</p>}
        {tasks.map((t) => (
          <div key={t.task_id} className="p-3 border-t flex items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium text-sm truncate">{t.goal_ko}</span>
                <span className={`text-[11px] px-1.5 py-0.5 rounded ${t.state === 'active' ? 'bg-emerald-100 text-emerald-700' : t.state === 'waiting_approval' ? 'bg-amber-100 text-amber-800' : 'bg-gray-100'}`}>
                  {STATE_KO[t.state] || t.state}
                </span>
                <span className="text-[11px] text-gray-400">정책 v{t.policy_version} · 탭 {(JSON.parse(t.tabs_json || '[]')).length}개 연결</span>
              </div>
              <div className="text-[11px] text-gray-400 mt-0.5 font-mono">
                {t.task_id.slice(0, 18)}… · 하네스 {t.harness_id.slice(0, 10)}… · 리스 {t.lease_id.slice(0, 12)}…
              </div>
            </div>
            <button className="btn-secondary text-xs shrink-0" onClick={() => openTimeline(t.task_id)}>타임라인</button>
          </div>
        ))}
      </div>

      {/* Timeline drawer */}
      {timeline && (
        <div className="card">
          <div className="p-3 border-b flex items-center justify-between">
            <h2 className="font-semibold text-sm">증거 타임라인 · {timeline.task.goal_ko}</h2>
            <button className="text-xs text-gray-400" onClick={() => setTimeline(null)}>닫기</button>
          </div>
          <div className="max-h-72 overflow-auto text-xs">
            {timeline.events.length === 0 && <p className="p-3 text-gray-500">기록된 이벤트가 없습니다.</p>}
            {timeline.events.map((e) => (
              <div key={e.id} className="px-3 py-2 border-t flex items-center gap-3">
                <span className="text-gray-400 w-36 shrink-0">{new Date(e.occurred_at).toLocaleString('ko-KR')}</span>
                <span className="px-1.5 py-0.5 rounded bg-gray-100">{e.action}</span>
                <span className="text-gray-500">{RISK_KO[e.risk_class] || e.risk_class}</span>
                <span className="truncate flex-1">{e.target_summary}</span>
                <span className="font-mono text-gray-400">{e.origin}</span>
                <span className={`px-1.5 py-0.5 rounded ${e.result === 'ok' ? 'bg-emerald-50 text-emerald-700' : 'bg-red-50 text-red-600'}`}>{e.result}</span>
              </div>
            ))}
          </div>
          <p className="px-3 py-2 text-[11px] text-gray-400 border-t">모든 이벤트는 정책 버전·그랜트 다이제스트·승인·효과 연산 ID에 귀속됩니다. 출처는 scheme://host로 재편집되어 저장됩니다.</p>
        </div>
      )}
    </div>
  )
}
