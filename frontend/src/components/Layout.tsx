import { useEffect, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Toaster } from '@/components/ui/sonner'
import Sidebar from '@/components/layout/Sidebar'
import Topbar from '@/components/layout/Topbar'
import { EmailPollProgressBar } from '@/components/EmailPollProgressBar'
import { SMLReadinessBanner } from '@/components/SMLReadinessBanner'
import { CommandPalette } from '@/components/CommandPalette'
import { BreadcrumbProvider } from '@/lib/breadcrumbs'
import { useEventsStore } from '@/lib/events-store'
import { useChordHotkeys, useHotkeys } from '@/hooks/useHotkeys'
import { useAuth } from '@/hooks/useAuth'
import { SMLReadinessProvider } from '@/hooks/useSMLReadiness'
import { ENABLE_SHOPEE_EXCEL } from '@/lib/featureFlags'

// Routes that want a full-height, no-padding viewport (chat / inbox style).
// On these routes the page handles its own scroll regions; the Layout
// removes the default padded wrapper so the page can fill 100% of the area
// under the topbar.
const FULL_HEIGHT_ROUTES = ['/messages']
const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

export default function Layout() {
  const navigate = useNavigate()
  const location = useLocation()
  const [paletteOpen, setPaletteOpen] = useState(false)
  const { logout } = useAuth()

  // Open the SSE connection once per authenticated session. The store
  // multiplexes one EventSource across the whole app — child components
  // only need to call useChatEvents to subscribe to specific event types.
  // Disconnect on unmount (logout / hard refresh).
  const connectEvents = useEventsStore((s) => s.connect)
  const disconnectEvents = useEventsStore((s) => s.disconnect)
  useEffect(() => {
    if (PHASE < 2) return
    connectEvents()
    return () => disconnectEvents()
  }, [connectEvents, disconnectEvents])

  const isFullHeight = FULL_HEIGHT_ROUTES.some((p) => location.pathname.startsWith(p))
  const isBillDetail = /^\/(bills|sales-orders|sale-invoices)\/[^/]+/.test(location.pathname)

  useHotkeys([
    {
      key: 'k',
      mod: true,
      preventDefault: true,
      description: 'เปิด Command Palette',
      action: () => setPaletteOpen((o) => !o),
    },
  ])

  useChordHotkeys({
    'g d': () => navigate('/dashboard'),
    'g b': () => navigate('/bills'),
    ...(PHASE >= 2
      ? {
          'g i': () => navigate('/import'),
          ...(ENABLE_SHOPEE_EXCEL ? { 'g s': () => navigate('/import/shopee') } : {}),
        }
      : {}),
    'g m': () => navigate('/mappings'),
    'g l': () => navigate('/logs'),
    'g c': () => navigate('/settings/catalog'),
    'g x': () => {
      logout()
      navigate('/login')
    },
  })

  return (
    <BreadcrumbProvider>
      <SMLReadinessProvider>
        <div className="flex h-screen w-full overflow-hidden bg-background text-foreground">
          <Sidebar />
          <div className="flex min-w-0 flex-1 flex-col">
            <Topbar onOpenPalette={() => setPaletteOpen(true)} />
            <EmailPollProgressBar />
            <SMLReadinessBanner />
            {isFullHeight ? (
              // Full-height pages (chat, inbox) handle their own scroll regions.
              // No padding, no max-width — the page fills the viewport under topbar.
              <main className="flex min-h-0 flex-1 flex-col overflow-hidden">
                <Outlet />
              </main>
            ) : (
              <main className="flex-1 overflow-y-auto bg-[radial-gradient(circle_at_top_left,hsl(var(--accent)/0.45),transparent_32rem)]">
                <div className={isBillDetail ? 'w-full p-4 lg:p-5' : 'mx-auto w-full max-w-[1480px] p-5 lg:p-6'}>
                  <Outlet />
                </div>
              </main>
            )}
          </div>
          <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
          <Toaster />
        </div>
      </SMLReadinessProvider>
    </BreadcrumbProvider>
  )
}
