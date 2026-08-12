import { useState, useEffect } from 'react'

export default function Fleet() {
  const [inventory, setInventory] = useState<any[]>([])

  useEffect(() => {
    fetch('/api/fleet/inventory', { headers: authHeaders() })
      .then(r => r.json())
      .then(data => setInventory(Array.isArray(data) ? data : []))
      .catch(() => setInventory([]))
  }, [])

  const riskBadge = (score: number) => {
    if (score >= 0.8) return 'badge-red'
    if (score >= 0.5) return 'badge-yellow'
    return 'badge-green'
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">플릿 관리 <span className="text-gray-400 text-lg font-normal">Fleet Management</span></h1>
      <div className="card">
        {inventory.length === 0 ? (
          <p className="text-gray-400 text-center py-8">등록된 하네스가 없습니다</p>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">하네스 ID</th>
                <th className="pb-3">사용자</th>
                <th className="pb-3">상태</th>
                <th className="pb-3">위험도</th>
                <th className="pb-3">세션</th>
                <th className="pb-3">보안 발견</th>
              </tr>
            </thead>
            <tbody>
              {inventory.map((h: any) => (
                <tr key={h.harness?.id || Math.random()} className="border-b border-gray-100 last:border-0">
                  <td className="py-3 font-mono text-xs">{h.harness?.harness_id?.slice(0, 25)}</td>
                  <td className="py-3">{h.user?.name_ko || h.user?.name || '-'}</td>
                  <td className="py-3">
                    <span className={h.harness?.status === 'active' ? 'badge-green' : h.harness?.status === 'revoked' ? 'badge-red' : 'badge-yellow'}>
                      {h.harness?.status}
                    </span>
                  </td>
                  <td className="py-3">
                    <span className={riskBadge(h.risk_score)}>
                      {(h.risk_score * 100).toFixed(0)}%
                    </span>
                  </td>
                  <td className="py-3 text-sm">{h.sessions?.length || 0}</td>
                  <td className="py-3 text-sm">{h.security_findings || 0}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
