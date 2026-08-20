import { useEffect, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { Modal, ModalFooter } from '../components/Modal'
import { GovernedActionModal } from '../components/GovernedActionModal'

type Listing = {
  slug: string; name: string; name_ko: string; type: string; category: string
  trust_label: string; trust_ko: string; featured: boolean; sponsored: boolean
  latest_version: string; install_count: number; publisher_id: string
}
type Publisher = { publisher_id: string; display_name: string; trust_state: string; email: string }
type Report = { id: number; slug: string; version: string; kind: string; detail: string; state: string }

const TRUST_TONE: Record<string, string> = {
  community: 'bg-gray-100 text-gray-600', verified_publisher: 'bg-sky-100 text-sky-700',
  reviewed: 'bg-violet-100 text-violet-700', official: 'bg-emerald-100 text-emerald-700',
}
const KIND_KO: Record<string, string> = {
  malicious: '악성', deceptive: '기만적', abandoned: '방치됨', impersonating: '사칭', broken: '오작동',
}

export default function MarketplaceConsole() {
  const [tab, setTab] = useState<'catalog' | 'reports' | 'publishers'>('catalog')
  const [listings, setListings] = useState<Listing[]>([])
  const [reports, setReports] = useState<Report[]>([])
  const [publishers, setPublishers] = useState<Publisher[]>([])
  const [query, setQuery] = useState('')
  const [trust, setTrust] = useState('')
  const [detail, setDetail] = useState<any>(null)
  const [modFor, setModFor] = useState<{ action: string; slug: string; label: string; version?: string } | null>(null)
  const [reason, setReason] = useState('')

  const load = () => {
    api.mkSearch(query, trust).then((d: Listing[]) => setListings(Array.isArray(d) ? d : [])).catch(() => {})
    api.mkReports('open').then((d: Report[]) => setReports(Array.isArray(d) ? d : [])).catch(() => {})
    api.mkPublishers().then((d: Publisher[]) => setPublishers(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(load, [])

  const openDetail = (slug: string) =>
    api.mkListing(slug).then(setDetail).catch((e: any) => showToast(e.message))

  const doModerate = () => {
    if (!modFor) return
    const m = modFor
    api.mkModerate({ action: m.action, slug: m.slug, version: m.version || '', reason })
      .then(() => { setModFor(null); setReason(''); showToast('검열 조치를 실행했습니다'); load() })
      .catch((e: any) => showToast(e.message))
  }

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">마켓플레이스 <span className="text-xs text-gray-400 ml-2">PAT-1438 · 영구 레지스트리 · 신뢰 · 검열</span></h1>
          <p className="text-sm text-gray-500 mt-1">버전은 불변 콘텐츠 주소이고, 자동 검사를 통과한 버전만 검색에 노출됩니다. 후원·큐레이션은 신뢰 등급과 완전히 분리된 표시 필드입니다.</p>
        </div>
      </div>

      <div className="flex gap-2">
        {(['catalog', 'reports', 'publishers'] as const).map((t) => (
          <button key={t} className={`text-sm px-3 py-1.5 rounded-lg ${tab === t ? 'bg-gray-900 text-white' : 'bg-gray-100 text-gray-600'}`}
            onClick={() => setTab(t)}>
            {t === 'catalog' ? '카탈로그' : t === 'reports' ? `신고 (${reports.length})` : '게시자'}
          </button>
        ))}
      </div>

      {tab === 'catalog' && (
        <div className="card">
          <div className="p-3 border-b flex gap-2">
            <input className="input flex-1" placeholder="검색…" value={query}
              onChange={(e) => setQuery(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && load()} />
            <select className="input w-44" value={trust} onChange={(e) => setTrust(e.target.value)}>
              <option value="">전체 신뢰</option>
              <option value="community">커뮤니티</option>
              <option value="verified_publisher">검증된 게시자</option>
              <option value="reviewed">심사 완료</option>
              <option value="official">공식</option>
            </select>
            <button className="btn text-sm" onClick={load}>검색</button>
          </div>
          {listings.map((l) => (
            <div key={l.slug} className="p-3 border-t flex items-center justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-medium text-sm">{l.name_ko || l.name}</span>
                  <span className={`text-[11px] px-1.5 py-0.5 rounded ${TRUST_TONE[l.trust_label] || 'bg-gray-100'}`}>{l.trust_ko}</span>
                  <span className="text-[11px] px-1.5 py-0.5 rounded bg-gray-100">{l.type}</span>
                  {l.featured && <span className="text-[11px] px-1.5 py-0.5 rounded bg-violet-50 text-violet-700">큐레이션</span>}
                  {l.sponsored && <span className="text-[11px] px-1.5 py-0.5 rounded bg-amber-50 text-amber-700">후원</span>}
                </div>
                <div className="text-[11px] text-gray-400 mt-0.5 font-mono">{l.slug} · v{l.latest_version} · 설치 {l.install_count}</div>
              </div>
              <div className="flex gap-1.5 shrink-0">
                <button className="btn-secondary text-xs" onClick={() => openDetail(l.slug)}>상세</button>
                <button className="btn-secondary text-xs" onClick={() => api.mkPlacement(l.slug, !l.featured).then(load).catch((e: any) => showToast(e.message))}>
                  {l.featured ? '큐레이션 해제' : '큐레이션'}
                </button>
                <button className="btn-secondary text-xs" onClick={() => { setModFor({ action: 'critical_disable', slug: l.slug, label: '긴급 비활성화' }); setReason('') }}>긴급 조치</button>
              </div>
            </div>
          ))}
          {listings.length === 0 && <p className="p-4 text-sm text-gray-500">결과가 없습니다.</p>}
        </div>
      )}

      {tab === 'reports' && (
        <div className="card">
          <div className="p-3 border-b"><h2 className="font-semibold text-sm">공개 신고 큐</h2></div>
          {reports.length === 0 && <p className="p-4 text-sm text-gray-500">열린 신고가 없습니다.</p>}
          {reports.map((rep) => (
            <div key={rep.id} className="p-3 border-t flex items-center justify-between gap-3">
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-[11px] px-1.5 py-0.5 rounded bg-red-50 text-red-600">{KIND_KO[rep.kind] || rep.kind}</span>
                  <span className="font-mono text-sm">{rep.slug}</span>
                  {rep.version && <span className="text-[11px] text-gray-400">v{rep.version}</span>}
                </div>
                {rep.detail && <div className="text-xs text-gray-500 mt-0.5">{rep.detail}</div>}
              </div>
              <div className="flex gap-1.5">
                <button className="btn-secondary text-xs" onClick={() => { setModFor({ action: 'quarantine_version', slug: rep.slug, version: rep.version, label: '버전 검역' }); setReason('') }}>버전 검역</button>
                <button className="btn-secondary text-xs" onClick={() => { setModFor({ action: 'block_listing', slug: rep.slug, label: '목록 차단' }); setReason('') }}>목록 차단</button>
                <button className="btn-secondary text-xs" onClick={() => api.mkModerate({ action: 'resolve_report', slug: rep.slug, reason: '검토 완료' }).then(load).catch((e: any) => showToast(e.message))}>해결</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {tab === 'publishers' && (
        <div className="card">
          <div className="p-3 border-b"><h2 className="font-semibold text-sm">게시자 신뢰</h2></div>
          {publishers.map((p) => (
            <div key={p.publisher_id} className="p-3 border-t flex items-center justify-between text-sm">
              <div className="flex items-center gap-2">
                <span className="font-medium">{p.display_name}</span>
                <span className={`text-[11px] px-1.5 py-0.5 rounded ${p.trust_state === 'verified' || p.trust_state === 'official' ? 'bg-sky-100 text-sky-700' : p.trust_state === 'revoked' ? 'bg-red-100 text-red-700' : 'bg-gray-100'}`}>
                  {p.trust_state}
                </span>
              </div>
              <div className="flex gap-1.5">
                {p.trust_state !== 'verified' && (
                  <button className="btn-secondary text-xs" onClick={() => api.mkSetTrust(p.publisher_id, 'verified').then(load).catch((e: any) => showToast(e.message))}>검증</button>
                )}
                {p.trust_state !== 'revoked' && (
                  <button className="btn-secondary text-xs" onClick={() => api.mkSetTrust(p.publisher_id, 'revoked').then(load).catch((e: any) => showToast(e.message))}>신뢰 철회</button>
                )}
              </div>
            </div>
          ))}
          {publishers.length === 0 && <p className="p-4 text-sm text-gray-500">게시자가 없습니다.</p>}
        </div>
      )}

      {/* Listing detail */}
      <Modal open={!!detail} title={detail?.listing?.name || ''} onClose={() => setDetail(null)} size="lg"
        footer={<ModalFooter onCancel={() => setDetail(null)} onConfirm={() => setDetail(null)} confirmLabel="닫기" />}>
        {detail && (
          <div className="space-y-3 text-sm">
            <div className="flex gap-2 flex-wrap">
              <span className={`text-[11px] px-1.5 py-0.5 rounded ${TRUST_TONE[detail.listing.trust_label]}`}>{detail.listing.trust_label}</span>
              <span className="text-xs text-gray-500">게시자 {detail.publisher?.display_name} ({detail.publisher?.trust_state})</span>
            </div>
            <p className="text-gray-600">{detail.listing.description}</p>
            <div className="max-h-48 overflow-auto text-xs">
              <table className="w-full">
                <thead className="text-gray-500 text-left"><tr><th className="py-1">버전</th><th>해시</th><th>상태</th><th>검사</th></tr></thead>
                <tbody>
                  {(detail.versions || []).map((v: any) => (
                    <tr key={v.Version || v.version} className="border-t">
                      <td className="py-1">{v.Version || v.version}</td>
                      <td className="font-mono text-gray-400">{(v.ContentHash || v.content_hash || '').slice(0, 19)}…</td>
                      <td>{v.State || v.state}</td>
                      <td className="text-gray-400">{JSON.stringify(JSON.parse(v.ChecksJSON || v.checks_json || '[]')).split('"pass":false').length - 1} 실패</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </Modal>

      {/* Governed moderation */}
      {modFor && (
        <GovernedActionModal
          open
          danger
          requireConfirmPhrase
          confirmPhraseLabel="검역이 설치를 차단하고 설치된 사용자에게 경고함을 확인했습니다"
          title={`${modFor.label} · ${modFor.slug}`}
          preview={<p className="text-sm">버전 {modFor.version || '(전체)'} — 조치 후에도 기록과 증거는 보존됩니다.</p>}
          confirmLabel="실행"
          reason={reason}
          onReasonChange={setReason}
          onCancel={() => setModFor(null)}
          onConfirm={doModerate}
        />
      )}
    </div>
  )
}
