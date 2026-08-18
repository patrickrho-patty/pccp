const API_BASE = '';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const token = sessionStorage.getItem('pccp_token');
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options?.headers as Record<string, string>),
  };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  const resp = await fetch(`${API_BASE}${path}`, { ...options, headers });
  if (!resp.ok) {
    if (resp.status === 401 && token) {
      sessionStorage.removeItem('pccp_token')
      window.dispatchEvent(new Event('pccp-auth-expired'))
    }
    const err = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(err.error || 'API error');
  }
  return resp.json();
}

async function requestCursorPage<T>(path: string): Promise<{ data: T; nextCursor: string }> {
  const token = sessionStorage.getItem('pccp_token')
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (token) headers.Authorization = `Bearer ${token}`
  const resp = await fetch(`${API_BASE}${path}`, { headers })
  if (!resp.ok) {
    if (resp.status === 401 && token) {
      sessionStorage.removeItem('pccp_token')
      window.dispatchEvent(new Event('pccp-auth-expired'))
    }
    const err = await resp.json().catch(() => ({ error: resp.statusText }))
    throw new Error(err.error || 'API error')
  }
  return { data: await resp.json(), nextCursor: resp.headers.get('X-Next-Cursor') || '' }
}

export const api = {
  // Auth
  login: (email: string, password: string, mfaCode?: string) =>
    request<{ token: string }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password, mfa_code: mfaCode }),
    }),
  mfaSetup: () => request<{ secret: string; otpauth: string }>('/api/auth/mfa/setup', { method: 'POST' }),
  mfaVerify: (code: string) =>
    request<any>('/api/auth/mfa/verify', { method: 'POST', body: JSON.stringify({ code }) }),
  bootstrap: (email: string, password: string, orgName: string, profile?: string, policyPack?: string, demoData?: boolean) =>
    request<{ organization_id: string }>('/api/auth/bootstrap', {
      method: 'POST',
      body: JSON.stringify({ email, password, org_name: orgName, profile, policy_pack: policyPack, demo_data: demoData }),
    }),

  // SSO (§8.2). The browser selects only its organization; all IdP endpoints,
  // client identifiers, redirect URIs, and transaction state are server-owned.
  ssoOIDCAuthUrl: (organization: string) =>
    request<{ auth_url: string }>(`/api/sso/oidc/auth-url?organization=${encodeURIComponent(organization)}`),
  ssoSAMLRedirect: (organization: string) =>
    request<{ redirect_url: string }>('/api/sso/saml/redirect', {
      method: 'POST',
      body: JSON.stringify({ organization }),
    }),
  ssoSessionExchange: (code: string, provider: 'oidc' | 'saml') =>
	request<any>('/api/sso/session', { method: 'POST', body: JSON.stringify({ code, provider }) }),

  // Dashboard
  dashboard: () => request<any>('/api/dashboard'),

  // Organizations
  listOrganizations: () => request<any[]>('/api/organizations'),
  getSeatUsage: () => request<any>('/api/organizations/seats'),
  createOrganization: (data: any) =>
    request<any>('/api/organizations', { method: 'POST', body: JSON.stringify(data) }),

  // Users
  listUsers: () => request<any[]>('/api/users'),
  listUsersPaged: (query: string) =>
    request<{ data: any[]; total: number; page: number; size: number }>(`/api/users?${query}`),
  createUser: (data: any) =>
    request<any>('/api/users', { method: 'POST', body: JSON.stringify(data) }),
  updateUser: (id: string, data: any) =>
    request<any>(`/api/users/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteUser: (id: string, reason?: string) =>
    request<any>(`/api/users/${id}`, { method: 'DELETE', body: JSON.stringify({ reason }) }),
  offboardUser: (id: string, reason: string) =>
    request<any>(`/api/users/${id}/offboard`, { method: 'POST', body: JSON.stringify({ reason }) }),
  suspendUser: (id: string, reason: string) =>
    request<any>(`/api/users/${id}/suspend`, { method: 'POST', body: JSON.stringify({ reason }) }),
  resumeUser: (id: string, reason: string) =>
    request<any>(`/api/users/${id}/resume`, { method: 'POST', body: JSON.stringify({ reason }) }),
  getUser: (id: string) =>
    request<any>(`/api/users/${id}`),
  getUserAudit: (id: string) =>
    request<any[]>(`/api/users/${id}/audit`),
  getUserHarnesses: (id: string) =>
    request<any[]>(`/api/users/${id}/harnesses`),
  grantUserHarness: (id: string, harnessId: string) =>
    request<any>(`/api/users/${id}/harnesses`, { method: 'POST', body: JSON.stringify({ harness_id: harnessId }) }),
  revokeUserHarness: (id: string, harnessId: string) =>
    request<any>(`/api/users/${id}/harnesses/${harnessId}`, { method: 'DELETE' }),
  getUserUsage: (id: string, rangeParam = '30d', cursor = '', signal?: AbortSignal, summaryOnly = false) =>
    request<any>(`/api/users/${id}/usage?range=${rangeParam}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}${summaryOnly ? '&summary_only=1' : ''}`, { signal }),
  importUsersCSV: (file: File, apply: boolean) => {
    const form = new FormData()
    form.append('file', file)
    const token = sessionStorage.getItem('pccp_token')
    const headers: Record<string, string> = {}
    if (token) headers['Authorization'] = `Bearer ${token}`
    return fetch(`/api/users/import${apply ? '?apply=true' : ''}`, { method: 'POST', headers, body: form })
      .then(async r => {
        const body = await r.json().catch(() => ({ error: r.statusText }))
        if (!r.ok) throw new Error(body?.error || `CSV import failed (${r.status})`)
        return body
      })
  },
  getUserEntitlements: (id: string) =>
    request<any>(`/api/users/${id}/entitlements`),
  putUserEntitlements: (id: string, roles: any[]) =>
    request<any>(`/api/users/${id}/entitlements`, { method: 'PUT', body: JSON.stringify({ roles }) }),
  listRoles: () => request<any[]>('/api/roles'),
  getUserSSOStatus: (id: string) =>
    request<any>(`/api/users/${id}/sso-status`),
  putContractor: (id: string, profile: any) =>
    request<any>(`/api/users/${id}/contractor`, { method: 'PUT', body: JSON.stringify(profile) }),
  getHarnessAudit: (id: string) =>
    request<any[]>(`/api/harnesses/${id}/audit`),

  // Business Units (Korean org hierarchy — PRD §12.1)
  listBusinessUnits: () => request<any[]>('/api/business-units'),
  createBusinessUnit: (data: any) =>
    request<any>('/api/business-units', { method: 'POST', body: JSON.stringify(data) }),
  updateBusinessUnit: (id: string, data: any) =>
    request<any>(`/api/business-units/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteBusinessUnit: (id: string) =>
    request<any>(`/api/business-units/${id}`, { method: 'DELETE' }),

  // Unified search (00 A11)
  search: (q: string) => request<any[]>(`/api/search?q=${encodeURIComponent(q)}`),

  // Harnesses
  listHarnesses: (params?: Record<string, string>) =>
    request<any>(`/api/harnesses${params ? '?' + new URLSearchParams(params).toString() : ''}`),
  getHarness: (id: string) => request<any>(`/api/harnesses/${id}`),
  getHarnessDetail: (id: string) => request<any>(`/api/harnesses/${id}/detail`),
  harnessHeartbeat: (data: any) =>
    request<any>('/api/harnesses/heartbeat', { method: 'POST', body: JSON.stringify(data) }),
  issueEnrollmentCode: (userId: string) =>
    request<{ enrollment_code: string; expires_at: string }>(`/api/users/${userId}/enrollment-code`, { method: 'POST' }),
  enrollHarness: (data: any) =>
    request<any>('/api/harnesses/enroll', { method: 'POST', body: JSON.stringify(data) }),
  revokeHarness: (id: string, reason: string) =>
    request<any>(`/api/harnesses/${id}/revoke`, { method: 'POST', body: JSON.stringify({ reason }) }),
  quarantineHarness: (id: string) =>
    request<any>(`/api/harnesses/${id}/quarantine`, { method: 'POST' }),
  reactivateHarness: (id: string) =>
    request<any>(`/api/harnesses/${id}/reactivate`, { method: 'POST' }),

  // Projects
  listProjects: (params?: Record<string, string>) =>
    request<any>(`/api/projects${params ? '?' + new URLSearchParams(params).toString() : ''}`),
  getProject: (id: string) => request<any>(`/api/projects/${id}`),
  getProjectDetail: (id: string) => request<any>(`/api/projects/${id}/detail`),
  listProjectMembers: (id: string) => request<any[]>(`/api/projects/${id}/members`),
  addProjectMember: (id: string, data: any) =>
    request<any>(`/api/projects/${id}/members`, { method: 'POST', body: JSON.stringify(data) }),
  removeProjectMember: (id: string, userId: string) =>
    request<any>(`/api/projects/${id}/members/${userId}`, { method: 'DELETE' }),
  restoreProject: (id: string) =>
    request<any>(`/api/projects/${id}/restore`, { method: 'POST' }),
  projectArchiveImpact: (id: string) => request<any>(`/api/projects/${id}/archive-impact`),
  projectUsage: (id: string, rangeParam = '30d', cursor = '', signal?: AbortSignal) => request<any>(`/api/projects/${id}/usage?range=${rangeParam}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`, { signal }),
  projectChangeRequests: (id: string) => request<any[]>(`/api/projects/${id}/change-requests`),
  decideChangeRequest: (id: string, approve: boolean, reason?: string) =>
    request<any>(`/api/change-requests/${id}/decide`, { method: 'POST', body: JSON.stringify({ approve, reason }) }),
  bindProjectPolicyPack: (id: string, policyPackId: string) =>
    request<any>(`/api/projects/${id}/policy-pack`, { method: 'POST', body: JSON.stringify({ policy_pack_id: policyPackId }) }),
  createProject: (data: any) =>
    request<any>('/api/projects', { method: 'POST', body: JSON.stringify(data) }),
  updateProject: (id: string, data: any) =>
    request<any>(`/api/projects/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteProject: (id: string) =>
    request<any>(`/api/projects/${id}`, { method: 'DELETE' }),

  // Repositories
  listRepositories: (params?: Record<string, string>) =>
    request<any>(`/api/repositories${params ? '?' + new URLSearchParams(params).toString() : ''}`),
  getRepository: (id: string) => request<any>(`/api/repositories/${id}`),
  syncRepository: (id: string) =>
    request<any>(`/api/repositories/${id}/sync`, { method: 'POST' }),
  repoTree: (id: string, path?: string) =>
    request<any[]>(`/api/repositories/${id}/tree${path ? '?path=' + encodeURIComponent(path) : ''}`),
  repoFile: (id: string, path: string) =>
    request<any>(`/api/repositories/${id}/file?path=${encodeURIComponent(path)}`),
  repoBaselines: (id: string) => request<any[]>(`/api/repositories/${id}/baselines`),
  repoBranches: (id: string) => request<any[]>(`/api/repositories/${id}/branches`),
  repoWebhookInfo: (id: string) => request<any>(`/api/repositories/${id}/webhook`),
  rotateWebhookSecret: (id: string) =>
    request<any>(`/api/repositories/${id}/webhook/rotate`, { method: 'POST' }),
  registerRepository: (data: any) =>
    request<any>('/api/repositories', { method: 'POST', body: JSON.stringify(data) }),
  updateRepository: (id: string, data: any) =>
    request<any>(`/api/repositories/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  createRepository: (data: any) =>
    request<any>('/api/repositories', { method: 'POST', body: JSON.stringify(data) }),
  deleteRepository: (id: string) =>
    request<any>(`/api/repositories/${id}`, { method: 'DELETE' }),

  // Sessions
  listSessions: () => request<any[]>('/api/sessions'),
  listSessionsPaged: (query: string) =>
    request<{ data: any[]; total: number; page: number; size: number }>(`/api/sessions?${query}`),
  liveSessions: (limit = 50, filters?: Record<string, string>) => {
	const params = new URLSearchParams({ limit: String(limit) })
	Object.entries(filters || {}).forEach(([key, value]) => { if (value) params.set(key, value) })
	return request<any>(`/api/sessions/live?${params.toString()}`)
  },
  liveStreamTicket: () => request<{ stream_url: string; expires_at: string; transcript_visible: boolean }>('/api/realtime/ticket', { method: 'POST' }),
  openSession: (data: any) =>
    request<any>('/api/sessions', { method: 'POST', body: JSON.stringify(data) }),
  sessionAction: (id: string, action: string) =>
    request<any>(`/api/sessions/${id}/${action}`, { method: 'POST' }),
  bulkSessions: (ids: string[], action: string, reason: string) =>
	request<any>('/api/sessions/bulk', { method: 'POST', body: JSON.stringify({ ids, action, reason }) }),
  getSessionDetail: (id: string) => request<any>(`/api/sessions/${id}/detail`),
  getSessionUsage: (id: string, rangeParam = '30d', cursor = '', signal?: AbortSignal) => request<any>(`/api/sessions/${id}/usage?range=${rangeParam}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`, { signal }),
  getSessionDecisions: (id: string) => request<any>(`/api/sessions/${id}/decisions`),
  getSessionReplay: (id: string) => request<any>(`/api/sessions/${id}/replay`),
  getSessionVisibility: (id: string) => request<any>(`/api/sessions/${id}/visibility`),
  // Server-side paginated sessions (web/01): total count + page slice.
  listSessionsPage: (page: number, size = 25, search = '') =>
    request<{ data: any[]; total: number; page: number; size: number }>(
      `/api/sessions?page=${page}&size=${size}${search ? `&search=${encodeURIComponent(search)}` : ''}`
    ),
  closeSession: (id: string) =>
    request<any>(`/api/sessions/${id}/close`, { method: 'POST' }),
  pauseSession: (id: string) =>
    request<any>(`/api/sessions/${id}/pause`, { method: 'POST' }),
  resumeSession: (id: string) =>
    request<any>(`/api/sessions/${id}/resume`, { method: 'POST' }),
  getProvenanceReceipts: (id: string) => request<any[]>(`/api/sessions/${id}/provenance/receipts`),
  provenanceSearch: (q: string) => request<any>(`/api/provenance/search?q=${encodeURIComponent(q)}`),
  getProvenance: (id: string) =>
    request<any>(`/api/sessions/${id}/provenance`),

  // Models
  listModels: () => request<any[]>('/api/models'),
	getModel: (id: string) => request<any>(`/api/models/${encodeURIComponent(id)}`),
	modelRecallImpact: (id: string) => request<any>(`/api/models/${encodeURIComponent(id)}/recall-impact`),
  registerModel: (data: any) =>
    request<any>('/api/models', { method: 'POST', body: JSON.stringify(data) }),
  publishModel: (id: string) =>
    request<any>(`/api/models/${id}/publish`, { method: 'POST' }),
  recallModel: (id: string) =>
    request<any>(`/api/models/${id}/recall`, { method: 'POST' }),

  // Endpoints
  listEndpoints: () => request<any[]>('/api/endpoints'),
  enrollEndpoint: (data: any) =>
    request<any>('/api/endpoints/enroll', { method: 'POST', body: JSON.stringify(data) }),
  issueEndpointLease: (id: string) =>
    request<any>(`/api/endpoints/${id}/lease`, { method: 'POST' }),

  // Policy
  listEpochs: () => request<any[]>('/api/policy/epochs'),
  createEpoch: (data: any) =>
    request<any>('/api/policy/epochs', { method: 'POST', body: JSON.stringify(data) }),
  getEpochDiff: (epochId: string, against: string) =>
    request<any>(`/api/policy/epochs/${epochId}/diff?against=${encodeURIComponent(against)}`),
  listEpochAcks: (epochId: string) => request<any>(`/api/policy/epochs/${epochId}/acks`),
  ackEpoch: (epochId: string) =>
    request<any>(`/api/policy/epochs/${epochId}/ack`, { method: 'POST' }),
  requireEpochAck: (epochId: string) =>
    request<any>(`/api/policy/epochs/${epochId}/require-ack`, { method: 'POST' }),
  getEffectivePolicy: (scope?: { project_id?: string; repo_id?: string }) =>
    request<any>(`/api/policy/effective${scope?.project_id ? `?project_id=${scope.project_id}` : ''}${scope?.repo_id ? `${scope?.project_id ? '&' : '?'}repo_id=${scope.repo_id}` : ''}`),
  listPolicyPacks: () => request<any[]>('/api/policy/packs'),
  createPolicyPack: (data: any) =>
    request<any>('/api/policy/packs', { method: 'POST', body: JSON.stringify(data) }),
  importPolicyPack: (data: any) =>
    request<any>('/api/policy/packs/import', { method: 'POST', body: JSON.stringify(data) }),
  exportPolicyPack: (id: string) => request<any>(`/api/policy/packs/${id}/export`),
  assignPolicyPack: (id: string, scope: string, scopeId: string) =>
    request<any>(`/api/policy/packs/${id}/assign`, { method: 'POST', body: JSON.stringify({ scope, scope_id: scopeId }) }),
  listPolicyTemplates: () => request<any[]>('/api/policy/templates'),
  savePolicyTemplate: (data: any) =>
    request<any>('/api/policy/templates', { method: 'POST', body: JSON.stringify(data) }),
  deletePolicyTemplate: (id: string) =>
    request<any>(`/api/policy/templates/${id}`, { method: 'DELETE' }),
  listPolicyExceptions: () => request<any[]>('/api/policy/exceptions'),
  createPolicyException: (data: any) =>
    request<any>('/api/policy/exceptions', { method: 'POST', body: JSON.stringify(data) }),
  decidePolicyException: (id: string, approve: boolean, decidedBy?: string) =>
    request<any>(`/api/policy/exceptions/${id}/decide`, { method: 'POST', body: JSON.stringify({ approve, decided_by: decidedBy }) }),

  // Policy Rules (governance rules — PRD §13)
  listPolicyRules: () => request<any[]>('/api/policy/rules'),
  createPolicyRule: (data: any) =>
    request<any>('/api/policy/rules', { method: 'POST', body: JSON.stringify(data) }),
  deletePolicyRule: (id: string) =>
    request<any>(`/api/policy/rules/${id}`, { method: 'DELETE' }),
  approvePolicyRule: (id: string) =>
    request<any>(`/api/policy/rules/${id}/approve`, { method: 'POST' }),
  rejectPolicyRule: (id: string) =>
    request<any>(`/api/policy/rules/${id}/reject`, { method: 'POST' }),
  bulkPolicyRules: (ids: string[], enabled: boolean) =>
    request<any>('/api/policy/rules/bulk', { method: 'POST', body: JSON.stringify({ ids, enabled }) }),

  // Audit
  listAudit: () => request<any[]>('/api/audit'),
  listAuditPaged: (query: string) =>
    request<{ data: any[]; total: number; page: number; size: number }>(`/api/audit?${query}`),
  verifyAuditChain: () => request<any>('/api/audit/verify'),
  listAuditHolds: () => request<any[]>('/api/audit/holds'),
  placeAuditHold: (resourceType: string, resourceId: string, reason: string) =>
    request<any>('/api/audit/holds', { method: 'POST', body: JSON.stringify({ resource_type: resourceType, resource_id: resourceId, reason }) }),
  liftAuditHold: (id: string, reason: string) =>
    request<any>(`/api/audit/holds/${id}`, { method: 'DELETE', body: JSON.stringify({ reason }) }),
  auditEvidenceBundle: (ids: string[]) =>
    request<any>('/api/audit/evidence-bundle', { method: 'POST', body: JSON.stringify({ ids }) }),
  auditSIEMConfig: () => request<any>('/api/audit/siem'),
  putAuditSIEMConfig: (webhook: string, secret: string) =>
    request<any>('/api/audit/siem', { method: 'PUT', body: JSON.stringify({ webhook, secret }) }),

  // Analytics (web/16)
  usageExtended: (rangeParam: string, cursor = '', signal?: AbortSignal, summaryOnly = false) => {
    const query = new URLSearchParams({ range: rangeParam })
    if (cursor) query.set('cursor', cursor)
    if (summaryOnly) query.set('summary_only', '1')
    return request<any>(`/api/analytics/usage-extended?${query.toString()}`, { signal })
  },
  usageExportTicket: (rangeParam: string, windowStart?: string, windowEnd?: string, snapshotAt?: string) => {
    const query = new URLSearchParams({ range: rangeParam })
    if (windowStart && windowEnd && snapshotAt) {
      query.set('window_start', windowStart)
      query.set('window_end', windowEnd)
      query.set('snapshot_at', snapshotAt)
    }
    return request<{ download_url: string; expires_at: string }>(`/api/analytics/usage-export-ticket?${query.toString()}`, { method: 'POST' })
  },
  usageBreakdown: (rangeParam: string) =>
    request<any>(`/api/analytics/usage-breakdown?range=${rangeParam}`),

  // Model infra (web/18)
  assignModelRing: (id: string, releaseRing: string) =>
    request<any>(`/api/models/${id}/ring`, { method: 'PUT', body: JSON.stringify({ release_ring: releaseRing }) }),

  // Code explorer (web/19)
  codeExplorerSpans: (query: string) => request<any>(`/api/code-explorer/spans?${query}`),
  codeExplorerAttribution: (repoId: string) =>
    request<any[]>(`/api/code-explorer/attribution${repoId ? '?repository=' + encodeURIComponent(repoId) : ''}`),
  codeExplorerBlast: (repoId: string, filePath: string) =>
    request<any>(`/api/code-explorer/blast?repository=${encodeURIComponent(repoId)}&file=${encodeURIComponent(filePath)}`),
  // Incidents (Security SOC)
  simulatePolicy: (ruleIds: string[]) =>
    request<any>('/api/incidents/simulate-policy', { method: 'POST', body: JSON.stringify({ rule_ids: ruleIds }) }),


  // Security
  securityCheck: (text: string) =>
    request<any>('/api/security/check', { method: 'POST', body: JSON.stringify({ text }) }),
  securityRules: () => request<any[]>('/api/security/rules'),
  securityRuleOverrides: (scopeLevel: string, scopeId: string) =>
    request<any[]>(`/api/security/rules/overrides?scope_level=${encodeURIComponent(scopeLevel)}&scope_id=${encodeURIComponent(scopeId)}`),
  setSecurityRuleOverride: (payload: { scope_level: string; scope_id: string; rule_id: string; enabled?: boolean; severity?: string; action?: string }) =>
    request('/api/security/rules/overrides', { method: 'PUT', body: JSON.stringify(payload) }),
  deleteSecurityRuleOverride: (payload: { scope_level: string; scope_id: string; rule_id: string }) =>
    request('/api/security/rules/overrides', { method: 'DELETE', body: JSON.stringify(payload) }),
  securityFindings: (params?: Record<string, string>) =>
    request<any>(`/api/security/findings${params ? '?' + new URLSearchParams(params).toString() : ''}`),
  securityFindingDetail: (id: string) => request<any>(`/api/security/findings/${id}`),
  updateSecurityFinding: (id: string, data: any) =>
    request<any>(`/api/security/findings/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  bulkSecurityFindings: (ids: string[], status: string) =>
    request<any>('/api/security/findings/bulk', { method: 'POST', body: JSON.stringify({ ids, status }) }),
  suppressFinding: (id: string, data: any) =>
    request<any>(`/api/security/findings/${id}/suppress`, { method: 'POST', body: JSON.stringify(data) }),
  reopenFinding: (id: string) =>
    request<any>(`/api/security/findings/${id}/reopen`, { method: 'POST' }),
  scanSession: (sessionId: string) =>
    request<any>('/api/security/scan-session', { method: 'POST', body: JSON.stringify({ session_id: sessionId }) }),
  lockdownImpact: (scope?: string, projectId?: string) =>
    request<any>(`/api/security/lockdown-impact?scope=${scope || 'org'}${projectId ? `&project_id=${encodeURIComponent(projectId)}` : ''}`),
  securityLockdown: (data: any) =>
    request<any>('/api/security/lockdown', { method: 'POST', body: JSON.stringify(data) }),
  securityAlerts: (cursor = '') =>
	requestCursorPage<any[]>(`/api/security/alerts?limit=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  createSecurityAlert: (data: any) =>
    request<any>('/api/security/alerts', { method: 'POST', body: JSON.stringify(data) }),
  deleteSecurityAlert: (id: string) =>
    request<any>(`/api/security/alerts/${id}`, { method: 'DELETE' }),
  testSecurityAlert: (id: string) =>
    request<any>(`/api/security/alerts/${id}/test`, { method: 'POST' }),
  rotateSecurityAlert: (id: string, target: string, enable?: boolean) =>
    request<any>(`/api/security/alerts/${id}/rotate`, { method: 'POST', body: JSON.stringify({ target, ...(enable == null ? {} : { enable }) }) }),
  disableSecurityAlert: (id: string) =>
    request<any>(`/api/security/alerts/${id}/disable`, { method: 'POST' }),
  securityLexicon: () => request<any>('/api/security/lexicon'),
  updateSecurityLexicon: (data: any) =>
    request<any>('/api/security/lexicon', { method: 'PUT', body: JSON.stringify(data) }),

  // Fleet
  fleetInventory: () => request<any[]>('/api/fleet/inventory'),
  listFleetInventory: (query: string) =>
    request<any>(`/api/fleet/inventory${query ? '?' + query : ''}`),
  fleetAction: (data: any) =>
    request<any>('/api/fleet/actions', { method: 'POST', body: JSON.stringify(data) }),
  fleetBulkAction: (harnessIds: string[], action: string, reason: string, idempotencyKey: string) =>
	request<any>('/api/fleet/actions/bulk', { method: 'POST', headers: { 'Idempotency-Key': idempotencyKey }, body: JSON.stringify({ harness_ids: harnessIds, action, reason }) }),
  fleetActionHistory: (harnessId?: string) =>
    request<any[]>(`/api/fleet/actions${harnessId ? '?harness_id=' + encodeURIComponent(harnessId) : ''}`),
  fleetFreeze: (reason: string, reasonKo: string, affectedRepos: string[]) =>
    request<any>('/api/fleet/freeze', { method: 'POST', body: JSON.stringify({ reason, reason_ko: reasonKo, affected_repos: affectedRepos }) }),
  fleetForceVersion: (minVersion: string, releaseRing: string, deadline: string, reason: string) =>
    request<any>('/api/fleet/force-version', { method: 'POST', body: JSON.stringify({ min_version: minVersion, release_ring: releaseRing, deadline, reason }) }),
  fleetApprovals: () => request<any[]>('/api/fleet/approvals'),
  fleetImpact: (action: string, scope: string, projectId?: string) =>
    request<any>(`/api/fleet/impact?action=${encodeURIComponent(action)}&scope=${encodeURIComponent(scope)}${projectId ? `&project_id=${encodeURIComponent(projectId)}` : ''}`),
  fleetStatus: () => request<any>('/api/fleet/status'),

  // Communications (web/13)
  listConversations: () => request<any[]>('/api/communications/conversations'),
  createConversation: (data: any) =>
    request<any>('/api/communications/conversations', { method: 'POST', body: JSON.stringify(data) }),
  openDM: (userId: string) =>
    request<any>('/api/communications/conversations/dm', { method: 'POST', body: JSON.stringify({ user_id: userId }) }),
  listMessages: (convId: string) => request<any[]>(`/api/communications/conversations/${convId}/messages`),
  sendMessage: (convId: string, data: any) =>
    request<any>(`/api/communications/conversations/${convId}/messages`, { method: 'POST', body: JSON.stringify(data) }),
  editMessage: (id: string, content: string) =>
    request<any>(`/api/communications/messages/${id}`, { method: 'PUT', body: JSON.stringify({ content }) }),
  deleteMessage: (id: string, deletedBy: string) =>
    request<any>(`/api/communications/messages/${id}`, { method: 'DELETE', body: JSON.stringify({ deleted_by: deletedBy }) }),
  reactMessage: (id: string, emoji: string, userId: string) =>
    request<any>(`/api/communications/messages/${id}/react`, { method: 'POST', body: JSON.stringify({ emoji, user_id: userId }) }),
  readMessage: (id: string, userId: string) =>
    request<any>(`/api/communications/messages/${id}/read`, { method: 'POST', body: JSON.stringify({ user_id: userId }) }),
  linkMessage: (id: string, sessionId: string, exchangeId: string) =>
    request<any>(`/api/communications/messages/${id}/link`, { method: 'POST', body: JSON.stringify({ session_id: sessionId, exchange_id: exchangeId }) }),
  getPresence: () => request<any[]>('/api/communications/presence'),
  listBroadcasts: () => request<any[]>('/api/communications/broadcasts'),
  sendBroadcast: (data: any) =>
    request<any>('/api/communications/broadcasts', { method: 'POST', body: JSON.stringify(data) }),
  broadcastAcks: (id: string) => request<any>(`/api/communications/broadcasts/${id}/acks`),
  ackBroadcast: (id: string, userId: string) =>
    request<any>(`/api/communications/broadcasts/${id}/ack`, { method: 'POST', body: JSON.stringify({ user_id: userId }) }),
  listFileTransfers: () => request<any[]>('/api/communications/file-transfers'),
  createFileTransfer: (data: any) =>
    request<any>('/api/communications/file-transfers', { method: 'POST', body: JSON.stringify(data) }),
  uploadFileTransfer: (id: string, file: File) => {
    const form = new FormData()
    form.append('file', file)
    const token = sessionStorage.getItem('pccp_token')
    const headers: Record<string, string> = {}
    if (token) headers['Authorization'] = `Bearer ${token}`
    return fetch(`/api/communications/file-transfers/${id}/content`, { method: 'POST', headers, body: form })
      .then(r => r.json())
  },
  // Tools (web/14)
  listTools: () => request<any[]>('/api/tools'),
  registerTool: (data: any) =>
    request<any>('/api/tools', { method: 'POST', body: JSON.stringify(data) }),
  updateTool: (id: string, data: any) =>
    request<any>(`/api/tools/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  seedTools: () => request<{ added: number }>('/api/tools/seed-defaults', { method: 'POST' }),
  toolPresets: () => request<any>('/api/tools/presets'),
  toolApprovals: () => request<any[]>('/api/tools/approvals'),
  decideToolApproval: (id: string, decision: string, reviewerId: string) =>
    request<any>(`/api/tools/approvals/${id}/decide`, { method: 'POST', body: JSON.stringify({ decision, reviewer_id: reviewerId }) }),
  getProjectToolAllowlist: (projectId: string) =>
    request<any[]>(`/api/projects/${projectId}/tool-allowlist`),
  // Sandboxes (web/15)
  listSandboxes: () => request<any[]>('/api/sandboxes'),
  createSandbox: (data: any) =>
    request<any>('/api/sandboxes', { method: 'POST', body: JSON.stringify(data) }),
  destroySandbox: (id: string) =>
    request<any>(`/api/sandboxes/${id}/destroy`, { method: 'POST' }),
  snapshotSandbox: (id: string) =>
    request<any>(`/api/sandboxes/${id}/snapshot`, { method: 'POST' }),
  getSandboxImageAllowlist: () => request<{ images: string[]; enforced: boolean }>('/api/sandboxes/image-allowlist'),
  setSandboxImageAllowlist: (images: string[]) =>
    request<any>('/api/sandboxes/image-allowlist', { method: 'PUT', body: JSON.stringify({ images }) }),

  setProjectToolAllowlist: (projectId: string, toolNames: string[]) =>
    request<any>(`/api/projects/${projectId}/tool-allowlist`, { method: 'PUT', body: JSON.stringify({ tool_names: toolNames, granted_by: 'admin' }) }),

  transitionFileTransfer: (id: string, action: string) =>
    request<any>(`/api/communications/file-transfers/${id}/transition`, { method: 'POST', body: JSON.stringify({ action }) }),

  // SCM
  repoHeatmap: () => request<any[]>('/api/scm/heatmaps'),

  // Impact
  analyzeChange: (data: any) =>
    request<any>('/api/impact/analyze', { method: 'POST', body: JSON.stringify(data) }),

  // Context
  evaluateContext: (data: any) =>
    request<any>('/api/context/evaluate', { method: 'POST', body: JSON.stringify(data) }),

  // Sandbox
  queryEvents: (type?: string) =>
    request<any[]>(`/api/events${type ? '?type=' + type : ''}`),

  // MCP
  mcpServers: () => request<any[]>('/api/mcp/servers'),
  mcpEvaluate: (data: any) =>
    request<any>('/api/mcp/evaluate', { method: 'POST', body: JSON.stringify(data) }),

  // Network
  networkEvaluate: (data: any) =>
    request<any>('/api/network/evaluate', { method: 'POST', body: JSON.stringify(data) }),

  // Commands
  commandEvaluate: (data: any) =>
    request<any>('/api/commands/evaluate', { method: 'POST', body: JSON.stringify(data) }),

  // Billing
  entitlement: () => request<any>('/api/billing/entitlement'),

  // Incidents
  listIncidents: () => request<any[]>('/api/incidents'),
  createIncident: (data: any) =>
    request<any>('/api/incidents', { method: 'POST', body: JSON.stringify(data) }),
  containIncident: (data: any) =>
    request<any>('/api/incidents/contain', { method: 'POST', body: JSON.stringify(data) }),
  resolveIncident: (id: string, resolution: string) =>
    request<any>(`/api/incidents/${id}/resolve`, { method: 'POST', body: JSON.stringify({ resolution }) }),

  // Korean
  governanceBrief: () => request<any>('/api/korean/governance-brief'),
  skillsMatrix: () => request<any[]>('/api/korean/skills-matrix'),

  // Privacy
  legalHold: () => request<any>('/api/privacy/legal-hold'),

  // Reports
  generateReport: (type: string) =>
    request<any>('/api/reports/generate', { method: 'POST', body: JSON.stringify({ type }) }),

  // Telemetry
  telemetrySnapshot: () => request<any>('/api/telemetry/snapshot'),

  // Tools
  listToolApprovals: () => request<any[]>('/api/tools/approvals'),

  // Attestation
  attestLevels: (level: string) => request<any>(`/api/attestation/levels/${level}`),

  // Compliance
  complianceCerts: () => request<any[]>('/api/compliance/certifications'),
  complianceMeta: () => request<any[]>('/api/compliance/meta'),
  complianceAssess: (cert: string, scope: string, level: string) =>
    request<any>('/api/compliance/assess', { method: 'POST', body: JSON.stringify({ certification: cert, scope, level }) }),
  complianceHistory: () => request<any[]>('/api/compliance/history'),
  complianceEvidence: (cert: string, control?: string) =>
    request<any[]>(`/api/compliance/evidence?certification=${encodeURIComponent(cert)}${control ? '&control=' + encodeURIComponent(control) : ''}`),
  complianceEvidenceAdd: (cert: string, controlId: string, title: string, description: string, source: string, reference: string) =>
    request<any>('/api/compliance/evidence', { method: 'POST', body: JSON.stringify({ certification: cert, control_id: controlId, title, description, source, reference }) }),
  complianceEvidenceDelete: (id: string) =>
    request<any>(`/api/compliance/evidence/${id}`, { method: 'DELETE' }),
  complianceRemediations: (cert: string, status?: string) =>
    request<any[]>(`/api/compliance/remediations?certification=${encodeURIComponent(cert)}${status ? '&status=' + status : ''}`),
  complianceRemediationAdd: (cert: string, controlId: string, owner: string, dueDate: string, sla: string, notes: string) =>
    request<any>('/api/compliance/remediations', { method: 'POST', body: JSON.stringify({ certification: cert, control_id: controlId, owner, due_date: dueDate, sla, notes }) }),
  complianceRemediationUpdate: (id: string, status: string, owner: string, dueDate: string, notes: string) =>
    request<any>(`/api/compliance/remediations/${id}`, { method: 'PUT', body: JSON.stringify({ status, owner, due_date: dueDate, notes }) }),
  complianceBulkRemediate: (cert: string, owner: string) =>
    request<any>('/api/compliance/remediate-all', { method: 'POST', body: JSON.stringify({ certification: cert, owner }) }),

  // Connectors
  connectorsTypes: () => request<any[]>('/api/connectors/types'),
  listConnectors: () => request<any[]>('/api/connectors'),

  // GPU
  gpuEndpoints: () => request<any[]>('/api/gpu/endpoints'),
  gpuModels: () => request<any[]>('/api/gpu/models'),

  // Keys
  generateKey: (domain: string) =>
    request<any>('/api/keys/generate', { method: 'POST', body: JSON.stringify({ domain, validity_hours: 2160 }) }),

  // MCP Marketplace
  mcpMarketSearch: (q?: string) => request<any[]>(`/api/mcp-market?q=${q || ''}`),
  mcpMarketSeed: () =>
    request<any>('/api/mcp-market/seed', { method: 'POST' }),

  // Sovereign
  sovereignPendingUpdates: () => request<any[]>('/api/sovereign/updates/pending'),
  sovereignTimeProof: () => request<any>('/api/sovereign/time-proof'),

  // Realtime
  realtimeStatus: () => request<any>('/api/realtime/status'),

  // v2 Model Catalog
  catalogModels: () => request<any[]>('/api/catalog/models'),
  catalogSeed: () => request<any>('/api/catalog/seed', { method: 'POST' }),
  catalogEpoch: (accountId?: string) => request<any>(`/api/catalog/epoch${accountId ? '?account_id=' + accountId : ''}`),
  catalogWithdraw: (id: string) => request<any>(`/api/catalog/${id}/withdraw`, { method: 'POST' }),
  catalogAnnounce: (id: string) => request<any>(`/api/catalog/${id}/announce`, { method: 'POST' }),

  // v2 Public Cloud
  publicAccounts: () => request<any[]>('/api/public/accounts'),
  publicCreateAccount: (data: any) => request<any>('/api/public/accounts', { method: 'POST', body: JSON.stringify(data) }),
  publicGetAccount: (id: string) => request<any>(`/api/public/accounts/${id}`),
  publicCreateSub: (id: string, plan: string) => request<any>(`/api/public/accounts/${id}/subscription`, { method: 'POST', body: JSON.stringify({ plan }) }),
  publicLease: (id: string) => request<any>(`/api/public/accounts/${id}/lease`),
  publicSlots: (id: string) => request<any>(`/api/public/accounts/${id}/slots`),
};
