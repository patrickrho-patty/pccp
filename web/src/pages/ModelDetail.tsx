import { useState, useEffect, useRef } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { formatRelative } from '../utils/format'
import { modelPackageState } from '../allowedModelView'

// ModelDetail (00 A7 /{entity}/:id) — deep-linkable model package view
// with endpoints, entitlement, and publish/recall actions.
export default function ModelDetail() {
  const { id } = useParams<{ id: string }>()
  const [pkg, setPkg] = useState<any>(null)
  const [allEndpoints, setAllEndpoints] = useState<any[]>([])
  const [epochs, setEpochs] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
	const loadGeneration = useRef(0)

  const load = () => {
    if (!id) return
	const generation = ++loadGeneration.current
	setLoading(true)
	setPkg(null)
	setAllEndpoints([])
	setEpochs([])
	api.getModel(id).then((model: any) => {
	  if (loadGeneration.current === generation) setPkg(model || null)
	}).catch(() => {
	  if (loadGeneration.current === generation) setPkg(null)
	}).finally(() => {
	  if (loadGeneration.current === generation) setLoading(false)
	})
	api.listEndpoints().then((d: any[]) => {
	  if (loadGeneration.current === generation) setAllEndpoints(Array.isArray(d) ? d : [])
	}).catch(() => {
	  if (loadGeneration.current === generation) setAllEndpoints([])
	})
	api.listEpochs().then((d: any[]) => {
	  if (loadGeneration.current === generation) setEpochs(Array.isArray(d) ? d : [])
	}).catch(() => {
	  if (loadGeneration.current === generation) setEpochs([])
	})
  }
	useEffect(() => {
	  load()
	  return () => { loadGeneration.current++ }
	}, [id])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!pkg) return (
    <div>
      <Link to="/models" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 모델 목록</Link>
      <p className="text-gray-400 p-8 text-center">모델 패키지를 찾을 수 없습니다</p>
    </div>
  )
	const packageState = modelPackageState(pkg)
	const endpoints = allEndpoints.filter((endpoint: any) => endpoint.model_id === pkg.model_id || endpoint.model_package_id === pkg.package_id || endpoint.model_package_id === pkg.id)
  const latestEpoch = epochs[0]
  const isPolicyAllowed = !!(latestEpoch && Array.isArray(latestEpoch.allowed_models) && latestEpoch.allowed_models.includes(pkg.model_id))
  const hasActiveEndpoint = endpoints.some((e: any) => e.status === 'active' || e.status === 'healthy')
  const usability = (() => {
    if (packageState !== 'published') return { label: '게시되지 않음', color: 'badge-yellow', reason: `패키지 상태가 ${packageState} 입니다 — 게시 후 정책 허용과 배포가 필요합니다`, blocking: '게시' }
    if (!isPolicyAllowed) return { label: '정책 미허용', color: 'badge-red', reason: `최신 에포크 ${latestEpoch?.epoch_id?.slice(0, 12) || '-'} 에서 허용되지 않았습니다`, blocking: '정책 허용' }
    if (!hasActiveEndpoint) return { label: '배포 없음', color: 'badge-yellow', reason: '활성 엔드포인트가 없습니다 — 배포 후 사용 가능합니다', blocking: '배포' }
    return { label: '사용 가능', color: 'badge-green', reason: '게시·정책 허용·활성 엔드포인트가 모두 충족되었습니다', blocking: null }
  })()

  const statusBadge = (s: string) =>
    s === 'published' ? 'badge-green' : s === 'recalled' ? 'badge-red' : 'badge-yellow'
  const koreanState = (s: string) => s === 'published' ? '게시됨' : s === 'draft' ? '초안' : s === 'recalled' ? '리콜됨' : s === 'deprecated' ? '사용 중단' : s

  return (
    <div>
      <Link to="/models" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 모델 목록</Link>
      <div className="card mb-6 flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-bold">{pkg.name}</h1>
          <p className="text-xs text-gray-400 mt-1 font-mono">{pkg.model_id} · {pkg.package_id}</p>
        </div>
        <div className="flex gap-2 items-center shrink-0">
          <span className={statusBadge(packageState)}>{koreanState(packageState)}</span>
          <span className="badge-blue">{pkg.entitlement_class || 'standard'}</span>
        </div>
      </div>

      <div className="card mb-4 border-l-4" style={{ borderLeftColor: usability.color === 'badge-green' ? '#22c55e' : usability.color === 'badge-red' ? '#ef4444' : '#eab308' }}>
        <h3 className="text-sm font-semibold mb-2">종합 가용성 · Usability</h3>
        <div className="flex items-center gap-2 mb-1">
          <span className={usability.color}>{usability.label}</span>
          {usability.blocking && <span className="text-[11px] text-gray-500">차단: {usability.blocking}</span>}
        </div>
        <p className="text-xs text-gray-500">{usability.reason}</p>
        <div className="grid grid-cols-3 gap-2 mt-2 text-[11px]">
          <div><span className="text-gray-400">카탈로그:</span> <span className={packageState === 'published' ? 'text-green-600' : 'text-amber-600'}>{koreanState(packageState)}</span></div>
          <div><span className="text-gray-400">정책 허용:</span> <span className={isPolicyAllowed ? 'text-green-600' : 'text-red-600'}>{isPolicyAllowed ? '허용됨' : '허용 안 됨'}</span> {latestEpoch && <span className="text-gray-400">· {latestEpoch.epoch_id?.slice(0, 12)}</span>}</div>
          <div><span className="text-gray-400">배포:</span> <span className={hasActiveEndpoint ? 'text-green-600' : 'text-gray-500'}>{hasActiveEndpoint ? `활성 ${endpoints.filter((e: any) => e.status === 'active' || e.status === 'healthy').length}개` : '없음'}</span></div>
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

      <div className="flex gap-2 shrink-0 flex-wrap">
        {packageState === 'draft' && (
          <button className="btn-sm btn-primary" onClick={async () => { await api.publishModel(pkg.id); showToast('게시됨', 'success'); load() }}>게시 · Publish</button>
        )}
        {packageState === 'published' && (
          <button className="btn-sm btn-danger" onClick={async () => { await api.recallModel(pkg.id); showToast('리콜됨', 'info'); load() }}>리콜 · Recall</button>
        )}
      </div>
    </div>
  )
}
