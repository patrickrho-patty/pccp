import { useState, useEffect } from 'react'
import { api } from '../api'

export default function ModelCatalog() {
  const [models, setModels] = useState<any[]>([])
  const [epoch, setEpoch] = useState<any>(null)
  const [loading, setLoading] = useState(true)

  const load = () => {
    Promise.all([
      fetch('/api/catalog/models', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
      fetch('/api/catalog/epoch', { headers: authHeaders() }).then(r => r.json()).catch(() => null),
    ]).then(([m, e]) => {
      setModels(Array.isArray(m) ? m : [])
      setEpoch(e)
      setLoading(false)
    })
  }

  useEffect(() => { load() }, [])

  const handleSeed = async () => {
    await api.catalogSeed()
    load()
  }

  const handleWithdraw = async (id: string) => {
    if (!confirm('이 모델을 카탈로그에서 철회하시겠습니까? 철회 후 새 요청이 거부됩니다.')) return
    await api.catalogWithdraw(id)
    load()
  }

  const availBadge = (a: string) => a === 'available' ? 'badge-green' : a === 'degraded' ? 'badge-yellow' : 'badge-red'
  const availText = (a: string) => a === 'available' ? '사용 가능' : a === 'degraded' ? '성능 저하' : '철회됨'

  if (loading) return <div className="text-gray-500">로딩 중...</div>

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <div>
          <h1 className="text-2xl font-bold">모델 카탈로그 <span className="text-gray-400 text-lg font-normal">Model Catalog</span></h1>
          <p className="text-sm text-gray-500 mt-1">서버 권한 모델 관리 · Server-Authoritative Model Management (v2)</p>
        </div>
        <button onClick={handleSeed} className="btn-secondary text-sm">기본 모델 등록</button>
      </div>

      {/* Catalog Epoch Info */}
      {epoch && (
        <div className="bg-gray-50 border border-gray-200 rounded-lg p-4 mb-6">
          <div className="flex items-center justify-between">
            <div>
              <span className="text-sm font-medium">현재 카탈로그 에포크 · Current Catalog Epoch</span>
              <div className="font-mono text-xs text-gray-500 mt-1">{epoch.epoch_id?.slice(0, 40)}</div>
            </div>
            <div className="text-right">
              <span className="badge-blue">{models.length} 모델</span>
              {epoch.min_validity_secs && (
                <span className="text-xs text-gray-400 ml-2">유효 {epoch.min_validity_secs}초</span>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Model Cards */}
      <div className="grid grid-cols-2 gap-4">
        {models.length === 0 ? (
          <div className="col-span-2 card text-center py-12">
            <p className="text-gray-400 mb-2">등록된 카탈로그 모델이 없습니다.</p>
            <p className="text-sm text-gray-400">"기본 모델 등록" 버튼으로 Patty Code Standard/Pro를 추가하세요.</p>
            <p className="text-xs text-gray-400 mt-2">PCCP v2: 하네스는 로컬 모델 목록을 가지지 않으며, 이 카탈로그만 렌더링합니다.</p>
          </div>
        ) : models.map((m) => (
          <div key={m.catalog_model_id} className="card border-l-4" style={{ borderLeftColor: m.availability === 'available' ? '#10b981' : m.availability === 'degraded' ? '#f59e0b' : '#ef4444' }}>
            <div className="flex items-start justify-between mb-3">
              <div>
                <h3 className="font-bold text-lg">{m.display_name_ko || m.display_name}</h3>
                <p className="text-sm text-gray-500">{m.display_name}</p>
                <p className="text-xs text-gray-400 mt-1 font-mono">{m.catalog_model_id}</p>
              </div>
              <span className={availBadge(m.availability)}>{availText(m.availability)}</span>
            </div>

            {m.description_ko && <p className="text-sm text-gray-600 mb-3">{m.description_ko}</p>}

            {/* Capabilities */}
            <div className="grid grid-cols-2 gap-2 mb-3">
              <div className="flex items-center gap-1.5 text-xs">
                <span className={`w-1.5 h-1.5 rounded-full ${m.capabilities?.input?.text ? 'bg-green-500' : 'bg-gray-300'}`} />
                <span className="text-gray-600">텍스트 입력</span>
              </div>
              <div className="flex items-center gap-1.5 text-xs">
                <span className={`w-1.5 h-1.5 rounded-full ${m.capabilities?.input?.image ? 'bg-green-500' : 'bg-gray-300'}`} />
                <span className="text-gray-600">이미지 입력</span>
              </div>
              <div className="flex items-center gap-1.5 text-xs">
                <span className={`w-1.5 h-1.5 rounded-full ${m.capabilities?.tools?.client_tools ? 'bg-green-500' : 'bg-gray-300'}`} />
                <span className="text-gray-600">도구 호출</span>
              </div>
              <div className="flex items-center gap-1.5 text-xs">
                <span className={`w-1.5 h-1.5 rounded-full ${m.capabilities?.reasoning?.supported ? 'bg-green-500' : 'bg-gray-300'}`} />
                <span className="text-gray-600">추론 기능</span>
              </div>
              <div className="flex items-center gap-1.5 text-xs">
                <span className={`w-1.5 h-1.5 rounded-full ${m.capabilities?.cache?.prompt_cache ? 'bg-green-500' : 'bg-gray-300'}`} />
                <span className="text-gray-600">프롬프트 캐시</span>
              </div>
              <div className="flex items-center gap-1.5 text-xs">
                <span className={`w-1.5 h-1.5 rounded-full ${m.capabilities?.streaming ? 'bg-green-500' : 'bg-gray-300'}`} />
                <span className="text-gray-600">스트리밍</span>
              </div>
            </div>

            {/* Limits */}
            <div className="flex gap-4 text-xs text-gray-500 mb-3">
              <span>입력: {(m.limits?.max_input_tokens / 1000).toFixed(0)}K 토큰</span>
              <span>출력: {(m.limits?.max_output_tokens / 1000).toFixed(0)}K 토큰</span>
            </div>

            {/* Entitlement */}
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <span className="badge-blue">{m.entitlement?.ui_label_ko || m.entitlement?.ui_label || m.entitlement?.class}</span>
                <span className="text-xs text-gray-400">릴리스: {m.release_channel}</span>
              </div>
              {m.availability === 'available' && (
                <button onClick={() => handleWithdraw(m.catalog_model_id)} className="text-red-600 text-xs hover:underline">철회</button>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
