import { createContext, useContext, useState, type ReactNode } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { api, clearSession, TOKEN_KEY, USER_KEY, type AuthResponse } from "@/lib/api"

export type AuthUser = Omit<AuthResponse, "token">

interface AuthCtx {
  user: AuthUser | null
  login: (email: string, password: string) => Promise<void>
  setSession: (r: AuthResponse) => void
  logout: () => void
}

const Ctx = createContext<AuthCtx | null>(null)

function loadUser(): AuthUser | null {
  if (!localStorage.getItem(TOKEN_KEY)) return null
  try {
    const raw = localStorage.getItem(USER_KEY)
    return raw ? (JSON.parse(raw) as AuthUser) : null
  } catch {
    return null
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const [user, setUser] = useState<AuthUser | null>(loadUser)

  const setSession = (r: AuthResponse) => {
    const { token, ...u } = r
    localStorage.setItem(TOKEN_KEY, token)
    localStorage.setItem(USER_KEY, JSON.stringify(u))
    qc.clear()
    setUser(u)
  }

  const login = async (email: string, password: string) => {
    setSession(await api<AuthResponse>("/auth/login", { body: { email, password } }))
  }

  const logout = () => {
    clearSession()
    qc.clear()
    setUser(null)
  }

  return <Ctx.Provider value={{ user, login, setSession, logout }}>{children}</Ctx.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  const c = useContext(Ctx)
  if (!c) throw new Error("useAuth must be used inside AuthProvider")
  return c
}