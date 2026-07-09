import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { useAuthStore } from './store/auth'
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import NextStepMarketplace from './pages/NextStepMarketplace'
import Bills from './pages/Bills'
import BillDetail from './pages/BillDetail'
import Import from './pages/Import'
import ShopeeImport from './pages/ShopeeImport'
import ShopeeConnections from './pages/ShopeeConnections'
import ShopeeOperations from './pages/ShopeeOperations'
import ShopeeSettlement from './pages/ShopeeSettlement'
import LazadaImport from './pages/LazadaImport'
import TikTokImport from './pages/TikTokImport'
import Mappings from './pages/Mappings'
import MarketplaceAliases from './pages/MarketplaceAliases'
import OldDataSettings from './pages/OldDataSettings'
import Logs from './pages/Logs'
import BulkSendJobs from './pages/BulkSendJobs'
import CatalogSettings from './pages/CatalogSettings'
import EmailAccounts from './pages/EmailAccounts'
import ChannelDefaults from './pages/ChannelDefaults'
import InstanceSettings from './pages/InstanceSettings'
import AIUsage from './pages/AIUsage'
import UserSettings from './pages/UserSettings'
import MenuPermissions from './pages/MenuPermissions'
import LineNotifications from './pages/LineNotifications'
import LineMyShopSettings from './pages/LineMyShopSettings'
import ChatTags from './pages/ChatTags'
import LineOA from './pages/LineOA'
import Messages from './pages/Messages'
import QuickReplies from './pages/QuickReplies'
import Showcase from './pages/Showcase'
import { ENABLE_CHAT, ENABLE_LAZADA_EXCEL, ENABLE_LINE_MYSHOP, ENABLE_SALES_ORDERS, ENABLE_SHOPEE_EXCEL, ENABLE_SHOPEE_REALTIME_OPS, ENABLE_TIKTOK_EXCEL } from './lib/featureFlags'
import SetupCenter from './pages/SetupCenter'
import { canViewMenu, firstVisibleNavPath } from './lib/navigation'

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
    <BrowserRouter>
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
          <Route path="bills" element={<RequireMenu menuKey="purchase_orders"><Bills mode="purchase-order" /></RequireMenu>} />
          <Route path="sales-orders" element={ENABLE_SALES_ORDERS ? <RequireMenu menuKey="sales_orders"><Bills mode="sales-order" /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="sale-invoices" element={ENABLE_SALES_ORDERS ? <RequireMenu menuKey="sale_invoices"><Bills mode="sale-invoice" /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="bills/:id" element={<RequireMenu menuKey="purchase_orders"><BillDetail /></RequireMenu>} />
          <Route path="sales-orders/:id" element={ENABLE_SALES_ORDERS ? <RequireMenu menuKey="sales_orders"><BillDetail /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="sale-invoices/:id" element={ENABLE_SALES_ORDERS ? <RequireMenu menuKey="sale_invoices"><BillDetail /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="messages" element={ENABLE_CHAT ? <RequireMenu menuKey="messages"><Messages /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="import" element={<RequireMenu menuKey="import_shopee"><Import /></RequireMenu>} />
          <Route path="import/shopee" element={ENABLE_SHOPEE_EXCEL ? <RequireMenu menuKey="import_shopee"><ShopeeImport /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="shopee-operations" element={ENABLE_SHOPEE_REALTIME_OPS ? <RequireMenu menuKey="shopee_operations"><ShopeeOperations /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="shopee-settlements" element={ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS ? <RequireMenu menuKey="shopee_settlements"><ShopeeSettlement /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="import/lazada" element={ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS ? <RequireMenu menuKey="import_lazada"><LazadaImport /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="import/tiktok" element={ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS ? <RequireMenu menuKey="import_tiktok"><TikTokImport /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="mappings" element={<RequireMenu menuKey="mappings"><Mappings /></RequireMenu>} />
          <Route path="marketplace-aliases" element={ENABLE_SALES_ORDERS ? <RequireMenu menuKey="marketplace_aliases"><MarketplaceAliases /></RequireMenu> : <Navigate to="/dashboard" replace />} />
          <Route path="settings/old-data" element={<RequireMenu menuKey="old_data"><OldDataSettings /></RequireMenu>} />
          <Route path="settings" element={<Navigate to="/settings/instance" replace />} />
          <Route path="logs" element={<RequireMenu menuKey="logs"><Logs /></RequireMenu>} />
          <Route path="bulk-send-jobs" element={<RequireMenu menuKey="bulk_send_jobs"><BulkSendJobs /></RequireMenu>} />
          <Route path="settings/catalog" element={<RequireMenu menuKey="catalog"><CatalogSettings /></RequireMenu>} />
          <Route path="settings/email" element={<RequireMenu menuKey="email_accounts"><EmailAccounts /></RequireMenu>} />
          <Route path="settings/shopee-connections" element={<RequireAdmin><RequireMenu menuKey="shopee_connections"><ShopeeConnections /></RequireMenu></RequireAdmin>} />
          <Route path="settings/line-myshop" element={ENABLE_LINE_MYSHOP ? <RequireAdmin><RequireMenu menuKey="line_myshop"><LineMyShopSettings /></RequireMenu></RequireAdmin> : <Navigate to="/settings/instance" replace />} />
          <Route path="settings/channels" element={<RequireMenu menuKey="channel_defaults"><ChannelDefaults /></RequireMenu>} />
          <Route path="settings/instance" element={<RequireMenu menuKey="instance_settings"><InstanceSettings /></RequireMenu>} />
          <Route path="settings/ai-usage" element={<RequireMenu menuKey="ai_usage"><AIUsage /></RequireMenu>} />
          <Route path="settings/users" element={<RequireAdmin><RequireMenu menuKey="settings_users"><UserSettings /></RequireMenu></RequireAdmin>} />
          <Route path="settings/menu-permissions" element={<RequireAdmin><RequireMenu menuKey="settings_menu_permissions"><MenuPermissions /></RequireMenu></RequireAdmin>} />
          <Route path="settings/line-notifications" element={<RequireAdmin><RequireMenu menuKey="line_notifications"><LineNotifications /></RequireMenu></RequireAdmin>} />
          <Route path="settings/line-oa" element={ENABLE_CHAT ? <RequireMenu menuKey="line_oa"><LineOA /></RequireMenu> : <Navigate to="/settings/instance" replace />} />
          <Route path="settings/quick-replies" element={ENABLE_CHAT ? <RequireMenu menuKey="quick_replies"><QuickReplies /></RequireMenu> : <Navigate to="/settings/instance" replace />} />
          <Route path="settings/chat-tags" element={ENABLE_CHAT ? <RequireMenu menuKey="chat_tags"><ChatTags /></RequireMenu> : <Navigate to="/settings/instance" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
