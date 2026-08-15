import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { formatRelative } from '../utils/format'

// ModelDetail (00 A7 /{entity}/:id) — deep-linkable model package view
// with endpoints, entitlement, and publish/recall actions.
export default function ModelDetail() {
  const { id } = useParams<{ id: string }>()
  const [pkg, setPkg] = useState<any>(null)
  const [endpoints, setEndpoints] = useState<any[]>([])
  const [epochs, setEpochs] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  const load = () => {
    if (!id) return
    api.listModels().then((d: any[]) => {
      const m = (Array.isArray(d) ? d : []).find((x: any) => x.id === id || x.package_id === id || x.model_id === id)
      setPkg(m || null)
      setLoading(false)
    }).catch(() => setLoading(false))
    api.listEndpoints().then((d: any[]) => setEndpoints((Array.isArray(d) ? d : []).filter((e: any) => e.model_id === id || e.model_package_id === id || e.model_id === pkg?.model_id))).catch(() => {})
    api.listEpochs().then((d: any[]) => setEpochs(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(() => { load() }, [id])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!pkg) return (
    <div>
      <Link to="/models" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 모델 목록</Link>
      <p className="text-gray-400 p-8 text-center">모델 패키지를 찾을 수 없습니다</p>
    </div>
  )

  const statusBadge = (s: string) =>
    s === 'published' ? 'badge-green' : s === 'recalled' ? 'badge-red' : 'badge-yellow'

  return (
    <div>
      <Link to="/models" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 모델 목록</Link>
      <div className="card mb-6 flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-bold">{pkg.name}</h1>
          <p className="text-xs text-gray-400 mt-1 font-mono">{pkg.model_id} · {pkg.package_id}</p>
        </div>
        <div className="flex gap-2 items-center">
          <span className={statusBadge(pkg.status)}>{pkg.status || '-'}</span>
          <span className="badge-blue">{pkg.entitlement_class || 'standard'}</span>
        </div>
      </div>

      <div className="card mb-4">
        <h3 className="text-sm font-semibold mb-3">패키지 정보 · Package</h3>
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div><span className="text-gray-500">패밀리:</span> {pkg.family || '-'}</div>
          <div><span className="text-gray-500">버전:</span> {pkg.version || '-'}</div>
          <div><span className="text-gray-500">양자화:</span> {pkg.quant_type || '-'}</div>
          <div><span className="text-gray-500">최소 보증 레벨:</span> {pkg.minimum_endpoint_assurance || 'L1'}</div>
          <div><span className="text-gray-500">가중치 머클 루트:</span> <span className="font-mono text-xs">{(pkg.weights_merkle_root || '-').slice(0, 24)}</span></div>
          <div><span className="text-gray-500">컨테이너 다이제스트:</span> <span className="font-mono text-xs">{(pkg.container_digest || '-').slice(0, 24)}</span></div>
          <div><span className="text-gray-500">생성일:</span> {formatRelative(pkg.created_at)}</div>
        </div>
      </div>

      <div className="card mb-4">
        <h3 className="text-sm font-semibold mb-3">서빙 엔드포인트 · Endpoints ({endpoints.length})</h3>
        {endpoints.length === 0 ? <p className="text-xs text-gray-400">연결된 엔드포인트 없음</p> : (
          <div className="space-y-2">
            {endpoints.map(e => (
              <div key={e.id} className="flex items-center gap-3 text-xs p-2 bg-gray-50 rounded">
                <Link to={`/endpoints/${e.id}`} className="text-blue-600 hover:underline font-mono">{e.endpoint_id}</Link>
                <span className={e.status === 'healthy' ? 'badge-green' : e.status === 'draining' ? 'badge-yellow' : 'badge-gray'}>{e.status}</span>
                <span className="text-gray-400 ml-auto">보증: {e.assurance_level || 'L1'}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="card mb-4">
        <h3 className="text-sm font-semibold mb-3">정책 허용 · Policy Allowance</h3>
        {epochs.length === 0 ? <p className="text-xs text-gray-400">에포크 없음</p> : (
          <div className="space-y-1">
            {epochs.slice(0, 5).map(e => (
              <div key={e.id} className="flex items-center gap-2 text-xs">
                <span className="font-mono text-gray-500">{e.epoch_id?.slice(0, 20)}</span>
                <span className={Array.isArray(e.allowed_models) && e.allowed_models.includes(pkg.model_id) ? 'badge-green' : 'badge-red'}>
                  {Array.isArray(e.allowed_models) && e.allowed_models.includes(pkg.model_id) ? '허용됨' : '허용 안 됨'}
                </span>
                <span className="text-gray-400">{formatRelative(e.created_at)}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="flex gap-2">
        {pkg.status !== 'published' && (
          <button className="btn-sm btn-primary" onClick={async () => { await api.publishModel(pkg.id); showToast('게시됨', 'success'); load() }}>게시 · Publish</button>
        )}
        {pkg.status === 'published' && (
          <button className="btn-sm btn-danger" onClick={async () => { await api.recallModel(pkg.id); showToast('리콜됨', 'info'); load() }}>리콜 · Recall</button>
        )}
      </div>
    </div>
  )
}
