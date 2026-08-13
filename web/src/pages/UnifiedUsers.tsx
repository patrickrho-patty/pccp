import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useAuth, ConsoleProfile } from '../hooks/useAuth'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'

// ─── Profile-specific configurations ─────────────────────────
type ColumnDef = { key: string; label: string; render?: (v: any, row: any) => React.ReactNode; className?: string }
type FilterDef = FilterConfig
type FormField = { key: string; label: string; type: 'text' | 'select' | 'date' | 'checkbox'; options?: { value: string; label: string }[]; required?: boolean }
type DetailSection = { title: string; en: string; fields: { key: string; label: string; render?: (v: any, row: any) => React.ReactNode }[] }

interface ProfileConfig {
  title: string
  titleEn: string
  icon: string
  apiEndpoint: string
  searchFields: string[]
  filters: FilterDef
  columns: ColumnDef[]
  formFields: FormField[]
  detailSections: DetailSection[]
  stats: (data: any[]) => { label: string; value: number; color: string }[]
}

// ─── Patty Ops (Public Subscribers) ──────────────────────────
const pattyOpsConfig: ProfileConfig = {
  title: '구독자 관리',
  titleEn: 'Subscriber Management',
  icon: '👥',
  apiEndpoint: '/api/public/accounts',
  searchFields: ['email', 'display_name', 'display_name_ko', 'oauth_provider'],
  filters: {
    searchFields: ['email', 'display_name', 'oauth_provider'],
    searchPlaceholder: '이메일, 이름으로 검색...',
    dropdowns: [
      { key: 'subscription_status', label: '구독', options: [
        { value: 'active', label: '활성' }, { value: 'grace', label: '미납' },
        { value: 'past_due', label: '연체' }, { value: 'cancelled', label: '취소' },
        { value: 'expired', label: '만료' },
      ]},
      { key: 'subscription_plan', label: '플랜', options: [
        { value: 'free', label: 'Free' }, { value: 'developer', label: 'Developer' },
        { value: 'pro', label: 'Pro' }, { value: 'team', label: 'Team' },
      ]},
    ],
  },
  columns: [
    { key: '_name', label: '구독자', render: (_, r) => (
      <td className="py-3">
        <div className="font-medium text-sm">{r.display_name_ko || r.display_name || r.email}</div>
        <div className="text-xs text-gray-400">{r.email}</div>
      </td>
    )},
    { key: 'subscription_plan', label: '플랜', render: (v) => <td className="py-3"><span className="badge-gray">{v || 'free'}</span></td> },
    { key: 'subscription_status', label: '상태', render: (v) => {
      const m: Record<string,string> = { active:'badge-green', grace:'badge-yellow', past_due:'badge-orange', cancelled:'badge-gray', expired:'badge-red' }
      const l: Record<string,string> = { active:'활성', grace:'미납', past_due:'연체', cancelled:'취소', expired:'만료', none:'미가입' }
      return <td className="py-3"><span className={m[v] || 'badge-gray'}>{l[v] || v}</span></td>
    }},
    { key: 'oauth_provider', label: '로그인', render: (v) => <td className="py-3 text-xs text-gray-500">{v || 'email'}</td> },
    { key: 'locale', label: '지역', render: (v) => <td className="py-3 text-xs text-gray-500">{v || 'ko-KR'}</td> },
    { key: 'created_at', label: '가입일', render: (v) => <td className="py-3 text-xs text-gray-400">{v?.slice(0,10) || '-'}</td> },
  ],
  formFields: [
    { key: 'email', label: '이메일 · Email', type: 'text', required: true },
    { key: 'display_name', label: '이름 · Name', type: 'text' },
    { key: 'subscription_plan', label: '플랜 · Plan', type: 'select', options: [
      { value: 'free', label: 'Free' }, { value: 'developer', label: 'Developer' },
      { value: 'pro', label: 'Pro' }, { value: 'team', label: 'Team' },
    ]},
  ],
  detailSections: [
    { title: '계정 정보', en: 'Account Info', fields: [
      { key: 'email', label: '이메일' },
      { key: 'display_name', label: '이름' },
      { key: 'subscription_plan', label: '플랜' },
      { key: 'subscription_status', label: '구독 상태' },
      { key: 'subscription_expiry', label: '결제 만료', render: (v) => v?.slice(0,10) || '-' },
      { key: 'oauth_provider', label: '로그인 방식' },
      { key: 'locale', label: '로케일' },
      { key: 'timezone', label: '시간대' },
      { key: 'created_at', label: '가입일', render: (v) => v?.slice(0,10) || '-' },
    ]},
    { title: '리스크 상태', en: 'Risk States', fields: [
      { key: 'account_integrity_state', label: '계정 무결성' },
      { key: 'trust_safety_state', label: '신뢰·안전' },
      { key: 'platform_security_state', label: '플랫폼 보안' },
      { key: 'capacity_state', label: '용량' },
    ]},
    { title: '할당량', en: 'Quotas', fields: [
      { key: 'max_harnesses', label: '최대 하네스' },
      { key: 'max_active_harnesses', label: '동시 하네스' },
      { key: 'normal_work_slots', label: '일반 워크슬롯' },
      { key: 'heavy_work_slots', label: '헤비 워크슬롯' },
    ]},
  ],
  stats: (data) => [
    { label: '총 가입자', value: data.length, color: 'text-blue-600' },
    { label: '활성', value: data.filter(a => a.subscription_status === 'active').length, color: 'text-green-600' },
    { label: '유료', value: data.filter(a => ['developer','pro','team'].includes(a.subscription_plan)).length, color: 'text-purple-600' },
    { label: '미납', value: data.filter(a => a.subscription_status === 'grace').length, color: 'text-yellow-600' },
    { label: '위험', value: data.filter(a => a.account_integrity_state !== 'normal').length, color: 'text-red-600' },
    { label: '신규(30일)', value: data.filter(a => a.created_at && Date.now() - new Date(a.created_at).getTime() < 30*86400000).length, color: 'text-indigo-600' },
  ],
}

// ─── Enterprise / Government ─────────────────────────────────
const enterpriseConfig: ProfileConfig = {
  title: '사용자 관리',
  titleEn: 'User Management',
  icon: '◉',
  apiEndpoint: '/api/users',
  searchFields: ['name', 'name_ko', 'email', 'employee_id', 'title_ko'],
  filters: {
    searchFields: ['name', 'name_ko', 'email', 'employee_id'],
    searchPlaceholder: '이름, 이메일, 사번으로 검색...',
    dropdowns: [
      { key: 'status', label: '상태', options: [
        { value: 'active', label: '활성' }, { value: 'suspended', label: '정지' },
        { value: 'offboarded', label: '퇴사' },
      ]},
      { key: 'auth_method', label: '인증', options: [
        { value: 'oidc', label: 'OIDC' }, { value: 'saml', label: 'SAML' },
        { value: 'ldap', label: 'LDAP' }, { value: 'local', label: 'Local' },
      ]},
      { key: 'business_unit_id', label: '부서', options: [
        { value: 'dev', label: '개발팀' }, { value: 'qa', label: 'QA팀' },
        { value: 'devops', label: '데브옵스' }, { value: 'security', label: '보안팀' },
        { value: 'data', label: '데이터팀' }, { value: 'exec', label: '경영진' },
      ]},
    ],
  },
  columns: [
    { key: '_name', label: '사용자', render: (_, r) => (
      <td className="py-3">
        <div className="font-medium text-sm">{r.name_ko || r.name}</div>
        <div className="text-xs text-gray-400">{r.email}</div>
      </td>
    )},
    { key: 'employee_id', label: '사번', render: (v) => <td className="py-3 text-sm font-mono">{v || '-'}</td> },
    { key: 'business_unit_id', label: '부서', render: (v) => {
      const m: Record<string,string> = { dev:'개발팀', qa:'QA팀', devops:'데브옵스', security:'보안팀', data:'데이터', infra:'인프라', exec:'경영진' }
      return <td className="py-3 text-xs text-gray-600">{m[v] || v || '-'}</td>
    }},
    { key: 'title_ko', label: '직책', render: (v, r) => <td className="py-3 text-xs">{v || r.title || '-'}</td> },
    { key: 'auth_method', label: '인증', render: (v) => <td className="py-3"><span className="badge-gray">{v}</span></td> },
    { key: 'status', label: '상태', render: (v) => {
      const m: Record<string,string> = { active:'badge-green', suspended:'badge-yellow', offboarded:'badge-gray' }
      return <td className="py-3"><span className={m[v] || 'badge-gray'}>{v}</span></td>
    }},
    { key: 'last_login_at', label: '최근 로그인', render: (v) => <td className="py-3 text-xs text-gray-400">{v?.slice(0,16) || '-'}</td> },
  ],
  formFields: [
    { key: 'email', label: '이메일 · Email', type: 'text', required: true },
    { key: 'name', label: '이름 · Name', type: 'text', required: true },
    { key: 'name_ko', label: '한글명 · Korean Name', type: 'text' },
    { key: 'employee_id', label: '사번 · Employee Number', type: 'text' },
    { key: 'title_ko', label: '직책 · Title', type: 'text' },
    { key: 'business_unit_id', label: '부서 · Department', type: 'select', options: [
      { value: 'dev', label: '개발팀 · Development' },
      { value: 'qa', label: 'QA팀 · Quality Assurance' },
      { value: 'devops', label: '데브옵스 · DevOps' },
      { value: 'security', label: '보안팀 · Security' },
      { value: 'data', label: '데이터팀 · Data' },
      { value: 'exec', label: '경영진 · Executive' },
    ]},
    { key: 'auth_method', label: '인증 방식 · Auth Method', type: 'select', options: [
      { value: 'local', label: 'Local' }, { value: 'oidc', label: 'OIDC' },
      { value: 'saml', label: 'SAML' }, { value: 'ldap', label: 'LDAP' },
    ]},
  ],
  detailSections: [
    { title: '사용자 정보', en: 'User Info', fields: [
      { key: 'name_ko', label: '한글명' },
      { key: 'name', label: '이름' },
      { key: 'email', label: '이메일' },
      { key: 'employee_id', label: '사번' },
      { key: 'title_ko', label: '직책' },
      { key: 'business_unit_id', label: '부서' },
      { key: 'auth_method', label: '인증 방식' },
      { key: 'external_id', label: 'SSO ID' },
      { key: 'mfa_enrolled', label: 'MFA', render: (v) => v ? '✅' : '❌' },
      { key: 'locale', label: '로케일' },
      { key: 'timezone', label: '시간대' },
      { key: 'created_at', label: '등록일', render: (v) => v?.slice(0,10) || '-' },
      { key: 'last_login_at', label: '최근 로그인', render: (v) => v?.slice(0,16) || '-' },
    ]},
    { title: '계정 상태', en: 'Account Status', fields: [
      { key: 'status', label: '상태' },
      { key: 'offboarding_date', label: '퇴사일', render: (v) => v || '-' },
      { key: 'contractor_info', label: '계약 정보', render: (v) => v ? '외부 인력' : '-' },
    ]},
  ],
  stats: (data) => [
    { label: '전체 사용자', value: data.length, color: 'text-blue-600' },
    { label: '활성', value: data.filter(u => u.status === 'active').length, color: 'text-green-600' },
    { label: '정지', value: data.filter(u => u.status === 'suspended').length, color: 'text-yellow-600' },
    { label: '퇴사', value: data.filter(u => u.status === 'offboarded').length, color: 'text-gray-500' },
    { label: 'SSO', value: data.filter(u => u.auth_method !== 'local').length, color: 'text-purple-600' },
    { label: 'MFA', value: data.filter(u => u.mfa_enrolled).length, color: 'text-indigo-600' },
  ],
}

function getConfig(profile: ConsoleProfile): ProfileConfig {
  return profile === 'patty_ops' ? pattyOpsConfig : enterpriseConfig
}

// ─── Main Component ──────────────────────────────────────────
export default function UnifiedUsers() {
  const { profile } = useAuth()
  const config = getConfig(profile)
  const [data, setData] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })
  const [page, setPage] = useState(1)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [form, setForm] = useState<Record<string, any>>({})
  const pageSize = 20

  const load = () => {
    fetch(config.apiEndpoint, { headers: authHeaders() })
      .then(r => r.json())
      .then(d => { setData(Array.isArray(d) ? d : []); setLoading(false) })
      .catch(() => { setData([]); setLoading(false) })
  }
  useEffect(() => { load() }, [config.apiEndpoint])

  const filtered = useFilteredData(data, filters, config.filters)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)
  const selected = data.find(d => d.id === selectedId)
  const statCards = config.stats(data)

  const handleSave = async () => {
    const method = editingId ? 'PUT' : 'POST'
    const url = editingId ? `${config.apiEndpoint}/${editingId}` : config.apiEndpoint
    try {
      await fetch(url, { method, headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify(form) })
      setShowForm(false); setEditingId(null); setForm({}); load()
    } catch { alert('저장 실패') }
  }

  const startEdit = (row: any) => {
    setEditingId(row.id)
    const f: Record<string, any> = {}
    config.formFields.forEach(field => { f[field.key] = row[field.key] || '' })
    setForm(f); setShowForm(true)
  }

  const handleDelete = async (id: string) => {
    if (!confirm('삭제하시겠습니까?')) return
    try { await fetch(`${config.apiEndpoint}/${id}`, { method: 'DELETE', headers: authHeaders() }); load() } catch {}
  }

  const toggleSelect = (id: string) => {
    setSelectedIds(prev => { const n = new Set(prev); n.has(id) ? n.delete(id) : n.add(id); return n })
  }

  const handleBulkAction = async (action: string) => {
    if (!confirm(`${selectedIds.size}명을 ${action} 처리하시겠습니까?`)) return
    for (const id of selectedIds) {
      try {
        if (action === '정지' && profile === 'patty_ops') {
          await fetch(`${config.apiEndpoint}/${id}`, { method: 'PUT', headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ subscription_status: 'suspended' }) })
        } else if (action === '정지') {
          await fetch(`${config.apiEndpoint}/${id}`, { method: 'PUT', headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ status: 'suspended' }) })
        } else if (action === '활성화') {
          const statusKey = profile === 'patty_ops' ? 'subscription_status' : 'status'
          await fetch(`${config.apiEndpoint}/${id}`, { method: 'PUT', headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ [statusKey]: profile === 'patty_ops' ? 'active' : 'active' }) })
        }
      } catch {}
    }
    setSelectedIds(new Set()); load()
  }

  if (loading) return <div className="text-gray-500">로딩 중...</div>

  return (
    <div>
      <div className="flex justify-between items-center mb-4">
        <div>
          <h1 className="text-2xl font-bold">{config.title} <span className="text-gray-400 text-lg font-normal">{config.titleEn}</span></h1>
        </div>
        <button onClick={() => { setEditingId(null); setForm({}); setShowForm(!showForm) }} className="btn-primary text-sm">+ {profile === 'patty_ops' ? '구독자' : '사용자'} 추가</button>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-6 gap-3 mb-6">
        {statCards.map((s, i) => (
          <div key={i} className="card py-3 px-4 text-center">
            <div className={`text-2xl font-bold ${s.color}`}>{s.value}</div>
            <div className="text-xs text-gray-500">{s.label}</div>
          </div>
        ))}
      </div>

      {/* Create/Edit Form */}
      {showForm && (
        <div className="card mb-4">
          <h3 className="text-sm font-semibold mb-3">{editingId ? '수정' : '신규 등록'}</h3>
          <div className="grid grid-cols-3 gap-4">
            {config.formFields.map(field => (
              <div key={field.key}>
                <label className="label">{field.label}</label>
                {field.type === 'select' ? (
                  <select className="input" value={form[field.key] || ''} onChange={e => setForm({ ...form, [field.key]: e.target.value })}>
                    <option value="">선택</option>
                    {field.options?.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
                  </select>
                ) : field.type === 'checkbox' ? (
                  <label className="flex items-center gap-2 text-sm pt-6">
                    <input type="checkbox" checked={form[field.key] || false} onChange={e => setForm({ ...form, [field.key]: e.target.checked })} className="w-4 h-4" /> 활성화
                  </label>
                ) : (
                  <input className="input" type={field.type === 'date' ? 'date' : 'text'} value={form[field.key] || ''} onChange={e => setForm({ ...form, [field.key]: e.target.value })} disabled={!!editingId && field.key === 'email'} />
                )}
              </div>
            ))}
          </div>
          <div className="flex gap-2 mt-3">
            <button onClick={handleSave} className="btn-primary text-sm">{editingId ? '수정 저장' : '등록'}</button>
            <button onClick={() => { setShowForm(false); setEditingId(null) }} className="btn-secondary text-sm">취소</button>
          </div>
        </div>
      )}

      {/* Bulk action bar */}
      {selectedIds.size > 0 && (
        <div className="flex items-center gap-3 mb-4 p-3 bg-blue-50 rounded-lg">
          <span className="text-sm font-medium text-blue-700">{selectedIds.size}명 선택</span>
          <button onClick={() => handleBulkAction('활성화')} className="btn-sm btn-secondary">일괄 활성화</button>
          <button onClick={() => handleBulkAction('정지')} className="btn-sm btn-danger">일괄 정지</button>
          <button onClick={() => setSelectedIds(new Set())} className="btn-sm btn-secondary">선택 취소</button>
        </div>
      )}

      <FilterBar config={config.filters} onChange={setFilters} />

      <div className="grid grid-cols-12 gap-4">
        {/* Table */}
        <div className={selectedId ? 'col-span-8' : 'col-span-12'}>
          <div className="card">
            <table className="w-full">
              <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                <th className="pb-3 w-8"><input type="checkbox" onChange={(e) => { if (e.target.checked) setSelectedIds(new Set(paged.map(r => r.id))); else setSelectedIds(new Set()) }} /></th>
                {config.columns.map(c => <th key={c.key} className="pb-3">{c.label}</th>)}
                <th className="pb-3">작업</th>
              </tr></thead>
              <tbody>
                {paged.map(row => (
                  <>
                    <tr key={row.id} className={`border-b border-gray-100 last:border-0 ${selectedId === row.id ? 'bg-blue-50' : 'hover:bg-gray-50'}`}>
                      <td className="py-3" onClick={e => e.stopPropagation()}><input type="checkbox" checked={selectedIds.has(row.id)} onChange={() => toggleSelect(row.id)} /></td>
                      {config.columns.map(c => c.render ? c.render(row[c.key], row) : <td key={c.key} className="py-3 text-sm">{row[c.key]}</td>)}
                      <td className="py-3" onClick={e => e.stopPropagation()}>
                        <div className="flex gap-2">
                          <button onClick={() => setSelectedId(selectedId === row.id ? null : row.id)} className="text-xs text-blue-600 hover:underline">상세</button>
                          <button onClick={() => startEdit(row)} className="text-xs text-blue-600 hover:underline">편집</button>
                          <button onClick={() => handleDelete(row.id)} className="text-xs text-red-600 hover:underline">삭제</button>
                        </div>
                      </td>
                    </tr>
                    {/* Expandable detail */}
                    {selectedId === row.id && (
                      <tr className="bg-gray-50"><td colSpan={config.columns.length + 2} className="p-4">
                        <div className="grid grid-cols-3 gap-6">
                          {config.detailSections.map(section => (
                            <div key={section.en}>
                              <div className="text-xs font-semibold text-gray-600 mb-2">{section.title} <span className="text-gray-400 font-normal">{section.en}</span></div>
                              <div className="space-y-1 text-xs text-gray-500">
                                {section.fields.map(f => (
                                  <div key={f.key} className="flex justify-between">
                                    <span>{f.label}</span>
                                    <span className="font-medium text-gray-700">{f.render ? f.render(row[f.key], row) : (row[f.key] || '-')}</span>
                                  </div>
                                ))}
                              </div>
                            </div>
                          ))}
                        </div>
                      </td></tr>
                    )}
                  </>
                ))}
              </tbody>
            </table>
            <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />
          </div>
        </div>
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
