import { useState, useEffect } from 'react'
import { api } from '../api'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'

// System Prompts (PAT-1455) — managed prompt additions for org/team/fleet/user
// scopes. One active document per target, immutable version history, secret/
// size/interpolation validation on save, and a deterministic effective-preview.

const SCOPE_LABEL: Record<string, string> = {
  org: '조직', team: '팀', fleet: '플릿/하네스', user: '사용자',
}

type Doc = {
  id: string
  scope: string
  scope_id?: string
  title?: string
  content: string
  version: number
  digest: string
  enabled: boolean
  version_count?: number
}

type Version = { version: number; content: string; digest: string; created_by?: string; restored_from?: number }

export default function SystemPrompts() {
  const confirm = useConfirm()
  const [docs, setDocs] = useState<Doc[]>([])
  const [scope, setScope] = useState('org')
  const [scopeId, setScopeId] = useState('')
  const [selected, setSelected] = useState<Doc | null>(null)
  const [content, setContent] = useState('')
  const [title, setTitle] = useState('')
  const [effective, setEffective] = useState<any>(null)
  const [versions, setVersions] = useState<Version[]>([])
  const [loading, setLoading] = useState(true)
  const [showVersions, setShowVersions] = useState(false)
  const [saving, setSaving] = useState(false)

  const reload = () => {
    setLoading(true)
    api.listSystemPrompts('').then((data) => {
      setDocs(Array.isArray(data) ? data : [])
      setLoading(false)
    }).catch(() => setLoading(false))
  }
  useEffect(() => { reload() }, [])

  const refreshEffective = (sc = scope, sid = scopeId) => {
    api.systemPromptEffective(sc, sid).then(setEffective).catch(() => setEffective(null))
  }
  useEffect(() => { refreshEffective() }, [scope, scopeId, docs])

  const openEdit = (doc: Doc | null) => {
    setSelected(doc)
    setContent(doc?.content || '')
    setTitle(doc?.title || '')
    if (doc) { setScope(doc.scope); setScopeId(doc.scope_id || '') }
    setShowVersions(false)
  }

  const save = async () => {
    if (!content.trim()) { showToast('지침을 입력하세요', 'error'); return }
    setSaving(true)
    try {
      await api.saveSystemPrompt({ id: selected?.id, scope, scope_id: scopeId, title, content })
      showToast('저장됨 — 다음 요청부터 적용됩니다', 'success')
      setSelected(null)
      setContent(''); setTitle('')
      reload(); refreshEffective()
    } catch (e: any) {
      showToast(e.message || '저장 실패', 'error')
    } finally { setSaving(false) }
  }

  const toggleEnabled = async (doc: Doc) => {
    try {
      await api.setSystemPromptEnabled(doc.id, !doc.enabled)
      showToast(doc.enabled ? '비활성화됨' : '활성화됨', 'success')
      reload()
    } catch (e: any) { showToast(e.message || '실패', 'error') }
  }

  const loadVersions = async (doc: Doc) => {
    setSelected(doc)
    try {
      const vs = await api.listSystemPromptVersions(doc.id)
      setVersions(Array.isArray(vs) ? vs : [])
      setShowVersions(true)
    } catch (e: any) { showToast(e.message || '버전 로드 실패', 'error') }
  }

  const restore = async (v: Version) => {
    if (!selected) return
    if (!await confirm({ title: '복원', message: `v${v.version}을 새 버전으로 복원할까요? (기존 버전은 유지됩니다)`, danger: false })) return
    try {
      await api.restoreSystemPrompt(selected.id, v.version)
      showToast('복원됨', 'success')
      setShowVersions(false)
      reload(); refreshEffective()
      openEdit(selected)
    } catch (e: any) { showToast(e.message || '복원 실패', 'error') }
  }

  const deliver = async () => {
    try {
      const out = await api.deliverSystemPromptEpoch()
      showToast(`배포 완료 — epoch ${out.epoch_number} · 대상 ${out.targets}대`, 'success')
    } catch (e: any) { showToast(e.message || '배포 실패', 'error') }
  }

  const contributors = (effective?.contributors || []) as any[]
  const effBytes = contributors.filter((c: any) => c.enabled).reduce((a: number, c: any) => a + (c.content || '').length, 0)

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
        <div>
          <h1 className="text-xl font-semibold">시스템 프롬프트</h1>
          <p className="text-xs text-gray-500 mt-1">
            조직·팀·플릿·사용자 범위에 관리 지침을 추가합니다. 웹에서 편집 → 저장 시 자동으로
            버전·검증·서명·배포·감사됩니다.
          </p>
        </div>
        <div className="flex gap-2">
          {docs.length > 0 && (
            <button className="btn-sm btn-secondary" onClick={deliver}>📤 프롬프트 배포</button>
          )}
          <button className="btn-sm btn-primary" onClick={() => openEdit(null)}>+ 지침 추가</button>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 mb-4">
        <div className="lg:col-span-2 card p-4">
          <h2 className="text-sm font-medium mb-2">대상별 지침</h2>
          <div className="flex gap-2 mb-3 flex-wrap">
            <select className="input max-w-[150px] text-xs" value={scope} onChange={(e) => { setScope(e.target.value); setScopeId(''); refreshEffective(e.target.value, '') }}>
              <option value="org">조직</option>
              <option value="team">팀</option>
              <option value="fleet">플릿/하네스</option>
              <option value="user">사용자</option>
            </select>
            <input className="input max-w-[220px] text-xs" disabled={scope === 'org'} placeholder={scope === 'org' ? '(조직 전체)' : '대상 ID'} value={scopeId}
              onChange={e => { setScopeId(e.target.value); refreshEffective(scope, e.target.value) }} />
            <button className="btn-sm btn-secondary" onClick={() => refreshEffective()}>미리보기</button>
          </div>
          {loading ? <p className="text-sm text-gray-400">불러오는 중…</p> : docs.length === 0 ? (
            <EmptyState title="설정된 지침 없음" desc="관리 지침을 추가하면 여기에 표시됩니다." />
          ) : (
            <table className="w-full text-xs">
              <thead>
                <tr className="text-left text-gray-500 border-b border-gray-100">
                  <th className="py-2 px-2">범위</th>
                  <th className="py-2 px-2">제목</th>
                  <th className="py-2 px-2">버전</th>
                  <th className="py-2 px-2">상태</th>
                  <th className="py-2 px-2 text-right">동작</th>
                </tr>
              </thead>
              <tbody>
                {docs.map((d) => (
                  <tr key={d.id} className="border-b border-gray-50 hover:bg-gray-50/50">
                    <td className="py-2 px-2">{SCOPE_LABEL[d.scope] || d.scope}{d.scope !== 'org' ? <span className="text-gray-400 font-mono"> · {d.scope_id}</span> : null}</td>
                    <td className="py-2 px-2 text-gray-700">{d.title || '(제목 없음)'}
                      <div className="text-[10px] text-gray-400 font-mono">{d.digest.slice(0, 16)}…</div>
                    </td>
                    <td className="py-2 px-2">v{d.version}{d.version_count ? ` (+${d.version_count - 1} 이력)` : ''}</td>
                    <td className="py-2 px-2">{d.enabled
                      ? <span className="text-[10px] px-2 py-0.5 rounded-full border bg-green-50 text-green-700 border-green-200">활성</span>
                      : <span className="text-[10px] px-2 py-0.5 rounded-full border bg-gray-100 text-gray-500 border-gray-200">비활성</span>}
                    </td>
                    <td className="py-2 px-2 text-right whitespace-nowrap">
                      <button className="btn-sm btn-secondary" onClick={() => openEdit(d)}>편집</button>
                      <button className="btn-sm btn-secondary ml-1" onClick={() => loadVersions(d)}>이력</button>
                      <button className="btn-sm btn-secondary ml-1" onClick={() => toggleEnabled(d)}>{d.enabled ? '비활성' : '활성'}</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">효과 지침 미리보기</h2>
          <p className="text-[10px] text-gray-400 mb-2">우선순위: 조직 → 팀 → 플릿 → 사용자 → 로컬 지침</p>
          {contributors.length === 0 ? (
            <p className="text-xs text-gray-400">적용된 관리 지침이 없습니다.</p>
          ) : (
            <div className="space-y-2">
              {contributors.map((c: any, i: number) => (
                <div key={i} className={`border rounded p-2 text-left ${c.winning ? 'border-blue-200 bg-blue-50/40' : 'border-gray-100'}`}>
                  <div className="flex items-center gap-1 text-[10px] mb-1">
                    <span className={`px-1.5 py-0.5 rounded-full border ${c.winning ? 'bg-blue-100 text-blue-700 border-blue-300' : 'bg-gray-100 text-gray-500 border-gray-200'}`}>
                      {SCOPE_LABEL[c.scope] || c.scope}
                    </span>
                    {c.winning ? <span className="text-blue-700">승인 (적용)</span> : <span className="text-gray-400">상위 권한이 우선</span>}
                    {c.conflict ? <span className="text-red-600">충돌</span> : null}
                    <span className="text-gray-400">v{c.version}</span>
                  </div>
                  <div className="text-[11px] text-gray-600 line-clamp-2 whitespace-pre-wrap">{c.content}</div>
                  <div className="text-[9px] text-gray-400 font-mono mt-1">{c.digest.slice(0, 20)}…</div>
                </div>
              ))}
            </div>
          )}
          <div className="mt-2 text-[10px] text-gray-400">
            현재 버전 <span className="font-mono">{effective?.digest ? effective.digest.slice(0, 12) : '-'}</span> ·
            예산 <span className={effBytes > 8000 ? 'text-red-600 font-semibold' : ''}>{effBytes}/8,000B</span>
          </div>
        </div>
      </div>

      {(selected || showVersions) && (
        <Modal title={showVersions && selected ? '버전 이력' : `지침 ${selected ? '편집' : '추가'} — ${SCOPE_LABEL[selected?.scope || scope] || selected?.scope || scope}`}
          onClose={() => { setSelected(null); setShowVersions(false) }}>
          {showVersions && selected ? (
            <div className="space-y-2 max-h-[320px] overflow-y-auto">
              {versions.length === 0 ? <p className="text-xs text-gray-400">버전 이력 없음</p> : versions.map(v => (
                <div key={v.version} className="border border-gray-100 rounded p-2">
                  <div className="flex items-center justify-between text-[11px] mb-1">
                    <span className="font-medium">v{v.version}</span>
                    {v.restored_from ? <span className="text-gray-400">v{v.restored_from} 복원</span> : null}
                    <button className="btn-xs btn-secondary" onClick={() => restore(v)}>이 버전으로 복원</button>
                  </div>
                  <div className="text-[10px] text-gray-600 whitespace-pre-wrap line-clamp-3">{v.content}</div>
                  <div className="text-[9px] text-gray-400 font-mono mt-1">{v.digest.slice(0, 20)}…</div>
                </div>
              ))}
            </div>
          ) : selected ? (
            <EditForm scope={selected.scope} scopeId={selected.scope_id || ''} title={title} content={content} saving={saving}
              onScope={setScope} onScopeId={setScopeId} onTitle={setTitle} onContent={setContent} onSave={save} editMode />
          ) : (
            <EditForm scope={scope} scopeId={scopeId} title={title} content={content} saving={saving}
              onScope={setScope} onScopeId={setScopeId} onTitle={setTitle} onContent={setContent} onSave={save} editMode={false} />
          )}
          {!showVersions && (
            <ModalFooter>
              <button className="btn-sm btn-secondary" onClick={() => { setSelected(null); setShowVersions(false) }}>취소</button>
              <button className="btn-sm btn-primary" disabled={saving} onClick={save}>{saving ? '저장 중…' : '저장 → 다음 요청부터 적용'}</button>
            </ModalFooter>
          )}
        </Modal>
      )}
    </div>
  )
}

function EditForm(props: {
  scope: string; scopeId: string; title: string; content: string; saving: boolean
  onScope: (s: string) => void; onScopeId: (s: string) => void; onTitle: (s: string) => void; onContent: (s: string) => void
  onSave: () => void; editMode: boolean
}) {
  const { scope, scopeId, title, content, onScope, onScopeId, onTitle, onContent, editMode } = props
  return (
    <div className="space-y-3">
      {!editMode && (
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="text-xs text-gray-500">적용 범위</label>
            <select className="input text-xs" value={scope} onChange={(e) => onScope(e.target.value)}>
              <option value="org">조직</option>
              <option value="team">팀</option>
              <option value="fleet">플릿/하네스</option>
              <option value="user">사용자</option>
            </select>
          </div>
          <div>
            <label className="text-xs text-gray-500">{scope === 'org' ? '범위 ID' : '대상 ID'}</label>
            <input className="input text-xs" placeholder={scope === 'org' ? '(조직 전체)' : 'ID'} value={scopeId}
              disabled={scope === 'org'} onChange={(e) => onScopeId(e.target.value)} />
          </div>
        </div>
      )}
      <div>
        <label className="text-xs text-gray-500">제목</label>
        <input className="input text-xs" value={title} placeholder="예: 회사 컴플라이언스 지침" onChange={(e) => onTitle(e.target.value)} />
      </div>
      <div>
        <label className="text-xs text-gray-500">관리 지침 (최대 8,000자 · 정적 텍스트만)</label>
        <textarea className="input text-xs font-mono" rows={6} value={content}
          placeholder="추가할 시스템 프롬프트 지침…&#10;동적 변수({{…}}/${…}), 비밀 값, 핵심 지침 무시 표현은 저장할 수 없습니다." onChange={(e) => onContent(e.target.value)} />
      </div>
      <p className="text-[10px] text-gray-400">저장 시 검증 → 새 버전 → 다이제스트 → 서명 → 배포 → 감사가 자동으로 수행되며 다음 요청부터 적용됩니다.</p>
    </div>
  )
}
