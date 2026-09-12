import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, Clock3, Loader2, PlugZap, RefreshCw, ShieldCheck, Store } from 'lucide-react'

import client from '@/api/client'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { PageHeader } from '@/components/common/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

interface TikTokShopStatus {
  enabled: boolean
  configured: boolean
  mode: 'gateway'
  redirect_url?: string
}

interface TikTokShopConnection {
  gateway_connection_id: string
  shop_id: string
  shop_name: string
  shop_region: string
  seller_type: string
  shop_code: string
  granted_scopes: string[]
  access_expires_at: string
  refresh_expires_at: string
  disabled: boolean
  connected_at: string
  updated_at: string
}

interface TikTokOrderSyncSetting {
  shop_id: string
  enabled: boolean
  interval_seconds: number
  last_success_at?: string
  last_error_code?: string
  last_error_message?: string
}

interface TikTokOrderSyncResponse {
  worker_enabled: boolean
  data: TikTokOrderSyncSetting[]
}

export default function TikTokShopConnections() {
  const pollRef = useRef<number | null>(null)
  const [status, setStatus] = useState<TikTokShopStatus | null>(null)
  const [connections, setConnections] = useState<TikTokShopConnection[]>([])
  const [orderSync, setOrderSync] = useState<TikTokOrderSyncResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)

  const load = async () => {
    setError('')
    try {
      const statusResponse = await client.get<TikTokShopStatus>('/api/settings/tiktok-shop-api/status')
      const nextStatus = statusResponse.data
      setStatus(nextStatus)
      if (!nextStatus.enabled || !nextStatus.configured) {
        setConnections([])
        setOrderSync(null)
        return
      }
      const connectionResponse = await client.get<{ data: TikTokShopConnection[] }>('/api/tiktok-shop-api/connections')
      const syncResponse = await client.get<TikTokOrderSyncResponse>('/api/tiktok-shop-api/order-sync-settings')
      setConnections(connectionResponse.data.data ?? [])
      setOrderSync(syncResponse.data)
    } catch (cause: unknown) {
      setError(apiErrorMessage(cause, 'โหลดข้อมูลร้าน TikTok Shop ไม่สำเร็จ'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
    return () => {
      if (pollRef.current !== null) window.clearInterval(pollRef.current)
    }
  }, [])

  const connect = async () => {
    if (pollRef.current !== null) window.clearInterval(pollRef.current)
    const authWindow = window.open('', '_blank', 'popup=yes,width=1120,height=820')
    if (!authWindow) {
      setError('Browser บล็อกหน้าต่าง TikTok Shop ให้เปิด pop-up สำหรับ Nexflow แล้วลองใหม่')
      return
    }
    authWindow.document.title = 'กำลังเปิด TikTok Shop'
    authWindow.document.body.style.cssText = 'margin:0;font-family:system-ui,sans-serif;background:#f4f5ef;color:#111817;display:grid;place-items:center;min-height:100vh;'
    authWindow.document.body.textContent = 'กำลังเปิดหน้า TikTok Shop เพื่ออนุญาตร้าน...'
    setBusy(true)
    setError('')
    try {
      const response = await client.post<{ auth_url: string }>('/api/tiktok-shop-api/auth-url')
      authWindow.opener = null
      authWindow.location.href = response.data.auth_url
      let attempts = 0
      pollRef.current = window.setInterval(() => {
        attempts += 1
        void load()
        if (authWindow.closed || attempts >= 60) {
          if (pollRef.current !== null) window.clearInterval(pollRef.current)
          pollRef.current = null
          void load()
        }
      }, 2000)
    } catch (cause: unknown) {
      authWindow.close()
      setError(apiErrorMessage(cause, 'สร้างลิงก์เชื่อมต่อ TikTok Shop ไม่สำเร็จ'))
    } finally {
      setBusy(false)
    }
  }

  const activeConnections = connections.filter((connection) => !connection.disabled)
  const ready = Boolean(status?.enabled && status.configured)

  return (
    <div className="space-y-5">
      <PageHeader
        title="ร้าน TikTok Shop"
        description="เชื่อมร้านผ่าน Central Gateway โดยเก็บ App Secret และ token ไว้นอก tenant"
        actions={
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" size="sm" className="gap-2" onClick={() => void load()} disabled={loading || busy}>
              <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />
              รีเฟรช
            </Button>
            <Button size="sm" className="gap-2" onClick={() => setConfirmOpen(true)} disabled={!ready || busy} title={!ready ? 'TikTok Shop Gateway ยังไม่พร้อมสำหรับ tenant นี้' : undefined}>
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <PlugZap className="h-4 w-4" />}
              {activeConnections.length > 0 ? 'เชื่อมร้านเพิ่ม' : 'เชื่อมต่อร้าน TikTok Shop'}
            </Button>
          </div>
        }
      />

      {error && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>ดำเนินการไม่สำเร็จ</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <Card className={cn('shadow-none', ready ? 'border-success/30 bg-success/[0.04]' : 'border-warning/35 bg-warning/[0.06]')}>
        <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex min-w-0 items-start gap-3">
            {ready ? <ShieldCheck className="mt-0.5 h-5 w-5 shrink-0 text-accent-strong" /> : <Clock3 className="mt-0.5 h-5 w-5 shrink-0 text-warning" />}
            <div>
              <h2 className="text-sm font-semibold text-foreground">{ready ? 'Gateway พร้อมเชื่อมร้าน' : 'Gateway ยังไม่พร้อม'}</h2>
              <p className="mt-1 max-w-3xl text-sm text-muted-foreground">
                {ready ? 'ระบบขอเฉพาะสิทธิ์อ่านข้อมูลร้านและคำสั่งซื้อ การซิงก์ออเดอร์ทำงานเป็น Snapshot แบบ read-only และยังไม่ส่งสต๊อก ยืนยันจัดส่ง หรือสร้าง Bill/SML อัตโนมัติ' : status?.enabled ? 'ตรวจ Gateway URL, tenant identity และ internal secret บน server' : 'ฟีเจอร์ TikTok Shop Open API ยังปิดอยู่ใน tenant นี้'}
              </p>
              {status?.redirect_url && <p className="mt-2 break-all font-mono text-xs text-muted-foreground">Callback: {status.redirect_url}</p>}
            </div>
          </div>
          <Badge variant="outline" className="w-fit shrink-0">Central Gateway</Badge>
        </CardContent>
      </Card>

      <Card className="shadow-none">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <Store className="h-4 w-4 text-accent-strong" />
            ร้านที่อนุญาตแล้ว
          </CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="flex items-center gap-2 py-8 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />กำลังโหลดร้าน...</div>
          ) : connections.length === 0 ? (
            <div className="rounded-md border border-dashed border-border bg-muted/20 p-5 text-sm text-muted-foreground">
              ยังไม่มีร้านที่เชื่อม เมื่อ Gateway พร้อมแล้ว กด “เชื่อมต่อร้าน TikTok Shop” และอนุญาตร้าน AOY ในหน้าต่าง TikTok
            </div>
          ) : (
            <div className="divide-y rounded-md border border-border bg-background">
              {connections.map((connection) => (
                <div key={connection.gateway_connection_id} className="flex flex-col gap-3 p-4 lg:flex-row lg:items-center lg:justify-between">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium text-foreground">{connection.shop_name || connection.shop_code || connection.shop_id}</span>
                      <Badge variant={connection.disabled ? 'outline' : 'default'}>{connection.disabled ? 'ปิดใช้งาน' : 'เชื่อมต่อแล้ว'}</Badge>
                      {connection.shop_region && <Badge variant="outline">{connection.shop_region}</Badge>}
                    </div>
                    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                      <span className="font-mono">Shop ID {connection.shop_id}</span>
                      {connection.shop_code && <span>รหัสร้าน {connection.shop_code}</span>}
                      <span>อนุญาต {scopeLabel(connection.granted_scopes)}</span>
                    </div>
                    <OrderSyncLine workerEnabled={Boolean(orderSync?.worker_enabled)} setting={orderSync?.data.find((item) => item.shop_id === connection.shop_id)} />
                  </div>
                  <div className="shrink-0 text-xs text-muted-foreground lg:text-right">
                    <div>Access token ถึง {formatDateTime(connection.access_expires_at)}</div>
                    <div className="mt-1">Refresh token ถึง {formatDateTime(connection.refresh_expires_at)}</div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title="เปิดหน้าต่าง TikTok Shop เพื่อเชื่อมร้าน?"
        description="TikTok Shop จะแสดงร้านที่บัญชีนี้มีสิทธิ์จัดการ กรุณาเลือกเฉพาะร้าน AOY สำหรับ UAT รอบแรก หลังอนุญาตแล้ว Nexflow จะบันทึกทุกร้านที่ TikTok ส่งกลับภายใต้ tenant AOY"
        confirmLabel={activeConnections.length > 0 ? 'เชื่อมร้านเพิ่ม' : 'เปิด TikTok Shop'}
        onConfirm={connect}
      />
    </div>
  )
}

function OrderSyncLine({ workerEnabled, setting }: { workerEnabled: boolean; setting?: TikTokOrderSyncSetting }) {
  const active = Boolean(workerEnabled && setting?.enabled && !setting.last_error_code)
  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
      <span className={cn('inline-flex items-center gap-1 font-medium', active ? 'text-success' : 'text-warning')}>
        <span className={cn('h-1.5 w-1.5 rounded-full', active ? 'bg-success' : 'bg-warning')} />
        {active ? `ซิงก์ออเดอร์ทุก ${formatInterval(setting?.interval_seconds ?? 300)}` : workerEnabled ? 'ร้านนี้ยังปิดซิงก์ออเดอร์' : 'Worker ซิงก์ออเดอร์ยังปิด'}
      </span>
      {setting?.last_success_at && <span className="text-muted-foreground">สำเร็จล่าสุด {formatDateTime(setting.last_success_at)}</span>}
      {setting?.last_error_message && <span className="text-destructive">{setting.last_error_message}</span>}
    </div>
  )
}

function scopeLabel(scopes: string[]) {
  const labels: string[] = []
  if (scopes.includes('seller.authorization.info')) labels.push('ข้อมูลร้าน')
  if (scopes.includes('seller.order.info')) labels.push('คำสั่งซื้อ')
  return labels.length > 0 ? labels.join(' · ') : 'สิทธิ์ไม่ครบ'
}

function formatDateTime(value: string) {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return '—'
  return parsed.toLocaleString('th-TH-u-ca-gregory', { timeZone: 'Asia/Bangkok', dateStyle: 'medium', timeStyle: 'short' })
}

function formatInterval(seconds: number) {
  return seconds % 60 === 0 ? `${seconds / 60} นาที` : `${seconds} วินาที`
}

function apiErrorMessage(cause: unknown, fallback: string) {
  const data = (cause as { response?: { data?: { error?: { message?: string } | string } } })?.response?.data
  if (typeof data?.error === 'string') return data.error
  return data?.error?.message || fallback
}
