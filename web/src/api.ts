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

  // Harnesses
  listHarnesses: () => request<any[]>('/api/harnesses'),
  enrollHarness: (data: any) =>
    request<any>('/api/harnesses/enroll', { method: 'POST', body: JSON.stringify(data) }),
  revokeHarness: (id: string, reason: string) =>
    request<any>(`/api/harnesses/${id}/revoke`, { method: 'POST', body: JSON.stringify({ reason }) }),

  // Projects
  listProjects: () => request<any[]>('/api/projects'),
  createProject: (data: any) =>
    request<any>('/api/projects', { method: 'POST', body: JSON.stringify(data) }),

  // Repositories
  listRepositories: () => request<any[]>('/api/repositories'),
  registerRepository: (data: any) =>
    request<any>('/api/repositories', { method: 'POST', body: JSON.stringify(data) }),

  // Sessions
  listSessions: () => request<any[]>('/api/sessions'),
  openSession: (data: any) =>
    request<any>('/api/sessions', { method: 'POST', body: JSON.stringify(data) }),
  closeSession: (id: string) =>
    request<any>(`/api/sessions/${id}/close`, { method: 'POST' }),
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

  // Audit
  listAudit: () => request<any[]>('/api/audit'),
};
