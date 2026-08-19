import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { formatRelative } from '../utils/format'

// EndpointDetail (00 A7 /{entity}/:id) — deep-linkable inference
// endpoint view with attestation + lease actions.
export default function EndpointDetail() {
  const { id } = useParams<{ id: string }>()
  const [ep, setEp] = useState<any>(null)
  const [modelPkg, setModelPkg] = useState<any>(null)
  const [loading, setLoading] = useState(true)

  const load = () => {
    if (!id) return
    api.listEndpoints().then((d: any[]) => {
      const e = (Array.isArray(d) ? d : []).find((x: any) => x.id === id || x.endpoint_id === id)
      setEp(e || null)
      setLoading(false)
      if (e?.model_package_id) {
        api.getModel(e.model_package_id).then((m: any) => {
          setModelPkg(m || null)
        }).catch(() => {
          api.listModels().then((list: any[]) => {
            const found = (Array.isArray(list) ? list : []).find((m: any) => m.package_id === e.model_package_id || m.id === e.model_package_id)
            setModelPkg(found || null)
          }).catch(() => setModelPkg(null))
        })
      } else {
        setModelPkg(null)
      }
    }).catch(() => setLoading(false))
  }
  useEffect(() => { load() }, [id])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!ep) return (
    <div>
      <Link to="/models" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 모델 목록</Link>
      <p className="text-gray-400 p-8 text-center">엔드포인트를 찾을 수 없습니다</p>
    </div>
  )

  const statusBadge = (s: string) =>
    s === 'healthy' ? 'badge-green' : s === 'degraded' ? 'badge-yellow' : s === 'draining' ? 'badge-yellow' : 'badge-red'

  return (
    <div>
      <Link to="/models" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 모델 목록</Link>
      <div className="card mb-6 flex items-start justify-between">
        <div className="min-w-0 flex-1">
          <h1 className="text-2xl font-bold font-mono text-lg truncate" title={ep.endpoint_id}>{ep.endpoint_id}</h1>
          <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
            <span className="text-gray-500 shrink-0">모델:</span>
            {ep.model_package_id ? (
              modelPkg ? (
                <>
                  <Link
                    to={`/models/${modelPkg.package_id}`}
                    className="text-blue-600 hover:underline font-medium truncate max-w-[24rem]"
                    title={modelPkg.name_ko || modelPkg.name || modelPkg.package_id}
                    aria-label={`모델 ${modelPkg.name_ko || modelPkg.name || modelPkg.package_id}`}
                  >
                    {modelPkg.name_ko || modelPkg.name || modelPkg.package_id}
                  </Link>
                  <span className="text-gray-400 font-mono text-[11px] truncate" title={`${modelPkg.package_id} · ${modelPkg.version || '-'}`}>
                    {modelPkg.package_id} · {modelPkg.version || '-'}
                  </span>
                </>
              ) : (
                <>
                  <span className="text-gray-400" title="삭제되었거나 열람할 수 없습니다">모델을 확인할 수 없습니다</span>
                  <span className="text-gray-400 font-mono text-[11px]">{ep.model_package_id}</span>
                </>
              )
            ) : (
              <span className="text-gray-400">모델 정보 없음</span>
            )}
          </div>
          <div className="mt-1 flex items-center gap-2 text-xs">
            <span className="text-gray-500">상태:</span>
            <span className={statusBadge(ep.status)}>{ep.status || '-'}</span>
          </div>
        </div>
        <div className="flex gap-2 items-center shrink-0 ml-4">
          <span className="badge-blue">보증 {ep.assurance_level || 'L1'}</span>
        </div>
      </div>

      <div className="card mb-4">
        <h3 className="text-sm font-semibold mb-3">엔드포인트 정보 · Endpoint</h3>
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div><span className="text-gray-500">주소:</span> <span className="font-mono text-xs">{ep.address || '-'}</span></div>
          <div><span className="text-gray-500">엔진:</span> {ep.engine || ep.serving_engine || '-'}</div>
          <div><span className="text-gray-500">보증 레벨:</span> {ep.assurance_level || 'L1'}</div>
          <div><span className="text-gray-500">마지막 증명:</span> {formatRelative(ep.last_attestation_at || ep.attested_at)}</div>
          <div><span className="text-gray-500">리스 수:</span> {ep.lease_count ?? '-'}</div>
          <div><span className="text-gray-500">등록일:</span> {formatRelative(ep.created_at)}</div>
        </div>
      </div>

      <div className="flex gap-2 shrink-0 flex-wrap">
        <button className="btn-sm btn-primary" onClick={async () => { await api.issueEndpointLease(ep.id || ep.endpoint_id); showToast('리스 발급됨', 'success') }}>리스 발급 · Issue Lease</button>
        {ep.status !== 'draining' && (
          <button className="btn-sm btn-secondary" onClick={async () => { await fetch(`/api/endpoints/${ep.id}/drain`, { method: 'POST', headers: { Authorization: `Bearer ${sessionStorage.getItem('pccp_token') || ''}` } }); showToast('드레인 시작', 'info'); load() }}>드레인 · Drain</button>
        )}
      </div>
    </div>
  )
}
