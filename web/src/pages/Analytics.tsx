import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Analytics() {
  const [usage, setUsage] = useState<any>(null)
  const [engineering, setEngineering] = useState<any>(null)
  const [security, setSecurity] = useState<any>(null)

  useEffect(() => {
    api.dashboard().then(() => {})
    // Fetch analytics data
    fetch('/api/analytics/usage', { headers: authHeaders() }).then(r => r.json()).then(data => setUsage(Array.isArray(data) ? data : data || [])).catch(() => {})
    fetch('/api/analytics/engineering', { headers: authHeaders() }).then(r => r.json()).then(data => setEngineering(Array.isArray(data) ? data : data || [])).catch(() => {})
    fetch('/api/analytics/security', { headers: authHeaders() }).then(r => r.json()).then(data => setSecurity(Array.isArray(data) ? data : data || [])).catch(() => {})
  }, [])

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">분석 <span className="text-gray-400 text-lg font-normal">Analytics & Work Intelligence</span></h1>

      <div className="grid grid-cols-3 gap-6 mb-6">
        {/* Usage stats */}
        <div className="card">
          <h2 className="text-lg font-semibold mb-4">사용량 <span className="text-gray-400 text-sm font-normal">Usage</span></h2>
          {usage ? (
            <div className="space-y-2">
              <div className="flex justify-between"><span className="text-gray-500">입력 토큰</span><span className="font-mono">{usage.total_tokens_in?.toLocaleString() || 0}</span></div>
              <div className="flex justify-between"><span className="text-gray-500">출력 토큰</span><span className="font-mono">{usage.total_tokens_out?.toLocaleString() || 0}</span></div>
              <div className="flex justify-between"><span className="text-gray-500">모델 수</span><span className="font-mono">{Object.keys(usage.model_breakdown || {}).length}</span></div>
            </div>
          ) : <p className="text-gray-400 text-sm">데이터 없음</p>}
        </div>

        {/* Engineering metrics */}
        <div className="card">
          <h2 className="text-lg font-semibold mb-4">엔지니어링 <span className="text-gray-400 text-sm font-normal">Engineering</span></h2>
          {engineering ? (
            <div className="space-y-2">
              <div className="flex justify-between"><span className="text-gray-500">세션</span><span className="font-mono">{engineering.sessions || 0}</span></div>
              <div className="flex justify-between"><span className="text-gray-500">AI 추론</span><span className="font-mono">{engineering.ai_inferences || 0}</span></div>
              <div className="flex justify-between"><span className="text-gray-500">변경 세트</span><span className="font-mono">{engineering.changes_created || 0}</span></div>
              <div className="flex justify-between"><span className="text-gray-500">추가 라인</span><span className="font-mono text-green-600">+{engineering.lines_added || 0}</span></div>
            </div>
          ) : <p className="text-gray-400 text-sm">데이터 없음</p>}
        </div>

        {/* Security metrics */}
        <div className="card">
          <h2 className="text-lg font-semibold mb-4">보안 <span className="text-gray-400 text-sm font-normal">Security</span></h2>
          {security ? (
            <div className="space-y-2">
              <div className="flex justify-between"><span className="text-gray-500">전체 발견</span><span className="font-mono">{security.total_findings || 0}</span></div>
              <div className="flex justify-between"><span className="text-gray-500">치명적</span><span className="font-mono text-red-600">{security.critical_count || 0}</span></div>
              <div className="flex justify-between"><span className="text-gray-500">높음</span><span className="font-mono text-orange-600">{security.high_count || 0}</span></div>
              <div className="flex justify-between"><span className="text-gray-500">미해결</span><span className="font-mono text-yellow-600">{security.open_count || 0}</span></div>
            </div>
          ) : <p className="text-gray-400 text-sm">데이터 없음</p>}
        </div>
      </div>

      <div className="card">
        <h2 className="text-lg font-semibold mb-2">모델별 사용량 <span className="text-gray-400 text-sm font-normal">Model Breakdown</span></h2>
        {usage?.model_breakdown && Object.keys(usage.model_breakdown).length > 0 ? (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-2">모델</th>
                <th className="pb-2">입력 토큰</th>
                <th className="pb-2">출력 토큰</th>
                <th className="pb-2">세션</th>
              </tr>
            </thead>
            <tbody>
              {Object.entries(usage.model_breakdown).map(([model, data]: [string, any]) => (
                <tr key={model} className="border-b border-gray-100 last:border-0">
                  <td className="py-2 font-mono text-sm">{model}</td>
                  <td className="py-2 text-sm">{data.tokens_in?.toLocaleString()}</td>
                  <td className="py-2 text-sm">{data.tokens_out?.toLocaleString()}</td>
                  <td className="py-2 text-sm">{data.sessions}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : <p className="text-gray-400 text-sm">사용 데이터가 없습니다</p>}
      </div>

      <div className="card mt-6">
        <div className="flex items-center gap-2 mb-2">
          <h2 className="text-lg font-semibold">평가 카드</h2>
          <span className="badge-yellow">인간 최종 확인 필요</span>
        </div>
        <p className="text-sm text-gray-500">
          워크 인텔리전스 평가 카드는 증거 기반으로 생성되며, 고용 결정에는 반드시 인간의 최종 확인이 필요합니다. (PRD §26.1)
        </p>
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
