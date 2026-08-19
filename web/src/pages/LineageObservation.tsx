import { useEffect, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { Modal, ModalFooter } from '../components/Modal'

type Conn = {
  id: number; provider: string; provider_ko: string; base_url: string
  webhook_verified: boolean; health: string; last_reconciliation: string; known_gaps: string
}
type Event = {
  id: number; provider: string; provider_event_id: string; event_type: string
  actor: string; ref: string; commit_sha: string; ingested_at: string
}
type Attr = {
  commit_sha: string; lineage: string; lineage_ko: string; authoritative: boolean
  git_author: string; git_committer: string; author_distinct: boolean
  changeset_id?: string; session_id?: string; evidence_digest?: string; observed_at: string
}

const LINEAGE_TONE: Record<string, string> = {
  ai_created: 'bg-violet-100 text-violet-700', human_created: 'bg-emerald-100 text-emerald-700',
  human_modified_ai: 'bg-amber-100 text-amber-800', ai_modified_human: 'bg-sky-100 text-sky-700',
  mixed: 'bg-gray-200 text-gray-700', imported_unverifiable: 'bg-gray-100 text-gray-500',
}
const EVENT_KO: Record<string, string> = {
  push: '푸시', force_push: '강제 푸시 (재작성)', branch_create: '브랜치 생성', branch_delete: '브랜치 삭제',
  pr_opened: 'MR/PR 생성', pr_merged: 'MR/PR 병합', review: '리뷰', check: '검사/파이프라인',
  default_branch_change: '기본 브랜치 변경', repo_transferred: '저장소 이전', access_revoked: '접근 철회',
}
const HEALTH_KO: Record<string, string> = { healthy: '정상', stale: '지연 (완전성 보장 없음)', revoked: '철회됨', degraded: '성능 저하' }

export default function LineageObservation() {
  const [conns, setConns] = useState<Conn[]>([])
  const [events, setEvents] = useState<Event[]>([])
  const [attrs, setAttrs] = useState<Attr[]>([])
  const [legend, setLegend] = useState<Record<string, string>>({})
  const [connOpen, setConnOpen] = useState(false)
  const [conn, setConn] = useState({ provider: 'patty_git', base_url: '', credential_ref: '', webhook_secret: '' })
  const [bindOpen, setBindOpen] = useState(false)
  const [bind, setBind] = useState({ provider_repo_id: '', commit_sha: '', patch_digest: '', git_author: '', git_committer: '' })

  const load = () => {
    api.lgConnections().then((d: Conn[]) => setConns(Array.isArray(d) ? d : [])).catch(() => {})
    api.lgEvents().then((d: Event[]) => setEvents(Array.isArray(d) ? d : [])).catch(() => {})
    api.lgLineage().then((r: any) => { setAttrs(r.commits || []); setLegend(r.legend || {}) }).catch(() => {})
  }
  useEffect(() => { load() }, [])

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">코드 계보 관측 <span className="text-xs text-gray-400 ml-2">PAT-1453 · 읽기 전용 · 증거 기반 귀속</span></h1>
          <p className="text-sm text-gray-500 mt-1">Patty Git · GitLab · GitHub(GHES)을 어댑터로 정규화해 관측합니다. 제공자 쪽 변경은 없으며, 귀속은 다이제스트가 일치할 때만 인정됩니다.</p>
        </div>
        <div className="flex gap-2">
          <button className="btn-secondary text-sm" onClick={() => api.lgReconcile().then((r: any) => { showToast(`지연 연결 ${r.marked_stale}개 표시`); load() }).catch((e: any) => showToast(e.message))}>조정 점검</button>
          <button className="btn text-sm" onClick={() => setConnOpen(true)}>관측 연결 추가</button>
        </div>
      </div>

      {/* Connections */}
      <div className="card">
        <div className="p-4 border-b"><h2 className="font-semibold">관측 연결 (관리형 서비스 신원)</h2></div>
        {conns.map((c) => (
          <div key={c.id} className="p-3 border-t flex items-center justify-between text-xs">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="font-medium">{c.provider_ko}</span>
              <span className="text-gray-500 font-mono">{c.base_url || '—'}</span>
              <span className={`px-1.5 py-0.5 rounded ${c.health === 'healthy' ? 'bg-emerald-100 text-emerald-700' : c.health === 'stale' ? 'bg-amber-100 text-amber-800' : 'bg-gray-200'}`}>{HEALTH_KO[c.health] || c.health}</span>
              {!c.webhook_verified && <span className="text-amber-600">웹훅 미인증</span>}
            </div>
            <div className="flex items-center gap-2">
              <span className="text-gray-400">마지막 조정 {c.last_reconciliation ? new Date(c.last_reconciliation).toLocaleString('ko-KR') : '없음'}</span>
              {c.health !== 'revoked' && (
                <button className="btn-secondary text-[11px]" onClick={() => api.lgRevokeConnection(c.id).then(load).catch((e: any) => showToast(e.message))}>철회</button>
              )}
            </div>
          </div>
        ))}
        {conns.length === 0 && <p className="p-4 text-sm text-gray-500">연결이 없습니다.</p>}
      </div>

      {/* Lineage with legend */}
      <div className="card">
        <div className="p-4 border-b flex items-center justify-between flex-wrap gap-2">
          <h2 className="font-semibold">커밋 계보 (현재 라인 뷰)</h2>
          <div className="flex gap-1 flex-wrap">
            {Object.entries(legend).map(([k, ko]) => (
              <span key={k} className={`text-[11px] px-1.5 py-0.5 rounded ${LINEAGE_TONE[k] || 'bg-gray-100'}`}>{ko}</span>
            ))}
          </div>
          <button className="btn-secondary text-xs" onClick={() => setBindOpen(true)}>커밋 귀속 등록</button>
        </div>
        {attrs.length === 0 && <p className="p-4 text-sm text-gray-500">귀속 기록이 없습니다.</p>}
        {attrs.map((a) => (
          <div key={a.commit_sha} className="p-3 border-t flex items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-mono text-xs">{a.commit_sha.slice(0, 10)}</span>
                <span className={`text-[11px] px-1.5 py-0.5 rounded ${LINEAGE_TONE[a.lineage] || 'bg-gray-100'}`}>{a.lineage_ko}</span>
                {a.authoritative
                  ? <span className="text-[11px] px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-700">증거 결합 (다이제스트 일치)</span>
                  : <span className="text-[11px] px-1.5 py-0.5 rounded bg-gray-100 text-gray-500">가져옴/검증 불가</span>}
                {a.author_distinct && <span className="text-[11px] text-gray-500">작성자 ≠ 커미터</span>}
              </div>
              <div className="text-[11px] text-gray-400 mt-1">
                작성자 {a.git_author || '—'} · 커미터 {a.git_committer || '—'}
                {a.session_id && ` · 세션 ${a.session_id.slice(0, 14)}… · 증거 ${a.evidence_digest?.slice(0, 12)}…`}
              </div>
            </div>
          </div>
        ))}
      </div>

      {/* Events feed */}
      <div className="card max-h-72 overflow-auto">
        <div className="p-4 border-b sticky top-0 bg-white"><h2 className="font-semibold">관측된 저장소 이벤트 (중복 제거 · 재생 안전)</h2></div>
        {events.map((e) => (
          <div key={e.id} className="p-2.5 border-t flex items-center gap-3 text-xs">
            <span className="px-1.5 py-0.5 rounded bg-gray-100">{EVENT_KO[e.event_type] || e.event_type}</span>
            <span className="font-mono text-gray-500">{e.ref || '—'}</span>
            <span>{e.actor}</span>
            {e.commit_sha && <span className="font-mono text-gray-400">{e.commit_sha.slice(0, 8)}</span>}
            <span className="text-gray-400 ml-auto">{new Date(e.ingested_at).toLocaleString('ko-KR')}</span>
          </div>
        ))}
        {events.length === 0 && <p className="p-4 text-sm text-gray-500">이벤트가 없습니다.</p>}
      </div>

      {/* Connection create */}
      <Modal open={connOpen} title="관측 연결 추가 (읽기 전용)" onClose={() => setConnOpen(false)} size="md"
        footer={<ModalFooter onCancel={() => setConnOpen(false)}
          onConfirm={() => api.lgCreateConnection(conn).then(() => { setConnOpen(false); showToast('연결을 생성했습니다 (웹훅 인증 필요)'); load() }).catch((e: any) => showToast(e.message))}
          confirmLabel="생성" disabled={!conn.webhook_secret.trim()} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">제공자</label>
              <select className="input" value={conn.provider} onChange={(e) => setConn({ ...conn, provider: e.target.value })}>
                <option value="patty_git">Patty Git</option><option value="gitlab">GitLab (자체 관리 포함)</option><option value="github">GitHub (GHES 포함)</option>
              </select></div>
            <div><label className="label">Base URL (자체 관리 배포)</label>
              <input className="input" value={conn.base_url} onChange={(e) => setConn({ ...conn, base_url: e.target.value })} placeholder="https://github.example.com" /></div>
          </div>
          <div><label className="label">자격증명 참조 (비밀 저장소)</label>
            <input className="input font-mono" value={conn.credential_ref} onChange={(e) => setConn({ ...conn, credential_ref: e.target.value })} placeholder="secretstore:…" /></div>
          <div><label className="label">웹훅 비밀 *</label>
            <input className="input" value={conn.webhook_secret} onChange={(e) => setConn({ ...conn, webhook_secret: e.target.value })} /></div>
          <p className="text-xs text-gray-500">관리형 서비스 신원만 사용합니다(개인 토큰 금지). 자격증명은 마스킹되어 저장되며 UI에서 다시 볼 수 없습니다. 이 연결은 읽기 전용이며 제공자 쪽 상태를 변경하지 않습니다.</p>
        </div>
      </Modal>

      {/* Attribution bind */}
      <Modal open={bindOpen} title="커밋 귀속 등록 (다이제스트 일치 필수)" onClose={() => setBindOpen(false)} size="md"
        footer={<ModalFooter onCancel={() => setBindOpen(false)}
          onConfirm={() => api.lgBindAttribution(bind).then((r: any) => {
            setBindOpen(false)
            showToast(r.authoritative ? `결합 성공 — ${r.lineage_ko}` : '일치하는 증거가 없어 가져옴/검증 불가로 기록했습니다')
            load()
          }).catch((e: any) => showToast(e.message))}
          confirmLabel="등록" disabled={!bind.commit_sha.trim() || !bind.patch_digest.trim()} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">제공자 저장소 ID</label>
              <input className="input" value={bind.provider_repo_id} onChange={(e) => setBind({ ...bind, provider_repo_id: e.target.value })} /></div>
            <div><label className="label">커밋 SHA *</label>
              <input className="input font-mono" value={bind.commit_sha} onChange={(e) => setBind({ ...bind, commit_sha: e.target.value })} /></div>
          </div>
          <div><label className="label">패치 다이제스트 *</label>
            <input className="input font-mono" value={bind.patch_digest} onChange={(e) => setBind({ ...bind, patch_digest: e.target.value })} placeholder="sha256:…" /></div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">Git 작성자</label>
              <input className="input" value={bind.git_author} onChange={(e) => setBind({ ...bind, git_author: e.target.value })} /></div>
            <div><label className="label">Git 커미터</label>
              <input className="input" value={bind.git_committer} onChange={(e) => setBind({ ...bind, git_committer: e.target.value })} /></div>
          </div>
          <p className="text-xs text-gray-500">귀속은 기록된 변경 집합의 diff 다이제스트와 패치 다이제스트가 정확히 일치할 때만 인정됩니다. 커밋 메시지 · 시각 근접 · 작성자 · 브랜치 이름은 근거가 되지 않으며, 일치하지 않으면 가져옴/검증 불가로 남습니다.</p>
        </div>
      </Modal>
    </div>
  )
}
