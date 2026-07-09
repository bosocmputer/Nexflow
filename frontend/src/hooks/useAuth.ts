import { useAuthStore } from '../store/auth'

export function useAuth() {
  const { token, user, login, setUser, logout } = useAuthStore()
  return { token, user, login, setUser, logout, isAuthenticated: !!token }
}
