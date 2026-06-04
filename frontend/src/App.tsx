import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { useAuthStore } from './store/auth'
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Bills from './pages/Bills'
import BillDetail from './pages/BillDetail'
import Import from './pages/Import'
import ShopeeImport from './pages/ShopeeImport'
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
import LineNotifications from './pages/LineNotifications'
import ChatTags from './pages/ChatTags'
import LineOA from './pages/LineOA'
import Messages from './pages/Messages'
import QuickReplies from './pages/QuickReplies'
import Showcase from './pages/Showcase'
import { ENABLE_CHAT, ENABLE_LAZADA_EXCEL, ENABLE_SALES_ORDERS, ENABLE_SHOPEE_EXCEL, ENABLE_SHOPEE_REALTIME_OPS, ENABLE_TIKTOK_EXCEL } from './lib/featureFlags'
import SetupCenter from './pages/SetupCenter'

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
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="setup" element={<SetupCenter />} />
          <Route path="dashboard" element={<Dashboard />} />
          <Route path="bills" element={<Bills mode="purchase-order" />} />
          <Route path="sales-orders" element={ENABLE_SALES_ORDERS ? <Bills mode="sales-order" /> : <Navigate to="/dashboard" replace />} />
          <Route path="sale-invoices" element={ENABLE_SALES_ORDERS ? <Bills mode="sale-invoice" /> : <Navigate to="/dashboard" replace />} />
          <Route path="bills/:id" element={<BillDetail />} />
          <Route path="sales-orders/:id" element={ENABLE_SALES_ORDERS ? <BillDetail /> : <Navigate to="/dashboard" replace />} />
          <Route path="sale-invoices/:id" element={ENABLE_SALES_ORDERS ? <BillDetail /> : <Navigate to="/dashboard" replace />} />
          <Route path="messages" element={ENABLE_CHAT ? <Messages /> : <Navigate to="/dashboard" replace />} />
          <Route path="import" element={<Import />} />
          <Route path="import/shopee" element={ENABLE_SHOPEE_EXCEL ? <ShopeeImport /> : <Navigate to="/dashboard" replace />} />
          <Route path="shopee-operations" element={ENABLE_SHOPEE_REALTIME_OPS ? <ShopeeOperations /> : <Navigate to="/dashboard" replace />} />
          <Route path="shopee-settlements" element={ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS ? <ShopeeSettlement /> : <Navigate to="/dashboard" replace />} />
          <Route path="import/lazada" element={ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS ? <LazadaImport /> : <Navigate to="/dashboard" replace />} />
          <Route path="import/tiktok" element={ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS ? <TikTokImport /> : <Navigate to="/dashboard" replace />} />
          <Route path="mappings" element={<Mappings />} />
          <Route path="marketplace-aliases" element={ENABLE_SALES_ORDERS ? <MarketplaceAliases /> : <Navigate to="/dashboard" replace />} />
          <Route path="settings/old-data" element={<OldDataSettings />} />
          <Route path="settings" element={<Navigate to="/settings/instance" replace />} />
          <Route path="logs" element={<Logs />} />
          <Route path="bulk-send-jobs" element={<BulkSendJobs />} />
          <Route path="settings/catalog" element={<CatalogSettings />} />
          <Route path="settings/email" element={<EmailAccounts />} />
          <Route path="settings/channels" element={<ChannelDefaults />} />
          <Route path="settings/instance" element={<InstanceSettings />} />
          <Route path="settings/ai-usage" element={<AIUsage />} />
          <Route path="settings/users" element={<UserSettings />} />
          <Route path="settings/line-notifications" element={<RequireAdmin><LineNotifications /></RequireAdmin>} />
          <Route path="settings/line-oa" element={ENABLE_CHAT ? <LineOA /> : <Navigate to="/settings/instance" replace />} />
          <Route path="settings/quick-replies" element={ENABLE_CHAT ? <QuickReplies /> : <Navigate to="/settings/instance" replace />} />
          <Route path="settings/chat-tags" element={ENABLE_CHAT ? <ChatTags /> : <Navigate to="/settings/instance" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
