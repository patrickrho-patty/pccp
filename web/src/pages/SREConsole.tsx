import { useState, useEffect } from 'react'
import EmptyState from '../components/EmptyState'
import { Link } from 'react-router-dom'
import { api } from '../api'

export default function SREConsole() {
  const [tab, setTab] = useState<'overview' | 'accounts' | 'capacity' | 'risk'>('overview')
  const [accounts, setAccounts] = useState<any[]>([])
  const [health, setHealth] = useState<any>({})

  useEffect(() => {
    fetch('/api/public/accounts', { headers: authHeaders() })
      .then(r => r.json()).then(data => setAccounts(Array.isArray(data) ? data : []))
      .catch(() => setAccounts([]))

    // Health checks
    Promise.all([
      fetch('/api/realtime/status', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/telemetry/snapshot', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/health', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
    ]).then(([rt, tel, cp]) => {
      setHealth({ realtime: rt, telemetry: tel, cp })
    })
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
              <div className="text-3xl font-bold text-green-600">●</div>
              <div className="text-sm text-gray-500 mt-1">PAPER Relay</div>
              <div className="text-xs text-gray-400">port 8090</div>
            </div>
            <div className="card text-center">
              <div className="text-3xl font-bold text-green-600">●</div>
              <div className="text-sm text-gray-500 mt-1">PIA Inference</div>
              <div className="text-xs text-gray-400">port 9090</div>
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
                { name: 'OAuth/OIDC', nameKo: '인증 서비스', status: 'healthy' },
                { name: 'Subscription', nameKo: '구독 관리', status: 'healthy' },
                { name: 'Harness Registry', nameKo: '하네스 등록', status: 'healthy' },
                { name: 'PAPER Ingress', nameKo: 'PAPER 수신', status: 'healthy' },
                { name: 'Relay Fleet', nameKo: '릴레이 플릿', status: 'healthy' },
                { name: 'Capacity Authority', nameKo: '용량 관리', status: 'healthy' },
                { name: 'Model Catalog', nameKo: '모델 카탈로그', status: 'healthy' },
                { name: 'PIA/Model Plane', nameKo: 'PIA 추론', status: 'healthy' },
                { name: 'Event Spine', nameKo: '이벤트 파이프라인', status: 'healthy' },
                { name: 'Metering', nameKo: '미터링', status: 'healthy' },
                { name: 'Notifications', nameKo: '알림', status: 'degraded' },
                { name: 'Payments', nameKo: '결제', status: 'not_configured' },
              ].map(comp => (
                <div key={comp.name} className="flex items-center justify-between py-1.5 px-2 bg-gray-50 rounded">
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
              ))}
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
            <table className="w-full">
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
            <table className="w-full">
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

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
