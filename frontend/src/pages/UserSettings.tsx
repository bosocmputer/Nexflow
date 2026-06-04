import { useEffect, useMemo, useState } from 'react'
import type React from 'react'
import { ShieldCheck, Trash2, UserPlus, UsersRound } from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { PageHeader } from '@/components/common/PageHeader'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useAuth } from '@/hooks/useAuth'
import type { User } from '@/types'

interface FormState {
  id?: string
  email: string
  name: string
  role: User['role']
  password: string
}

const EMPTY_FORM: FormState = {
  email: '',
  name: '',
  role: 'staff',
  password: '',
}

const ROLE_LABEL: Record<User['role'], string> = {
  admin: 'ผู้ดูแลระบบ',
  staff: 'พนักงาน',
  viewer: 'ดูข้อมูลอย่างเดียว',
}

const ROLE_IMPACT: Record<User['role'], string> = {
  admin: 'เห็นและแก้ไข settings, ผู้ใช้, route, action อันตราย และงานส่ง SML ตามสิทธิ์ admin',
  staff: 'ใช้คิวงานประจำ เช่น import, ตรวจบิล และส่งงานที่ได้รับอนุญาต แต่ไม่ควรแก้ setting สำคัญ',
  viewer: 'ดูข้อมูลและสถานะย้อนหลังได้เท่านั้น ไม่ควรมีปุ่มเปลี่ยนข้อมูลหรือส่งเข้า SML',
}

export default function UserSettings() {
  const { user: currentUser } = useAuth()
  const [users, setUsers] = useState<User[]>([])
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deletingUser, setDeletingUser] = useState<User | null>(null)

  const editing = Boolean(form.id)
  const sortedUsers = useMemo(
    () => [...users].sort((a, b) => a.role.localeCompare(b.role) || a.email.localeCompare(b.email)),
    [users],
  )

  const load = async () => {
    setLoading(true)
    try {
      const res = await client.get<{ data: User[] }>('/api/settings/users')
      setUsers(res.data.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const reset = () => setForm(EMPTY_FORM)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      const payload = {
        email: form.email.trim(),
        name: form.name.trim(),
        role: form.role,
        password: form.password.trim(),
      }
      if (editing) {
        await client.put(`/api/settings/users/${form.id}`, payload)
        toast.success('อัปเดตผู้ใช้แล้ว')
      } else {
        await client.post('/api/settings/users', payload)
        toast.success('เพิ่มผู้ใช้แล้ว')
      }
      reset()
      await load()
    } catch (err: any) {
      toast.error(err.response?.data?.error ?? 'บันทึกผู้ใช้ไม่สำเร็จ')
    } finally {
      setSaving(false)
    }
  }

  const editUser = (u: User) => {
    setForm({
      id: u.id,
      email: u.email,
      name: u.name,
      role: u.role,
      password: '',
    })
  }

  const deleteUser = async (u: User) => {
    try {
      await client.delete(`/api/settings/users/${u.id}`)
      toast.success('ลบผู้ใช้แล้ว')
      if (form.id === u.id) reset()
      await load()
    } catch (err: any) {
      toast.error(err.response?.data?.error ?? 'ลบผู้ใช้ไม่สำเร็จ')
    }
  }

  if (currentUser?.role !== 'admin') {
    return (
      <div className="p-6">
        <PageHeader title="ผู้ใช้ระบบ" description="เฉพาะผู้ดูแลระบบเท่านั้น" />
      </div>
    )
  }

  return (
    <div className="space-y-5 p-6">
      <PageHeader
        title="ผู้ใช้ระบบ"
        description="กำหนดสิทธิ์ admin, staff และ viewer สำหรับการใช้งาน Nexflow"
        actions={
          <Button type="button" variant="outline" onClick={reset}>
            <UserPlus className="mr-2 h-4 w-4" />
            เพิ่มผู้ใช้
          </Button>
        }
      />

      <div className="grid gap-5 lg:grid-cols-[1fr_360px]">
        <div className="overflow-hidden rounded-lg border bg-card">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40">
                <TableHead>ผู้ใช้</TableHead>
                <TableHead>สิทธิ์</TableHead>
                <TableHead className="w-[160px] text-right">จัดการ</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={3} className="py-10 text-center text-sm text-muted-foreground">
                    กำลังโหลด...
                  </TableCell>
                </TableRow>
              ) : sortedUsers.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={3} className="py-10 text-center text-sm text-muted-foreground">
                    ยังไม่มีผู้ใช้
                  </TableCell>
                </TableRow>
              ) : (
                sortedUsers.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell>
                      <div className="font-medium">{u.name}</div>
                      <div className="text-xs text-muted-foreground">{u.email}</div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={u.role === 'admin' ? 'default' : 'secondary'} className="gap-1">
                        {u.role === 'admin' && <ShieldCheck className="h-3 w-3" />}
                        {ROLE_LABEL[u.role]}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <Button type="button" variant="outline" size="sm" onClick={() => editUser(u)}>
                          แก้ไข
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          onClick={() => setDeletingUser(u)}
                          disabled={u.id === currentUser.id}
                          aria-label={u.id === currentUser.id ? 'ลบผู้ใช้ตัวเองไม่ได้' : 'ลบผู้ใช้'}
                          title={u.id === currentUser.id ? 'ลบผู้ใช้ที่กำลัง login อยู่ไม่ได้' : 'ลบผู้ใช้นี้'}
                        >
                          <Trash2 className="h-4 w-4 text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        <Card className="shadow-none">
          <CardContent className="p-4">
            <form className="space-y-4" onSubmit={submit}>
              <div className="flex items-center gap-2">
                <UsersRound className="h-4 w-4 text-accent-strong" />
                <div className="font-semibold">{editing ? 'แก้ไขผู้ใช้' : 'เพิ่มผู้ใช้ใหม่'}</div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="user-name">ชื่อ</Label>
                <Input
                  id="user-name"
                  value={form.name}
                  onChange={(e) => setForm((s) => ({ ...s, name: e.target.value }))}
                  placeholder="เช่น Admin Nexflow"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="user-email">อีเมล</Label>
                <Input
                  id="user-email"
                  type="email"
                  value={form.email}
                  onChange={(e) => setForm((s) => ({ ...s, email: e.target.value }))}
                  placeholder="name@example.com"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label>สิทธิ์</Label>
                <Select value={form.role} onValueChange={(role: User['role']) => setForm((s) => ({ ...s, role }))}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="admin">ผู้ดูแลระบบ</SelectItem>
                    <SelectItem value="staff">พนักงาน</SelectItem>
                    <SelectItem value="viewer">ดูข้อมูลอย่างเดียว</SelectItem>
                  </SelectContent>
                </Select>
                <div className="rounded-md border border-info/20 bg-info/[0.04] px-3 py-2 text-xs leading-5 text-muted-foreground">
                  <span className="font-medium text-foreground">{ROLE_LABEL[form.role]}:</span>{' '}
                  {ROLE_IMPACT[form.role]}
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="user-password">{editing ? 'รหัสผ่านใหม่' : 'รหัสผ่าน'}</Label>
                <Input
                  id="user-password"
                  type="password"
                  value={form.password}
                  onChange={(e) => setForm((s) => ({ ...s, password: e.target.value }))}
                  placeholder={editing ? 'เว้นว่างถ้าไม่เปลี่ยน' : 'อย่างน้อย 6 ตัวอักษร'}
                  required={!editing}
                />
              </div>
              <div className="flex gap-2 pt-2">
                <Button type="submit" disabled={saving}>
                  {saving ? 'กำลังบันทึก...' : 'บันทึก'}
                </Button>
                {editing && (
                  <Button type="button" variant="outline" onClick={reset}>
                    ยกเลิก
                  </Button>
                )}
              </div>
            </form>
          </CardContent>
        </Card>
      </div>
      <ConfirmDialog
        open={deletingUser !== null}
        onOpenChange={(open) => !open && setDeletingUser(null)}
        title="ลบผู้ใช้ออกจาก production workspace?"
        description={deletingUser ? [
          `ผู้ใช้: ${deletingUser.name} · ${deletingUser.email}`,
          `สิทธิ์: ${ROLE_LABEL[deletingUser.role]}`,
          'ผลกระทบ: ผู้ใช้นี้จะ login เข้า Nexflow ไม่ได้อีก และจะไม่สามารถทำ action ใหม่ในระบบได้',
          'ข้อมูลเดิม: audit logs และเอกสารที่เคยทำไว้จะยังคงอยู่เพื่อการตรวจสอบย้อนหลัง',
          'Rollback: ต้องสร้างผู้ใช้ใหม่หรือ restore จาก backup หากลบผิด',
        ].join('\n') : ''}
        confirmLabel="ลบผู้ใช้"
        variant="destructive"
        onConfirm={async () => {
          if (!deletingUser) return
          await deleteUser(deletingUser)
          setDeletingUser(null)
        }}
      />
    </div>
  )
}
