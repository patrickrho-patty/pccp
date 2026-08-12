import { createContext, useContext, useState, useEffect, ReactNode } from 'react'

export type ConsoleProfile = 'patty_ops' | 'customer' | 'portal'

interface AuthState {
  isAuthenticated: boolean
  loading: boolean
  role: string
  email: string
  orgId: string
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
  profile: 'customer',
  login: () => {},
  logout: () => {},
  setProfile: () => {},
})

// Decode JWT payload (without verification — verification happens server-side)
function decodeJWT(token: string): { role: string; email: string; org_id: string } {
  try {
    const payload = token.split('.')[1]
    const padded = payload + '='.repeat((4 - payload.length % 4) % 4)
    const decoded = JSON.parse(atob(padded))
    return { role: decoded.role || '', email: decoded.email || '', org_id: decoded.org_id || '' }
  } catch {
    return { role: '', email: '', org_id: '' }
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [loading, setLoading] = useState(true)
  const [role, setRole] = useState('')
  const [email, setEmail] = useState('')
  const [orgId, setOrgId] = useState('')
  const [profile, setProfileState] = useState<ConsoleProfile>('customer')

  useEffect(() => {
    const token = localStorage.getItem('pccp_token')
    if (token) {
      const claims = decodeJWT(token)
      setIsAuthenticated(true)
      setRole(claims.role)
      setEmail(claims.email)
      setOrgId(claims.org_id)

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

  const login = (token: string) => {
    localStorage.setItem('pccp_token', token)
    const claims = decodeJWT(token)
    setIsAuthenticated(true)
    setRole(claims.role)
    setEmail(claims.email)
    setOrgId(claims.org_id)

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
    localStorage.removeItem('pccp_token')
    localStorage.removeItem('pccp_profile')
    setIsAuthenticated(false)
    setRole('')
    setEmail('')
    setOrgId('')
  }

  const setProfile = (p: ConsoleProfile) => {
    setProfileState(p)
    localStorage.setItem('pccp_profile', p)
  }

  return (
    <AuthContext.Provider value={{ isAuthenticated, loading, role, email, orgId, profile, login, logout, setProfile }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}
