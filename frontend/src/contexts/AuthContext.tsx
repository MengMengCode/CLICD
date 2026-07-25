import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react'
import { useNavigate } from 'react-router'
import api, { login as apiLogin, checkAuth, LoginResponse } from '../services/api'

interface AuthContextType {
  isAuthenticated: boolean
  isLoading: boolean
  username: string | null
  isSubUser: boolean
  containerIdentifiers: string[]
  login: (username: string, password: string) => Promise<void>
  accessCodeLogin: (code: string, password: string) => Promise<void>
  logout: () => void
  token: string | null
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [isLoading, setIsLoading] = useState(true)
  const [username, setUsername] = useState<string | null>(null)
  const [isSubUser, setIsSubUser] = useState(false)
  const [containerIdentifiers, setContainerIdentifiers] = useState<string[]>([])
  const [token, setToken] = useState<string | null>(null)
  const navigate = useNavigate()

  const saveAuth = (t: string, u: string, sub: boolean, ids: string[]) => {
    localStorage.setItem('clicd_token', t)
    localStorage.setItem('clicd_username', u)
    setToken(t)
    setUsername(u)
    setIsSubUser(sub)
    setContainerIdentifiers(ids)
    setIsAuthenticated(true)
  }

  useEffect(() => {
    const savedToken = localStorage.getItem('clicd_token')
    const savedUsername = localStorage.getItem('clicd_username')
    if (savedToken) {
      const payload = decodeTokenPayload(savedToken)
      const nextUsername = payload?.username || payload?.sub_user || savedUsername || null
      const nextContainerIdentifiers = Array.isArray(payload?.container_uuids) ? payload.container_uuids : []

      setToken(savedToken)
      setUsername(nextUsername)
      setIsSubUser(!!payload?.sub_user)
      setContainerIdentifiers(nextContainerIdentifiers)
      checkAuth()
        .then(() => {
          setIsAuthenticated(true)
        })
        .catch(() => {
          localStorage.removeItem('clicd_token')
          localStorage.removeItem('clicd_username')
          setToken(null)
          setUsername(null)
          setIsSubUser(false)
          setContainerIdentifiers([])
        })
        .finally(() => setIsLoading(false))
    } else {
      setIsLoading(false)
    }
  }, [navigate])

  const login = async (user: string, password: string) => {
    try {
      const response = await apiLogin(user, password)
      const data = response.data.data as LoginResponse
      saveAuth(data.token, data.username, false, [])
      navigate('/')
    } catch (adminError) {
      try {
        const res = await api.post('/sub-user/login', { username: user, password })
        const data = res.data.data as { token: string; username: string; container_uuids: string[] }
        saveAuth(data.token, data.username, true, data.container_uuids || [])
        const first = data.container_uuids?.[0]
        navigate(first ? `/container/${encodeURIComponent(first)}` : '/containers')
      } catch {
        throw adminError
      }
    }
  }

  const accessCodeLogin = async (code: string, password: string) => {
    const res = await api.post('/sub-user/access', { code, password })
    const data = res.data.data as { token: string; username: string; container_uuids: string[] }
    saveAuth(data.token, data.username, true, data.container_uuids || [])
    const first = data.container_uuids?.[0]
    navigate(first ? `/container/${encodeURIComponent(first)}` : '/containers')
  }

  const logout = () => {
    localStorage.removeItem('clicd_token')
    localStorage.removeItem('clicd_username')
    setToken(null)
    setUsername(null)
    setIsSubUser(false)
    setContainerIdentifiers([])
    setIsAuthenticated(false)
    navigate('/login')
  }

  return (
    <AuthContext.Provider value={{ isAuthenticated, isLoading, username, isSubUser, containerIdentifiers, login, accessCodeLogin, logout, token }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}

type TokenPayload = {
  username?: string
  sub_user?: string
  container_uuids?: string[]
}

function decodeTokenPayload(token: string): TokenPayload | null {
  try {
    const payload = token.split('.')[1]
    if (!payload) return null
    const normalized = payload.replace(/-/g, '+').replace(/_/g, '/')
    const padded = normalized.padEnd(normalized.length + ((4 - (normalized.length % 4)) % 4), '=')
    const json = decodeURIComponent(
      atob(padded)
        .split('')
        .map((char) => `%${(`00${char.charCodeAt(0).toString(16)}`).slice(-2)}`)
        .join('')
    )
    return JSON.parse(json) as TokenPayload
  } catch {
    return null
  }
}

function subUserTargetPath(containerIdentifiers: string[]) {
  const firstContainer = containerIdentifiers[0]
  return firstContainer ? `/container/${encodeURIComponent(firstContainer)}` : '/containers'
}
