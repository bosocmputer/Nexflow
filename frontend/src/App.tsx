import { lazy, Suspense } from 'react'
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { useAuthStore } from './store/auth'
import Layout from './components/Layout'
import { ENABLE_LAZADA_EXCEL, ENABLE_LINE_MYSHOP, ENABLE_SALES_ORDERS, ENABLE_SHOPEE_EXCEL, ENABLE_SHOPEE_REALTIME_OPS, ENABLE_TIKTOK_EXCEL } from './lib/featureFlags'
import { canViewMenu, firstVisibleNavPath } from './lib/navigation'

const Login = lazy(() => import('./pages/Login'))
const Dashboard = lazy(() => import('./pages/Dashboard'))
const NextStepMarketplace = lazy(() => import('./pages/NextStepMarketplace'))
const Bills = lazy(() => import('./pages/Bills'))
const BillDetail = lazy(() => import('./pages/BillDetail'))
const ShopeeImport = lazy(() => import('./pages/ShopeeImport'))
const ShopeeConnections = lazy(() => import('./pages/ShopeeConnections'))
const ShopeeOperations = lazy(() => import('./pages/ShopeeOperations'))
const ShopeeSettlement = lazy(() => import('./pages/ShopeeSettlement'))
const LazadaImport = lazy(() => import('./pages/LazadaImport'))
const TikTokImport = lazy(() => import('./pages/TikTokImport'))
const OldDataSettings = lazy(() => import('./pages/OldDataSettings'))
const Logs = lazy(() => import('./pages/Logs'))
const BulkSendJobs = lazy(() => import('./pages/BulkSendJobs'))
const CatalogSettings = lazy(() => import('./pages/CatalogSettings'))
const ChannelDefaults = lazy(() => import('./pages/ChannelDefaults'))
const InstanceSettings = lazy(() => import('./pages/InstanceSettings'))
const UserSettings = lazy(() => import('./pages/UserSettings'))
const MenuPermissions = lazy(() => import('./pages/MenuPermissions'))
const LineNotifications = lazy(() => import('./pages/LineNotifications'))
const LineMyShopSettings = lazy(() => import('./pages/LineMyShopSettings'))
const Showcase = lazy(() => import('./pages/Showcase'))
const SetupCenter = lazy(() => import('./pages/SetupCenter'))
const ShopeeStock = lazy(() => import('./pages/ShopeeStock'))
const MarketplaceAliases = lazy(() => import('./pages/MarketplaceAliases'))

const routeFallback = <div className="p-6 text-sm text-muted-foreground">กำลังโหลด...</div>

function RequireAuth({ children }: { children: React.ReactNode }) {
  const token = useAuthStore((s) => s.token)
  if (!token) return <Navigate to="/login" replace />
  return <>{children}</>
}

function RequireAdmin({ children }: { children: React.ReactNode }) {
  const user = useAuthStore((s) => s.user)
  if (user?.role !== 'admin') return <Navigate to="/dashboard" replace />
  return <>{children}</>
}

function RequireMenu({ menuKey, children }: { menuKey: string; children: React.ReactNode }) {
  const user = useAuthStore((s) => s.user)
  const location = useLocation()
  if (canViewMenu(user, menuKey)) return <>{children}</>
  const fallback = firstVisibleNavPath(user)
  if (fallback && fallback !== location.pathname) return <Navigate to={fallback} replace />
  return <NoMenuAccess />
}

function IndexRedirect() {
  const user = useAuthStore((s) => s.user)
  return <Navigate to={firstVisibleNavPath(user)} replace />
}

function NoMenuAccess() {
  return (
    <div className="p-6">
      <div className="rounded-lg border bg-card p-6">
        <h1 className="text-lg font-semibold">ไม่มีสิทธิ์เข้าเมนูนี้</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          ติดต่อผู้ดูแลระบบเพื่อเปิดสิทธิ์เมนูที่ต้องใช้งาน
        </p>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <Suspense fallback={routeFallback}>
        <Routes>
        {import.meta.env.DEV && (
          <Route path="/dev/showcase" element={<Showcase />} />
        )}
        <Route path="/login" element={<Login />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <Layout />
            </RequireAuth>
          }
        >
          <Route index element={<IndexRedirect />} />
          <Route path="setup" element={<RequireMenu menuKey="setup"><SetupCenter /></RequireMenu>} />
          <Route path="dashboard" element={<RequireMenu menuKey="dashboard"><Dashboard /></RequireMenu>} />
          <Route path="nextstep-marketplace" element={<RequireMenu menuKey="nextstep_marketplace"><NextStepMarketplace /></RequireMenu>} />
          <Route path="bills" element={<Navigate to="/dashboard" replace />} />
          <Route path="sales-orders" element={ENABLE_SALES_ORDERS ? <RequireMenu menuKey="sales_orders"><Bills mode="sales-order" /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="sale-invoices" element={ENABLE_SALES_ORDERS ? <RequireMenu menuKey="sale_invoices"><Bills mode="sale-invoice" /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="bills/:id" element={<Navigate to="/dashboard" replace />} />
          <Route path="sales-orders/:id" element={ENABLE_SALES_ORDERS ? <RequireMenu menuKey="sales_orders"><BillDetail /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="sale-invoices/:id" element={ENABLE_SALES_ORDERS ? <RequireMenu menuKey="sale_invoices"><BillDetail /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="messages" element={<Navigate to="/dashboard" replace />} />
          <Route path="import" element={<Navigate to="/import/shopee" replace />} />
          <Route path="import/shopee" element={ENABLE_SHOPEE_EXCEL ? <RequireMenu menuKey="import_shopee"><ShopeeImport /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="shopee-operations" element={ENABLE_SHOPEE_REALTIME_OPS ? <RequireMenu menuKey="shopee_operations"><ShopeeOperations /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="shopee-settlements" element={ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS ? <RequireMenu menuKey="shopee_settlements"><ShopeeSettlement /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="import/lazada" element={ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS ? <RequireMenu menuKey="import_lazada"><LazadaImport /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="import/tiktok" element={ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS ? <RequireMenu menuKey="import_tiktok"><TikTokImport /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="mappings" element={<Navigate to="/marketplace-aliases" replace />} />
          <Route path="marketplace-aliases" element={ENABLE_SALES_ORDERS ? <RequireMenu menuKey="marketplace_aliases"><MarketplaceAliases /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="settings/old-data" element={<RequireMenu menuKey="old_data"><OldDataSettings /></RequireMenu>} />
          <Route path="settings" element={<Navigate to="/settings/instance" replace />} />
          <Route path="logs" element={<RequireMenu menuKey="logs"><Logs /></RequireMenu>} />
          <Route path="bulk-send-jobs" element={<RequireMenu menuKey="bulk_send_jobs"><BulkSendJobs /></RequireMenu>} />
          <Route path="settings/catalog" element={<RequireMenu menuKey="catalog"><CatalogSettings /></RequireMenu>} />
          <Route path="settings/email" element={<Navigate to="/dashboard" replace />} />
          <Route path="settings/shopee-connections" element={<RequireAdmin><RequireMenu menuKey="shopee_connections"><ShopeeConnections /></RequireMenu></RequireAdmin>} />
          <Route path="settings/shopee-stock" element={<RequireMenu menuKey="shopee_stock"><ShopeeStock /></RequireMenu>} />
          <Route path="settings/line-myshop" element={ENABLE_LINE_MYSHOP ? <RequireAdmin><RequireMenu menuKey="line_myshop"><LineMyShopSettings /></RequireMenu></RequireAdmin> : <Navigate to="/settings/instance" replace />} />
          <Route path="settings/channels" element={<RequireMenu menuKey="channel_defaults"><ChannelDefaults /></RequireMenu>} />
          <Route path="settings/instance" element={<RequireAdmin><RequireMenu menuKey="instance_settings"><InstanceSettings /></RequireMenu></RequireAdmin>} />
          <Route path="settings/ai-usage" element={<Navigate to="/dashboard" replace />} />
          <Route path="settings/users" element={<RequireAdmin><RequireMenu menuKey="settings_users"><UserSettings /></RequireMenu></RequireAdmin>} />
          <Route path="settings/menu-permissions" element={<RequireAdmin><RequireMenu menuKey="settings_menu_permissions"><MenuPermissions /></RequireMenu></RequireAdmin>} />
          <Route path="settings/line-notifications" element={<RequireAdmin><RequireMenu menuKey="line_notifications"><LineNotifications /></RequireMenu></RequireAdmin>} />
          <Route path="settings/line-oa" element={<Navigate to="/settings/line-notifications" replace />} />
          <Route path="settings/quick-replies" element={<Navigate to="/dashboard" replace />} />
          <Route path="settings/chat-tags" element={<Navigate to="/dashboard" replace />} />
        </Route>
        </Routes>
      </Suspense>
    </BrowserRouter>
  )
}
