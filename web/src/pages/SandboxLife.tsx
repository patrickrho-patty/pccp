import { useState, useEffect } from 'react'
import { api } from '../api'
import { Modal, ModalFooter } from '../components/Modal'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'

// 하드닝 샌드박스 라이프사이클 (PAT-1452) — 조직/팀/프로젝트/저장소/풀 범위의
// ephemeral(임시)·persistent(영구)·pinned(고정 워크스테이션) 모드. 강화 전용
// 상속, 불변 서명 템플릿, 러너 풀, 대기열 동시성, 드리프트/격리/폐기/초기화.
// 대화 재개는 환경 상태를 복원하지 않습니다.

type Policy = { id: string; scope: string; scope_id?: string; mode: string; template_id?: string }
type Tpl = { id: string; template_id: string; version: number; name?: string; image_ref?: string; image_digest?: string; status?: string }
type Runner = { id: string; runner_id: string; name?: string; runtime_type: string; status: string; compliance?: string; active_count?: number; max_concurrency?: number; pinned_user_id?: string }
type Env = { id: string; environment_id: string; user_id: string; repository_id?: string; mode: string; status: string; drift_status?: string; runner_id?: string; template_digest?: string; attached_session_id?: string; ready?: boolean }

const MODE_KO: Record<string, string> = { ephemeral: '임시', persistent: '영구', pinned: '고정 워크스테이션' }

export default function SandboxLife() {
  const confirm = useConfirm()
  const [policies, setPolicies] = useState<Policy[]>([])
  const [templates, setTemplates] = useState<Tpl[]>([])
  const [runners, setRunners] = useState<Runner[]>([])
  const [envs, setEnvs] = useState<Env[]>([])
  const [resolved, setResolved] = useState<any>(null)
  // Modals
  const [polModal, setPolModal] = useState(false)
  const [polForm, setPolForm] = useState({ scope: 'org', scope_id: '', mode: 'ephemeral', template_id: '' })
  const [tplModal, setTplModal] = useState(false)
  const [tplForm, setTplForm] = useState({ template_id: '', name: '', image_ref: '', image_digest: '' })
  const [runModal, setRunModal] = useState(false)
  const [runForm, setRunForm] = useState({ runner_id: '', name: '', runtime_type: 'docker', max_concurrency: 8, pinned_user_id: '' })
  const [prepModal, setPrepModal] = useState(false)
  const [prepForm, setPrepForm] = useState({ user_id: '', repository_id: '', session_id: '' })
  const [prepResult, setPrepResult] = useState<any>(null)
  const [busy, setBusy] = useState(false)

  const reload = () => {
    api.sandboxLifePolicy().then(setPolicies).catch(() => {})
    api.sandboxLifeTemplates().then(setTemplates).catch(() => {})
    api.sandboxLifeRunners().then(setRunners).catch(() => {})
    api.sandboxLifeEnvironments().then(setEnvs).catch(() => {})
  }
  useEffect(() => { reload() }, [])

  const savePolicy = async () => {
    try {
      await api.sandboxLifePolicy(polForm)
      showToast('정책 저장됨 (강화 전용)', 'success')
      setPolModal(false)
      reload()
    } catch (e: any) { showToast(e.message || '실패 (강화 전용 위반일 수 있음)', 'error') }
  }

  const saveTpl = async () => {
    if (!tplForm.template_id || !tplForm.image_ref || !tplForm.image_digest) { showToast('template_id/image_ref/image_digest 필요', 'error'); return }
    try {
      await api.sandboxLifeTemplates(tplForm)
      showToast('템플릿 등록됨 (불변 · 서명)', 'success')
      setTplModal(false)
      reload()
    } catch (e: any) { showToast(e.message || '실패', 'error') }
  }

  const saveRunner = async () => {
    if (!runForm.runner_id || !runForm.runtime_type) { showToast('runner_id/runtime_type 필요', 'error'); return }
    try {
      await api.sandboxLifeRunners(runForm)
      showToast('러너 등록됨', 'success')
      setRunModal(false)
      reload()
    } catch (e: any) { showToast(e.message || '실패', 'error') }
  }

  const prepare = async () => {
    if (!prepForm.user_id || !prepForm.session_id) { showToast('user_id/session_id 필요', 'error'); return }
    setBusy(true)
    try {
      const r = await api.sandboxLifePrepare(prepForm)
      setPrepResult(r)
      showToast(`준비됨 — ${MODE_KO[r.mode] || r.mode} · ${r.ready ? '준비 완료' : '미준비'}`, r.ready ? 'success' : 'error')
      reload()
    } catch (e: any) { showToast(e.message || '준비 실패 (고정 워크스테이션 미온라인 등)', 'error') }
    finally { setBusy(false) }
  }

  const envAction = async (env: Env, action: string) => {
    const destructive = action === 'destroy' || action === 'reset'
    if (destructive && !await confirm({ title: `${action.toUpperCase()}`, message: `${env.environment_id}를 ${action}할까요? (영구 작업공간 파괴는 경고 대상)`, danger: true })) return
    try {
      await api.sandboxLifeAction(env.environment_id, action)
      showToast(`${action} 완료`, 'success')
      reload()
    } catch (e: any) { showToast(e.message || '실패', 'error') }
  }

  const modeBadge = (m: string) => {
    const color = m === 'ephemeral' ? 'bg-blue-50 text-blue-700 border-blue-200' : m === 'pinned' ? 'bg-purple-50 text-purple-700 border-purple-200' : 'bg-gray-100 text-gray-600 border-gray-200'
    return <span className={`text-[10px] px-2 py-0.5 rounded-full border ${color}`}>{MODE_KO[m] || m}</span>
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
        <div>
          <h1 className="text-xl font-semibold">하드닝 샌드박스 라이프사이클</h1>
          <p className="text-xs text-gray-500 mt-1">
            조직·팀·프로젝트·저장소·풀 범위의 임시/영구/고정 모드. 더 구체적인 정책은 강화만 가능하고 핵심 격리 기준선은
            비활성화할 수 없습니다. 대화 재개는 환경 상태를 복원하지 않습니다(파일/프로세스/자격 증명 미복원).
          </p>
        </div>
        <div className="flex gap-2">
          <button className="btn-sm btn-secondary" onClick={() => setTplModal(true)}>+ 템플릿</button>
          <button className="btn-sm btn-secondary" onClick={() => setRunModal(true)}>+ 러너</button>
          <button className="btn-sm btn-primary" onClick={() => setPrepModal(true)}>▶ 환경 준비</button>
        </div>
      </div>

      {prepResult && (
        <div className="card p-3 mb-4 text-xs">
          <div className="font-medium">준비 결과 — {prepResult.environment_id}</div>
          <div className="flex gap-3 mt-1 text-gray-500">모드 {MODE_KO[prepResult.mode]} · 러너 <span className="font-mono">{prepResult.runner_id || '—'}</span> · 템플릿 v{prepResult.template_version || '—'} · {prepResult.ready ? '✅ ready' : `⚠ ${prepResult.reason}`}</div>
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 mb-4">
        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">라이프사이클 정책 (강화 전용)</h2>
          {policies.length === 0 ? <p className="text-xs text-gray-400">정책 없음 (기본: 임시 기준선)</p> : (
            <div className="space-y-1 text-xs">
              {policies.map(p => (
                <div key={p.id} className="flex items-center justify-between border-b border-gray-50 py-1">
                  <span className="text-gray-600">{p.scope}{p.scope_id ? <span className="font-mono">:{p.scope_id}</span> : ''}</span>
                  {modeBadge(p.mode)}
                </div>
              ))}
            </div>
          )}
          <button className="btn-sm btn-secondary mt-3" onClick={() => setPolModal(true)}>+ 정책 설정</button>
        </div>

        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">환경 템플릿 (불변 · 서명)</h2>
          {templates.length === 0 ? <p className="text-xs text-gray-400">템플릿 없음</p> : (
            <div className="space-y-1 text-xs">
              {templates.map(t => (
                <div key={t.id} className="border-b border-gray-50 py-1">
                  <div className="flex justify-between"><span className="font-medium">{t.name || t.template_id}</span><span className="text-gray-400">v{t.version}</span></div>
                  <div className="text-[9px] text-gray-400 font-mono truncate">{t.image_ref} @ {t.image_digest?.slice(0, 14)}</div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">러너 풀</h2>
          {runners.length === 0 ? <p className="text-xs text-gray-400">러너 없음</p> : (
            <div className="space-y-1 text-xs">
              {runners.map(r => (
                <div key={r.id} className="flex items-center justify-between border-b border-gray-50 py-1">
                  <div><div className="font-medium">{r.name || r.runner_id}</div><div className="text-[9px] text-gray-400">{r.runtime_type}{r.pinned_user_id ? ` · pinned:${r.pinned_user_id}` : ''}</div></div>
                  <span className={`text-[10px] px-2 py-0.5 rounded-full border ${r.status === 'ok' ? 'bg-green-50 text-green-700 border-green-200' : r.status === 'offline' ? 'bg-red-50 text-red-700 border-red-200' : 'bg-amber-50 text-amber-700 border-amber-200'}`}>{r.status} {r.active_count}/{r.max_concurrency}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      <div className="card p-4">
        <h2 className="text-sm font-medium mb-2">환경 인벤토리</h2>
        {envs.length === 0 ? <p className="text-xs text-gray-400">생성된 환경 없음</p> : (
          <table className="w-full text-xs">
            <thead><tr className="text-left text-gray-500 border-b border-gray-100">
              <th className="py-1 px-1">환경</th><th className="py-1 px-1">사용자/저장소</th><th className="py-1 px-1">모드</th><th className="py-1 px-1">상태</th><th className="py-1 px-1">드리프트</th><th className="py-1 px-1 text-right">동작</th>
            </tr></thead>
            <tbody>
              {envs.map(env => (
                <tr key={env.id} className="border-b border-gray-50">
                  <td className="py-1 px-1 font-mono text-[10px]">{env.environment_id}</td>
                  <td className="py-1 px-1"><span className="font-mono">{env.user_id}</span>{env.repository_id ? <span className="text-gray-400">/</span> : null}<span className="font-mono text-gray-400">{env.repository_id || ''}</span></td>
                  <td className="py-1 px-1">{modeBadge(env.mode)}</td>
                  <td className="py-1 px-1">{env.status}{env.ready ? ' · ✅' : ''}</td>
                  <td className="py-1 px-1">{env.drift_status !== 'none' ? <span className="text-red-600">{env.drift_status}</span> : <span className="text-gray-400">—</span>}</td>
                  <td className="py-1 px-1 text-right whitespace-nowrap">
                    {env.status === 'attached' && <button className="btn-xs btn-secondary" onClick={() => envAction(env, 'drain')}>드레인</button>}
                    {(env.status === 'attached' || env.status === 'ready') && <button className="btn-xs btn-secondary ml-1" onClick={() => envAction(env, 'quarantine')}>격리</button>}
                    <button className="btn-xs btn-secondary ml-1" onClick={() => envAction(env, 'reset')}>초기화</button>
                    <button className="btn-xs btn-danger ml-1" onClick={() => envAction(env, 'destroy')}>폐기</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {polModal && (
        <Modal title="라이프사이클 정책 (강화 전용)" onClose={() => setPolModal(false)}>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">범위</label>
                <select className="input text-xs" value={polForm.scope} onChange={e => setPolForm({ ...polForm, scope: e.target.value })}>
                  <option value="org">조직</option><option value="team">팀</option><option value="project">프로젝트</option><option value="repository">저장소</option><option value="pool">풀</option>
                </select></div>
              <div><label className="text-xs text-gray-500">모드</label>
                <select className="input text-xs" value={polForm.mode} onChange={e => setPolForm({ ...polForm, mode: e.target.value })}>
                  <option value="ephemeral">임시 (세션마다 새로)</option><option value="persistent">영구 (보존)</option><option value="pinned">고정 워크스테이션</option>
                </select></div>
              <div><label className="text-xs text-gray-500">대상 ID (org 제외)</label><input className="input text-xs" value={polForm.scope_id} disabled={polForm.scope === 'org'} onChange={e => setPolForm({ ...polForm, scope_id: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">템플릿 ID</label><input className="input text-xs" value={polForm.template_id} onChange={e => setPolForm({ ...polForm, template_id: e.target.value })} /></div>
            </div>
            <p className="text-[10px] text-gray-400">더 구체적인 범위는 상위 결정보다 약화할 수 없습니다. 핵심 격리 기준선(non-root, 제한 리소스, 최소 권한 마운트, 거부 기본 네트워크 등)은 항상 적용됩니다.</p>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setPolModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={savePolicy}>저장</button>
          </ModalFooter>
        </Modal>
      )}

      {tplModal && (
        <Modal title="환경 템플릿 등록" onClose={() => setTplModal(false)}>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">template_id</label><input className="input text-xs font-mono" value={tplForm.template_id} onChange={e => setTplForm({ ...tplForm, template_id: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">이름</label><input className="input text-xs" value={tplForm.name} onChange={e => setTplForm({ ...tplForm, name: e.target.value })} /></div>
              <div className="col-span-2"><label className="text-xs text-gray-500">image_ref (불변)</label><input className="input text-xs font-mono" value={tplForm.image_ref} onChange={e => setTplForm({ ...tplForm, image_ref: e.target.value })} /></div>
              <div className="col-span-2"><label className="text-xs text-gray-500">image_digest (검증)</label><input className="input text-xs font-mono" value={tplForm.image_digest} onChange={e => setTplForm({ ...tplForm, image_digest: e.target.value })} /></div>
            </div>
            <p className="text-[10px] text-gray-400">템플릿에 비밀 값을 포함할 수 없습니다. 버전마다 불변하며 서명·승인이 필요합니다.</p>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setTplModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={saveTpl}>등록</button>
          </ModalFooter>
        </Modal>
      )}

      {runModal && (
        <Modal title="러너 등록" onClose={() => setRunModal(false)}>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">runner_id</label><input className="input text-xs font-mono" value={runForm.runner_id} onChange={e => setRunForm({ ...runForm, runner_id: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">이름</label><input className="input text-xs" value={runForm.name} onChange={e => setRunForm({ ...runForm, name: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">런타임 유형</label>
                <select className="input text-xs" value={runForm.runtime_type} onChange={e => setRunForm({ ...runForm, runtime_type: e.target.value })}>
                  <option value="docker">Docker</option><option value="kubernetes">Kubernetes</option><option value="vm">VM</option><option value="workstation">고정 워크스테이션</option>
                </select></div>
              <div><label className="text-xs text-gray-500">동시성</label><input type="number" className="input text-xs" value={runForm.max_concurrency} onChange={e => setRunForm({ ...runForm, max_concurrency: +e.target.value })} /></div>
              <div className="col-span-2"><label className="text-xs text-gray-500">pinned_user_id (워크스테이션용)</label><input className="input text-xs" value={runForm.pinned_user_id} onChange={e => setRunForm({ ...runForm, pinned_user_id: e.target.value })} /></div>
            </div>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setRunModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={saveRunner}>등록</button>
          </ModalFooter>
        </Modal>
      )}

      {prepModal && (
        <Modal title="환경 준비 / 재연결" onClose={() => setPrepModal(false)}>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div><label className="text-xs text-gray-500">사용자 ID</label><input className="input text-xs font-mono" value={prepForm.user_id} onChange={e => setPrepForm({ ...prepForm, user_id: e.target.value })} /></div>
              <div><label className="text-xs text-gray-500">저장소 ID</label><input className="input text-xs font-mono" value={prepForm.repository_id} onChange={e => setPrepForm({ ...prepForm, repository_id: e.target.value })} /></div>
              <div className="col-span-2"><label className="text-xs text-gray-500">세션 ID</label><input className="input text-xs font-mono" value={prepForm.session_id} onChange={e => setPrepForm({ ...prepForm, session_id: e.target.value })} /></div>
            </div>
            <p className="text-[10px] text-gray-400">영구 모드는 동일 사용자+저장소 작업공간에 재연결됩니다 (상태 자동 리셋 없음). 고정 모드는 지정 워크스테이션에만 라우팅되며 오프라인 시 자동 대체가 없습니다.</p>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setPrepModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={prepare} disabled={busy}>{busy ? '…' : '준비/재연결'}</button>
          </ModalFooter>
        </Modal>
      )}
    </div>
  )
}
