import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'

import client from '@/api/client'
import type { SMLReadiness } from '@/types'

type SettingsStatusResponse = {
  sml_readiness?: SMLReadiness
}

interface SMLReadinessContextValue {
  readiness: SMLReadiness | null
  loading: boolean
  refresh: (force?: boolean) => Promise<void>
}

const SMLReadinessContext = createContext<SMLReadinessContextValue>({
  readiness: null,
  loading: false,
  refresh: async () => undefined,
})

export function SMLReadinessProvider({ children }: { children: React.ReactNode }) {
  const [readiness, setReadiness] = useState<SMLReadiness | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async (force = false) => {
    setLoading(true)
    try {
      const path = force ? '/api/settings/status?refresh_sml=1' : '/api/settings/status'
      const res = await client.get<SettingsStatusResponse>(path)
      setReadiness(res.data.sml_readiness ?? null)
    } catch {
      setReadiness({
        configured: false,
        ready: false,
        status: 'unreachable',
        message: 'ตรวจสถานะ SML ไม่สำเร็จ กรุณาตรวจ backend หรือ network',
        checked_at: new Date().toISOString(),
      })
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    let alive = true
    const run = async (force = false) => {
      if (!alive) return
      await refresh(force)
    }
    void run()
    const t = window.setInterval(() => {
      void run()
    }, 60_000)
    return () => {
      alive = false
      window.clearInterval(t)
    }
  }, [refresh])

  const value = useMemo(() => ({ readiness, loading, refresh }), [readiness, loading, refresh])
  return <SMLReadinessContext.Provider value={value}>{children}</SMLReadinessContext.Provider>
}

export function useSMLReadiness() {
  return useContext(SMLReadinessContext)
}
