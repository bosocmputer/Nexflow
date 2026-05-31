import { useAuthStore } from '../store/auth'

export function useAuth() {
  const { token, user, login, logout } = useAuthStore()
  return { token, user, login, logout, isAuthenticated: !!token }
}
