import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Compliance() {
  const [certs, setCerts] = useState<string[]>([])
  const [assessment, setAssessment] = useState<any>(null)
  const [selectedCert, setSelectedCert] = useState('')

  useEffect(() => {
    api.complianceCerts().then(setCerts).catch(() => {})
  }, [])

  const assess = async (cert: string) => {
    setSelectedCert(cert)
    try {
      const r = await api.complianceAssess(cert)
      setAssessment(r)
    } catch {}
  }

  const statusBadge = (s: string) => s === 'compliant' ? 'badge-green' : s === 'partially_compliant' ? 'badge-yellow' : 'badge-red'

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">컴플라이언스 <span className="text-gray-400 text-lg font-normal">Compliance</span></h1>

      <div className="grid grid-cols-5 gap-3 mb-6">
        {certs.map((c) => (
          <button
            key={c}
            onClick={() => assess(c)}
            className={`card text-center cursor-pointer hover:border-patty-400 ${selectedCert === c ? 'border-patty-500 ring-2 ring-patty-200' : ''}`}
          >
            <div className="font-bold text-sm">{c}</div>
          </button>
        ))}
      </div>

      {assessment && (
        <div className="card">
          <div className="flex items-center gap-3 mb-4">
            <h2 className="text-lg font-semibold">{assessment.certification} 평가 결과</h2>
            <span className={statusBadge(assessment.overall_status)}>
              {assessment.overall_status === 'compliant' ? '준수' : assessment.overall_status === 'partially_compliant' ? '부분 준수' : '갭 존재'}
            </span>
            {assessment.open_gaps > 0 && (
              <span className="badge-red">{assessment.open_gaps}개 갭</span>
            )}
          </div>

          {assessment.recommendations?.map((r: string, i: number) => (
            <div key={i} className="text-sm text-gray-600 mb-1">• {r}</div>
          ))}

          {assessment.control_results?.length > 0 && (
            <table className="w-full mt-4">
              <thead>
                <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                  <th className="pb-2">통제 ID</th>
                  <th className="pb-2">상태</th>
                  <th className="pb-2">비고</th>
                </tr>
              </thead>
              <tbody>
                {assessment.control_results.map((c: any, i: number) => (
                  <tr key={i} className="border-b border-gray-100 last:border-0">
                    <td className="py-2 font-mono text-xs">{c.control_id}</td>
                    <td className="py-2"><span className={statusBadge(c.status)}>{c.status}</span></td>
                    <td className="py-2 text-sm text-gray-500">{c.gap_description_ko || c.evidence || '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  )
}
