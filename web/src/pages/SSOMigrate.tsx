import { useState, useEffect } from 'react'
import { api } from '../api'
import { Modal, ModalFooter } from '../components/Modal'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'

// SSO 마이그레이션 (PAT-1442) — Keycloak → Authentik 호환성 레이어.
// 불변 issuer+subject→Patty 사용자 매핑, 브리지 인증(토큰 복사 금지, 새 세션 발급),
// 멱등 발견 매니페스트 + 조정 보고, 단계별 컷오버 웨이브 서명. 신뢰 도메인/인프라
// 배포 자체는 인프라 작업으로 별도 범위.

type Link = { id: string; legacy_issuer: string; legacy_subject: string; patty_user_id: string; status: string; target_issuer?: string; target_subject?: string }
type Manifest = { id: string; manifest_id: string; name?: string; source?: string; wave: number; status: string; source_count: number; linked_count: number; ambiguous_count: number; excluded_count: number }
type Wave = { id: string; wave: number; name?: string; owner_id?: string; status: string }

export default function SSOMigrate() {
  const confirm = useConfirm()
  const [links, setLinks] = useState<Link[]>([])
  const [manifests, setManifests] = useState<Manifest[]>([])
  const [waves, setWaves] = useState<Wave[]>([])
  const [recon, setRecon] = useState<any>(null)
  // Modals
  const [linkModal, setLinkModal] = useState(false)
  const [linkForm, setLinkForm] = useState({ legacy_issuer: 'https://kc.patty.io/realms/work', legacy_subject: '', patty_user_id: '', target_issuer: '', target_subject: '', note: '' })
  const [bridgeForm, setBridgeForm] = useState({ legacy_issuer: 'https://kc.patty.io/realms/work', legacy_subject: '' })
  const [bridgeResult, setBridgeResult] = useState<any>(null)
  const [manifestModal, setManifestModal] = useState(false)
  const [manifestForm, setManifestForm] = useState({ name: '', source: '', import_id: '', wave: 1, itemsJson: '' })
  const [busy, setBusy] = useState(false)

  const reload = () => {
    api.ssoMigrateLinks().then(setLinks).catch(() => {})
    api.ssoMigrateManifests().then(setManifests).catch(() => {})
    api.ssoMigrateWaves().then(setWaves).catch(() => {})
  }
  useEffect(() => { reload() }, [])

  const link = async () => {
    if (!linkForm.legacy_subject || !linkForm.patty_user_id) { showToast('subject와 Patty 사용자 필요', 'error'); return }
    try {
      await api.ssoMigrateLink(linkForm)
      showToast('매핑 등록됨', 'success')
      setLinkModal(false)
      reload()
    } catch (e: any) { showToast(e.message || '갈등 (수동 해결 필요)', 'error') }
  }

  const bridge = async () => {
    setBusy(true)
    try {
      const out = await api.ssoMigrateBridge(bridgeForm)
      setBridgeResult(out)
      showToast(out.new_session_issued ? '새 세션 발급 (토큰 미복사)' : `브리지 결과: ${out.decision}`, out.new_session_issued ? 'success' : 'error')
    } catch (e: any) {
      // 422 = fail closed decision.
      if (e.message && e.detail) setBridgeResult(e.detail)
      showToast(e.message || '브리지 실패', 'error')
    } finally { setBusy(false) }
  }

  const importManifest = async () => {
    if (!manifestForm.import_id || !manifestForm.itemsJson.trim()) { showToast('import_id와 items JSON 필요', 'error'); return }
    let items: any[] = []
    try { items = JSON.parse(manifestForm.itemsJson) } catch { showToast('items JSON 형식 오류', 'error'); return }
    try {
      await api.ssoMigrateManifests({ name: manifestForm.name, source: manifestForm.source, wave: manifestForm.wave, import_id: manifestForm.import_id, items })
      showToast('매니페스트 임포트됨 (멱등)', 'success')
      setManifestModal(false)
      reload()
    } catch (e: any) { showToast(e.message || '임포트 실패', 'error') }
  }

  const reconcile = async (m: Manifest) => {
    try {
      const r = await api.ssoMigrateReconcile(m.manifest_id)
      setRecon(r)
      showToast('조정 완료', 'success')
      reload()
    } catch (e: any) { showToast(e.message || '조정 실패', 'error') }
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
        <div>
          <h1 className="text-xl font-semibold">SSO 마이그레이션 <span className="text-sm text-gray-400">(Keycloak → Authentik)</span></h1>
          <p className="text-xs text-gray-500 mt-1">
            호환 우선 · 단계적 · 복귀 가능한 마이그레이션. 인증은 불변 issuer+subject와 Patty 사용자 ID로 매핑되며,
            브리지는 Keycloak 토큰·비밀번호를 절대 복사하지 않고 새 세션만 발급합니다. 자격 증명은 저장되지 않습니다.
          </p>
        </div>
        <div className="flex gap-2">
          <button className="btn-sm btn-secondary" onClick={() => setLinkModal(true)}>+ 매핑</button>
          <button className="btn-sm btn-secondary" onClick={() => setManifestModal(true)}>+ 발견 매니페스트</button>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4">
        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">브리지 인증 (새 세션만 발급)</h2>
          <div className="flex gap-2 mb-2">
            <input className="input text-xs flex-1 font-mono" placeholder="legacy issuer" value={bridgeForm.legacy_issuer} onChange={e => setBridgeForm({ ...bridgeForm, legacy_issuer: e.target.value })} />
          </div>
          <div className="flex gap-2 mb-2">
            <input className="input text-xs flex-1 font-mono" placeholder="legacy subject (불변)" value={bridgeForm.legacy_subject} onChange={e => setBridgeForm({ ...bridgeForm, legacy_subject: e.target.value })} />
            <button className="btn-sm btn-primary" onClick={bridge} disabled={busy}>{busy ? '…' : '브리지 실행'}</button>
          </div>
          {bridgeResult && (
            <div className={`text-[11px] border rounded p-2 ${bridgeResult.new_session_issued ? 'bg-green-50 border-green-200' : 'bg-amber-50 border-amber-200'}`}>
              <div>결정: <span className="font-mono">{bridgeResult.decision}</span>{bridgeResult.new_session_issued ? ' · ✅ 새 세션 발급 (토큰 미복사)' : ' · ⛔ 새 세션 없음'}</div>
              {bridgeResult.patty_user_id && <div>Patty 사용자: <span className="font-mono">{bridgeResult.patty_user_id}</span></div>}
            </div>
          )}
          <p className="text-[10px] text-gray-400 mt-2">미연결/모호/비활성은 즉시 실패(클로즈드) — 이메일만으로 자동 연결하지 않습니다.</p>
        </div>

        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">불변 매핑 레지스트리</h2>
          {links.length === 0 ? <p className="text-xs text-gray-400">등록된 매핑 없음</p> : (
            <table className="w-full text-xs">
              <thead><tr className="text-left text-gray-500 border-b border-gray-100">
                <th className="py-1 px-1">legacy issuer</th><th className="py-1 px-1">subject</th><th className="py-1 px-1">Patty 사용자</th><th className="py-1 px-1">상태</th>
              </tr></thead>
              <tbody>
                {links.map(l => (
                  <tr key={l.id} className="border-b border-gray-50">
                    <td className="py-1 px-1 font-mono text-[9px]">{l.legacy_issuer}</td>
                    <td className="py-1 px-1 font-mono text-[9px]">{l.legacy_subject}</td>
                    <td className="py-1 px-1 font-mono">{l.patty_user_id}</td>
                    <td className="py-1 px-1">{l.status === 'linked' ? <span className="text-green-600">연결</span> : l.status === 'ambiguous' ? <span className="text-red-600">모호</span> : <span className="text-gray-500">{l.status}</span>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      <div className="card p-4 mb-4">
        <h2 className="text-sm font-medium mb-2">발견 매니페스트 + 조정</h2>
        {manifests.length === 0 ? <p className="text-xs text-gray-400">임포트된 발견 매니페스트 없음</p> : (
          <table className="w-full text-xs">
            <thead><tr className="text-left text-gray-500 border-b border-gray-100">
              <th className="py-1 px-1">매니페스트</th><th className="py-1 px-1">웨이브</th><th className="py-1 px-1">소스</th><th className="py-1 px-1">연결/모호/제외</th><th className="py-1 px-1">상태</th><th className="py-1 px-1 text-right">동작</th>
            </tr></thead>
            <tbody>
              {manifests.map(m => (
                <tr key={m.id} className="border-b border-gray-50">
                  <td className="py-1 px-1"><div className="font-mono">{m.manifest_id}</div><div className="text-[9px] text-gray-400">{m.name}</div></td>
                  <td className="py-1 px-1">웨이브 {m.wave}</td>
                  <td className="py-1 px-1 text-gray-500">{m.source}</td>
                  <td className="py-1 px-1">{m.linked_count}/{m.ambiguous_count}/{m.excluded_count}</td>
                  <td className="py-1 px-1">{m.status}</td>
                  <td className="py-1 px-1 text-right"><button className="btn-xs btn-secondary" onClick={() => reconcile(m)}>조정</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {recon && (
          <div className="text-[11px] border rounded p-2 mt-2 bg-gray-50">
            <div className="font-medium mb-1">조정 요약 — {recon.manifest?.manifest_id}</div>
            소스 {recon.manifest?.source_count} · 연결 {recon.manifest?.linked_count} · 모호 {recon.manifest?.ambiguous_count} · 제외 {recon.manifest?.excluded_count}
          </div>
        )}
      </div>

      <div className="card p-4">
        <h2 className="text-sm font-medium mb-2">컷오버 웨이브</h2>
        {waves.length === 0 ? <p className="text-xs text-gray-400">웨이브 없음</p> : (
          <table className="w-full text-xs">
            <thead><tr className="text-left text-gray-500 border-b border-gray-100">
              <th className="py-1 px-1">웨이브</th><th className="py-1 px-1">이름</th><th className="py-1 px-1">소유자</th><th className="py-1 px-1">상태</th><th className="py-1 px-1 text-right">동작</th>
            </tr></thead>
            <tbody>
              {waves.map(w => (
                <tr key={w.id} className="border-b border-gray-50">
                  <td className="py-1 px-1">{w.wave}</td>
                  <td className="py-1 px-1">{w.name || '—'}</td>
                  <td className="py-1 px-1 font-mono">{w.owner_id || '—'}</td>
                  <td className="py-1 px-1">{w.status}</td>
                  <td className="py-1 px-1 text-right">
                    {w.status !== 'signed_off' && <button className="btn-xs btn-primary" onClick={async () => {
                      if (!await confirm({ title: '웨이브 서명', message: '앱 소유자 승인을 기록합니다. 롤백 교육과 모니터링이 선행되어야 합니다.', danger: false })) return
                      try { await api.ssoMigrateSignOff(w.id, '48h'); showToast('서명됨', 'success'); reload() } catch (e: any) { showToast(e.message || '실패', 'error') }
                    }}>서명</button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {linkModal && (
        <Modal title="불변 매핑 등록" onClose={() => setLinkModal(false)}>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">legacy issuer</label><input className="input text-xs font-mono" value={linkForm.legacy_issuer} onChange={e => setLinkForm({ ...linkForm, legacy_issuer: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">legacy subject (불변)</label><input className="input text-xs font-mono" value={linkForm.legacy_subject} onChange={e => setLinkForm({ ...linkForm, legacy_subject: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">Patty 사용자 ID</label><input className="input text-xs font-mono" value={linkForm.patty_user_id} onChange={e => setLinkForm({ ...linkForm, patty_user_id: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">target subject (인증키)</label><input className="input text-xs font-mono" value={linkForm.target_subject} onChange={e => setLinkForm({ ...linkForm, target_subject: e.target.value })} /></div>
            </div>
            <p className="text-[10px] text-gray-400">같은 legacy issuer+subject는 정확히 한 명의 Patty 사용자에만 매핑됩니다. 충돌은 모호로 표시되어 수동 해결됩니다.</p>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setLinkModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={link}>등록</button>
          </ModalFooter>
        </Modal>
      )}

      {manifestModal && (
        <Modal title="발견 매니페스트 임포트 (멱등)" onClose={() => setManifestModal(false)}>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">이름</label><input className="input text-xs" value={manifestForm.name} onChange={e => setManifestForm({ ...manifestForm, name: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">source</label><input className="input text-xs" value={manifestForm.source} onChange={e => setManifestForm({ ...manifestForm, source: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">import_id (멱등 키)</label><input className="input text-xs font-mono" value={manifestForm.import_id} onChange={e => setManifestForm({ ...manifestForm, import_id: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">웨이브</label><input type="number" className="input text-xs" value={manifestForm.wave} onChange={e => setManifestForm({ ...manifestForm, wave: +e.target.value })} /></div>
            </div>
            <div><label className="text-xs text-gray-500">items (JSON 배열: relief/client/user 등)</label>
              <textarea className="input text-xs font-mono" rows={8} value={manifestForm.itemsJson} onChange={e => setManifestForm({ ...manifestForm, itemsJson: e.target.value })}
                placeholder={`[\n  {"kind":"realm","legacy_key":"work","criticality":"high"},\n  {"kind":"client","legacy_key":"pccp","protocol":"oidc","criticality":"critical"},\n  {"kind":"user","legacy_key":"sub-1","criticality":"medium"}\n]`} />
            </div>
            <p className="text-[10px] text-gray-400">자격 증명(비밀번호/토큰/키)은 절대 포함하지 마십시오. 재임포트는 같은 import_id로 항목을 교체합니다.</p>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setManifestModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={importManifest}>임포트</button>
          </ModalFooter>
        </Modal>
      )}
    </div>
  )
}
