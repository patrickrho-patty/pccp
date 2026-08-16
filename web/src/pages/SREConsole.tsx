import { useState, useEffect } from 'react'
import EmptyState from '../components/EmptyState'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { formatRelative } from '../utils/format'

// probeStatus maps a probe/API status to the component tri-state.
const probeStatus = (s?: string): 'healthy' | 'degraded' | 'not_configured' =>
  s === 'up' || s === 'ok' ? 'healthy' : s === 'down' ? 'degraded' : 'not_configured'
// probeReason: fetch-failed (null) / probe-failed / unconfigured are
// three distinct states — never conflated, never green when unchecked.
const probeReason = (p: { status?: string; addr?: string } | undefined | null, cfgKey: string): string => {
  if (p == null) return '조회 실패 — 세션 만료 또는 서버 오류'
  if (p?.status === 'down') return `프로브 실패: ${p.addr ?? cfgKey}`
  if (p?.status === 'up') return `프로브 ${p.addr ?? 'CP'}`
  return `프로브 미설정 (${cfgKey})`
}
// probeStatusOf: a failed fetch (null) renders degraded.
const probeStatusOf = (p: { status?: string } | undefined | null): 'healthy' | 'degraded' | 'not_configured' =>
  p == null ? 'degraded' : probeStatus(p?.status)

// dbStatus: ok+rows → healthy; ok+0 → not_configured (기록 없음 — an
// existing table nothing writes is NOT a healthy pipeline); query
// failed or fetch failed → degraded (never green when unchecked).
const dbStatus = (s: string | undefined | null, count: number | undefined | null): 'healthy' | 'degraded' | 'not_configured' => {
  if (s == null || s === 'down') return 'degraded'
  if (s === 'ok') return (count ?? 0) > 0 ? 'healthy' : 'not_configured'
  return 'not_configured'
}
const dbReason = (s: string | undefined | null, count: number | undefined | null, prefix: string, suffix: string): string => {
  if (s == null || s === 'down') return '조회 실패 — 상태를 확인할 수 없음'
  if (s === 'ok') return (count ?? 0) > 0 ? `${prefix} ${count}${suffix}` : `${prefix} 0${suffix} — 기록 없음 (파이프라인 미연결 가능)`
  return '상태 조회 불가'
}

const probeColor = (s?: string) => s === 'up' ? 'text-green-600' : s === 'down' ? 'text-red-600' : 'text-gray-400'
const probeLabel = (p?: { status?: string; addr?: string }) => {
  if (!p?.status || p.status === 'unconfigured') return '미설정 · unconfigured'
  return p.status === 'up' ? `${p.addr} · 정상` : `${p.addr || '?'} · 접속 불가`
}

export default function SREConsole() {
  const [tab, setTab] = useState<'overview' | 'reliability' | 'accounts' | 'capacity' | 'risk'>('overview')
  const [accounts, setAccounts] = useState<any[]>([])
  const [incidents, setIncidents] = useState<any[]>([])
  const [health, setHealth] = useState<any>({})

  // Health/account state refreshes on an interval + tab focus: a status
  // page must never freeze on a stale one-shot snapshot (races with
  // login/token refresh recorded permanent false-negatives live).
  const loadAccounts = () => {
    fetch('/api/public/accounts', { headers: authHeaders() })
      .then(r => r.json().then(data => ({ ok: r.ok, data })))
      .then(({ ok, data }) => {
        setAccounts(Array.isArray(data) ? data : [])
        setHealth((h: any) => ({ ...h, accounts: ok }))
      })
      .catch(() => { setAccounts([]); setHealth((h: any) => ({ ...h, accounts: false })) })
  }
  const loadHealth = () => {
    Promise.all([
      fetch('/api/realtime/status', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/telemetry/snapshot', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/health', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/sre/probes', { headers: authHeaders() }).then(r => (r.ok ? r.json() : {})).catch(() => ({})),
    ]).then(([rt, tel, cp, probes]: any[]) => {
      setHealth((h: any) => ({
        ...h,
        realtime: rt && !rt.error ? rt : null,
        telemetry: tel && !tel.error ? tel : null,
        cp: cp && !cp.error ? cp : null,
        probes: probes && !probes.error ? probes : null,
        authed: rt !== undefined && rt !== null,
      }))
    })
  }

  useEffect(() => {
    loadAccounts()
    loadHealth()
    const timer = window.setInterval(() => { loadHealth(); loadAccounts() }, 10000)
    const onVisible = () => { if (document.visibilityState === 'visible') { loadHealth(); loadAccounts() } }
    document.addEventListener('visibilitychange', onVisible)
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', onVisible) }
  }, [])

  useEffect(() => {
    fetch('/api/incidents', { headers: authHeaders() })
      .then(r => r.json()).then(data => setIncidents(Array.isArray(data) ? data : []))
      .catch(() => setIncidents([]))
  }, [])

  const stats = {
    totalAccounts: accounts.length,
    activeSubs: accounts.filter(a => a.subscription_status === 'active').length,
    graceSubs: accounts.filter(a => a.subscription_status === 'grace').length,
    integrityFlags: accounts.filter(a => a.account_integrity_state !== 'normal').length,
    tsFlags: accounts.filter(a => a.trust_safety_state !== 'normal').length,
    capacityFlags: accounts.filter(a => a.capacity_state !== 'normal').length,
  }

  const stateBadge = (s: string, normalLabel = '정상') => 
    s === 'normal' ? 'badge-green' : s === 'flagged' || s === 'reviewing' || s === 'high_usage' ? 'badge-yellow' : 'badge-red'

  return (
    <div>
      <h1 className="text-2xl font-bold mb-2">SRE 운영 콘솔 <span className="text-gray-400 text-lg font-normal">SRE Operations Console</span></h1>
      <p className="text-sm text-gray-500 mb-6">퍼블릭 클라우드 서비스 운영 · Public Cloud Service Operations (v2)</p>

      {/* Tab navigation */}
      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'overview', label: '서비스 현황', labelEn: 'Overview' },
          { id: 'reliability', label: '신뢰성', labelEn: 'Reliability' },
          { id: 'accounts', label: '계정 관리', labelEn: 'Accounts' },
          { id: 'capacity', label: '용량', labelEn: 'Capacity' },
          { id: 'risk', label: '위험 관리', labelEn: 'Risk' },
        ].map(t => (
          <button
            key={t.id}
            onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
          >
            {t.label} <span className="text-xs text-gray-400">{t.labelEn}</span>
          </button>
        ))}
      </div>

      {/* Overview Tab */}
      {tab === 'overview' && (
        <div>
          {/* Service Health */}
          <div className="grid grid-cols-4 gap-4 mb-6">
            <div className="card text-center">
              <div className="text-3xl font-bold text-green-600">●</div>
              <div className="text-sm text-gray-500 mt-1">Control Plane</div>
              <div className="text-xs text-gray-400">{health.cp?.version || 'v0.1.0'}</div>
            </div>
            <div className="card text-center">
              <div className={`text-3xl font-bold ${probeColor(health.probes?.relay?.status)}`}>●</div>
              <div className="text-sm text-gray-500 mt-1">DARI Relay</div>
              <div className="text-xs text-gray-400">{probeLabel(health.probes?.relay)}</div>
            </div>
            <div className="card text-center">
              <div className={`text-3xl font-bold ${probeColor(health.probes?.pia?.status)}`}>●</div>
              <div className="text-sm text-gray-500 mt-1">PIA Inference</div>
              <div className="text-xs text-gray-400">{probeLabel(health.probes?.pia)}</div>
            </div>
            <div className="card text-center">
              <div className={`text-3xl font-bold ${health.realtime?.connected_clients > 0 ? 'text-green-600' : 'text-gray-400'}`}>●</div>
              <div className="text-sm text-gray-500 mt-1">Realtime</div>
              <div className="text-xs text-gray-400">{health.realtime?.connected_clients || 0} clients</div>
            </div>
          </div>

          {/* Account Summary */}
          <div className="grid grid-cols-4 gap-4 mb-6">
            <div className="card">
              <div className="text-2xl font-bold">{stats.totalAccounts}</div>
              <div className="text-xs text-gray-500">총 계정 · Total Accounts</div>
            </div>
            <div className="card">
              <div className="text-2xl font-bold text-green-600">{stats.activeSubs}</div>
              <div className="text-xs text-gray-500">활성 구독 · Active Subs</div>
            </div>
            <div className="card">
              <div className="text-2xl font-bold text-yellow-600">{stats.graceSubs}</div>
              <div className="text-xs text-gray-500">유예 · Grace</div>
            </div>
            <div className="card">
              <div className="text-2xl font-bold text-red-600">{stats.integrityFlags + stats.tsFlags}</div>
              <div className="text-xs text-gray-500">위험 플래그 · Risk Flags</div>
            </div>
          </div>

          {/* System Components */}
          <div className="card">
            <h3 className="text-sm font-semibold mb-3">시스템 구성 요소 · System Components (v2 §7.1)</h3>
            <div className="grid grid-cols-3 gap-3">
              {[
{ name: 'OAuth/OIDC', nameKo: '인증 서비스', status: health.authed === true ? 'healthy' : health.authed === false ? 'degraded' : 'not_configured', reason: health.authed === true ? '인증 API 응답 정상 (이 페이지 세션)' : '인증 API 조회 실패' },
                { name: 'Subscription', nameKo: '구독 관리', status: health.accounts === true ? 'healthy' : health.accounts === false ? 'degraded' : 'not_configured', reason: health.accounts === true ? `계정 API 정상 (${accounts.length}계정)` : '계정 API 조회 실패' },
                { name: 'Harness Registry', nameKo: '하네스 등록', status: health.accounts === true ? 'healthy' : health.accounts === false ? 'degraded' : 'not_configured', reason: health.accounts === true ? '하네스 목록 조회 정상' : '조회 실패' },
                { name: 'DARI Ingress', nameKo: 'DARI 수신', status: probeStatusOf(health.probes?.relay), reason: probeReason(health.probes?.relay ?? null, 'relay_probe_addr') },
                { name: 'Relay Fleet', nameKo: '릴레이 플릿', status: probeStatusOf(health.probes?.relay), reason: probeReason(health.probes?.relay ?? null, 'relay_probe_addr') },
                { name: 'Capacity Authority', nameKo: '용량 관리', status: health.accounts === true ? 'healthy' : health.accounts === false ? 'degraded' : 'not_configured', reason: health.accounts === true ? '용량 조회 정상' : '조회 실패' },
                { name: 'Model Catalog', nameKo: '모델 카탈로그', status: dbStatus(health.realtime?.catalog, health.realtime?.catalog_count), reason: dbReason(health.realtime?.catalog, health.realtime?.catalog_count, '카탈로그', '개 모델') },
                { name: 'PIA/Model Plane', nameKo: 'PIA 추론', status: probeStatusOf(health.probes?.pia), reason: probeReason(health.probes?.pia ?? null, 'pia_probe_addr') },
                { name: 'Event Spine', nameKo: '이벤트 파이프라인', status: dbStatus(health.realtime?.event_spine, health.realtime?.event_spine_count), reason: dbReason(health.realtime?.event_spine, health.realtime?.event_spine_count, 'CP 감사 24시간', '개 이벤트') + ' (CP 기준)' },
                { name: 'Metering', nameKo: '미터링', status: dbStatus(health.realtime?.metering, health.realtime?.metering_count), reason: dbReason(health.realtime?.metering, health.realtime?.metering_count, '24시간', '건 기록') },
                { name: 'Notifications', nameKo: '알림', status: 'not_configured', reason: '알림 채널(Slack/Email) 미연동 — 연동 전까지 발송 없음' },
                { name: 'Payments', nameKo: '결제', status: 'not_configured', reason: '결제 게이트 미연동 (§29.9)' },
              ].map(comp => (
                <div key={comp.name} className="py-1.5 px-2 bg-gray-50 rounded">
                  <div className="flex items-center justify-between flex-wrap gap-2">
                    <div>
                      <span className="text-sm">{comp.nameKo}</span>
                      <span className="text-xs text-gray-400 ml-1">{comp.name}</span>
                    </div>
                    <span className={
                      comp.status === 'healthy' ? 'badge-green' :
                      comp.status === 'degraded' ? 'badge-yellow' : 'badge-gray'
                    }>
                      {comp.status === 'healthy' ? '정상' : comp.status === 'degraded' ? '저하' : '미설정'}
                    </span>
                  </div>
                  {comp.status !== 'healthy' && (
                    <div className="text-[11px] text-gray-500 mt-0.5 leading-tight">{comp.reason}</div>
                  )}
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Reliability Tab */}
      {tab === 'reliability' && (
        <div className="space-y-4">
          {/* SLO / Burn Rate */}
          <div className="card">
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-semibold">SLO / 버닝레이트 · SLO & Burn Rate</h3>
              <span className="badge-yellow">미계측 · Not instrumented</span>
            </div>
            <p className="text-xs text-gray-400 mb-3">
              SLO 메트릭 파이프라인(§43)이 아직 연결되지 않아 버닝레이트/에러 버짓을 계산할 수 없습니다. 아래는 텔레메트리 스냅샷의 실측값입니다.
            </p>
            {Object.keys(health.telemetry?.counters || {}).length > 0 || Object.keys(health.telemetry?.gauges || {}).length > 0 ? (
              <div className="space-y-1">
                {Object.entries(health.telemetry?.counters || {}).map(([k, v]) => (
                  <div key={`c-${k}`} className="flex items-center gap-3 text-xs py-1 px-2 bg-gray-50 rounded">
                    <span className="font-mono truncate flex-1">{k}</span>
                    <span className="text-gray-400">counter</span>
                    <span className="font-medium">{String(v)}</span>
                  </div>
                ))}
                {Object.entries(health.telemetry?.gauges || {}).map(([k, v]) => (
                  <div key={`g-${k}`} className="flex items-center gap-3 text-xs py-1 px-2 bg-gray-50 rounded">
                    <span className="font-mono truncate flex-1">{k}</span>
                    <span className="text-gray-400">gauge</span>
                    <span className="font-medium">{String(v)}</span>
                  </div>
                ))}
                <p className="text-[10px] text-gray-400 mt-1">스냅샷 시각 · Snapshot: {health.telemetry?.timestamp?.slice(0, 19).replace('T', ' ') || '-'}</p>
              </div>
            ) : (
              <EmptyState icon="📉" title="수집된 SLO 메트릭이 없습니다" message="텔레메트리 파이프라인이 연결되면 표시됩니다 (데모 데이터 생성 없음)" />
            )}
          </div>

          {/* Incident Timeline */}
          <div className="card">
            <h3 className="text-sm font-semibold mb-3">인시던트 타임라인 · Incident Timeline</h3>
            {incidents.length === 0 ? (
              <EmptyState icon="🚨" title="기록된 인시던트가 없습니다" message="/api/incidents로 생성된 인시던트가 표시됩니다" />
            ) : (
              <div className="space-y-2">
                {incidents.map((inc: any) => (
                  <div key={inc.id} className="p-3 bg-gray-50 rounded-lg">
                    <div className="flex items-center gap-2 mb-1">
                      <span className={inc.severity === 'critical' ? 'badge-red' : inc.severity === 'high' ? 'badge-yellow' : 'badge-gray'}>{inc.severity}</span>
                      <span className="text-sm font-medium">{inc.title_ko || inc.title}</span>
                      <span className={`ml-auto text-xs font-medium ${inc.status === 'resolved' || inc.status === 'closed' ? 'text-green-600' : 'text-red-600'}`}>{inc.status}</span>
                    </div>
                    {inc.description && <p className="text-xs text-gray-500 mb-2">{inc.description}</p>}
                    <div className="flex items-center gap-4 text-[10px] text-gray-400 flex-wrap">
                      <span>발생 · First seen: {inc.first_seen_at?.slice(0, 19).replace('T', ' ') || '-'}</span>
                      <span>격리 · Contained: {inc.contained_at ? inc.contained_at.slice(0, 19).replace('T', ' ') : '-'}</span>
                      <span>해결 · Resolved: {inc.resolved_at ? inc.resolved_at.slice(0, 19).replace('T', ' ') : '미해결'}</span>
                      {inc.category && <span className="font-mono">{inc.category}</span>}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Alert Routing Config */}
          <div className="card">
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-semibold">알림 라우팅 설정 · Alert Routing (SEV)</h3>
              <span className="badge-yellow">설정 저장 미구현 · Not wired</span>
            </div>
            <p className="text-xs text-gray-400 mb-3">
              Slack/이메일/온콜 채널 연동이 아직 구축되지 않았습니다(§10C.14). 아래는 정의된 라우팅 정책이며 저장·발송은 불가합니다.
            </p>
            <table className="w-full overflow-x-auto block">
              <thead>
                <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                  <th className="pb-2">심각도</th>
                  <th className="pb-2">채널</th>
                  <th className="pb-2">대응</th>
                  <th className="pb-2">에스컬레이션</th>
                </tr>
              </thead>
              <tbody>
                {[
                  { sev: 'SEV-1', ko: '서비스 전면 장애', channels: 'Slack #sev1 + 이메일 + 온콜 호출', response: '즉시 (15분 내)', escalation: 'SRE 리드 → CTO' },
                  { sev: 'SEV-2', ko: '부분 저하', channels: 'Slack #alerts + 온콜 알림', response: '업무시간 내', escalation: '당직 → SRE 리드' },
                  { sev: 'SEV-3', ko: '경고', channels: 'Slack #alerts', response: '다음 영업일', escalation: '티켓 생성' },
                ].map(s => (
                  <tr key={s.sev} className="border-b border-gray-100 last:border-0">
                    <td className="py-2"><span className={s.sev === 'SEV-1' ? 'badge-red' : s.sev === 'SEV-2' ? 'badge-yellow' : 'badge-gray'}>{s.sev}</span> <span className="text-xs text-gray-500">{s.ko}</span></td>
                    <td className="py-2 text-xs text-gray-500">{s.channels}</td>
                    <td className="py-2 text-xs text-gray-500">{s.response}</td>
                    <td className="py-2 text-xs text-gray-500">{s.escalation}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Regional Health */}
          <div className="card">
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-semibold">지역별 상태 · Regional Health</h3>
              <span className="badge-gray">단일 지역 배포 · Single region</span>
            </div>
            <p className="text-xs text-gray-400 mb-3">
              다중 지역 상태는 아직 계측되지 않았습니다(§7.1). 현재 배포의 실측 상태만 표시됩니다.
            </p>
            <div className="p-3 bg-gray-50 rounded-lg">
              <div className="flex items-center gap-2 mb-2">
                <span className="text-sm font-medium">kr-central <span className="text-xs text-gray-400 font-normal">현재 배포 · Current deployment</span></span>
                <span className={health.cp?.status === 'ok' ? 'badge-green ml-auto' : 'badge-yellow ml-auto'}>{health.cp?.status || 'unknown'}</span>
              </div>
              <div className="flex items-center gap-4 text-[10px] text-gray-400 flex-wrap">
                <span>컨트롤 플레인: {health.cp?.service || '-'} v{health.cp?.version || '-'}</span>
                <span>실시간 클라이언트: {health.realtime?.connected_clients ?? 0}</span>
                <span>헬스 체크: {health.cp?.timestamp ? formatRelative(health.cp.timestamp) : '-'}</span>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Accounts Tab */}
      {tab === 'accounts' && (
        <div className="card">
          <h3 className="text-lg font-semibold mb-4">구독자 계정 · Subscriber Accounts</h3>
          {accounts.length === 0 ? (
            <div className="text-center py-8">
              <EmptyState icon="📡" title="등록된 퍼블릭 계정이 없습니다" message="계정이 생성되면 표시됩니다" />
              <p className="text-sm text-gray-400 mt-1">Public Cloud 계정은 /api/public/accounts API로 생성할 수 있습니다.</p>
            </div>
          ) : (
            <table className="w-full overflow-x-auto block">
              <thead>
                <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                  <th className="pb-2">계정 · Account</th>
                  <th className="pb-2">플랜 · Plan</th>
                  <th className="pb-2">구독 · Sub</th>
                  <th className="pb-2">무결성 · Integrity</th>
                  <th className="pb-2">T&S</th>
                  <th className="pb-2">용량 · Capacity</th>
                  <th className="pb-2">하네스 · Max</th>
                </tr>
              </thead>
              <tbody>
                {accounts.map(a => (
                  <tr key={a.id} className="border-b border-gray-100 last:border-0 hover:bg-gray-50">
                    <td className="py-3">
                      <div className="font-medium text-sm">{a.display_name_ko || a.display_name}</div>
                      <div className="text-xs text-gray-400">{a.email}</div>
                    </td>
                    <td className="py-3"><span className="badge-blue">{a.subscription_plan || 'none'}</span></td>
                    <td className="py-3">
                      <span className={a.subscription_status === 'active' ? 'badge-green' : a.subscription_status === 'grace' ? 'badge-yellow' : 'badge-red'}>
                        {a.subscription_status}
                      </span>
                    </td>
                    <td className="py-3"><span className={stateBadge(a.account_integrity_state)}>{a.account_integrity_state}</span></td>
                    <td className="py-3"><span className={stateBadge(a.trust_safety_state)}>{a.trust_safety_state}</span></td>
                    <td className="py-3"><span className={stateBadge(a.capacity_state)}>{a.capacity_state}</span></td>
                    <td className="py-3 text-sm">{a.max_harnesses}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* Capacity Tab */}
      {tab === 'capacity' && (
        <div>
          <div className="card mb-4">
            <h3 className="text-sm font-semibold mb-3">용량 개념 · Capacity Concepts (v2 §10C)</h3>
            <div className="space-y-2 text-sm text-gray-600">
              <p>• <strong>Agent Work Slot</strong>: 시맨틱 동시성 단위 (소켓 수가 아님) — 하네스 하나에 여러 슬롯 가능</p>
              <p>• <strong>Compute Load Unit (CLU)</strong>: 정규화된 부하 추정치 — prefill + decode + KV residency + context</p>
              <p>• <strong>Account Capacity Lease</strong>: 계정별 단기 서명된 용량 권한 — 릴레이가 로컬에서 승인</p>
              <p>• <strong>Fair Scheduler</strong>: 가중 공정 스케줄링 — 한 사용자의 다중 서브에이전트가 GPU 독점 방지</p>
            </div>
          </div>

          <div className="card">
            <h3 className="text-sm font-semibold mb-3">슬롯 정책 · Slot Policy per Plan</h3>
            <table className="w-full overflow-x-auto block">
              <thead>
                <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                  <th className="pb-2">플랜 · Plan</th>
                  <th className="pb-2">하네스 · Max</th>
                  <th className="pb-2">활성 하네스</th>
                  <th className="pb-2">일반 슬롯</th>
                  <th className="pb-2">헤비 슬롯</th>
                  <th className="pb-2">우선순위</th>
                </tr>
              </thead>
              <tbody>
                {[
                  { plan: 'free', harness: 1, active: 1, slots: 1, heavy: 0, priority: 1 },
                  { plan: 'developer', harness: 2, active: 2, slots: 5, heavy: 1, priority: 10 },
                  { plan: 'pro', harness: 3, active: 2, slots: 5, heavy: 2, priority: 30 },
                  { plan: 'team', harness: 3, active: 3, slots: 8, heavy: 3, priority: 50 },
                  { plan: 'enterprise', harness: 10, active: 5, slots: 10, heavy: 5, priority: 100 },
                ].map(p => (
                  <tr key={p.plan} className="border-b border-gray-100 last:border-0">
                    <td className="py-2"><span className="badge-blue">{p.plan}</span></td>
                    <td className="py-2 text-sm">{p.harness}</td>
                    <td className="py-2 text-sm">{p.active}</td>
                    <td className="py-2 text-sm">{p.slots}</td>
                    <td className="py-2 text-sm">{p.heavy}</td>
                    <td className="py-2 text-sm">{p.priority}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Risk Tab */}
      {tab === 'risk' && (
        <div>
          <div className="card mb-4">
            <h3 className="text-sm font-semibold mb-3">위험 도메인 분리 · Risk Domain Separation (v2 §10C.9-11)</h3>
            <p className="text-sm text-gray-600 mb-3">
              PCCP v2는 4개의 독립적인 위험 도메인을 분리합니다. 하나의 도메인에서 플래그가 설정되어도 다른 도메인에 영향을 주지 않습니다.
            </p>
            <div className="grid grid-cols-4 gap-3">
              <div className="bg-gray-50 rounded-lg p-3">
                <div className="font-medium text-sm mb-1">계정 무결성</div>
                <div className="text-xs text-gray-500 mb-2">Account Integrity</div>
                <div className="text-xs text-gray-400">계정 공유, 자격증명 재생, 의심스러운 활동</div>
                <div className="mt-2"><span className="badge-yellow">{stats.integrityFlags} 플래그</span></div>
              </div>
              <div className="bg-gray-50 rounded-lg p-3">
                <div className="font-medium text-sm mb-1">신뢰 및 안전</div>
                <div className="text-xs text-gray-500 mb-2">Trust & Safety</div>
                <div className="text-xs text-gray-400">서비스 악용, 금지 콘텐츠, 이용약관 위반</div>
                <div className="mt-2"><span className="badge-yellow">{stats.tsFlags} 케이스</span></div>
              </div>
              <div className="bg-gray-50 rounded-lg p-3">
                <div className="font-medium text-sm mb-1">플랫폼 보안</div>
                <div className="text-xs text-gray-500 mb-2">Platform Security</div>
                <div className="text-xs text-gray-400">인프라 공격, 프로토콜 악용, 악성 클라이언트</div>
                <div className="mt-2"><span className="badge-green">정상</span></div>
              </div>
              <div className="bg-gray-50 rounded-lg p-3">
                <div className="font-medium text-sm mb-1">용량</div>
                <div className="text-xs text-gray-500 mb-2">Capacity</div>
                <div className="text-xs text-gray-400">높은 사용량 (남용 아님), 스로틀링</div>
                <div className="mt-2"><span className="badge-yellow">{stats.capacityFlags} 플래그</span></div>
              </div>
            </div>
          </div>

          <div className="card">
            <h3 className="text-sm font-semibold mb-3">단계적 대응 · Graduated Response (v2 §10C.10)</h3>
            <div className="space-y-2">
              {[
                { step: 1, action: '관찰', actionEn: 'Observe', desc: '신호 감지, 조치 없음' },
                { step: 2, action: '위험 플래그', actionEn: 'Risk Flag', desc: '플래그 설정, 모니터링 강화' },
                { step: 3, action: '단계적 인증', actionEn: 'Step-up Auth', desc: '재인증 요청' },
                { step: 4, action: '사용자 확인', actionEn: 'User Confirm', desc: '"본인이 맞습니까?"' },
                { step: 5, action: '하네스 폐기', actionEn: 'Revoke Harness', desc: '의심스러운 하네스 폐기' },
                { step: 6, action: '동시성 감소', actionEn: 'Reduce Slots', desc: '일시적 슬롯 감소' },
                { step: 7, action: '계정 제한', actionEn: 'Account Restrict', desc: '일시적 계정 제한' },
                { step: 8, action: '인간 검토', actionEn: 'Human Review', desc: '수동 검토 필요' },
                { step: 9, action: '정지/차단', actionEn: 'Suspend/Ban', desc: '확인된 위반 시 정지' },
              ].map(s => (
                <div key={s.step} className="flex items-center gap-3 py-1.5 border-b border-gray-50 last:border-0">
                  <span className="w-6 h-6 rounded-full bg-gray-200 text-gray-600 text-xs font-bold flex items-center justify-center">{s.step}</span>
                  <div className="flex-1">
                    <span className="text-sm font-medium">{s.action}</span>
                    <span className="text-xs text-gray-400 ml-2">{s.actionEn}</span>
                  </div>
                  <span className="text-xs text-gray-500">{s.desc}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
