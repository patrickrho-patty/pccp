import { useEffect, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { Modal, ModalFooter } from '../components/Modal'

type Hit = {
  domain: string; source_id: string; scope_ref: string; label: string
  rank_kind: string; locator: Record<string, any>; verification: string; masked: boolean
}
type Grant = { id: number; admin_email: string; scope_kind: string; scope_ref: string; can_reveal: boolean; expires_at: string; revoked: boolean }

const DOMAIN_KO: Record<string, string> = {
  conversations: '대화', code: '코드', provenance: '출처', trails: 'Trails',
}
const RANK_KO: Record<string, string> = { exact: '정확 일치', lexical: '어휘 일치' }
const VERIFY_KO: Record<string, string> = {
  verified: '검증됨', modified: '수정됨', unavailable: '사용 불가', superseded: '대체됨', legacy_unverified: '레거시 미검증',
}

export default function EvidenceSearch() {
  const [q, setQ] = useState('')
  const [domains, setDomains] = useState<string[]>(['conversations', 'code', 'provenance', 'trails'])
  const [results, setResults] = useState<Record<string, Hit[]> | null>(null)
  const [note, setNote] = useState('')
  const [searched, setSearched] = useState(false)
  const [grants, setGrants] = useState<Grant[]>([])
  const [grantOpen, setGrantOpen] = useState(false)
  const [grant, setGrant] = useState({ admin_email: '', can_reveal: false, reason: '' })
  const [revealFor, setRevealFor] = useState<Hit | null>(null)
  const [revealReason, setRevealReason] = useState('')
  const [revealed, setRevealed] = useState<any>(null)

  const loadGrants = () => {
    api.esGrants().then((d: Grant[]) => setGrants(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(() => { loadGrants() }, [])

  const search = () => {
    if (!q.trim()) return
    api.esSearch({ query: q, domains }).then((r: any) => {
      setResults(r.results || {}); setNote(r.ranking_note || ''); setSearched(true)
    }).catch((e: any) => showToast(e.message))
  }

  const reveal = () => {
    if (!revealFor) return
    const h = revealFor
    api.esReveal({ domain: h.domain, source_id: h.source_id, reason: revealReason })
      .then((r: any) => { setRevealed(r); setRevealFor(null); setRevealReason(''); showToast('민감 내용을 표시했습니다 (감사 기록됨)') })
      .catch((e: any) => showToast(e.message))
  }

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">증거 검색 <span className="text-xs text-gray-400 ml-2">PAT-1451 · 관리자 전용 · 증거 등급</span></h1>
          <p className="text-sm text-gray-500 mt-1">별도 승인된 관리자만 사용할 수 있습니다. 결과는 불변 위치(세션/커밋/다이제스트)로 귀결되며, 민감 내용은 기본 마스킹됩니다. 일괄 내보내기는 없습니다.</p>
        </div>
        <button className="btn-secondary text-sm" onClick={() => setGrantOpen(true)}>권한 부여</button>
      </div>

      {/* Search bar */}
      <div className="card p-4 space-y-3">
        <div className="flex gap-2">
          <input className="input flex-1" value={q} onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && search()} placeholder="세션 ID, 조치 ID, 파일 경로, 심볼, 키워드…" />
          <button className="btn text-sm" onClick={search}>검색</button>
        </div>
        <div className="flex gap-3 items-center flex-wrap">
          {Object.entries(DOMAIN_KO).map(([d, ko]) => (
            <label key={d} className="flex items-center gap-1.5 text-sm">
              <input type="checkbox" checked={domains.includes(d)}
                onChange={(e) => setDomains(e.target.checked ? [...domains, d] : domains.filter((x) => x !== d))} />
              {ko}
            </label>
          ))}
          {note && <span className="text-[11px] text-gray-400 ml-auto">{note}</span>}
        </div>
      </div>

      {/* Grouped results */}
      {searched && results && (
        <div className="space-y-4">
          {Object.entries(DOMAIN_KO).map(([d, ko]) => {
            const hits = results[d] || []
            if (!domains.includes(d)) return null
            return (
              <div key={d} className="card">
                <div className="p-3 border-b flex items-center justify-between">
                  <h2 className="font-semibold text-sm">{ko} <span className="text-gray-400 font-normal">{hits.length}건</span></h2>
                  <span className="text-[11px] text-gray-400">도메인별 순위 — 상호 비교 없음</span>
                </div>
                {hits.length === 0 && <p className="p-3 text-xs text-gray-500">결과 없음</p>}
                {hits.map((h) => (
                  <div key={h.source_id + h.rank_kind} className="p-3 border-t flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className={`text-[11px] px-1.5 py-0.5 rounded ${h.rank_kind === 'exact' ? 'bg-sky-100 text-sky-700' : 'bg-gray-100'}`}>{RANK_KO[h.rank_kind]}</span>
                        <span className="text-sm truncate">{h.label || '(마스킹됨)'}</span>
                        {h.masked && <span className="text-[11px] px-1.5 py-0.5 rounded bg-amber-100 text-amber-800">마스킹됨</span>}
                        <span className="text-[11px] px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-700">{VERIFY_KO[h.verification] || h.verification}</span>
                      </div>
                      <div className="text-[11px] text-gray-400 mt-1 font-mono">
                        {Object.entries(h.locator || {}).slice(0, 4).map(([k, v]) => `${k}=${String(v).slice(0, 24)}`).join(' · ')}
                      </div>
                    </div>
                    <div className="flex gap-1.5 shrink-0">
                      {h.masked && (
                        <button className="btn-secondary text-[11px]" onClick={() => { setRevealFor(h); setRevealReason('') }}>민감 내용 표시</button>
                      )}
                      <button className="btn-secondary text-[11px]" onClick={() =>
                        api.esOpen(h.domain, h.source_id).then((r: any) => showToast(`불변 위치 열람: ${JSON.stringify(Object.entries(r).slice(0, 3))}`)).catch((e: any) => showToast(e.message))
                      }>위치 열기</button>
                    </div>
                  </div>
                ))}
              </div>
            )
          })}
        </div>
      )}

      {/* Revealed content */}
      {revealed && (
        <div className="card p-4">
          <div className="flex items-center justify-between mb-2">
            <h2 className="font-semibold text-sm">표시된 민감 내용 <span className="text-red-500">(해석 아님 — 원본 증거)</span></h2>
            <button className="text-xs text-gray-400" onClick={() => setRevealed(null)}>닫기</button>
          </div>
          <pre className="text-xs bg-gray-50 rounded p-3 whitespace-pre-wrap max-h-48 overflow-auto">{revealed.prompt || ''}</pre>
        </div>
      )}

      {/* Grants */}
      <div className="card">
        <div className="p-3 border-b"><h2 className="font-semibold text-sm">증거 검색 권한 (별도 승인 · 만료 · 철회)</h2></div>
        {grants.map((g) => (
          <div key={g.id} className="p-3 border-t flex items-center justify-between text-xs">
            <div className="flex items-center gap-2">
              <span className="font-mono">{g.admin_email}</span>
              {g.can_reveal && <span className="px-1.5 py-0.5 rounded bg-amber-100 text-amber-800">민감 표시 가능</span>}
              {g.revoked ? <span className="px-1.5 py-0.5 rounded bg-gray-200">철회됨</span>
                : <span className="text-gray-400">만료 {new Date(g.expires_at).toLocaleDateString('ko-KR')}</span>}
            </div>
            {!g.revoked && (
              <button className="btn-secondary text-[11px]" onClick={() => api.esRevokeGrant(g.id).then(loadGrants).catch((e: any) => showToast(e.message))}>철회</button>
            )}
          </div>
        ))}
        {grants.length === 0 && <p className="p-3 text-xs text-gray-500">부여된 권한이 없습니다.</p>}
      </div>

      {/* Grant create */}
      <Modal open={grantOpen} title="증거 검색 권한 부여" onClose={() => setGrantOpen(false)} size="sm"
        footer={<ModalFooter onCancel={() => setGrantOpen(false)}
          onConfirm={() => api.esCreateGrant(grant).then(() => { setGrantOpen(false); showToast('권한을 부여했습니다'); loadGrants() }).catch((e: any) => showToast(e.message))}
          confirmLabel="부여" disabled={!grant.admin_email.trim()} />}>
        <div className="space-y-3">
          <div><label className="label">관리자 이메일</label>
            <input className="input" value={grant.admin_email} onChange={(e) => setGrant({ ...grant, admin_email: e.target.value })} /></div>
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={grant.can_reveal} onChange={(e) => setGrant({ ...grant, can_reveal: e.target.checked })} />
            민감 내용 표시 권한 포함 (별도 승인)</label>
          <div><label className="label">사유</label>
            <input className="input" value={grant.reason} onChange={(e) => setGrant({ ...grant, reason: e.target.value })} /></div>
          <p className="text-xs text-gray-500">권한은 30일 후 자동 만료되며 철회 즉시 적용됩니다. 검색 · 열람 · 표시는 모두 감사에 기록됩니다.</p>
        </div>
      </Modal>

      {/* Reveal governed action */}
      {revealFor && (
        <Modal open title="민감 내용 표시 (별도 권한 · 감사 기록)" size="sm" onClose={() => setRevealFor(null)}
          footer={<ModalFooter onCancel={() => setRevealFor(null)} onConfirm={reveal} confirmLabel="표시" danger disabled={!revealReason.trim()} />}>
          <div className="space-y-3">
            <p className="text-sm">대상: <span className="font-mono text-xs">{revealFor.scope_ref}</span></p>
            <div><label className="label">사유 (필수)</label>
              <input className="input" value={revealReason} onChange={(e) => setRevealReason(e.target.value)} placeholder="예: 침해 사고 조사 케이스 #123" /></div>
            <p className="text-xs text-gray-500">표시된 내용은 원본 증거이며 해석이 아닙니다. 이 작업은 테넌트 범위 감사 이벤트로 기록됩니다.</p>
          </div>
        </Modal>
      )}
    </div>
  )
}
