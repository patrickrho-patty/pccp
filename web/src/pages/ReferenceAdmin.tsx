import { useState, useEffect } from 'react'
import { api } from '../api'
import { Modal, ModalFooter } from '../components/Modal'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'

// Patty Reference (PAT-1404) — governed technical-documentation retrieval.
// Source registry (authority/licensing/tier), bounded retrieval preview
// (resolve + search with citations + version), and signed package
// import/stage/activate/rollback with atomic activation.

type Source = {
  id: string
  source_id: string
  name: string
  name_ko?: string
  library_id?: string
  tier: string
  authority: string
  version_scheme?: string
  license?: string
  redistributable?: boolean
  acquisition?: string
  status: string
}

type Hit = {
  chunk_id: string
  source_id: string
  source_name: string
  doc_path: string
  title: string
  version?: string
  authority: string
  effective_date?: string
  score: number
  citation: string
}

type Pkg = {
  id: string
  package_id: string
  name?: string
  schema_version?: string
  state: string
  source_count?: number
  chunk_count?: number
  manifest_digest?: string
  imported_at?: string
  activated_at?: string
}

const TIER_KO: Record<string, string> = { tier1: 'Tier 1 · 한국 플랫폼', tier2: 'Tier 2 · 공공', tier3: 'Tier 3 · 한국어 글로벌', tenant: '테넌트 전용' }

export default function ReferenceAdmin() {
  const confirm = useConfirm()
  const [sources, setSources] = useState<Source[]>([])
  const [packages, setPackages] = useState<Pkg[]>([])
  const [catalog, setCatalog] = useState<any>(null)
  // Retrieval preview
  const [resolveQ, setResolveQ] = useState('toss-payments')
  const [evidence, setEvidence] = useState('')
  const [resolved, setResolved] = useState<any>(null)
  const [hits, setHits] = useState<Hit[]>([])
  // Modals
  const [srcModal, setSrcModal] = useState(false)
  const [srcForm, setSrcForm] = useState({ source_id: '', name: '', name_ko: '', library_id: '', tier: 'tier1', authority: 'official', version_scheme: 'semver', license: '', redistributable: true, acquisition: 'crawl', aliases: '' })
  const [pkgModal, setPkgModal] = useState(false)
  const [pkgForm, setPkgForm] = useState({ manifest: '', signature_hex: '', publisher: 'patty' })
  const [busy, setBusy] = useState(false)

  const reload = () => {
    api.referenceSources('').then(setSources).catch(() => {})
    api.referencePackages().then(setPackages).catch(() => {})
    api.referenceCatalog(null).then(setCatalog).catch(() => {})
  }
  useEffect(() => { reload() }, [])

  const doResolve = async () => {
    setBusy(true)
    try {
      const r = await api.referenceResolve(resolveQ, evidence, '')
      setResolved(r)
      const h = await api.referenceSearch(resolveQ, r.library_id || r.source_id || '', r.chosen_version || '', 'ko')
      setHits(Array.isArray(h.hits) ? h.hits : [])
    } catch (e: any) { showToast(e.message || '검색 실패', 'error'); setHits([]); setResolved(null) }
    finally { setBusy(false) }
  }

  const saveSource = async () => {
    if (!srcForm.source_id || !srcForm.name) { showToast('source_id와 이름 필요', 'error'); return }
    try {
      const data = { ...srcForm, aliases: srcForm.aliases.split(',').map(a => a.trim()).filter(Boolean), current: true }
      await api.saveReferenceSource(data)
      showToast('소스 등록됨', 'success')
      setSrcModal(false)
      reload()
    } catch (e: any) { showToast(e.message || '실패', 'error') }
  }

  const removeSource = async (src: Source) => {
    if (!await confirm({ title: '소스 제거', message: `${src.name}(${src.source_id})를 현재 검색에서 제거합니다. 과거 패키지 증거는 유지됩니다.`, danger: true })) return
    try {
      await api.deleteReferenceSource(src.source_id)
      showToast('제거됨 (톰스톤)', 'success')
      reload()
    } catch (e: any) { showToast(e.message || '실패', 'error') }
  }

  const importPkg = async () => {
    if (!pkgForm.manifest.trim()) { showToast('매니페스트 JSON 필요', 'error'); return }
    setBusy(true)
    try {
      await api.importReferencePackage(pkgForm)
      showToast('패키지 스테이징됨 (검증 완료)', 'success')
      setPkgModal(false)
      reload()
    } catch (e: any) { showToast(e.message || '임포트 실패', 'error') }
    finally { setBusy(false) }
  }

  const activate = async (p: Pkg) => {
    if (!await confirm({ title: '패키지 활성화', message: `패키지를 활성화하면 이전 활성 패키지가 대체됩니다. 관리자 승인 단계로 간주합니다.`, danger: true })) return
    try {
      await api.activateReferencePackage(p.id, 'admin activation')
      showToast('활성화됨 (원자적 전환)', 'success')
      reload()
    } catch (e: any) { showToast(e.message || '활성화 실패', 'error') }
  }

  const rollback = async (p: Pkg) => {
    if (!await confirm({ title: '롤백', message: '이 패키지를 비활성 상태로 되돌립니다.', danger: true })) return
    try {
      await api.rollbackReferencePackage(p.id)
      showToast('롤백됨', 'success')
      reload()
    } catch (e: any) { showToast(e.message || '롤백 실패', 'error') }
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
        <div>
          <h1 className="text-xl font-semibold">Patty 레퍼런스 <span className="text-sm text-gray-400">(Patty Reference)</span></h1>
          <p className="text-xs text-gray-500 mt-1">
            거버넌스 기반 기술 문서 검색. 소스/승인/버전/신선도/인용이 항상 유지되며, 검색은 읽기 전용이고 기존 정책(MCP·도구 권한·감사)의 적용을 받습니다.
          </p>
        </div>
        <div className="flex gap-2">
          <button className="btn-sm btn-secondary" onClick={() => setSrcModal(true)}>+ 소스</button>
          <button className="btn-sm btn-primary" onClick={() => setPkgModal(true)}>📦 패키지 임포트</button>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4">
        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">검증 미리보기 (라이브)</h2>
          <div className="flex gap-2 mb-2">
            <input className="input text-xs flex-1" placeholder="라이브러리/이름/별칭 (예: toss-payments, 카카오 로그인)" value={resolveQ} onChange={e => setResolveQ(e.target.value)} />
            <button className="btn-sm btn-primary" onClick={doResolve} disabled={busy}>{busy ? '…' : '검색'}</button>
          </div>
          <input className="input text-xs mb-2" placeholder="project evidence (선택, 버전 감지용): toss-payments: v1.4.0" value={evidence} onChange={e => setEvidence(e.target.value)} />
          {resolved && (
            <div className="text-[11px] space-y-1 border rounded p-2 bg-gray-50 mb-2">
              <div><span className="font-medium">{resolved.name_ko || resolved.name}</span> · {resolved.source_id} · {resolved.authority}</div>
              <div className="text-gray-500">선택 버전: <span className="font-mono">{resolved.chosen_version || '—'}</span></div>
              <div className="text-gray-400">{resolved.version_note}</div>
            </div>
          )}
          {hits.length > 0 && (
            <div className="space-y-2 max-h-[300px] overflow-y-auto">
              {hits.map((h, i) => (
                <div key={i} className="text-[11px] border border-gray-100 rounded p-2">
                  <div className="text-gray-700 font-medium">{h.title}</div>
                  <div className="text-gray-400 font-mono text-[10px]">{h.citation}</div>
                  <div className="text-gray-500 mt-1 line-clamp-2">{h.body}</div>
                  {h.code && <pre className="text-[10px] bg-gray-50 p-1 rounded mt-1 overflow-x-auto">{h.code}</pre>}
                  <div className="text-[9px] text-gray-400 mt-1">v{h.version || '—'} · {h.authority} · {h.effective_date || '신선도 n/a'} · score {h.score}</div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">소스 레지스트리</h2>
          {sources.length === 0 ? <p className="text-xs text-gray-400">등록된 소스 없음</p> : (
            <table className="w-full text-xs">
              <thead><tr className="text-left text-gray-500 border-b border-gray-100">
                <th className="py-1 px-1">소스</th><th className="py-1 px-1">계층</th><th className="py-1 px-1">권한</th><th className="py-1 px-1">상태</th><th className="py-1 px-1 text-right">동작</th>
              </tr></thead>
              <tbody>
                {sources.map(src => (
                  <tr key={src.source_id} className="border-b border-gray-50">
                    <td className="py-1 px-1"><div className="font-medium">{src.name_ko || src.name}</div><div className="text-[9px] text-gray-400 font-mono">{src.source_id}</div></td>
                    <td className="py-1 px-1 text-gray-500">{TIER_KO[src.tier] || src.tier}</td>
                    <td className="py-1 px-1">{src.authority}{src.redistributable ? '' : <span className="text-[9px] text-red-600"> ⛔ 비재배포</span>}</td>
                    <td className="py-1 px-1">{src.status === 'removed' ? <span className="text-red-600">제거됨</span> : <span className="text-green-600">활성</span>}</td>
                    <td className="py-1 px-1 text-right"><button className="btn-xs btn-secondary" onClick={() => removeSource(src)}>제거</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      <div className="card p-4">
        <div className="flex items-center justify-between mb-2">
          <h2 className="text-sm font-medium">패키지 / 활성 코퍼스</h2>
          {catalog && <span className="text-[10px] text-gray-400">활성 패키지: {catalog.active_package?.package_id || '—'} · 동기화 {catalog.state?.sync_enabled ? '켜짐' : '꺼짐'} · 자동 활성화 {catalog.state?.auto_activate ? '켜짐' : '꺼짐(안전 기본)'}</span>}
        </div>
        {packages.length === 0 ? <p className="text-xs text-gray-400">임포트된 패키지 없음</p> : (
          <table className="w-full text-xs">
            <thead><tr className="text-left text-gray-500 border-b border-gray-100">
              <th className="py-1 px-1">패키지</th><th className="py-1 px-1">스키마</th><th className="py-1 px-1">소스/청크</th><th className="py-1 px-1">다이제스트</th><th className="py-1 px-1">상태</th><th className="py-1 px-1 text-right">동작</th>
            </tr></thead>
            <tbody>
              {packages.map(p => (
                <tr key={p.id} className="border-b border-gray-50">
                  <td className="py-1 px-1"><div className="font-mono">{p.package_id}</div><div className="text-[9px] text-gray-400">{p.imported_at}</div></td>
                  <td className="py-1 px-1">{p.schema_version}</td>
                  <td className="py-1 px-1">{p.source_count}/{p.chunk_count}</td>
                  <td className="py-1 px-1 font-mono text-[9px]">{p.manifest_digest?.slice(0, 14)}</td>
                  <td className="py-1 px-1">
                    <span className={`px-1.5 py-0.5 rounded-full border text-[9px] ${
                      p.state === 'active' ? 'bg-green-50 text-green-700 border-green-200' :
                      p.state === 'staged' ? 'bg-amber-50 text-amber-700 border-amber-200' : 'bg-gray-100 text-gray-500 border-gray-200'}`}>
                      {p.state}
                    </span>
                  </td>
                  <td className="py-1 px-1 text-right whitespace-nowrap">
                    {p.state === 'staged' && <button className="btn-xs btn-primary" onClick={() => activate(p)}>활성화</button>}
                    {p.state === 'active' && <button className="btn-xs btn-secondary ml-1" onClick={() => rollback(p)}>롤백</button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {srcModal && (
        <Modal title="소스 등록" onClose={() => setSrcModal(false)}>
          <div className="space-y-3 max-h-[65vh] overflow-y-auto">
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">source_id (캐노니컬)</label><input className="input text-xs font-mono" value={srcForm.source_id} onChange={e => setSrcForm({ ...srcForm, source_id: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">library_id</label><input className="input text-xs font-mono" value={srcForm.library_id} onChange={e => setSrcForm({ ...srcForm, library_id: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">이름 (EN)</label><input className="input text-xs" value={srcForm.name} onChange={e => setSrcForm({ ...srcForm, name: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">이름 (KO)</label><input className="input text-xs" value={srcForm.name_ko} onChange={e => setSrcForm({ ...srcForm, name_ko: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">계층</label>
                <select className="input text-xs" value={srcForm.tier} onChange={e => setSrcForm({ ...srcForm, tier: e.target.value })}>
                  <option value="tier1">Tier 1 · 한국 플랫폼</option><option value="tier2">Tier 2 · 공공</option><option value="tier3">Tier 3 · 한국어 글로벌</option><option value="tenant">테넌트 전용</option>
                </select></div>
              <div><label className="text-xs text-gray-500">권한</label>
                <select className="input text-xs" value={srcForm.authority} onChange={e => setSrcForm({ ...srcForm, authority: e.target.value })}>
                  <option value="official">공식</option><option value="vendor">벤더</option><option value="customer">고객 전용</option>
                </select></div>
              <div><label className="text-xs text-gray-500">버전 방식</label>
                <select className="input text-xs" value={srcForm.version_scheme} onChange={e => setSrcForm({ ...srcForm, version_scheme: e.target.value })}>
                  <option value="semver">SemVer</option><option value="date">날짜</option><option value="unversioned">미버전</option>
                </select></div>
              <div><label className="text-xs text-gray-500">취득</label>
                <select className="input text-xs" value={srcForm.acquisition} onChange={e => setSrcForm({ ...srcForm, acquisition: e.target.value })}>
                  <option value="crawl">크롤링</option><option value="import">임포트</option><option value="customer">고객 제공</option>
                </select></div>
            </div>
            <div><label className="text-xs text-gray-500">별칭 (쉼표 구분)</label><input className="input text-xs" value={srcForm.aliases} onChange={e => setSrcForm({ ...srcForm, aliases: e.target.value })} /></div>
            <div className="flex items-center gap-2">
              <input type="checkbox" checked={srcForm.redistributable} onChange={e => setSrcForm({ ...srcForm, redistributable: e.target.checked })} />
              <span className="text-xs text-gray-500">재배포 허용 (라이선스)</span>
            </div>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setSrcModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={saveSource}>등록</button>
          </ModalFooter>
        </Modal>
      )}

      {pkgModal && (
        <Modal title="서명 패키지 임포트 (스테이징 후 관리자 활성화)" onClose={() => setPkgModal(false)}>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">게시자</label><input className="input text-xs" value={pkgForm.publisher} onChange={e => setPkgForm({ ...pkgForm, publisher: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">서명 (detached hex)</label><input className="input text-xs font-mono" value={pkgForm.signature_hex} onChange={e => setPkgForm({ ...pkgForm, signature_hex: e.target.value })} /></div>
            </div>
            <div><label className="text-xs text-gray-500">매니페스트 (JSON)</label>
              <textarea className="input text-xs font-mono" rows={10} value={pkgForm.manifest} onChange={e => setPkgForm({ ...pkgForm, manifest: e.target.value })}
                placeholder={`{\n  "schema_version": "1",\n  "corpus_id": "corpus-a",\n  "publisher": "patty",\n  "sources": ["kakao-login"],\n  "chunks": [{\n    "source_id": "kakao-login",\n    "doc_path": "docs/login.md",\n    "body": "카카오 로그인: 동의 후 인증 토큰 발급",\n    "version": "2.0.0",\n    "library_id": "kakao-login"\n  }]\n}`} />
            </div>
            <p className="text-[10px] text-gray-400">경로 탈주, NUL 주입, 서명/다이제스트 불일치, 비호환 스키마는 스테이징 전에 거부됩니다. 임포트는 검증·스테이징만 수행하고, 활성화는 관리자 승인 후 원자적으로 전환됩니다.</p>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setPkgModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={importPkg} disabled={busy}>{busy ? '…' : '임포트 + 검증'}</button>
          </ModalFooter>
        </Modal>
      )}
    </div>
  )
}
