import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'

const FLEET_ACTIONS = [
  { id: 'request_reauthentication', label: '재인증 요청', labelEn: 'Re-auth', severity: 'normal', icon: '🔑' },
  { id: 'force_policy_refresh', label: '정책 갱신', labelEn: 'Policy Refresh', severity: 'normal', icon: '🔄' },
  { id: 'force_config_refresh', label: '설정 갱신', labelEn: 'Config Refresh', severity: 'normal', icon: '⚙️' },
  { id: 'require_client_upgrade', label: '업그레이드 요구', labelEn: 'Force Upgrade', severity: 'normal', icon: '⬆️' },
  { id: 'move_release_ring', label: '릴리스 링 변경', labelEn: 'Move Ring', severity: 'normal', icon: '🎯' },
  { id: 'pause_agent_execution', label: '실행 일시정지', labelEn: 'Pause', severity: 'warning', icon: '⏸️' },
  { id: 'suspend_model_access', label: '모델 접근 정지', labelEn: 'Suspend Model', severity: 'warning', icon: '🚫' },
  { id: 'reduce_tool_capabilities', label: '도구 제한', labelEn: 'Reduce Tools', severity: 'warning', icon: '🔧' },
  { id: 'disable_mcp_server', label: 'MCP 비활성화', labelEn: 'Disable MCP', severity: 'warning', icon: '🔌' },
  { id: 'change_quota', label: '할당량 변경', labelEn: 'Change Quota', severity: 'normal', icon: '📊' },
  { id: 'terminate_session', label: '세션 종료', labelEn: 'Terminate Session', severity: 'danger', icon: '⏹️' },
  { id: 'isolate_sandbox', label: '샌드박스 격리', labelEn: 'Isolate Sandbox', severity: 'warning', icon: '🏝️' },
  { id: 'revoke_harness_certificate', label: '인증서 폐기', labelEn: 'Revoke Cert', severity: 'danger', icon: '❌' },
  { id: 'quarantine_device', label: '기기 격리', labelEn: 'Quarantine', severity: 'danger', icon: '🔒' },
  { id: 'invalidate_privilege', label: '권한 무효화', labelEn: 'Invalidate Priv', severity: 'warning', icon: '🚷' },
  { id: 'request_forensic_snapshot', label: '포렌식 스냅샷', labelEn: 'Forensic Snap', severity: 'normal', icon: '📸' },
  { id: 'create_incident', label: '인시던트 생성', labelEn: 'Create Incident', severity: 'warning', icon: '🚨' },
  { id: 'send_admin_message', label: '관리자 메시지', labelEn: 'Admin Msg', severity: 'normal', icon: '💬' },
]

const EMERGENCY_ACTIONS = [
  { id: 'emergency_lockdown', label: '전체 비상 잠금', labelEn: 'Emergency Lockdown', severity: 'danger', icon: '🔴' },
]

export default function Fleet() {
  const [inventory, setInventory] = useState<any[]>([])
  const [selectedHarness, setSelectedHarness] = useState<string | null>(null)
  const [actionPanel, setActionPanel] = useState<string | null>(null)
  const [actionReason, setActionReason] = useState('')
  const [actionHistory, setActionHistory] = useState<any[]>([])
  const [sessionInspector, setSessionInspector] = useState<any>(null)
  const [inspectorSessionId, setInspectorSessionId] = useState<string | null>(null)

  const load = () => {
    fetch('/api/fleet/inventory', { headers: authHeaders() })
      .then(r => r.json())
      .then(data => setInventory(Array.isArray(data) ? data : []))
      .catch(() => setInventory([]))
    fetch('/api/audit?limit=20', { headers: authHeaders() })
      .then(r => r.json())
      .then(data => {
        const arr = Array.isArray(data) ? data : (data?.events || [])
        setActionHistory(arr.filter((e: any) => e.event_type?.includes('fleet')))
      })
      .catch(() => {})
  }

  useEffect(() => { load() }, [])

  const executeAction = async (harnessId: string, action: string, reason: string) => {
    const orgId = localStorage.getItem('pccp_org_id') || ''
    try {
      const res = await fetch('/api/fleet/actions', {
        method: 'POST',
        headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({
          organization_id: orgId,
          harness_id: harnessId,
          action,
          reason,
          performed_by: localStorage.getItem('pccp_user_id') || 'admin',
        })
      })
      if (res.ok) {
        alert(`✓ ${action} 실행됨`)
        setActionPanel(null)
        setActionReason('')
        load()
      } else {
        alert('실행 실패: ' + await res.text())
      }
    } catch (e) {
      alert('오류: ' + e)
    }
  }

  const inspectSession = async (sessionId: string) => {
    try {
      const res = await fetch(`/api/fleet/sessions/${sessionId}/inspect`, { headers: authHeaders() })
      if (res.ok) {
        const data = await res.json()
        setSessionInspector(data)
        setInspectorSessionId(sessionId)
      }
    } catch {}
  }

  const riskBadge = (score: number) => {
    const pct = (score * 100).toFixed(0)
    if (score >= 0.8) return <span className="badge-red">{pct}%</span>
    if (score >= 0.5) return <span className="badge-yellow">{pct}%</span>
    return <span className="badge-green">{pct}%</span>
  }

  const statusBadge = (status: string) => {
    const map: Record<string, string> = {
      active: 'badge-green', enrolled: 'badge-green', revoked: 'badge-red',
      quarantined: 'badge-red', suspended: 'badge-yellow', offboarded: 'badge-gray',
    }
    return <span className={map[status] || 'badge-gray'}>{status}</span>
  }

  const selectedH = inventory.find(h => h.harness?.harness_id === selectedHarness)

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">플릿 관리 <span className="text-gray-400 text-lg font-normal">Fleet Management</span></h1>
        <div className="flex gap-3 items-center">
          <span className="text-sm text-gray-500">{inventory.filter(h => h.is_active).length} 활성 / {inventory.length} 전체</span>
          <button onClick={() => {
            const orgId = localStorage.getItem('pccp_org_id') || ''
            if (confirm('⚠️ 전체 비상 잠금을 실행하시겠습니까?\n모든 활성 세션이 종료됩니다.')) {
              executeAction('', 'emergency_lockdown', 'Emergency lockdown by admin')
            }
          }} className="btn-danger text-sm">🔴 비상 잠금</button>
        </div>
      </div>

      <div className="grid grid-cols-12 gap-4">
        {/* Fleet Table */}
        <div className={selectedHarness ? 'col-span-7' : 'col-span-12'}>
          <div className="card">
            {inventory.length === 0 ? (
              <p className="text-gray-400 text-center py-8">등록된 하네스가 없습니다</p>
            ) : (
              <table className="w-full">
                <thead>
                  <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                    <th className="pb-3">하네스</th>
                    <th className="pb-3">사용자</th>
                    <th className="pb-3">상태</th>
                    <th className="pb-3">위험도</th>
                    <th className="pb-3">세션</th>
                    <th className="pb-3">승인</th>
                    <th className="pb-3">발견</th>
                    <th className="pb-3">작업</th>
                  </tr>
                </thead>
                <tbody>
                  {inventory.map((h: any, idx: number) => (
                    <tr key={h.harness?.id || h.harness?.harness_id || idx}
                      className={`border-b border-gray-100 last:border-0 cursor-pointer ${selectedHarness === h.harness?.harness_id ? 'bg-blue-50' : 'hover:bg-gray-50'}`}
                      onClick={() => setSelectedHarness(selectedHarness === h.harness?.harness_id ? null : h.harness?.harness_id)}>
                      <td className="py-3">
                        <div className="font-mono text-xs">{h.harness?.harness_id?.slice(0, 20)}</div>
                        <div className="text-xs text-gray-400">v{h.harness?.binary_version}</div>
                      </td>
                      <td className="py-3 text-sm">{h.user?.name_ko || h.user?.name || '-'}</td>
                      <td className="py-3">{statusBadge(h.harness?.status)}</td>
                      <td className="py-3">{riskBadge(h.risk_score || 0)}</td>
                      <td className="py-3 text-sm">
                        {h.sessions?.length > 0 ? (
                          <button onClick={(e) => { e.stopPropagation(); inspectSession(h.sessions[0].session_id) }} className="text-blue-600 hover:underline">
                            {h.sessions.length}개 ▸
                          </button>
                        ) : <span className="text-gray-300">0</span>}
                      </td>
                      <td className="py-3">
                        {h.open_approvals > 0 ? <span className="badge-yellow">{h.open_approvals}</span> : <span className="text-gray-300">0</span>}
                      </td>
                      <td className="py-3">
                        {h.security_findings > 0 ? <span className="badge-red">{h.security_findings}</span> : <span className="text-gray-300">0</span>}
                      </td>
                      <td className="py-3">
                        <button onClick={(e) => { e.stopPropagation(); setActionPanel(h.harness?.harness_id) }}
                          className="btn-sm btn-secondary">관리 ▾</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>

          {/* Action History */}
          {actionHistory.length > 0 && (
            <div className="card mt-4">
              <h3 className="text-sm font-semibold mb-3">최근 플릿 작업 이력</h3>
              <div className="space-y-1">
                {actionHistory.slice(0, 8).map((e: any, i: number) => (
                  <div key={i} className="flex items-center gap-3 text-xs py-1 border-b border-gray-50 last:border-0">
                    <span className="font-mono text-gray-500">{e.occurred_at?.slice(11, 19)}</span>
                    <span className="font-medium text-gray-700">{e.action?.replace(/_/g, ' ')}</span>
                    <span className="text-gray-400">{e.resource_id?.slice(0, 15)}</span>
                    <span className="text-gray-400 ml-auto">{e.details?.slice(0, 40)}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Harness Detail Panel */}
        {selectedH && (
          <div className="col-span-5 space-y-4">
            <div className="card">
              <div className="flex items-center justify-between mb-3">
                <h3 className="text-sm font-semibold">하네스 상세</h3>
                <button onClick={() => setSelectedHarness(null)} className="text-gray-400 hover:text-gray-600">✕</button>
              </div>
              <div className="space-y-2 text-sm">
                <div className="flex justify-between"><span className="text-gray-500">하네스 ID</span><span className="font-mono text-xs">{selectedH.harness?.harness_id?.slice(0, 30)}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">사용자</span><span>{selectedH.user?.name_ko || selectedH.user?.name || '-'}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">이메일</span><span className="text-xs">{selectedH.user?.email || '-'}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">상태</span>{statusBadge(selectedH.harness?.status)}</div>
                <div className="flex justify-between"><span className="text-gray-500">위험도</span>{riskBadge(selectedH.risk_score || 0)}</div>
                <div className="flex justify-between"><span className="text-gray-500">버전</span><span>v{selectedH.harness?.binary_version}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">빌드</span><span className="font-mono text-xs">{selectedH.harness?.build_hash?.slice(0, 16) || '-'}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">릴리스 채널</span><span>{selectedH.harness?.release_channel || 'stable'}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">등록일</span><span className="text-xs">{selectedH.harness?.enrolled_at?.slice(0, 10)}</span></div>
                {selectedH.device?.id && (
                  <>
                    <div className="flex justify-between"><span className="text-gray-500">기기</span><span className="text-xs">{selectedH.device?.hostname || '-'}</span></div>
                    <div className="flex justify-between"><span className="text-gray-500">OS</span><span className="text-xs">{selectedH.device?.os || '-'}</span></div>
                  </>
                )}
              </div>
            </div>

            {/* Active Sessions */}
            {selectedH.sessions && selectedH.sessions.length > 0 && (
              <div className="card">
                <h3 className="text-sm font-semibold mb-3">활성 세션 ({selectedH.sessions.length})</h3>
                <div className="space-y-2">
                  {selectedH.sessions.map((s: any) => (
                    <div key={s.id} className="flex items-center justify-between p-2 bg-gray-50 rounded text-xs">
                      <div>
                        <div className="font-medium">{s.title || '제목 없음'}</div>
                        <div className="text-gray-400 font-mono">{s.session_id?.slice(0, 20)}</div>
                      </div>
                      <div className="flex gap-1">
                        <button onClick={() => inspectSession(s.session_id)} className="btn-sm btn-secondary">검사</button>
                        <button onClick={() => {
                          if (confirm('세션을 종료하시겠습니까?')) executeAction(selectedH.harness.harness_id, 'terminate_session', 'Session terminated by admin')
                        }} className="btn-sm btn-danger">종료</button>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Action Panel Modal */}
        {actionPanel && (
          <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50" onClick={() => setActionPanel(null)}>
            <div className="bg-white rounded-xl shadow-xl max-w-2xl w-full mx-4 max-h-[80vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
              <div className="p-5 border-b border-gray-100 flex items-center justify-between">
                <h3 className="font-semibold">플릿 작업 · Fleet Action</h3>
                <button onClick={() => setActionPanel(null)} className="text-gray-400 hover:text-gray-600">✕</button>
              </div>
              <div className="p-5">
                <div className="mb-4">
                  <label className="label">작업 사유 · Reason (필수)</label>
                  <input className="input" value={actionReason} onChange={e => setActionReason(e.target.value)} placeholder="작업 사유를 입력하세요" />
                </div>
                <div className="grid grid-cols-2 gap-2">
                  {FLEET_ACTIONS.map(a => (
                    <button key={a.id}
                      disabled={!actionReason}
                      onClick={() => {
                        if (a.severity === 'danger') {
                          if (confirm(`⚠️ ${a.label} (${a.labelEn})을(를) 실행하시겠습니까?\n이 작업은 되돌릴 수 없습니다.`)) {
                            executeAction(actionPanel, a.id, actionReason)
                          }
                        } else {
                          executeAction(actionPanel, a.id, actionReason)
                        }
                      }}
                      className={`p-3 rounded-lg text-left text-sm border transition-all disabled:opacity-30 disabled:cursor-not-allowed
                        ${a.severity === 'danger' ? 'border-red-200 hover:bg-red-50 hover:border-red-300' :
                          a.severity === 'warning' ? 'border-yellow-200 hover:bg-yellow-50 hover:border-yellow-300' :
                          'border-gray-200 hover:bg-blue-50 hover:border-blue-300'}`}>
                      <span className="mr-1">{a.icon}</span>
                      <span className="font-medium">{a.label}</span>
                      <span className="text-xs text-gray-400 block">{a.labelEn}</span>
                    </button>
                  ))}
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Session Inspector Modal */}
        {sessionInspector && (
          <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50" onClick={() => { setSessionInspector(null); setInspectorSessionId(null) }}>
            <div className="bg-white rounded-xl shadow-xl max-w-4xl w-full mx-4 max-h-[85vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
              <div className="p-5 border-b border-gray-100 flex items-center justify-between sticky top-0 bg-white z-10">
                <div>
                  <h3 className="font-semibold">세션 검사기 · Session Inspector</h3>
                  <p className="text-xs text-gray-500 font-mono">{inspectorSessionId?.slice(0, 30)}</p>
                </div>
                <button onClick={() => { setSessionInspector(null); setInspectorSessionId(null) }} className="text-gray-400 hover:text-gray-600">✕</button>
              </div>
              <div className="p-5 space-y-4">
                {/* Summary View */}
                <div className="grid grid-cols-3 gap-3">
                  <div className="bg-gray-50 rounded-lg p-3">
                    <div className="text-xs text-gray-500 mb-1">사용자</div>
                    <div className="font-medium text-sm">{sessionInspector.user?.name_ko || sessionInspector.user?.name || '-'}</div>
                  </div>
                  <div className="bg-gray-50 rounded-lg p-3">
                    <div className="text-xs text-gray-500 mb-1">모델</div>
                    <div className="font-medium text-sm">{sessionInspector.session?.model_class || '-'}</div>
                  </div>
                  <div className="bg-gray-50 rounded-lg p-3">
                    <div className="text-xs text-gray-500 mb-1">상태</div>
                    <div className="font-medium text-sm">{sessionInspector.session?.status || '-'}</div>
                  </div>
                </div>

                {/* Timeline */}
                <div>
                  <h4 className="text-xs font-semibold text-gray-600 mb-2">타임라인 · Timeline ({sessionInspector.actions?.length || 0} events)</h4>
                  <div className="space-y-1 max-h-48 overflow-y-auto">
                    {sessionInspector.actions?.map((a: any, i: number) => (
                      <div key={i} className="flex items-center gap-3 text-xs py-1 px-2 bg-gray-50 rounded">
                        <span className="font-mono text-gray-400 w-20">{a.occurred_at?.slice(11, 19)}</span>
                        <span className="font-medium w-32 truncate">{a.action_type || a.action || '-'}</span>
                        <span className="text-gray-500 truncate flex-1">{a.description || a.tool_name || '-'}</span>
                        <span className={`px-1.5 py-0.5 rounded text-[10px] ${a.outcome === 'success' ? 'bg-green-100 text-green-700' : a.outcome === 'error' ? 'bg-red-100 text-red-700' : 'bg-gray-100 text-gray-500'}`}>{a.outcome || a.policy_decision || '-'}</span>
                      </div>
                    ))}
                    {(!sessionInspector.actions || sessionInspector.actions.length === 0) && (
                      <p className="text-xs text-gray-400 text-center py-3">이벤트 없음</p>
                    )}
                  </div>
                </div>

                {/* Change Sets */}
                {sessionInspector.change_sets && sessionInspector.change_sets.length > 0 && (
                  <div>
                    <h4 className="text-xs font-semibold text-gray-600 mb-2">변경셋 · Change Sets ({sessionInspector.change_sets.length})</h4>
                    <div className="space-y-1">
                      {sessionInspector.change_sets.map((cs: any, i: number) => (
                        <div key={i} className="flex items-center gap-2 text-xs p-2 bg-gray-50 rounded">
                          <span className={`px-1.5 py-0.5 rounded ${cs.ai_generated ? 'bg-green-100 text-green-700' : 'bg-blue-100 text-blue-700'}`}>
                            {cs.ai_generated ? 'AI' : 'Human'}
                          </span>
                          <span className="font-mono">{cs.commit_sha?.slice(0, 12) || '-'}</span>
                          <span className="text-gray-500 truncate">{cs.message || cs.title || '-'}</span>
                          <span className="text-gray-400 ml-auto">+{cs.additions || 0} -{cs.deletions || 0}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* Security Findings */}
                {sessionInspector.security_findings && sessionInspector.security_findings.length > 0 && (
                  <div>
                    <h4 className="text-xs font-semibold text-gray-600 mb-2">보안 발견 · Security Findings ({sessionInspector.security_findings.length})</h4>
                    <div className="space-y-1">
                      {sessionInspector.security_findings.map((f: any, i: number) => (
                        <div key={i} className="flex items-center gap-2 text-xs p-2 bg-red-50 rounded">
                          <span className={`px-1.5 py-0.5 rounded ${f.severity === 'critical' ? 'bg-red-200 text-red-800' : 'bg-yellow-200 text-yellow-800'}`}>{f.severity}</span>
                          <span className="font-medium">{f.finding_type}</span>
                          <span className="text-gray-500 truncate">{f.title_ko || f.title || '-'}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* Open Approvals */}
                {sessionInspector.open_approvals && sessionInspector.open_approvals.length > 0 && (
                  <div>
                    <h4 className="text-xs font-semibold text-gray-600 mb-2">대기 중인 승인 · Open Approvals ({sessionInspector.open_approvals.length})</h4>
                    <div className="space-y-1">
                      {sessionInspector.open_approvals.map((a: any, i: number) => (
                        <div key={i} className="flex items-center gap-2 text-xs p-2 bg-yellow-50 rounded">
                          <span className="font-medium">{a.action_type || 'approval'}</span>
                          <span className="text-gray-500 truncate">{a.description || '-'}</span>
                          <span className="text-yellow-700 ml-auto">대기 중</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
