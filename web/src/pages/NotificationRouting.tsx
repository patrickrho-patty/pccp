import { useEffect, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { Modal, ModalFooter } from '../components/Modal'

type Policy = {
  routing_json: string; managed_by_json: string
  ack_deadline_minutes: number; escalation_steps: number
  quiet_hours_start: string; quiet_hours_end: string; air_gapped: boolean
}
type Group = { id?: number; name: string; members_json: string; escalation_order: number; timezone: string }
type Channel = { id: number; channel: string; managed_by: string; masked_endpoint: string; verified: boolean; healthy: boolean; last_failure: string }
type Incident = {
  id: number; fingerprint: string; source_type: string; service: string; rule: string
  severity: string; title_ko: string; safe_summary_ko: string
  state: string; escalation_step: number; acked_by: string; acked_via: string
  first_seen_at: string; last_seen_at: string
}
type Job = { id: number; incident_id: number; kind: string; channel: string; target: string; state: string; attempts: number; last_error: string }

const SEV_KO: Record<string, string> = { critical: '긴급', high: '높음', medium: '보통', low: '낮음' }
const SEV_BG: Record<string, string> = { critical: 'bg-red-100 text-red-700', high: 'bg-orange-100 text-orange-700', medium: 'bg-amber-100 text-amber-700', low: 'bg-gray-100 text-gray-600' }
const STATE_KO: Record<string, string> = { open: '열림', acknowledged: '확인됨', escalated: '에스컬레이션', resolved: '해결됨', suppressed: '억제됨' }
const JOB_STATE_KO: Record<string, string> = { queued: '대기', sent: '발송됨', failed: '실패', dead_letter: '배달 불가', cancelled: '취소됨' }

export default function NotificationRouting() {
  const [policy, setPolicy] = useState<Policy | null>(null)
  const [groups, setGroups] = useState<Group[]>([])
  const [channels, setChannels] = useState<Channel[]>([])
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [jobs, setJobs] = useState<Job[]>([])
  const [health, setHealth] = useState<any>(null)
  const [groupOpen, setGroupOpen] = useState(false)
  const [group, setGroup] = useState<Group>({ name: '', members_json: '[{"kind":"email","target":"","verified":false}]', escalation_order: 1, timezone: 'Asia/Seoul' })
  const [channelOpen, setChannelOpen] = useState(false)
  const [channel, setChannel] = useState({ channel: 'email', managed_by: 'customer', endpoint: '' })
  const [sourceOpen, setSourceOpen] = useState(false)
  const [source, setSource] = useState({ source_type: 'security_finding', service: '', rule: '', severity: 'high', title_ko: '', safe_summary_ko: '' })
  const [testOpen, setTestOpen] = useState(false)
  const [test, setTest] = useState({ channel: 'email', target: '' })

  const load = () => {
    api.inPolicy().then((d: any) => setPolicy(d)).catch(() => {})
    api.inGroups().then((d: Group[]) => setGroups(Array.isArray(d) ? d : [])).catch(() => {})
    api.inChannels().then((d: Channel[]) => setChannels(Array.isArray(d) ? d : [])).catch(() => {})
    api.inIncidents().then((d: Incident[]) => setIncidents(Array.isArray(d) ? d : [])).catch(() => {})
    api.inJobs().then((d: Job[]) => setJobs(Array.isArray(d) ? d : [])).catch(() => {})
    api.inHealth().then((h: any) => {
      // unhealthy_channels arrives as a JSON string; parse for the count.
      let unhealthy: string[] = []
      try { unhealthy = JSON.parse(h?.unhealthy_channels || '[]') } catch { /* keep [] */ }
      setHealth({ ...h, unhealthy_channels: unhealthy })
    }).catch(() => {})
  }
  useEffect(load, [])

  const routing = policy ? JSON.parse(policy.routing_json || '{}') : {}
  const managedBy = policy ? JSON.parse(policy.managed_by_json || '{}') : {}

  const savePolicy = () =>
    api.inSavePolicy(policy!).then(() => { showToast('알림 정책을 저장했습니다'); load() })
      .catch((e: any) => showToast(e.message || '정책 저장 실패'))

  const ack = (inc: Incident) =>
    api.inAck(inc.id).then(() => { showToast('알림을 확인 처리했습니다'); load() }).catch((e: any) => showToast(e.message))
  const resolve = (inc: Incident) =>
    api.inResolve(inc.id).then(() => { showToast('알림을 해결 처리했습니다'); load() }).catch((e: any) => showToast(e.message))

  const dispatch = () =>
    api.inDispatch().then((r: any) => { showToast(`발송 ${r.sent}건 · 실패 ${r.failed}건 · 배달불가 ${r.dead_letter}건`); load() })
      .catch((e: any) => showToast(e.message))

  const sweep = () =>
    api.inEscalationSweep().then((r: any) => { showToast(`에스컬레이션 ${r.escalated}건 진행`); load() })
      .catch((e: any) => showToast(e.message))

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">알림 라우팅 <span className="text-xs text-gray-400 ml-2">PAT-1454 · SMS / 이메일 / Slack</span></h1>
          <p className="text-sm text-gray-500 mt-1">보안 탐지, 장애, 시스템 오류를 하나의 알림 정체성으로 통합해 관리자에게 전달합니다. 외부로 나가는 내용은 최소 안전 필드로 제한됩니다.</p>
        </div>
        <div className="flex gap-2">
          <button className="btn-secondary text-sm" onClick={sweep}>에스컬레이션 점검</button>
          <button className="btn-secondary text-sm" onClick={dispatch}>발송 큐 처리</button>
          <button className="btn text-sm" onClick={() => setSourceOpen(true)}>소스 이벤트 등록</button>
        </div>
      </div>

      {/* Health summary */}
      {health && (
        <div className="grid grid-cols-4 gap-3">
          {[
            { label: '대기 큐', value: health.queue_depth },
            { label: '배달 불가', value: health.dead_letters },
            { label: '24시간 실패', value: health.failures_24h },
            { label: '비정상 채널', value: health.unhealthy_channels?.length ?? 0 },
          ].map((m) => (
            <div key={m.label} className="card p-3">
              <div className="text-xs text-gray-500">{m.label}</div>
              <div className="text-xl font-bold mt-0.5">{m.value}</div>
            </div>
          ))}
        </div>
      )}

      {/* Policy */}
      <div className="card p-4 space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold">심각도별 라우팅 정책</h2>
          <button className="btn text-sm" onClick={savePolicy}>정책 저장</button>
        </div>
        <div className="grid grid-cols-4 gap-3">
          {['critical', 'high', 'medium', 'low'].map((sev) => {
            const r = routing[sev] || {}
            const chans: string[] = r.channels || []
            return (
              <div key={sev} className="border rounded-lg p-3 space-y-2">
                <div className="flex items-center gap-2">
                  <span className={`text-[11px] px-1.5 py-0.5 rounded ${SEV_BG[sev]}`}>{SEV_KO[sev]}</span>
                  {r.ack_required && <span className="text-[11px] text-red-600">확인 필수</span>}
                </div>
                <div className="flex gap-1 flex-wrap">
                  {chans.length === 0 && <span className="text-xs text-gray-400">수신함만</span>}
                  {chans.map((c) => (
                    <label key={c} className="flex items-center gap-1 text-xs">
                      <input type="checkbox" checked={chans.includes(c)} onChange={(e) => {
                        const next = e.target.checked ? [...new Set([...chans, c])] : sev === 'critical' ? chans : chans.filter((x) => x !== c)
                        setPolicy({ ...policy!, routing_json: JSON.stringify({ ...routing, [sev]: { ...r, channels: next } }) })
                      }} />
                      {c === 'sms' ? 'SMS' : c === 'email' ? '이메일' : 'Slack'}
                    </label>
                  ))}
                </div>
                <div className="text-[11px] text-gray-400">관리 주체: {managedBy[sev === 'critical' ? 'sms' : 'email'] === 'patty' ? 'Patty 관리' : '고객 관리'}</div>
              </div>
            )
          })}
        </div>
        <div className="grid grid-cols-4 gap-3 items-end">
          <div><label className="label">확인 기한 (분)</label>
            <input type="number" className="input" value={policy?.ack_deadline_minutes ?? 15}
              onChange={(e) => setPolicy({ ...policy!, ack_deadline_minutes: Number(e.target.value) })} /></div>
          <div><label className="label">에스컬레이션 단계</label>
            <input type="number" className="input" value={policy?.escalation_steps ?? 3}
              onChange={(e) => setPolicy({ ...policy!, escalation_steps: Number(e.target.value) })} /></div>
          <div><label className="label">야간 시간 (비긴급만)</label>
            <input className="input" placeholder="23:00" value={policy?.quiet_hours_start ?? ''}
              onChange={(e) => setPolicy({ ...policy!, quiet_hours_start: e.target.value })} /></div>
          <div><label className="label">에어갭 배포</label>
            <label className="flex items-center gap-2 text-sm mt-2">
              <input type="checkbox" checked={policy?.air_gapped ?? false}
                onChange={(e) => setPolicy({ ...policy!, air_gapped: e.target.checked })} />
              Patty 관리 발송 차단
            </label></div>
        </div>
        <p className="text-xs text-gray-400">긴급(critical)은 SMS·이메일·Slack 즉시 발송이 고정 기본 정책이며 약화할 수 없습니다.</p>
      </div>

      {/* Groups & channels */}
      <div className="grid grid-cols-2 gap-4">
        <div className="card">
          <div className="p-4 border-b flex items-center justify-between">
            <h2 className="font-semibold">수신 그룹 · 에스컬레이션 순서</h2>
            <button className="btn-secondary text-xs" onClick={() => setGroupOpen(true)}>그룹 추가</button>
          </div>
          {groups.map((g) => (
            <div key={g.id} className="p-3 border-t">
              <div className="flex items-center gap-2">
                <span className="text-xs px-1.5 py-0.5 rounded bg-gray-100">{g.escalation_order}순위</span>
                <span className="font-medium text-sm">{g.name}</span>
                <span className="text-xs text-gray-400">{g.timezone}</span>
              </div>
              <div className="text-xs text-gray-500 mt-1">
                {(JSON.parse(g.members_json || '[]') as any[]).map((m, i) => (
                  <span key={i} className="mr-2">{m.kind === 'sms' ? 'SMS' : m.kind} {m.verified ? '' : '(미인증)'}</span>
                ))}
              </div>
            </div>
          ))}
          {groups.length === 0 && <p className="p-4 text-sm text-gray-500">그룹이 없습니다.</p>}
        </div>
        <div className="card">
          <div className="p-4 border-b flex items-center justify-between">
            <h2 className="font-semibold">발송 채널 (자격증명 마스킹)</h2>
            <button className="btn-secondary text-xs" onClick={() => setChannelOpen(true)}>채널 구성</button>
          </div>
          {channels.map((c) => (
            <div key={c.id} className="p-3 border-t flex items-center justify-between">
              <div>
                <div className="flex items-center gap-2">
                  <span className="font-medium text-sm">{c.channel === 'sms' ? 'SMS' : c.channel === 'email' ? '이메일' : 'Slack'}</span>
                  <span className="text-[11px] px-1.5 py-0.5 rounded bg-gray-100">{c.managed_by === 'patty' ? 'Patty 관리' : '고객 관리'}</span>
                  {!c.verified && <span className="text-[11px] text-amber-600">미인증</span>}
                  {!c.healthy && <span className="text-[11px] text-red-600">비정상</span>}
                </div>
                <div className="text-xs text-gray-500 mt-0.5">{c.masked_endpoint}{c.last_failure && ` · ${c.last_failure}`}</div>
              </div>
              {!c.verified && (
                <button className="btn-secondary text-xs" onClick={() => api.inVerifyChannel(c.id).then(load).catch((e: any) => showToast(e.message))}>인증 완료</button>
              )}
            </div>
          ))}
          {channels.length === 0 && <p className="p-4 text-sm text-gray-500">채널이 없습니다.</p>}
          <div className="p-3 border-t">
            <button className="text-xs text-gray-500 hover:text-gray-700" onClick={() => setTestOpen(true)}>테스트 알림 보내기 (명확히 라벨링됨)</button>
          </div>
        </div>
      </div>

      {/* Incidents */}
      <div className="card">
        <div className="p-4 border-b"><h2 className="font-semibold">상관된 알림 (하나의 정체성으로 중복 억제)</h2></div>
        {incidents.length === 0 && <p className="p-4 text-sm text-gray-500">알림이 없습니다.</p>}
        {incidents.map((inc) => (
          <div key={inc.id} className="p-4 border-t flex items-start justify-between gap-4">
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className={`text-[11px] px-1.5 py-0.5 rounded ${SEV_BG[inc.severity]}`}>{SEV_KO[inc.severity]}</span>
                <span className="font-medium truncate">{inc.title_ko}</span>
                <span className="text-[11px] px-1.5 py-0.5 rounded bg-gray-100">{STATE_KO[inc.state] || inc.state}</span>
                {inc.escalation_step > 0 && <span className="text-[11px] px-1.5 py-0.5 rounded bg-amber-100 text-amber-800">에스컬레이션 {inc.escalation_step}단계</span>}
              </div>
              <div className="text-xs text-gray-500 mt-1">
                {inc.source_type} · {inc.service}{inc.rule && ` · ${inc.rule}`} · 지문 {inc.fingerprint.slice(0, 14)}…
                {inc.acked_by && ` · 확인 ${inc.acked_by} (${inc.acked_via})`}
              </div>
            </div>
            <div className="flex gap-2 shrink-0">
              {inc.state !== 'acknowledged' && inc.state !== 'resolved' && (
                <button className="btn-secondary text-xs" onClick={() => ack(inc)}>확인 (Ack)</button>
              )}
              {inc.state !== 'resolved' && <button className="btn-secondary text-xs" onClick={() => resolve(inc)}>해결</button>}
            </div>
          </div>
        ))}
      </div>

      {/* Jobs / dead letter */}
      <div className="card">
        <div className="p-4 border-b flex items-center justify-between">
          <h2 className="font-semibold">발송 작업 · 배달 불가 큐</h2>
          <span className="text-xs text-gray-400">재시도는 지수 백오프로 제한되며, 영구 실패는 배달 불가로 분류됩니다</span>
        </div>
        <div className="max-h-64 overflow-auto">
          {jobs.map((j) => (
            <div key={j.id} className="p-2.5 border-t flex items-center justify-between text-xs">
              <div className="flex items-center gap-2">
                <span className={`px-1.5 py-0.5 rounded ${j.state === 'dead_letter' ? 'bg-red-100 text-red-700' : j.state === 'sent' ? 'bg-emerald-100 text-emerald-700' : 'bg-gray-100'}`}>
                  {JOB_STATE_KO[j.state] || j.state}
                </span>
                <span>{j.kind}</span>
                <span className="text-gray-500">{j.channel} → {j.state === 'sent' ? j.target : '***'}</span>
                <span className="text-gray-400">시도 {j.attempts}/{5}</span>
                {j.last_error && <span className="text-red-500">{j.last_error}</span>}
              </div>
            </div>
          ))}
          {jobs.length === 0 && <p className="p-4 text-sm text-gray-500">작업이 없습니다.</p>}
        </div>
      </div>

      {/* Modals */}
      <Modal open={groupOpen} title="수신 그룹 추가" onClose={() => setGroupOpen(false)} size="md"
        footer={<ModalFooter onCancel={() => setGroupOpen(false)}
          onConfirm={() => api.inSaveGroup(group).then(() => { setGroupOpen(false); load(); showToast('그룹을 저장했습니다') }).catch((e: any) => showToast(e.message))}
          confirmLabel="저장" disabled={!group.name.trim()} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-3 gap-3">
            <div><label className="label">이름</label>
              <input className="input" value={group.name} onChange={(e) => setGroup({ ...group, name: e.target.value })} placeholder="1차 온콜" /></div>
            <div><label className="label">에스컬레이션 순서</label>
              <input type="number" className="input" value={group.escalation_order} onChange={(e) => setGroup({ ...group, escalation_order: Number(e.target.value) })} /></div>
            <div><label className="label">시간대</label>
              <input className="input" value={group.timezone} onChange={(e) => setGroup({ ...group, timezone: e.target.value })} /></div>
          </div>
          <div><label className="label">구성원 JSON (kind: email|sms|slack, target, verified)</label>
            <textarea className="input h-24 font-mono text-xs" value={group.members_json} onChange={(e) => setGroup({ ...group, members_json: e.target.value })} /></div>
          <p className="text-xs text-gray-500">대상은 인증(verified)된 구성원만 발송에 사용됩니다. 연락처는 암호화 · 마스킹되어 저장됩니다.</p>
        </div>
      </Modal>

      <Modal open={channelOpen} title="발송 채널 구성" onClose={() => setChannelOpen(false)} size="sm"
        footer={<ModalFooter onCancel={() => setChannelOpen(false)}
          onConfirm={() => api.inSaveChannel(channel).then(() => { setChannelOpen(false); load(); showToast('채널을 저장했습니다 (인증 필요)') }).catch((e: any) => showToast(e.message))}
          confirmLabel="저장" disabled={!channel.endpoint.trim()} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">채널</label>
              <select className="input" value={channel.channel} onChange={(e) => setChannel({ ...channel, channel: e.target.value })}>
                <option value="email">이메일</option><option value="sms">SMS</option><option value="slack">Slack</option>
              </select></div>
            <div><label className="label">관리 주체</label>
              <select className="input" value={channel.managed_by} onChange={(e) => setChannel({ ...channel, managed_by: e.target.value })}>
                <option value="customer">고객 관리 (기본)</option><option value="patty">Patty 관리 발송</option>
              </select></div>
          </div>
          <div><label className="label">엔드포인트</label>
            <input className="input" value={channel.endpoint} onChange={(e) => setChannel({ ...channel, endpoint: e.target.value })} placeholder="oncall@example.com" /></div>
          <p className="text-xs text-gray-500">Patty 관리 발송을 선택하면 최소 안전 엔벨로프(테넌트 표시명, 알림 ID, 심각도, 제목, 시각, 링크)만 Patty 전달 서비스로 나갑니다. 저장 후 엔드포인트 인증이 필요합니다.</p>
        </div>
      </Modal>

      <Modal open={sourceOpen} title="소스 이벤트 등록 (상관 · 중복 억제)" onClose={() => setSourceOpen(false)} size="md"
        footer={<ModalFooter onCancel={() => setSourceOpen(false)}
          onConfirm={() => api.inIngestSource(source).then(() => { setSourceOpen(false); load(); showToast('소스 이벤트를 등록했습니다') }).catch((e: any) => showToast(e.message))}
          confirmLabel="등록" disabled={!source.title_ko.trim() || !source.source_type.trim()} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">소스 유형</label>
              <select className="input" value={source.source_type} onChange={(e) => setSource({ ...source, source_type: e.target.value })}>
                <option value="security_finding">보안 탐지</option><option value="outage">서비스 중단</option>
                <option value="degradation">성능 저하</option><option value="system_failure">시스템 오류</option>
              </select></div>
            <div><label className="label">심각도</label>
              <select className="input" value={source.severity} onChange={(e) => setSource({ ...source, severity: e.target.value })}>
                <option value="critical">긴급</option><option value="high">높음</option>
                <option value="medium">보통</option><option value="low">낮음</option>
              </select></div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">서비스</label>
              <input className="input" value={source.service} onChange={(e) => setSource({ ...source, service: e.target.value })} /></div>
            <div><label className="label">탐지 규칙</label>
              <input className="input" value={source.rule} onChange={(e) => setSource({ ...source, rule: e.target.value })} /></div>
          </div>
          <div><label className="label">한국어 제목</label>
            <input className="input" value={source.title_ko} onChange={(e) => setSource({ ...source, title_ko: e.target.value })} /></div>
          <div><label className="label">안전 요약 (외부 전달 가능한 문구만)</label>
            <textarea className="input h-16" value={source.safe_summary_ko} onChange={(e) => setSource({ ...source, safe_summary_ko: e.target.value })} /></div>
          <p className="text-xs text-gray-500">동일 지문(소스+서비스+규칙)의 반복 이벤트는 기존 알림에 갱신되며, 심각도 상승과 같은 실질 변화가 없으면 재발송되지 않습니다. 원본 증거는 PCCP 내부에만 남습니다.</p>
        </div>
      </Modal>

      <Modal open={testOpen} title="테스트 알림 발송" onClose={() => setTestOpen(false)} size="sm"
        footer={<ModalFooter onCancel={() => setTestOpen(false)}
          onConfirm={() => api.inSendTest(test).then(() => { setTestOpen(false); showToast('테스트 알림을 큐에 넣었습니다. [테스트] 라벨이 붙습니다') }).catch((e: any) => showToast(e.message))}
          confirmLabel="테스트 발송" disabled={!test.target.trim()} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">채널</label>
              <select className="input" value={test.channel} onChange={(e) => setTest({ ...test, channel: e.target.value })}>
                <option value="email">이메일</option><option value="sms">SMS</option><option value="slack">Slack</option>
              </select></div>
            <div><label className="label">대상</label>
              <input className="input" value={test.target} onChange={(e) => setTest({ ...test, target: e.target.value })} /></div>
          </div>
          <p className="text-xs text-gray-500">테스트 알림은 제목에 <b>[테스트]</b>가 붙어 실제 장애 알림과 명확히 구분됩니다.</p>
        </div>
      </Modal>
    </div>
  )
}
