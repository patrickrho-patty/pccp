import { useState, useEffect } from 'react'
import { api } from '../api'
import EmptyState from '../components/EmptyState'

const CERT_NAMES: Record<string, { ko: string; en: string }> = {
  CSAP: { ko: '클라우드보안인증', en: 'CSAP' },
  ISMS_P: { ko: '정보보호관리체계 인증', en: 'ISMS-P' },
  ISMSP: { ko: '정보보호관리체계 인증', en: 'ISMS-P' },
  KISA: { ko: '한국인터넷진흥원 가이드라인', en: 'KISA Guidelines' },
  PRIVACY: { ko: '개인정보보호법', en: 'Privacy Act' },
  AI_BASIC: { ko: '인공지능 기본법', en: 'AI Basic Act' },
  'AI-BASIC': { ko: '인공지능 기본법', en: 'AI Basic Act' },
}

export default function Compliance() {
  const [certs, setCerts] = useState<string[]>([])
  const [selected, setSelected] = useState<string>('')
  const [assessment, setAssessment] = useState<any>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    api.complianceCerts().then(data => {
      const list = Array.isArray(data) ? data : []
      setCerts(list)
      if (list.length > 0 && !selected) {
        setSelected(list[0])
      }
    }).catch(() => {})
  }, [])

  useEffect(() => {
    if (!selected) return
    setLoading(true)
    api.complianceAssess(selected).then(data => setAssessment(data)).catch(() => setAssessment(null)).finally(() => setLoading(false))
  }, [selected])

  const overallBadge = (s: string) => {
    if (s === 'compliant') return 'badge-green'
    if (s === 'partially_compliant') return 'badge-yellow'
    return 'badge-red'
  }
  const overallLabel = (s: string) => {
    if (s === 'compliant') return '준수 · Compliant'
    if (s === 'partially_compliant') return '부분 준수 · Partial'
    return '갭 존재 · Gaps'
  }
  const controlBadge = (s: string) => s === 'compliant' ? 'badge-green' : s === 'partial' ? 'badge-yellow' : 'badge-red'

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">컴플라이언스 <span className="text-gray-400 text-lg font-normal">Compliance</span></h1>
        <select className="input w-auto" value={selected} onChange={e => setSelected(e.target.value)}>
          {certs.map(c => <option key={c} value={c}>{CERT_NAMES[c]?.ko || c} ({CERT_NAMES[c]?.en || c})</option>)}
        </select>
      </div>

      {loading ? (
        <div className="text-gray-400 text-center py-16 text-sm">평가 중... · Assessing...</div>
      ) : !assessment ? (
        <EmptyState icon="📋" title="평가를 불러올 수 없습니다" message="백엔드 컴플라이언스 서비스가 필요합니다" />
      ) : (
        <>
          {/* Overall status */}
          <div className="card mb-6 flex items-center justify-between">
            <div>
              <h2 className="text-sm font-semibold">{CERT_NAMES[assessment.certification]?.ko || assessment.certification} 평가</h2>
              <p className="text-xs text-gray-400 mt-1">평가 시간: {assessment.assessed_at?.slice(0, 19) || '-'}</p>
            </div>
            <span className={overallBadge(assessment.overall_status)}>{overallLabel(assessment.overall_status)}</span>
          </div>

          {/* Summary cards */}
          <div className="grid grid-cols-3 gap-4 mb-6">
            <div className="card text-center">
              <div className="text-2xl font-bold text-gray-700">{assessment.control_results?.length || 0}</div>
              <div className="text-xs text-gray-500">전체 통제</div>
            </div>
            <div className="card text-center">
              <div className="text-2xl font-bold text-green-600">{assessment.control_results?.filter((c: any) => c.status === 'compliant').length || 0}</div>
              <div className="text-xs text-gray-500">준수</div>
            </div>
            <div className="card text-center">
              <div className="text-2xl font-bold text-red-600">{assessment.open_gaps || 0}</div>
              <div className="text-xs text-gray-500">갭</div>
            </div>
          </div>

          {/* Control results */}
          <div className="card mb-6">
            <h3 className="text-sm font-semibold mb-3">통제 평가 결과 · Control Results</h3>
            <table className="w-full">
              <thead>
                <tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                  <th className="pb-3">통제 ID</th>
                  <th className="pb-3">상태</th>
                  <th className="pb-3">증거</th>
                </tr>
              </thead>
              <tbody>
                {(assessment.control_results || []).map((c: any, i: number) => (
                  <tr key={i} className="border-b border-gray-100 last:border-0">
                    <td className="py-3 text-sm font-mono">{c.control_id}</td>
                    <td className="py-3"><span className={controlBadge(c.status)}>{c.status}</span></td>
                    <td className="py-3 text-xs text-gray-500">{c.evidence || c.gap_desc || c.gap_desc_ko || '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Recommendations */}
          {assessment.recommendations && assessment.recommendations.length > 0 && (
            <div className="card">
              <h3 className="text-sm font-semibold mb-3">권장 사항 · Recommendations</h3>
              <ul className="space-y-2">
                {assessment.recommendations.map((r: string, i: number) => (
                  <li key={i} className="text-sm text-gray-600 flex items-start gap-2">
                    <span className="text-blue-500 mt-0.5">•</span>
                    <span>{r}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Disclaimer */}
          <p className="text-[10px] text-gray-400 mt-4 text-center">
            이 평가는 자가 평가(self-assessment)이며 공인 인증을 대체하지 않습니다. · This is a self-assessment; it does not constitute certified compliance.
          </p>
        </>
      )}
    </div>
  )
}
