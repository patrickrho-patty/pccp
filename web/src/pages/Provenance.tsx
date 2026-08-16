import { useState, useEffect } from 'react'
import { api } from '../api'
import { useParams, Link } from 'react-router-dom'

export default function Provenance() {
  const { id } = useParams()
  const [chain, setChain] = useState<any>(null)
  const [receipts, setReceipts] = useState<any[]>([])
  const [search, setSearch] = useState('')
  const [searchResults, setSearchResults] = useState<any>(null)
  const [tab, setTab] = useState<'chain' | 'receipts' | 'search'>('chain')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (id) api.getProvenance(id).then(data => setChain(Array.isArray(data) ? data : data || [])).finally(() => setLoading(false))
  }, [id])
  useEffect(() => {
    if (id) api.getProvenanceReceipts(id).then((d: any[]) => setReceipts(Array.isArray(d) ? d : [])).catch(() => setReceipts([]))
  }, [id])

  const runSearch = async () => {
    if (!search.trim()) return
    try { setSearchResults(await api.provenanceSearch(search)) } catch { setSearchResults(null) }
  }
  const downloadBundle = async () => {
    const token = localStorage.getItem('pccp_token')
    try {
      const resp = await fetch(`/api/sessions/${id}/provenance/export`, { headers: token ? { Authorization: `Bearer ${token}` } : {} })
      if (!resp.ok) throw new Error('export failed')
      const blob = await resp.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `provenance-${id}.bundle.json`
      a.click()
      URL.revokeObjectURL(url)
    } catch { /* noop */ }
  }

  if (loading) return <div className="text-gray-500">로딩 중...</div>
  if (!chain) return <div>데이터 없음</div>

  return (
    <div>
      <div className="flex items-center gap-4 mb-6">
        <Link to="/sessions" className="text-patty-600 hover:underline">← 세션</Link>
        <h1 className="text-2xl font-bold">프로바이던스 체인 <span className="text-gray-400 text-lg font-normal">Provenance Chain</span></h1>
        <div className="flex gap-2 ml-auto">
          <input className="input text-xs w-48" placeholder="교차 세션 검색 (파일/심볼)" value={search} onChange={e => setSearch(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') runSearch() }} />
          <button className="btn-sm btn-secondary" onClick={runSearch}>검색</button>
          <button className="btn-sm btn-secondary" onClick={downloadBundle}>서명 번들 내보내기</button>
        </div>
      </div>

      <div className="flex gap-1 border-b border-gray-200 mb-4">
        {[
          { id: 'chain', label: '체인' },
          { id: 'receipts', label: `증거 영수증 (${receipts.length})` },
          { id: 'search', label: '교차 세션 검색' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-3 py-2 text-xs ${tab === t.id ? 'border-b-2 border-blue-600 text-blue-600 font-semibold' : 'text-gray-500'}`}>
            {t.label}
          </button>
        ))}
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
                  <div className="flex gap-2 shrink-0 flex-wrap">
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

      {tab === 'receipts' && (
        <div className="card p-4">
          <h2 className="text-sm font-bold mb-2">증거 영수증 (서명 검증 상태)</h2>
          {receipts.length === 0 && <p className="text-xs text-gray-400">영수증 없음</p>}
          {receipts.map((r: any) => (
            <div key={r.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
              <span className="text-gray-700 font-mono">{r.exchange_id?.slice(0, 14)}</span>
              <span className={r.verified ? 'text-green-600' : 'text-red-500'}>{r.verification}</span>
              <span className="text-gray-400">events {r.first_event_seq}-{r.last_event_seq}</span>
            </div>
          ))}
        </div>
      )}

      {tab === 'search' && (
        <div className="card p-4">
          <h2 className="text-sm font-bold mb-2">교차 세션 검색 결과</h2>
          {!searchResults && <p className="text-xs text-gray-400">검색어를 입력하세요 (파일 경로 / 심볼).</p>}
          {searchResults && (
            <>
              <p className="text-[10px] text-gray-400 mb-1">스팬 {searchResults.spans?.length || 0} · 변경셋 {searchResults.change_sets?.length || 0}</p>
              {(searchResults.spans || []).map((s: any) => (
                <div key={s.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
                  <Link to={`/sessions/${s.session_id}`} className="text-blue-600 hover:underline">{s.file_path}:{s.start_line}-{s.end_line}</Link>
                  <span className="text-gray-400">{s.attribution_state}</span>
                </div>
              ))}
              {(searchResults.change_sets || []).map((c: any) => (
                <div key={c.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
                  <Link to={`/sessions/${c.session_id}`} className="text-blue-600 hover:underline">{c.files_changed}</Link>
                  <span className="text-gray-400">{c.branch}</span>
                </div>
              ))}
            </>
          )}
        </div>
      )}
    </div>
  )
}
