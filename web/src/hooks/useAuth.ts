import { useState, useEffect } from 'react'

export function useAuth() {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const token = localStorage.getItem('pccp_token')
    setIsAuthenticated(!!token)
    setLoading(false)
  }, [])

  const login = (token: string) => {
    localStorage.setItem('pccp_token', token)
    setIsAuthenticated(true)
  }

  const logout = () => {
    localStorage.removeItem('pccp_token')
    setIsAuthenticated(false)
  }

  return { isAuthenticated, loading, login, logout }
}
