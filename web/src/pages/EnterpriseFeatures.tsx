import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'

const CATEGORY_INFO: Record<string, { icon: string; name: string; nameEn: string }> = {
  governance: { icon: '⚖️', name: '거버넌스', nameEn: 'Governance' },
  security: { icon: '🛡', name: '보안', nameEn: 'Security' },
  compliance: { icon: '📋', name: '컴플라이언스', nameEn: 'Compliance' },
  identity: { icon: '🔑', name: '신원', nameEn: 'Identity' },
  audit: { icon: '☰', name: '감사', nameEn: 'Audit' },
}

type Feature = {
  id: string
  feature_key: string
  feature_name: string
  feature_name_ko: string
  category: string
  prd_ref: string
  enabled: boolean
  enforced: boolean
  status: string
  last_reported_at: string
  violation_count: number
  config: string
}

type Violation = {
  id: string
  harness_id: string
  session_id: string
  feature_key: string
  severity: string
  description: string
  description_ko: string
  resolved: boolean
  occurred_at: string
}

export default function EnterpriseFeatures() {
  const [features, setFeatures] = useState<Feature[]>([])
  const [violations, setViolations] = useState<Violation[]>([])
  const [tab, setTab] = useState<'features' | 'violations'>('features')
  const [loading, setLoading] = useState(true)

  const load = () => {
    const h = authHeaders()
    Promise.all([
      fetch('/api/enterprise/features', { headers: h }).then(r => r.json()).catch(() => []),
      fetch('/api/enterprise/violations', { headers: h }).then(r => r.json()).catch(() => []),
    ]).then(([f, v]) => {
      setFeatures(Array.isArray(f) ? f : [])
      setViolations(Array.isArray(v) ? v : [])
      setLoading(false)
    })
  }
  useEffect(() => { load() }, [])

  const seed = async () => {
    await fetch('/api/enterprise/features/seed', { method: 'POST', headers: authHeaders() })
    load()
  }

  const toggleFeature = async (f: Feature, field: 'enabled' | 'enforced') => {
    await fetch(`/api/enterprise/features/${f.id}`, {
      method: 'PUT',
      headers: { ...authHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...f, [field]: !f[field] }),
    })
    load()
  }

  const resolveViolation = async (id: string) => {
    await fetch(`/api/enterprise/violations/${id}`, { method: 'PUT', headers: authHeaders() })
    load()
  }

  if (loading) return <div className="text-gray-500">로딩 중...</div>

  const stats = {
    total: features.length,
    enabled: features.filter(f => f.enabled).length,
    enforced: features.filter(f => f.enforced).length,
    violations: violations.filter(v => !v.resolved).length,
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-4">
        <div>
          <h1 className="text-2xl font-bold">엔터프라이즈 하네스 기능 <span className="text-gray-400 text-lg font-normal">Enterprise Harness Features</span></h1>
          <p className="text-xs text-gray-400 mt-1">기업/정부 전용 하네스 기능 · PRD §33 Korean Enterprise Differentiators · 공용 에디션 미제공</p>
        </div>
        {features.length === 0 && <button onClick={seed} className="btn-primary text-sm">20개 기본 기능 등록</button>}
      </div>

      {/* Stats */}
      <div className="grid grid-cols-4 gap-3 mb-6">
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-blue-600">{stats.total}</div><div className="text-xs text-gray-500">전체 기능</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-green-600">{stats.enabled}</div><div className="text-xs text-gray-500">활성화</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-purple-600">{stats.enforced}</div><div className="text-xs text-gray-500">강제 적용</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-red-600">{stats.violations}</div><div className="text-xs text-gray-500">위반</div></div>
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'features', label: '기능 목록', en: 'Features' },
          { id: 'violations', label: '위반 사항', en: 'Violations', count: stats.violations },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.count !== undefined && t.count > 0 && <span className="badge-red text-[10px] ml-1">{t.count}</span>}
          </button>
        ))}
      </div>

      {/* Features Tab */}
      {tab === 'features' && (
        <div className="space-y-4">
          {Object.entries(CATEGORY_INFO).map(([catId, catInfo]) => {
            const catFeatures = features.filter(f => f.category === catId)
            if (catFeatures.length === 0) return null
            return (
              <div key={catId} className="card">
                <div className="flex items-center gap-2 mb-3">
                  <span className="text-xl">{catInfo.icon}</span>
                  <h3 className="text-sm font-semibold">{catInfo.name} <span className="text-gray-400 font-normal">{catInfo.nameEn}</span></h3>
                  <span className="badge-gray ml-auto">{catFeatures.length}개</span>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  {catFeatures.map(f => (
                    <div key={f.id} className={`p-3 rounded-lg border ${f.enabled ? 'border-gray-200' : 'border-gray-100 bg-gray-50 opacity-60'}`}>
                      <div className="flex items-start justify-between mb-1">
                        <div className="flex-1">
                          <div className="text-sm font-medium">{f.feature_name_ko || f.feature_name}</div>
                          <div className="text-xs text-gray-400">{f.feature_name}</div>
                        </div>
                        <div className="flex items-center gap-1">
                          {f.enforced && <span className="badge-red text-[10px]">의무</span>}
                          <Link to="/audit" className="text-[10px] text-gray-400 hover:underline">{f.prd_ref}</Link>
                        </div>
                      </div>
                      <div className="flex items-center justify-between mt-2">
                        <div className="flex items-center gap-2 text-xs">
                          {f.violation_count > 0 && <span className="text-red-500">⚠ {f.violation_count} 위반</span>}
                          {f.last_reported_at && <span className="text-gray-400">마지막 보고: {f.last_reported_at.slice(0, 16)}</span>}
                        </div>
                        <div className="flex items-center gap-3">
                          <label className="flex items-center gap-1 text-xs cursor-pointer">
                            <input type="checkbox" checked={f.enforced} onChange={() => toggleFeature(f, 'enforced')} className="w-3 h-3" />
                            <span className="text-gray-500">강제</span>
                          </label>
                          <button onClick={() => toggleFeature(f, 'enabled')}
                            className={`relative inline-flex h-4 w-7 items-center rounded-full transition-colors ${f.enabled ? 'bg-patty-600' : 'bg-gray-300'}`}>
                            <span className={`inline-block h-2.5 w-2.5 rounded-full bg-white transition-transform ${f.enabled ? 'translate-x-4' : 'translate-x-1'}`} />
                          </button>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )
          })}
          {features.length === 0 && (
            <div className="card text-center py-12">
              <p className="text-gray-400 mb-2">등록된 엔터프라이즈 기능이 없습니다</p>
              <button onClick={seed} className="btn-primary text-sm mt-2">20개 기본 기능 등록</button>
            </div>
          )}
        </div>
      )}

      {/* Violations Tab */}
      {tab === 'violations' && (
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">엔터프라이즈 정책 위반 · Enterprise Policy Violations</h3>
          {violations.length === 0 ? (
            <p className="text-gray-400 text-center py-8">위반 사항이 없습니다 ✅</p>
          ) : (
            <table className="w-full">
              <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                <th className="pb-3">기능</th><th className="pb-3">심각도</th><th className="pb-3">설명</th>
                <th className="pb-3">하네스</th><th className="pb-3">시간</th><th className="pb-3"></th>
              </tr></thead>
              <tbody>
                {violations.map(v => (
                  <tr key={v.id} className="border-b border-gray-100 last:border-0">
                    <td className="py-3 text-xs font-mono">{v.feature_key}</td>
                    <td className="py-3"><span className={v.severity === 'critical' ? 'badge-red' : v.severity === 'high' ? 'badge-yellow' : 'badge-gray'}>{v.severity}</span></td>
                    <td className="py-3 text-sm">{v.description_ko || v.description}</td>
                    <td className="py-3 text-xs font-mono"><Link to="/harnesses" className="text-blue-600 hover:underline">{v.harness_id?.slice(0, 15)}</Link></td>
                    <td className="py-3 text-xs text-gray-400">{v.occurred_at?.slice(0, 19)}</td>
                    <td className="py-3"><button onClick={() => resolveViolation(v.id)} className="text-xs text-green-600 hover:underline">해결</button></td>
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

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
