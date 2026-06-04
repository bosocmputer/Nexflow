import axios from 'axios'
import { useAuthStore } from '../store/auth'

const client = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '',
  timeout: 30000,
})

// Attach JWT token to every request
client.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// On 401, clear auth and redirect to login — skip if already on login page
client.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401 && !window.location.pathname.startsWith('/login')) {
      useAuthStore.getState().logout()
      window.location.href = '/login'
    }
    return Promise.reject(err)
  },
)

export default client
