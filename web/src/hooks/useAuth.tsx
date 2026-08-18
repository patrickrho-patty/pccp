import { createContext, useContext, useState, useEffect, ReactNode } from 'react'

export type ConsoleProfile = 'patty_ops' | 'customer' | 'portal'

interface AuthState {
  isAuthenticated: boolean
  loading: boolean
  role: string
  email: string
  orgId: string
  permissions: string[]
  can: (permission: string) => boolean
  profile: ConsoleProfile
  login: (token: string) => void
  logout: () => void
  setProfile: (p: ConsoleProfile) => void
}

const AuthContext = createContext<AuthState>({
  isAuthenticated: false,
  loading: true,
  role: '',
  email: '',
  orgId: '',
  permissions: [],
  can: () => false,
  profile: 'customer',
  login: () => {},
  logout: () => {},
  setProfile: () => {},
})

// Decode JWT payload (without verification — verification happens server-side)
function decodeJWT(token: string): { role: string; email: string; org_id: string; permissions: string[] } {
  try {
    const payload = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')
    const padded = payload + '='.repeat((4 - payload.length % 4) % 4)
    const decoded = JSON.parse(atob(padded))
    return { role: decoded.role || '', email: decoded.email || '', org_id: decoded.org_id || '', permissions: Array.isArray(decoded.permissions) ? decoded.permissions : [] }
  } catch {
    return { role: '', email: '', org_id: '', permissions: [] }
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [loading, setLoading] = useState(true)
  const [role, setRole] = useState('')
  const [email, setEmail] = useState('')
  const [orgId, setOrgId] = useState('')
  const [permissions, setPermissions] = useState<string[]>([])
  const [profile, setProfileState] = useState<ConsoleProfile>('customer')

  useEffect(() => {
    const token = sessionStorage.getItem('pccp_token')
    if (token) {
      const claims = decodeJWT(token)
      setIsAuthenticated(true)
      setRole(claims.role)
      setEmail(claims.email)
      setOrgId(claims.org_id)
      setPermissions(claims.permissions)

      // Determine default profile from role
      const savedProfile = localStorage.getItem('pccp_profile') as ConsoleProfile | null
      if (savedProfile) {
        setProfileState(savedProfile)
      } else if (claims.role === 'super_admin' || claims.email.includes('@patty.')) {
        setProfileState('patty_ops')
      } else {
        setProfileState('customer')
      }
    }
    setLoading(false)
  }, [])

	useEffect(() => {
		const expire = () => {
			setIsAuthenticated(false)
			setRole('')
			setEmail('')
			setOrgId('')
			setPermissions([])
		}
		window.addEventListener('pccp-auth-expired', expire)
		return () => window.removeEventListener('pccp-auth-expired', expire)
	}, [])

  const login = (token: string) => {
    sessionStorage.setItem('pccp_token', token)
    const claims = decodeJWT(token)
    setIsAuthenticated(true)
    setRole(claims.role)
    setEmail(claims.email)
    setOrgId(claims.org_id)
    setPermissions(claims.permissions)

    // Auto-select profile based on role
    if (claims.role === 'super_admin' || claims.email.includes('@patty.')) {
      setProfileState('patty_ops')
      localStorage.setItem('pccp_profile', 'patty_ops')
    } else {
      setProfileState('customer')
      localStorage.setItem('pccp_profile', 'customer')
    }
  }

  const logout = () => {
    sessionStorage.removeItem('pccp_token')
    localStorage.removeItem('pccp_profile')
    setIsAuthenticated(false)
    setRole('')
    setEmail('')
    setOrgId('')
    setPermissions([])
  }

  const setProfile = (p: ConsoleProfile) => {
    setProfileState(p)
    localStorage.setItem('pccp_profile', p)
  }

  const can = (permission: string) => {
    if (['admin', 'owner', 'super_admin', 'security_admin'].includes(role)) return true
    if (['viewer', 'auditor', 'security_viewer'].includes(role) && permission === 'security.alert_endpoint.read') return true
    return permissions.includes(permission) || permissions.includes('security.alert_endpoint.*')
  }

  return (
    <AuthContext.Provider value={{ isAuthenticated, loading, role, email, orgId, permissions, can, profile, login, logout, setProfile }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}
