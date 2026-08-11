import { useState, useEffect } from 'react'
import { api } from '../api'
import { useParams, Link } from 'react-router-dom'

export default function Provenance() {
  const { id } = useParams()
  const [chain, setChain] = useState<any>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (id) api.getProvenance(id).then(setChain).finally(() => setLoading(false))
  }, [id])

  if (loading) return <div className="text-gray-500">로딩 중...</div>
  if (!chain) return <div>데이터 없음</div>

  return (
    <div>
      <div className="flex items-center gap-4 mb-6">
        <Link to="/sessions" className="text-patty-600 hover:underline">← 세션</Link>
        <h1 className="text-2xl font-bold">프로바이던스 체인 <span className="text-gray-400 text-lg font-normal">Provenance Chain</span></h1>
      </div>

      {/* Session info */}
      {chain.session && (
        <div className="card mb-6">
          <h2 className="text-lg font-semibold mb-4">세션 정보</h2>
          <div className="grid grid-cols-3 gap-4 text-sm">
            <div><span className="text-gray-500">제목:</span> {chain.session.title}</div>
            <div><span className="text-gray-500">상태:</span> {chain.session.status}</div>
            <div><span className="text-gray-500">모델:</span> {chain.session.model_class}</div>
            <div><span className="text-gray-500">사용자:</span> {chain.session.user_id}</div>
            <div><span className="text-gray-500">하네스:</span> {chain.session.harness_id}</div>
            <div><span className="text-gray-500">브랜치:</span> {chain.session.branch}</div>
          </div>
        </div>
      )}

      {/* Action timeline */}
      <div className="card mb-6">
        <h2 className="text-lg font-semibold mb-4">액션 타임라인 ({chain.actions?.length || 0})</h2>
        {chain.actions?.length === 0 ? (
          <p className="text-gray-400 text-sm">기록된 액션이 없습니다</p>
        ) : (
          <div className="space-y-3">
            {chain.actions?.map((a: any, i: number) => (
              <div key={a.id} className="flex items-start gap-3">
                <div className="flex flex-col items-center">
                  <div className="w-8 h-8 rounded-full bg-patty-100 text-patty-600 flex items-center justify-center text-sm font-bold">{i + 1}</div>
                  {i < chain.actions.length - 1 && <div className="w-px h-8 bg-gray-200" />}
                </div>
                <div className="flex-1 pb-4">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{a.action_type}</span>
                    <span className="badge-green">{a.verdict_result}</span>
                  </div>
                  <div className="text-xs text-gray-400 mt-1">{a.occurred_at}</div>
                  <div className="text-xs text-gray-500 mt-1">
                    모델: {a.model_package_id || '-'} · 엔드포인트: {a.endpoint_id || '-'}
                  </div>
                  <div className="text-xs font-mono text-gray-400 mt-1">digest: {a.envelope_digest?.slice(0, 40)}</div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Change sets */}
      {chain.change_sets?.length > 0 && (
        <div className="card mb-6">
          <h2 className="text-lg font-semibold mb-4">변경 세트 ({chain.change_sets.length})</h2>
          <div className="space-y-3">
            {chain.change_sets.map((cs: any) => (
              <div key={cs.id} className="border border-gray-200 rounded-lg p-4">
                <div className="flex justify-between items-center mb-2">
                  <span className="font-medium text-sm">{cs.files_changed}</span>
                  <div className="flex gap-2">
                    <span className="text-xs text-green-600">+{cs.lines_added}</span>
                    <span className="text-xs text-red-600">-{cs.lines_removed}</span>
                    <span className="badge-blue">{cs.attribution_state}</span>
                  </div>
                </div>
                <pre className="text-xs bg-gray-50 p-2 rounded overflow-x-auto max-h-40">{cs.diff_summary}</pre>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Evidence receipts */}
      {chain.receipts?.length > 0 && (
        <div className="card">
          <h2 className="text-lg font-semibold mb-4">증거 영수증 ({chain.receipts.length})</h2>
          <div className="space-y-2">
            {chain.receipts.map((r: any) => (
              <div key={r.id} className="border border-gray-200 rounded-lg p-3 text-sm">
                <div className="flex justify-between">
                  <span className="font-mono text-xs">{r.exchange_id}</span>
                  <span className="badge-green">{r.final_state}</span>
                </div>
                <div className="text-xs text-gray-500 mt-1">체인 루트: {r.chain_root?.slice(0, 40)}</div>
                <div className="text-xs text-gray-500">서명: {r.signature?.slice(0, 40)}...</div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
