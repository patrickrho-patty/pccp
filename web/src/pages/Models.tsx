import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Models() {
  const [models, setModels] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)

  useEffect(() => { api.listModels().then(setModels) }, [])

  const handlePublish = async (id: string) => {
    await api.publishModel(id)
    api.listModels().then(setModels)
  }
  const handleRecall = async (id: string) => {
    if (!confirm('이 모델을 리콜하시겠습니까?')) return
    await api.recallModel(id)
    api.listModels().then(setModels)
  }

  const stateBadge = (s: string) => {
    const map: Record<string, string> = { draft: 'badge-gray', published: 'badge-green', deprecated: 'badge-yellow', recalled: 'badge-red' }
    return map[s] || 'badge-gray'
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">모델 패키지 <span className="text-gray-400 text-lg font-normal">Model Packages</span></h1>
      </div>
      <div className="card">
        {models.length === 0 ? (
          <div className="text-center py-8">
            <p className="text-gray-400 mb-4">등록된 모델이 없습니다</p>
            <p className="text-sm text-gray-400">API를 통해 모델 패키지를 등록하세요: POST /api/models</p>
          </div>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">모델명</th>
                <th className="pb-3">패키지 ID</th>
                <th className="pb-3">버전</th>
                <th className="pb-3">상태</th>
                <th className="pb-3">보증 수준</th>
                <th className="pb-3"></th>
              </tr>
            </thead>
            <tbody>
              {models.map((m) => (
                <tr key={m.id} className="border-b border-gray-100 last:border-0">
                  <td className="py-3"><div className="font-medium">{m.name_ko || m.name}</div><div className="text-xs text-gray-400">{m.model_id}</div></td>
                  <td className="py-3 font-mono text-xs">{m.package_id}</td>
                  <td className="py-3 text-sm">{m.version}</td>
                  <td className="py-3"><span className={stateBadge(m.state)}>{m.state}</span></td>
                  <td className="py-3"><span className="badge-blue">{m.minimum_endpoint_assurance}</span></td>
                  <td className="py-3">
                    {m.state === 'draft' && <button onClick={() => handlePublish(m.id)} className="text-green-600 text-sm hover:underline mr-3">게시</button>}
                    {m.state !== 'recalled' && <button onClick={() => handleRecall(m.id)} className="text-red-600 text-sm hover:underline">리콜</button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
