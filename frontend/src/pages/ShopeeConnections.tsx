import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, CheckCircle2, Clock3, Loader2, Pencil, PlugZap, Power, RefreshCw, Save, ShieldCheck, Store, X } from 'lucide-react'

import client from '@/api/client'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { PageHeader } from '@/components/common/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

type CheckStatus = 'ok' | 'warning' | 'blocked'

interface ReadinessCheck {
  key: string
  label: string
  status: CheckStatus
  detail?: string
}

interface ShopeeAPIStatus {
  enabled: boolean
  configured: boolean
  environment: string
  base_url?: string
  partner_id?: number
  redirect_url?: string
  connected: boolean
  shop_id?: number
  shop_name?: string
  access_expires_at?: string
  refresh_expires_at?: string
  last_sync_at?: string
  last_sync_status?: string
  last_sync_error?: string
  token_state?: string
  can_connect?: boolean
  can_fetch?: boolean
  blocking_reason?: string
  checks?: ReadinessCheck[]
}

interface ShopeeAPIConnection {
  id: string
  shop_id: number
  merchant_id?: number
  shop_name?: string
  label: string
  environment: string
  access_expires_at: string
  refresh_expires_at: string
  disabled_at?: string
  last_sync_at?: string
  last_sync_status?: string
  last_sync_error?: string
  token_state: string
  can_fetch: boolean
  updated_at?: string
}

const CANONICAL_PUBLIC_URL = 'https://animal-galvanize-tameness.ngrok-free.dev'

export default function ShopeeConnections() {
  const apiAuthPollRef = useRef<number | null>(null)
  const [status, setStatus] = useState<ShopeeAPIStatus | null>(null)
  const [connections, setConnections] = useState<ShopeeAPIConnection[]>([])
  const [loadError, setLoadError] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirmConnectOpen, setConfirmConnectOpen] = useState(false)
  const [disableConnection, setDisableConnection] = useState<ShopeeAPIConnection | null>(null)
  const [editingID, setEditingID] = useState('')
  const [editingLabel, setEditingLabel] = useState('')

  const load = async () => {
    setLoadError('')
    try {
      const [statusRes, connRes] = await Promise.all([
        client.get<ShopeeAPIStatus>('/api/settings/shopee-api/status'),
        client.get<{ data: ShopeeAPIConnection[] }>('/api/shopee-api/connections'),
      ])
      setStatus(statusRes.data)
      setConnections(connRes.data.data ?? [])
    } catch (err: unknown) {
      setLoadError(apiErrorMessage(err, 'โหลดข้อมูลร้าน Shopee ไม่สำเร็จ'))
    }
  }

  useEffect(() => {
    void load()
    return () => {
      if (apiAuthPollRef.current !== null) window.clearInterval(apiAuthPollRef.current)
    }
  }, [])

  const connect = async () => {
    if (apiAuthPollRef.current !== null) {
      window.clearInterval(apiAuthPollRef.current)
      apiAuthPollRef.current = null
    }
    const authWindow = window.open('', '_blank', 'popup=yes,width=1120,height=820')
    if (!authWindow) {
      setLoadError('Browser บล็อกหน้าต่าง Shopee ให้เปิด pop-up สำหรับ Nexflow แล้วลองใหม่')
      return
    }
    authWindow.document.title = 'กำลังเปิด Shopee Open API'
    authWindow.document.body.style.cssText =
      'margin:0;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#F8FBFF;color:#0F172A;display:grid;place-items:center;min-height:100vh;'
    authWindow.document.body.textContent = 'กำลังเปิดหน้า Shopee เพื่อเชื่อมต่อร้าน...'

    setBusy(true)
    try {
      const res = await client.post<{ auth_url: string }>('/api/shopee-api/auth-url')
      authWindow.opener = null
      authWindow.location.href = res.data.auth_url
      let pollCount = 0
      apiAuthPollRef.current = window.setInterval(() => {
        pollCount += 1
        void load()
        if (authWindow.closed || pollCount >= 60) {
          if (apiAuthPollRef.current !== null) {
            window.clearInterval(apiAuthPollRef.current)
            apiAuthPollRef.current = null
          }
          void load()
        }
      }, 2000)
    } catch (err: unknown) {
      authWindow.close()
      setLoadError(apiErrorMessage(err, 'สร้างลิงก์เชื่อมต่อ Shopee API ไม่ได้'))
    } finally {
      setBusy(false)
    }
  }

  const saveLabel = async (conn: ShopeeAPIConnection) => {
    const label = editingLabel.trim()
    if (!label) {
      setLoadError('ชื่อร้านต้องไม่ว่าง')
      return
    }
    setBusy(true)
    try {
      await client.patch(`/api/shopee-api/connections/${conn.id}`, { label })
      setEditingID('')
      await load()
    } catch (err: unknown) {
      setLoadError(apiErrorMessage(err, 'บันทึกชื่อร้านไม่สำเร็จ'))
    } finally {
      setBusy(false)
    }
  }

  const toggleDisabled = async (conn: ShopeeAPIConnection) => {
    setBusy(true)
    try {
      await client.patch(`/api/shopee-api/connections/${conn.id}`, { disabled: !conn.disabled_at })
      await load()
    } catch (err: unknown) {
      setLoadError(apiErrorMessage(err, 'เปลี่ยนสถานะร้านไม่สำเร็จ'))
    } finally {
      setBusy(false)
    }
  }

  const readiness = status ? shopeeReadiness(status) : null
  const active = connections.filter((c) => !c.disabled_at)
  const currentHost = typeof window !== 'undefined' ? window.location.host : ''
  const redirectHost = hostFromURL(status?.redirect_url)
  const canonicalHost = hostFromURL(CANONICAL_PUBLIC_URL)
  const domainMismatch = Boolean(status?.enabled && redirectHost && currentHost && currentHost !== redirectHost)

  return (
    <div className="space-y-5">
      <PageHeader
        title="ร้าน Shopee"
        description="ตั้งค่า OAuth, token และร้านที่ใช้กับคำสั่งซื้อ Shopee และการนำเข้าย้อนหลัง"
        actions={
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" size="sm" className="gap-2" onClick={load} disabled={busy}>
              <RefreshCw className={cn('h-4 w-4', busy && 'animate-spin')} />
              รีเฟรช
            </Button>
            <Button size="sm" className="gap-2" onClick={() => setConfirmConnectOpen(true)} disabled={busy || !status?.can_connect} title={!status?.can_connect ? status?.blocking_reason || 'Shopee API ยังไม่พร้อมเชื่อมร้าน' : undefined}>
              <PlugZap className="h-4 w-4" />
              {active.length > 0 ? 'เชื่อมร้านเพิ่ม' : 'เชื่อมต่อร้าน Shopee'}
            </Button>
          </div>
        }
      />

      {loadError && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>โหลดข้อมูลร้าน Shopee ไม่สำเร็จ</AlertTitle>
          <AlertDescription>{loadError}</AlertDescription>
        </Alert>
      )}

      {readiness && (
        <Card className={cn('border', readinessToneClass(readiness.tone))}>
          <CardContent className="p-4">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <ShieldCheck className="h-4 w-4" />
                  <h2 className="text-sm font-semibold">{readiness.title}</h2>
                  <Badge variant="outline" className="h-6">
                    {status?.environment || '—'}
                  </Badge>
                </div>
                <p className="mt-1 text-sm text-muted-foreground">{readiness.description}</p>
              </div>
              <div className="grid min-w-[280px] gap-2 text-xs sm:grid-cols-2">
                {(status?.checks ?? []).slice(0, 4).map((check) => (
                  <div key={check.key} className="flex items-start gap-2 rounded-md border border-border bg-background px-3 py-2">
                    {check.status === 'ok' ? <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 text-success" /> : check.status === 'warning' ? <Clock3 className="mt-0.5 h-3.5 w-3.5 text-warning" /> : <AlertTriangle className="mt-0.5 h-3.5 w-3.5 text-destructive" />}
                    <div className="min-w-0">
                      <div className="font-medium text-foreground">{check.label}</div>
                      {check.detail && <div className="truncate text-muted-foreground" title={check.detail}>{check.detail}</div>}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {domainMismatch && (
        <Alert>
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>ตรวจ domain ก่อนเชื่อมร้าน</AlertTitle>
          <AlertDescription>
            หน้านี้เปิดจาก {currentHost} แต่ Shopee redirect ชี้ไปที่ {redirectHost}. สำหรับ production ให้ใช้ {canonicalHost} เพื่อกัน OAuth callback ผิด domain.
          </AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <Store className="h-4 w-4 text-accent-strong" />
            ร้านที่เชื่อมต่อ
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {!status?.enabled ? (
            <div className="rounded-md border border-warning/35 bg-warning/[0.08] px-3 py-2 text-sm text-warning">
              Shopee API ปิดใช้งานใน instance นี้ จึงไม่ควรดึง order หรือเชื่อมร้านจนกว่าจะเปิดค่าบน server
            </div>
          ) : connections.length === 0 ? (
            <div className="rounded-md border border-dashed border-border bg-muted/25 p-5 text-sm text-muted-foreground">
              ยังไม่มีร้าน Shopee ที่เชื่อมต่อ กด “เชื่อมต่อร้าน Shopee” เพื่อเริ่ม OAuth ผ่าน Shopee Open Platform
            </div>
          ) : (
            <div className="grid gap-3">
              {connections.map((conn) => {
                const disabled = Boolean(conn.disabled_at)
                const editing = editingID === conn.id
                return (
                  <div key={conn.id} className={cn('rounded-md border border-border bg-background p-4', disabled && 'bg-muted/30 opacity-75')}>
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                      <div className="min-w-0 space-y-2">
                        <div className="flex flex-wrap items-center gap-2">
                          {editing ? (
                            <Input value={editingLabel} onChange={(event) => setEditingLabel(event.target.value)} className="h-8 max-w-sm" autoFocus />
                          ) : (
                            <div className="text-sm font-semibold text-foreground">{conn.label || conn.shop_name || conn.shop_id}</div>
                          )}
                          <Badge variant={disabled ? 'outline' : conn.can_fetch ? 'default' : 'secondary'}>
                            {disabled ? 'ปิดใช้งาน' : conn.can_fetch ? 'พร้อมดึง order' : tokenStateLabel(conn.token_state)}
                          </Badge>
                          <Badge variant="outline">shop {conn.shop_id}</Badge>
                        </div>
                        <div className="grid gap-1 text-xs text-muted-foreground sm:grid-cols-2 lg:grid-cols-4">
                          <span>Token: {tokenStateLabel(conn.token_state)}</span>
                          <span>Sync: {conn.last_sync_status || '—'}</span>
                          <span>ล่าสุด: {fmtDateTime(conn.last_sync_at || conn.updated_at || '')}</span>
                          <span>หมดอายุ: {fmtDateTime(conn.refresh_expires_at)}</span>
                        </div>
                        {conn.last_sync_error && <div className="text-xs text-destructive">{conn.last_sync_error}</div>}
                      </div>
                      <div className="flex flex-wrap gap-2">
                        {editing ? (
                          <>
                            <Button size="sm" className="h-8 gap-1" onClick={() => saveLabel(conn)} disabled={busy}>
                              <Save className="h-3.5 w-3.5" />
                              บันทึก
                            </Button>
                            <Button size="sm" variant="ghost" className="h-8 gap-1" onClick={() => setEditingID('')} disabled={busy}>
                              <X className="h-3.5 w-3.5" />
                              ยกเลิก
                            </Button>
                          </>
                        ) : (
                          <>
                            <Button size="sm" variant="outline" className="h-8 gap-1" onClick={() => { setEditingID(conn.id); setEditingLabel(conn.label || conn.shop_name || String(conn.shop_id)) }} disabled={busy}>
                              <Pencil className="h-3.5 w-3.5" />
                              เปลี่ยนชื่อ
                            </Button>
                            <Button size="sm" variant={disabled ? 'outline' : 'ghost'} className="h-8 gap-1" onClick={() => disabled ? void toggleDisabled(conn) : setDisableConnection(conn)} disabled={busy}>
                              <Power className="h-3.5 w-3.5" />
                              {disabled ? 'เปิดใช้' : 'ปิดใช้'}
                            </Button>
                          </>
                        )}
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm">ข้อมูลสำหรับตรวจระบบ</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-2 text-xs text-muted-foreground md:grid-cols-3">
          <InfoBlock label="Partner" value={status?.partner_id ? String(status.partner_id) : 'ยังไม่ได้ตั้งค่า'} />
          <InfoBlock label="Base URL" value={status?.base_url || '—'} />
          <InfoBlock label="Redirect" value={status?.redirect_url || '—'} />
        </CardContent>
      </Card>

      <ConfirmDialog
        open={confirmConnectOpen}
        onOpenChange={setConfirmConnectOpen}
        title="เปิด Shopee OAuth?"
        description={`ระบบจะเปิดหน้าต่าง Shopee เพื่อเชื่อมร้านเข้ากับ Nexflow\n\nCurrent domain: ${currentHost || '—'}\nCanonical domain: ${CANONICAL_PUBLIC_URL}\nRedirect domain: ${status?.redirect_url || '—'}\n\nหลังเชื่อมสำเร็จ หน้าต่าง Shopee จะปิดเองหรือกลับมาที่ callback ของ Nexflow`}
        confirmLabel={active.length > 0 ? 'เชื่อมร้านเพิ่ม' : 'เชื่อมต่อร้าน Shopee'}
        onConfirm={connect}
      />
      <ConfirmDialog
        open={disableConnection !== null}
        onOpenChange={(open) => !open && setDisableConnection(null)}
        title="ปิดใช้งานร้าน Shopee นี้?"
        description={`ร้าน ${disableConnection?.label || disableConnection?.shop_name || disableConnection?.shop_id || ''} จะไม่ถูกใช้ดึง order ใหม่ แต่เอกสารและประวัติเดิมยังอยู่ครบ สามารถเปิดกลับมาใช้ใหม่ได้ภายหลัง`}
        confirmLabel="ปิดใช้งานร้าน"
        variant="destructive"
        onConfirm={async () => {
          if (!disableConnection) return
          await toggleDisabled(disableConnection)
          setDisableConnection(null)
        }}
      />
    </div>
  )
}

function InfoBlock({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border bg-background p-3">
      <div className="font-medium text-foreground">{label}</div>
      <div className="mt-1 truncate font-mono" title={value}>{value}</div>
    </div>
  )
}

function shopeeReadiness(status: ShopeeAPIStatus) {
  if (!status.enabled) {
    return { title: 'Shopee API ปิดใช้งาน', description: status.blocking_reason || 'เปิดใช้งานบน server ก่อนเชื่อมร้าน', tone: 'danger' as const }
  }
  if (!status.configured) {
    return { title: 'ยังไม่ได้ตั้งค่า Partner ID / Key', description: 'ต้องตั้งค่า Shopee Open API ให้ครบก่อนสร้าง OAuth URL', tone: 'warning' as const }
  }
  if (!status.connected) {
    return { title: 'พร้อมเชื่อมร้าน', description: 'ตรวจ redirect domain ให้ตรงกับ ngrok production แล้วเริ่ม OAuth', tone: 'success' as const }
  }
  if (status.last_sync_status === 'ok' && !status.last_sync_error) {
    return { title: 'ร้าน Shopee พร้อมใช้งาน', description: 'Token และ sync ล่าสุดกลับมา OK แล้ว ใช้ได้ทั้งคำสั่งซื้อ Shopee และนำเข้าย้อนหลัง', tone: 'success' as const }
  }
  return { title: 'เชื่อมร้านแล้ว แต่ควรตรวจ sync ล่าสุด', description: status.last_sync_error || status.blocking_reason || 'ตรวจ token และ sync status ก่อนใช้งานจริง', tone: 'warning' as const }
}

function readinessToneClass(tone: 'success' | 'warning' | 'danger') {
  if (tone === 'success') return 'border-success/30 bg-success/5 text-success'
  if (tone === 'warning') return 'border-warning/35 bg-warning/10 text-warning'
  return 'border-destructive/30 bg-destructive/5 text-destructive'
}

function tokenStateLabel(v?: string) {
  switch (v) {
    case 'access_valid':
      return 'พร้อมใช้'
    case 'access_expiring':
      return 'ใกล้ refresh'
    case 'refresh_required':
      return 'ต้อง refresh'
    case 'refresh_expired':
      return 'หมดอายุ'
    default:
      return '—'
  }
}

function fmtDateTime(value?: string) {
  if (!value) return '—'
  const time = new Date(value)
  if (Number.isNaN(time.getTime())) return '—'
  return time.toLocaleString('th-TH', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })
}

function hostFromURL(value?: string) {
  if (!value) return ''
  try {
    return new URL(value).host
  } catch {
    return ''
  }
}

function apiErrorMessage(err: unknown, fallback: string) {
  const data = (err as { response?: { data?: { error?: string; error_code?: string } } })?.response?.data
  const raw = data?.error ?? ''
  switch (data?.error_code) {
    case 'not_configured':
      return 'Shopee Open API ยังไม่ได้ตั้งค่า Partner ID/Key บน server'
    case 'redirect_not_ready':
      return 'Redirect URL ยังไม่พร้อม ให้ตรวจ PUBLIC_BASE_URL และ Shopee Console ว่าตรงกัน'
    case 'not_connected':
      return 'ยังไม่ได้เชื่อมต่อร้าน Shopee'
    case 'token_error':
      return 'Shopee token ใช้งานไม่ได้หรือหมดอายุ ให้กดเชื่อมต่อร้านใหม่'
    case 'permission_denied':
      return 'Shopee ยังไม่อนุญาตสิทธิ์นี้'
  }
  return raw || fallback
}
