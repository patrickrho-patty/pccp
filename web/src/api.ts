const API_BASE = '';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const token = localStorage.getItem('pccp_token');
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options?.headers as Record<string, string>),
  };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  const resp = await fetch(`${API_BASE}${path}`, { ...options, headers });
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(err.error || 'API error');
  }
  return resp.json();
}

export const api = {
  // Auth
  login: (email: string, password: string) =>
    request<{ token: string }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    }),
  bootstrap: (email: string, password: string, orgName: string) =>
    request<{ organization_id: string }>('/api/auth/bootstrap', {
      method: 'POST',
      body: JSON.stringify({ email, password, org_name: orgName }),
    }),

  // Dashboard
  dashboard: () => request<any>('/api/dashboard'),

  // Organizations
  listOrganizations: () => request<any[]>('/api/organizations'),
  createOrganization: (data: any) =>
    request<any>('/api/organizations', { method: 'POST', body: JSON.stringify(data) }),

  // Users
  listUsers: () => request<any[]>('/api/users'),
  createUser: (data: any) =>
    request<any>('/api/users', { method: 'POST', body: JSON.stringify(data) }),
  updateUser: (id: string, data: any) =>
    request<any>(`/api/users/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteUser: (id: string) =>
    request<any>(`/api/users/${id}`, { method: 'DELETE' }),
  getUser: (id: string) =>
    request<any>(`/api/users/${id}`),
  getUserAudit: (id: string) =>
    request<any[]>(`/api/users/${id}/audit`),
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

  // Harnesses
  listHarnesses: () => request<any[]>('/api/harnesses'),
  enrollHarness: (data: any) =>
    request<any>('/api/harnesses/enroll', { method: 'POST', body: JSON.stringify(data) }),
  revokeHarness: (id: string, reason: string) =>
    request<any>(`/api/harnesses/${id}/revoke`, { method: 'POST', body: JSON.stringify({ reason }) }),
  quarantineHarness: (id: string) =>
    request<any>(`/api/harnesses/${id}/quarantine`, { method: 'POST' }),
  reactivateHarness: (id: string) =>
    request<any>(`/api/harnesses/${id}/reactivate`, { method: 'POST' }),

  // Projects
  listProjects: () => request<any[]>('/api/projects'),
  createProject: (data: any) =>
    request<any>('/api/projects', { method: 'POST', body: JSON.stringify(data) }),
  updateProject: (id: string, data: any) =>
    request<any>(`/api/projects/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteProject: (id: string) =>
    request<any>(`/api/projects/${id}`, { method: 'DELETE' }),

  // Repositories
  listRepositories: () => request<any[]>('/api/repositories'),
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
  openSession: (data: any) =>
    request<any>('/api/sessions', { method: 'POST', body: JSON.stringify(data) }),
  closeSession: (id: string) =>
    request<any>(`/api/sessions/${id}/close`, { method: 'POST' }),
  pauseSession: (id: string) =>
    request<any>(`/api/sessions/${id}/pause`, { method: 'POST' }),
  resumeSession: (id: string) =>
    request<any>(`/api/sessions/${id}/resume`, { method: 'POST' }),
  getProvenance: (id: string) =>
    request<any>(`/api/sessions/${id}/provenance`),

  // Models
  listModels: () => request<any[]>('/api/models'),
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

  // Policy Rules (governance rules — PRD §13)
  listPolicyRules: () => request<any[]>('/api/policy/rules'),
  createPolicyRule: (data: any) =>
    request<any>('/api/policy/rules', { method: 'POST', body: JSON.stringify(data) }),
  deletePolicyRule: (id: string) =>
    request<any>(`/api/policy/rules/${id}`, { method: 'DELETE' }),

  // Audit
  listAudit: () => request<any[]>('/api/audit'),


  // Security
  securityCheck: (text: string) =>
    request<any>('/api/security/check', { method: 'POST', body: JSON.stringify({ text }) }),
  securityRules: () => request<any[]>('/api/security/rules'),

  // Fleet
  fleetInventory: () => request<any[]>('/api/fleet/inventory'),
  fleetAction: (data: any) =>
    request<any>('/api/fleet/actions', { method: 'POST', body: JSON.stringify(data) }),

  // SCM
  repoHeatmap: () => request<any[]>('/api/scm/heatmaps'),

  // Impact
  analyzeChange: (data: any) =>
    request<any>('/api/impact/analyze', { method: 'POST', body: JSON.stringify(data) }),

  // Context
  evaluateContext: (data: any) =>
    request<any>('/api/context/evaluate', { method: 'POST', body: JSON.stringify(data) }),

  // Sandbox
  listSandboxes: () => request<any[]>('/api/sandboxes'),
  createSandbox: (data: any) =>
    request<any>('/api/sandboxes', { method: 'POST', body: JSON.stringify(data) }),

  // Events
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
  listTools: () => request<any[]>('/api/tools'),
  seedTools: () =>
    request<any>('/api/tools/seed-defaults', { method: 'POST', body: JSON.stringify({}) }),
  listToolApprovals: () => request<any[]>('/api/tools/approvals'),

  // Attestation
  attestLevels: (level: string) => request<any>(`/api/attestation/levels/${level}`),

  // Compliance
  complianceCerts: () => request<any[]>('/api/compliance/certifications'),
  complianceAssess: (cert: string) =>
    request<any>('/api/compliance/assess', { method: 'POST', body: JSON.stringify({ certification: cert }) }),

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