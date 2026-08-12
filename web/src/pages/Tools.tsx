import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Tools() {
  const [tools, setTools] = useState<any[]>([])

  useEffect(() => {
    api.listTools().then(data => setTools(Array.isArray(data) ? data : data || [])).catch(() => {})
  }, [])

  const seed = async () => {
    await api.seedTools()
    api.listTools().then(data => setTools(Array.isArray(data) ? data : data || []))
  }

  const dangerBadge = (d: string) => {
    const m: Record<string,string> = { low: 'badge-green', medium: 'badge-blue', high: 'badge-yellow', critical: 'badge-red' }
    return m[d] || 'badge-gray'
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">도구 관리 <span className="text-gray-400 text-lg font-normal">Tools</span></h1>
        <button onClick={seed} className="btn-secondary">기본 도구 등록</button>
      </div>

      <div className="card">
        {tools.length === 0 ? (
          <p className="text-gray-400 text-center py-8">등록된 도구가 없습니다. 기본 도구를 등록하세요.</p>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">도구명</th>
                <th className="pb-3">한글명</th>
                <th className="pb-3">분류</th>
                <th className="pb-3">위험도</th>
                <th className="pb-3">승인 필요</th>
              </tr>
            </thead>
            <tbody>
              {tools.map((t) => (
                <tr key={t.id} className="border-b border-gray-100 last:border-0">
                  <td className="py-3 font-mono text-sm">{t.name}</td>
                  <td className="py-3">{t.name_ko || '-'}</td>
                  <td className="py-3"><span className="badge-gray">{t.category}</span></td>
                  <td className="py-3"><span className={dangerBadge(t.danger_level)}>{t.danger_level}</span></td>
                  <td className="py-3">{t.requires_approval ? '✓' : '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
