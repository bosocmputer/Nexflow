import { useState, FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { AlertCircle, ArrowRight, CheckCircle2, LockKeyhole, ShieldCheck } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import client from '@/api/client'
import { NexflowLogo } from '@/components/common/NexflowLogo'
import { useAuthStore } from '@/store/auth'
import type { User } from '@/types'

interface LoginResponse {
  token: string
  user: User
}

export default function Login() {
  const navigate = useNavigate()
  const login = useAuthStore((s) => s.login)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setLoading(true)
    try {
      const res = await client.post<LoginResponse>('/api/auth/login', { email, password })
      login(res.data.token, res.data.user)
      navigate('/dashboard')
    } catch {
      setError('อีเมลหรือรหัสผ่านไม่ถูกต้อง')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center bg-background p-3 text-foreground sm:p-6">
      <div className="mx-auto grid w-full max-w-5xl overflow-hidden rounded-lg border border-border bg-card lg:min-h-[680px] lg:grid-cols-[0.82fr_1fr]">
        <section className="flex min-h-[300px] flex-col justify-between bg-sidebar p-6 text-sidebar-foreground sm:p-8 lg:min-h-0">
          <div className="flex items-center gap-3">
            <NexflowLogo className="h-12 w-12" />
            <div>
              <div className="text-2xl font-semibold leading-tight">Nexflow</div>
              <div className="text-xs font-medium text-sidebar-foreground/62">Operations Console</div>
            </div>
          </div>

          <div className="py-8 lg:py-0">
            <div className="inline-flex items-center gap-2 rounded-md border border-sidebar-border bg-sidebar-accent px-3 py-1.5 text-xs font-medium text-sidebar-foreground/78">
              <ShieldCheck className="h-3.5 w-3.5 text-primary" />
              พื้นที่ใช้งานจริง
            </div>
            <h1 className="mt-5 max-w-md text-2xl font-semibold leading-tight sm:text-[32px]">
              เข้าสู่งานเอกสารขายและ SML ของ Nexflow
            </h1>
            <p className="mt-4 max-w-md text-sm leading-6 text-sidebar-foreground/68">
              สำหรับทีมบัญชีและทีมปฏิบัติการที่ติดตามคำสั่งซื้อ Shopee, จัดการเอกสารขาย และติดตามผลส่งเข้า SML ทุกวัน
            </p>
          </div>

          <div className="space-y-3 border-t border-sidebar-border pt-5">
            {[
              ['ตรวจรายการ', 'ดึงรายการจากช่องทางขายและตรวจซ้ำก่อนสร้างเอกสาร'],
              ['ส่งเข้า SML', 'ส่งเฉพาะรายการที่พร้อม พร้อมประวัติการทำงานย้อนหลัง'],
            ].map(([title, detail]) => (
              <div key={title} className="flex gap-3">
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                <div className="min-w-0">
                  <div className="text-sm font-medium">{title}</div>
                  <div className="mt-0.5 text-xs leading-5 text-sidebar-foreground/62">{detail}</div>
                </div>
              </div>
            ))}
          </div>
        </section>

        <section className="flex min-h-[520px] flex-col justify-center p-6 sm:p-9">
          <div className="mx-auto w-full max-w-[420px]">
            <div>
              <div className="mb-4 inline-flex items-center gap-2 rounded-md border border-border bg-muted/45 px-2.5 py-1 text-xs font-medium text-muted-foreground">
                <LockKeyhole className="h-3.5 w-3.5 text-accent-strong" />
                เข้าสู่ระบบอย่างปลอดภัย
              </div>
              <h2 className="text-2xl font-semibold leading-tight sm:text-[28px]">เข้าสู่ระบบ</h2>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">
                ใช้บัญชีที่ได้รับสิทธิ์เพื่อเข้าสู่ Operations Console
              </p>
            </div>

          <form onSubmit={handleSubmit} className="mt-8 space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">อีเมล</Label>
              <Input
                id="email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@company.com"
                required
                autoFocus
                autoComplete="email"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">รหัสผ่าน</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="กรอกรหัสผ่าน"
                required
                autoComplete="current-password"
              />
            </div>

            {error && (
              <Alert variant="destructive">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription>ตรวจอีเมลหรือรหัสผ่านอีกครั้ง หากยังเข้าไม่ได้ให้ติดต่อผู้ดูแลระบบ</AlertDescription>
              </Alert>
            )}

            <Button type="submit" className="h-10 w-full gap-2" disabled={loading}>
              <ArrowRight className="h-4 w-4" />
              {loading ? 'กำลังเข้าสู่ระบบ…' : 'เข้าสู่ระบบ'}
            </Button>
          </form>

            <p className="mt-6 text-xs leading-5 text-muted-foreground">
              หลังเข้าสู่ระบบ ระบบจะพาไปที่ภาพรวมงานวันนี้โดยอัตโนมัติ
            </p>
          </div>
        </section>
      </div>
    </div>
  )
}
