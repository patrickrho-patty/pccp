import { useState, useEffect } from 'react'
import EmptyState from '../components/EmptyState'
import { Link } from 'react-router-dom'
import { showToast } from '../components/Toast'
import { exportCSV } from '../utils/csv'

export default function Analytics() {
  const [usage, setUsage] = useState<any>(null)
  const [engineering, setEngineering] = useState<any>(null)
  const [security, setSecurity] = useState<any>(null)
  const [brief, setBrief] = useState<any>(null)
  const [cost, setCost] = useState<any>(null)

  useEffect(() => {
    fetch('/api/analytics/usage', { headers: authHeaders() }).then(r => r.json()).then(setUsage).catch(() => {})
    fetch('/api/analytics/engineering', { headers: authHeaders() }).then(r => r.json()).then(setEngineering).catch(() => {})
    fetch('/api/analytics/security', { headers: authHeaders() }).then(r => r.json()).then(setSecurity).catch(() => {})
    fetch('/api/korean/governance-brief', { headers: authHeaders() }).then(r => r.json()).then(setBrief).catch(() => {})
    // Server-side cost (real pricing from ModelPackage unit prices)
    fetch('/api/analytics/cost?days=30', { headers: authHeaders() as Record<string, string> }).then(r => r.json()).then(setCost).catch(() => setCost(null))
  }, [])

  const fmt = (n: number) => n?.toLocaleString() || '0'

  const doExport = () => {
    const rows: (string|number)[][] = []
    if (usage) {
      rows.push(['total_tokens_in', usage.total_tokens_in || 0])
      rows.push(['total_tokens_out', usage.total_tokens_out || 0])
    }
    exportCSV(`analytics_${new Date().toISOString().slice(0,10)}.csv`, ['metric', 'value'], rows)
    showToast('분석 데이터 내보내기 완료', 'success')
  }

  // Simple bar chart component using CSS
  const Bar = ({ label, value, max, color }: { label: string; value: number; max: number; color: string }) => {
    const pct = max > 0 ? Math.min(100, (value / max) * 100) : 0
    return (
      <div className="space-y-1">
        <div className="flex justify-between text-xs">
          <span className="text-gray-600">{label}</span>
          <span className="font-mono text-gray-800">{fmt(value)}</span>
        </div>
        <div className="h-6 bg-gray-100 rounded overflow-hidden">
          <div className={`h-full ${color} rounded flex items-center justify-end pr-2 text-xs text-white font-medium transition-all`}
               style={{ width: `${Math.max(pct, 2)}%` }}>
            {pct > 10 && `${pct.toFixed(0)}%`}
          </div>
        </div>
      </div>
    )
  }

  // Multi-bar comparison
  const BarGroup = ({ title, titleEn, bars }: { title: string; titleEn: string; bars: { label: string; value: number; color: string }[] }) => {
    const max = Math.max(...bars.map(b => b.value), 1)
    return (
      <div className="card">
        <h3 className="text-sm font-semibold mb-4">{title} <span className="text-gray-400 font-normal">{titleEn}</span></h3>
        <div className="space-y-3">
          {bars.map(b => <Bar key={b.label} label={b.label} value={b.value} max={max} color={b.color} />)}
        </div>
      </div>
    )
  }

  return (
    <div>
      <div className="flex justify-end mb-4"><button onClick={doExport} className="btn-secondary text-sm">CSV Export</button></div>
      <h1 className="text-2xl font-bold mb-6">분석 <span className="text-gray-400 text-lg font-normal">Analytics & Work Intelligence</span></h1>

      {/* Executive Summary */}
      {brief && (
        <div className="card mb-6">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold">경영진 요약 · Executive Summary</h3>
            <span className={brief.compliance_status?.includes('양호') ? 'badge-green' : 'badge-yellow'}>
              {brief.compliance_status || '상태 확인 중'}
            </span>
          </div>
          <div className="grid grid-cols-5 gap-3">
            {[
              { label: '세션', value: brief.total_sessions || 0, color: 'text-blue-600' },
              { label: '하네스', value: brief.active_harnesses || 0, color: 'text-green-600' },
              { label: 'AI 추론', value: brief.model_invocations || 0, color: 'text-purple-600' },
              { label: '코드 변경', value: brief.code_changes || 0, color: 'text-orange-600' },
              { label: '보안 발견', value: brief.security_findings || 0, color: (brief.security_findings || 0) > 0 ? 'text-red-600' : 'text-green-600' },
            ].map(s => (
              <div key={s.label} className="text-center p-3 bg-gray-50 rounded-lg">
                <div className={`text-2xl font-bold ${s.color}`}>{s.value}</div>
                <div className="text-xs text-gray-500">{s.label}</div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Charts */}
      <div className="grid grid-cols-2 gap-4 mb-6">
        <BarGroup title="토큰 사용량" titleEn="Token Usage" bars={[
          { label: '입력 토큰 · Input', value: usage?.total_tokens_in || 0, color: 'bg-blue-500' },
          { label: '출력 토큰 · Output', value: usage?.total_tokens_out || 0, color: 'bg-green-500' },
        ]} />

        <BarGroup title="엔지니어링 지표" titleEn="Engineering Metrics" bars={[
          { label: '세션 · Sessions', value: engineering?.sessions || 0, color: 'bg-purple-500' },
          { label: 'AI 추론 · Inferences', value: engineering?.ai_inferences || 0, color: 'bg-blue-500' },
          { label: '변경 세트 · Change Sets', value: engineering?.changes_created || 0, color: 'bg-green-500' },
          { label: '추가 라인 · Lines Added', value: engineering?.lines_added || 0, color: 'bg-teal-500' },
        ]} />
      </div>

      {/* Security + Model breakdown */}
      <div className="grid grid-cols-2 gap-4 mb-6">
        <BarGroup title="보안 현황" titleEn="Security Posture" bars={[
          { label: '전체 발견 · Total', value: security?.total_findings || 0, color: 'bg-gray-500' },
          { label: '치명적 · Critical', value: security?.critical_count || 0, color: 'bg-red-500' },
          { label: '높음 · High', value: security?.high_count || 0, color: 'bg-orange-500' },
          { label: '미해결 · Open', value: security?.open_count || 0, color: 'bg-yellow-500' },
        ]} />

        <div className="card">
          <h3 className="text-sm font-semibold mb-4">모델별 사용량 <span className="text-gray-400 font-normal">Model Breakdown</span></h3>
          {usage?.model_breakdown && Object.keys(usage.model_breakdown).length > 0 ? (
            <div className="space-y-2">
              {Object.entries(usage.model_breakdown).map(([model, tokens]: [string, any]) => {
                const maxTokens = Math.max(...Object.values(usage.model_breakdown) as number[], 1)
                return <Bar key={model} label={model} value={tokens} max={maxTokens} color="bg-blue-500" />
              })}
            </div>
          ) : (
            <EmptyState icon="📊" title="사용 데이터가 없습니다" message="세션이 실행되면 분석 데이터가 표시됩니다" />
          )}
        </div>
      </div>

      {/* Cost analysis — server-computed from ModelPackage unit prices */}
      <div className="card mb-6">
        <h3 className="text-sm font-semibold mb-4">비용 분석 · Cost Analysis</h3>
        <div className="grid grid-cols-3 gap-4 mb-4">
          <div className="text-center p-3 bg-gray-50 rounded-lg">
            <div className="text-2xl font-bold text-blue-600">{fmt(usage?.total_tokens_in || 0)}</div>
            <div className="text-xs text-gray-500">입력 토큰</div>
          </div>
          <div className="text-center p-3 bg-gray-50 rounded-lg">
            <div className="text-2xl font-bold text-green-600">{fmt(usage?.total_tokens_out || 0)}</div>
            <div className="text-xs text-gray-500">출력 토큰</div>
          </div>
          <div className="text-center p-3 bg-gray-50 rounded-lg">
            <div className="text-2xl font-bold text-purple-600">
              {cost?.any_priced
                ? Math.round(cost.total_cost_krw).toLocaleString() + ' KRW'
                : '단가 미설정'}
            </div>
            <div className="text-xs text-gray-500">예상 비용 (KRW, 최근 30일)</div>
          </div>
        </div>
        {cost?.models && cost.models.length > 0 && (
          <div className="mt-4 pt-3 border-t border-gray-100">
            <div className="text-xs font-semibold text-gray-600 mb-2">모델별 비용 · Cost by Model</div>
            <div className="space-y-2">
              {cost.models.map((m: any) => (
                <div key={m.model_package_id} className="flex justify-between items-center text-xs">
                  <span className="font-mono text-gray-600">{m.model_name || m.model_package_id}</span>
                  <div className="flex gap-4">
                    <span className="text-gray-500">{fmt((m.tokens_in || 0) + (m.tokens_out || 0))} 토큰</span>
                    <span className="font-medium text-gray-700">
                      {m.priced ? Math.round(m.cost_krw).toLocaleString() + ' KRW' : '단가 미설정'}
                    </span>
                  </div>
                </div>
              ))}
            </div>
            <p className="text-[10px] text-gray-400 mt-2">단가는 모델 패키지의 KRW/1K토큰 설정값 기반 · 가격이 없는 모델은 단가 미설정으로 표시</p>
          </div>
        )}
      </div>

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

function authHeaders(): Record<string, string> { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }
