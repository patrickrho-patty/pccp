import { useState, useEffect } from 'react'
import { api } from '../api'

type CertResult = {
  control_id: string; control_name: string; control_name_ko: string;
  category: string; status: string; evidence_query: string;
  implementing_feature: string; gap_description_ko?: string;
}

type Assessment = {
  certification: string; overall_status: string; open_gaps: number;
  recommendations: string[]; control_results: any[];
}

const CERT_INFO: Record<string, { name: string; name_ko: string; desc: string; desc_ko: string }> = {
  'CSAP': { name: 'Cloud Security Assurance Program', name_ko: '클라우드보안인증', desc: 'Government cloud security certification', desc_ko: '공공 클라우드 서비스 보안인증' },
  'KISA': { name: 'KISA Security Guidelines', name_ko: 'KISA 보안가이드라인', desc: 'Korea Internet & Security Agency guidelines', desc_ko: '한국인터넷진흥원 보안 가이드라인' },
  'ISMS-P': { name: 'ISMS-P', name_ko: '정보보호관리체계 인증', desc: 'Information Security Management System Certification', desc_ko: '정보보호 관리체계 인증 (개인정보보호)' },
  'PRIVACY': { name: 'Privacy Act', name_ko: '개인정보보호법', desc: 'Korean Personal Information Protection Act', desc_ko: '개인정보보호법 준수' },
  'AI-BASIC': { name: 'AI Basic Act', name_ko: '인공지능 기본법', desc: 'AI Development and Trust Foundation Act', desc_ko: '인공지능 발전과 신뢰 기반 조성 등에 관한 기본법' },
}

export default function Compliance() {
  const [tab, setTab] = useState<'overview' | 'assessment' | 'evidence'>('overview')
  const [certs, setCerts] = useState<string[]>([])
  const [assessment, setAssessment] = useState<Assessment | null>(null)
  const [selectedCert, setSelectedCert] = useState('')

  useEffect(() => {
    api.complianceCerts().then(data => setCerts(Array.isArray(data) ? data : [])).catch(() => {})
  }, [])

  const runAssessment = async (cert: string) => {
    setSelectedCert(cert)
    setTab('assessment')
    try {
      const r = await api.complianceAssess(cert)
      setAssessment(r)
    } catch {
      setAssessment(null)
    }
  }

  const statusColor = (s: string) => s === 'compliant' ? 'badge-green' : s === 'partial' ? 'badge-yellow' : 'badge-red'
  const statusText = (s: string) => s === 'compliant' ? '준수' : s === 'partial' ? '부분 준수' : s === 'gap' ? '갭' : 'N/A'

  const overallBadge = (s: string) => s === 'compliant' ? 'badge-green' : s === 'partially_compliant' ? 'badge-yellow' : 'badge-red'
  const overallText = (s: string) => s === 'compliant' ? '준수 (Compliant)' : s === 'partially_compliant' ? '부분 준수 (Partially Compliant)' : '갭 존재 (Gaps Exist)'

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">거버넌스 및 컴플라이언스 <span className="text-gray-400 text-lg font-normal">Governance & Compliance</span></h1>

      {/* Tab navigation */}
      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'overview', label: '프레임워크 개요', labelEn: 'Framework Overview' },
          { id: 'assessment', label: '평가 결과', labelEn: 'Assessment' },
          { id: 'evidence', label: '증거 수집', labelEn: 'Evidence' },
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
          <div className="grid grid-cols-3 gap-4 mb-6">
            {certs.map(cert => {
              const info = CERT_INFO[cert] || { name: cert, name_ko: cert, desc: '', desc_ko: '' }
              return (
                <div key={cert} className="card cursor-pointer hover:border-patty-400 hover:shadow-md transition-all"
                     onClick={() => runAssessment(cert)}>
                  <div className="flex items-start justify-between mb-2">
                    <div>
                      <h3 className="font-bold text-lg">{cert}</h3>
                      <p className="text-sm font-medium text-gray-600">{info.name_ko}</p>
                    </div>
                    <span className="badge-blue">평가 가능</span>
                  </div>
                  <p className="text-xs text-gray-500 mb-3">{info.desc_ko}</p>
                  <p className="text-xs text-gray-400">{info.name}</p>
                  <button className="btn-primary text-xs w-full mt-3">평가 실행 · Run Assessment</button>
                </div>
              )
            })}
          </div>

          {/* What is Compliance Management */}
          <div className="card">
            <h3 className="text-sm font-semibold mb-3">컴플라이언스 관리란? · About Compliance Management</h3>
            <div className="space-y-2 text-sm text-gray-600">
              <p>• PCCP는 각 인증 프레임워크의 통제(control)를 실제 시스템 기능에 매핑합니다.</p>
              <p>• 각 통제는 PCCP의 구현된 보안 기능에 대응되며, 증거 쿼리를 통해 감사 증거를 수집할 수 있습니다.</p>
              <p>• <strong>중요:</strong> 맵핑과 증거는 제품의 일부이지만, 인증 자체는 고객의 프로세스입니다. (PRD §41.4)</p>
              <p>• 인증 프레임워크는 법적 의무, 인증 기준, 공식 가이드라인, 보안 권장사항을 구분합니다. (PRD §41.3)</p>
            </div>
          </div>
        </div>
      )}

      {/* Assessment Tab */}
      {tab === 'assessment' && (
        <div>
          {selectedCert && (
            <div className="mb-4 flex gap-2 flex-wrap">
              {certs.map(cert => (
                <button
                  key={cert}
                  onClick={() => runAssessment(cert)}
                  className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${
                    selectedCert === cert ? 'bg-patty-600 text-white' : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                  }`}
                >
                  {cert}
                </button>
              ))}
            </div>
          )}

          {assessment ? (
            <div>
              {/* Overall Status */}
              <div className="card mb-4">
                <div className="flex items-center justify-between">
                  <div>
                    <h2 className="text-xl font-bold">
                      {assessment.certification} 평가 결과
                    </h2>
                    <p className="text-sm text-gray-500 mt-1">
                      {CERT_INFO[assessment.certification]?.name_ko || assessment.certification}
                    </p>
                  </div>
                  <div className="text-right">
                    <span className={`text-lg font-bold px-4 py-2 rounded-lg ${assessment.overall_status === 'compliant' ? 'bg-green-100 text-green-800' : assessment.overall_status === 'partially_compliant' ? 'bg-yellow-100 text-yellow-800' : 'bg-red-100 text-red-800'}`}>
                      {overallText(assessment.overall_status)}
                    </span>
                    {assessment.open_gaps > 0 && (
                      <div className="text-sm text-red-600 mt-1">{assessment.open_gaps}개 통제 갭</div>
                    )}
                  </div>
                </div>
              </div>

              {/* Recommendations */}
              {assessment.recommendations && assessment.recommendations.length > 0 && (
                <div className="card mb-4">
                  <h3 className="text-sm font-semibold mb-2">권장 조치 · Recommendations</h3>
                  <ul className="space-y-1">
                    {assessment.recommendations.map((r, i) => (
                      <li key={i} className="text-sm text-gray-600 flex items-start gap-2">
                        <span className="text-patty-500 mt-0.5">▸</span>
                        <span>{r}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {/* Control Details */}
              <div className="card">
                <h3 className="text-sm font-semibold mb-3">통제별 상세 · Control Details</h3>
                <table className="w-full">
                  <thead>
                    <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                      <th className="pb-2">통제 ID</th>
                      <th className="pb-2">통제명</th>
                      <th className="pb-2">구현 기능</th>
                      <th className="pb-2">상태</th>
                      <th className="pb-2">조치</th>
                    </tr>
                  </thead>
                  <tbody>
                    {assessment.control_results?.map((c: any, i: number) => (
                      <tr key={i} className="border-b border-gray-100 last:border-0 hover:bg-gray-50">
                        <td className="py-3 font-mono text-xs">{c.control_id}</td>
                        <td className="py-3">
                          <div className="text-sm font-medium">{c.control_name_ko || c.control_id}</div>
                          <div className="text-xs text-gray-400">{c.control_name || ''}</div>
                        </td>
                        <td className="py-3 text-xs text-gray-500">
                          {c.implementing_feature || '-'}
                        </td>
                        <td className="py-3">
                          <span className={statusColor(c.status)}>{statusText(c.status)}</span>
                        </td>
                        <td className="py-3">
                          {c.status !== 'compliant' && (
                            <button className="text-patty-600 text-xs hover:underline">
                              해결 계획 →
                            </button>
                          )}
                          {c.status === 'compliant' && c.evidence && (
                            <button className="text-gray-500 text-xs hover:underline">
                              증거 보기 →
                            </button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          ) : (
            <div className="card text-center py-12">
              <p className="text-gray-500">왼쪽에서 인증 프레임워크를 선택하여 평가를 실행하세요.</p>
            </div>
          )}
        </div>
      )}

      {/* Evidence Tab */}
      {tab === 'evidence' && (
        <div>
          <div className="card mb-4">
            <h3 className="text-lg font-semibold mb-2">증거 번들 생성 · Generate Evidence Bundle</h3>
            <p className="text-sm text-gray-500 mb-4">
              감사자 또는 인증 기관에 제출할 수 있는 증거 번들을 생성합니다.
              증거에는 감사 로그, 보안 발견, 정책 결정, 프로바이던스 체인이 포함됩니다.
            </p>
            <div className="flex gap-3">
              <select className="input max-w-xs">
                <option value="">기간 선택 · Period</option>
                <option value="7">최근 7일 · Last 7 days</option>
                <option value="30">최근 30일 · Last 30 days</option>
                <option value="90">최근 90일 · Last 90 days</option>
              </select>
              <button className="btn-primary" onClick={() => {
                api.generateReport('compliance_audit').then(() => {
                  alert('증거 번들이 생성되었습니다. 감사 로그에서 확인할 수 있습니다.')
                }).catch(() => alert('증거 번들 생성에 실패했습니다.'))
              }}>
                증거 번들 생성 · Generate
              </button>
            </div>
          </div>

          <div className="card">
            <h3 className="text-sm font-semibold mb-3">증거 항목 · Evidence Items</h3>
            <div className="space-y-2">
              {[
                { type: '감사 로그', typeEn: 'Audit Log', desc: '모든 관리자 활동 및 시스템 이벤트', count: '자동 수집' },
                { type: '보안 발견', typeEn: 'Security Findings', desc: 'DLP/PII/시크릿/인젝션 발견 이력', count: '실시간' },
                { type: '정책 결정', typeEn: 'Policy Decisions', desc: '거버넌스 교환 승인/거부 이력', count: '실시간' },
                { type: '프로바이던스 체인', typeEn: 'Provenance Chain', desc: '코드 변경에 대한 전체 추적 가능성', count: '영구' },
                { type: '증거 영수증', typeEn: 'Evidence Receipts', desc: 'COSE-Sign1 서명된 거버넌스 영수증', count: '영구' },
                { type: '엔드포인트 증명', typeEn: 'Endpoint Attestation', desc: '모델 엔드포인트 보증 수준 기록', count: '주기적' },
              ].map(item => (
                <div key={item.typeEn} className="flex items-center justify-between py-2 border-b border-gray-100 last:border-0">
                  <div>
                    <span className="text-sm font-medium">{item.type}</span>
                    <span className="text-xs text-gray-400 ml-2">{item.typeEn}</span>
                  </div>
                  <div className="text-right">
                    <div className="text-xs text-gray-500">{item.desc}</div>
                    <div className="text-xs text-gray-400">{item.count}</div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
