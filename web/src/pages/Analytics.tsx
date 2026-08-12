import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Analytics() {
  const [usage, setUsage] = useState<any>(null)
  const [engineering, setEngineering] = useState<any>(null)
  const [security, setSecurity] = useState<any>(null)
  const [brief, setBrief] = useState<any>(null)

  useEffect(() => {
    fetch('/api/analytics/usage', { headers: authHeaders() }).then(r => r.json()).then(setUsage).catch(() => {})
    fetch('/api/analytics/engineering', { headers: authHeaders() }).then(r => r.json()).then(setEngineering).catch(() => {})
    fetch('/api/analytics/security', { headers: authHeaders() }).then(r => r.json()).then(setSecurity).catch(() => {})
    fetch('/api/korean/governance-brief', { headers: authHeaders() }).then(r => r.json()).then(setBrief).catch(() => {})
  }, [])

  const fmt = (n: number) => n?.toLocaleString() || '0'

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">분석 및 워크 인텔리전스 <span className="text-gray-400 text-lg font-normal">Analytics & Work Intelligence</span></h1>

      {/* Executive Summary */}
      {brief && (
        <div className="card mb-6">
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-lg font-semibold">경영진 요약 <span className="text-gray-400 text-sm font-normal">Executive Summary</span></h2>
            <span className={brief.compliance_status?.includes('양호') ? 'badge-green' : 'badge-yellow'}>
              {brief.compliance_status || '상태 확인 중'}
            </span>
          </div>
          <div className="grid grid-cols-5 gap-4">
            <div className="text-center p-3 bg-gray-50 rounded-lg">
              <div className="text-2xl font-bold">{brief.total_sessions || 0}</div>
              <div className="text-xs text-gray-500">세션 · Sessions</div>
            </div>
            <div className="text-center p-3 bg-gray-50 rounded-lg">
              <div className="text-2xl font-bold">{brief.active_harnesses || 0}</div>
              <div className="text-xs text-gray-500">하네스 · Harnesses</div>
            </div>
            <div className="text-center p-3 bg-gray-50 rounded-lg">
              <div className="text-2xl font-bold">{brief.model_invocations || 0}</div>
              <div className="text-xs text-gray-500">AI 추론 · Inferences</div>
            </div>
            <div className="text-center p-3 bg-gray-50 rounded-lg">
              <div className="text-2xl font-bold">{brief.code_changes || 0}</div>
              <div className="text-xs text-gray-500">코드 변경 · Changes</div>
            </div>
            <div className="text-center p-3 bg-gray-50 rounded-lg">
              <div className={`text-2xl font-bold ${(brief.security_findings || 0) > 0 ? 'text-red-600' : 'text-green-600'}`}>
                {brief.security_findings || 0}
              </div>
              <div className="text-xs text-gray-500">보안 발견 · Findings</div>
            </div>
          </div>
        </div>
      )}

      <div className="grid grid-cols-3 gap-6 mb-6">
        {/* Token Usage */}
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">토큰 사용량 · Token Usage</h3>
          <div className="space-y-3">
            <div>
              <div className="flex justify-between text-sm mb-1">
                <span className="text-gray-500">입력 토큰 · Input</span>
                <span className="font-bold">{fmt(usage?.total_tokens_in)}</span>
              </div>
              <div className="h-2 bg-gray-200 rounded-full overflow-hidden">
                <div className="h-full bg-blue-500 rounded-full" style={{ width: `${Math.min(100, (usage?.total_tokens_in || 0) / 100)}%` }} />
              </div>
            </div>
            <div>
              <div className="flex justify-between text-sm mb-1">
                <span className="text-gray-500">출력 토큰 · Output</span>
                <span className="font-bold">{fmt(usage?.total_tokens_out)}</span>
              </div>
              <div className="h-2 bg-gray-200 rounded-full overflow-hidden">
                <div className="h-full bg-green-500 rounded-full" style={{ width: `${Math.min(100, (usage?.total_tokens_out || 0) / 100)}%` }} />
              </div>
            </div>
          </div>
          {usage?.model_breakdown && Object.keys(usage.model_breakdown).length > 0 && (
            <div className="mt-4 pt-3 border-t border-gray-100">
              <h4 className="text-xs text-gray-500 mb-2">모델별 · By Model</h4>
              {Object.entries(usage.model_breakdown).map(([model, tokens]: [string, any]) => (
                <div key={model} className="flex justify-between text-xs py-1">
                  <span className="font-mono">{model}</span>
                  <span>{fmt(tokens)}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Engineering Metrics */}
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">엔지니어링 지표 · Engineering</h3>
          <div className="space-y-2">
            <div className="flex justify-between text-sm">
              <span className="text-gray-500">세션 · Sessions</span>
              <span className="font-bold">{engineering?.sessions || 0}</span>
            </div>
            <div className="flex justify-between text-sm">
              <span className="text-gray-500">AI 추론 · AI Inferences</span>
              <span className="font-bold">{engineering?.ai_inferences || 0}</span>
            </div>
            <div className="flex justify-between text-sm">
              <span className="text-gray-500">변경 세트 · Change Sets</span>
              <span className="font-bold">{engineering?.changes_created || 0}</span>
            </div>
            <div className="flex justify-between text-sm">
              <span className="text-gray-500">추가 라인 · Lines Added</span>
              <span className="font-bold text-green-600">+{engineering?.lines_added || 0}</span>
            </div>
            <div className="flex justify-between text-sm">
              <span className="text-gray-500">삭제 라인 · Lines Removed</span>
              <span className="font-bold text-red-600">-{engineering?.lines_removed || 0}</span>
            </div>
          </div>
        </div>

        {/* Security Metrics */}
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">보안 현황 · Security Posture</h3>
          <div className="space-y-2">
            <div className="flex justify-between text-sm">
              <span className="text-gray-500">전체 발견 · Total Findings</span>
              <span className="font-bold">{security?.total_findings || 0}</span>
            </div>
            <div className="flex justify-between text-sm">
              <span className="text-gray-500">치명적 · Critical</span>
              <span className="font-bold text-red-600">{security?.critical_count || 0}</span>
            </div>
            <div className="flex justify-between text-sm">
              <span className="text-gray-500">높음 · High</span>
              <span className="font-bold text-orange-600">{security?.high_count || 0}</span>
            </div>
            <div className="flex justify-between text-sm">
              <span className="text-gray-500">미해결 · Open</span>
              <span className="font-bold text-yellow-600">{security?.open_count || 0}</span>
            </div>
            <div className="flex justify-between text-sm">
              <span className="text-gray-500">해결됨 · Resolved</span>
              <span className="font-bold text-green-600">{security?.resolved_count || 0}</span>
            </div>
          </div>
          {security?.finding_by_type && Object.keys(security.finding_by_type).length > 0 && (
            <div className="mt-3 pt-3 border-t border-gray-100">
              <h4 className="text-xs text-gray-500 mb-2">유형별 · By Type</h4>
              {Object.entries(security.finding_by_type).map(([type, count]: [string, any]) => (
                <div key={type} className="flex justify-between text-xs py-0.5">
                  <span className="font-mono">{type}</span>
                  <span>{count}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Work Intelligence Notice */}
      <div className="card">
        <div className="flex items-start gap-3">
          <div className="text-yellow-500 text-xl">⚠</div>
          <div>
            <h3 className="text-sm font-semibold">평가 카드 고지사항 · Scorecard Notice</h3>
            <p className="text-xs text-gray-500 mt-1">
              워크 인텔리전스 평가 카드는 증거 기반으로 생성되며, 고용 결정에는 반드시 인간의 최종 확인이 필요합니다. (PRD §26.1)
              <br />
              Work Intelligence scorecards are evidence-based. Human finalization is required for any employment decision.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
