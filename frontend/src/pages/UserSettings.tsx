import { useEffect, useMemo, useState } from 'react'
import type React from 'react'
import { Save, ShieldCheck, Trash2, UserPlus, UsersRound } from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { PageHeader } from '@/components/common/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
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
import { NAV_GROUPS, permissionForMenu } from '@/lib/navigation'
import { cn } from '@/lib/utils'
import type { NavGroup, NavItem } from '@/lib/navigation'
import type { User, UserMenuPermission } from '@/types'

interface FormState {
  id?: string
  email: string
  name: string
  role: User['role']
  password: string
}

type PermissionDraft = Record<string, UserMenuPermission>
type PermissionAction = keyof Pick<UserMenuPermission, 'can_view' | 'can_create' | 'can_update' | 'can_delete'>

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
  admin: 'จัดการระบบและตั้งค่าสำคัญได้ เหมาะกับผู้ดูแล workspace',
  staff: 'ใช้เมนูงานประจำวัน เช่น คิวออเดอร์ นำเข้า ตรวจบิล และส่งงานที่ได้รับอนุญาต',
  viewer: 'เหมาะกับการดูข้อมูลและรายงาน ไม่ควรมีปุ่มเปลี่ยนข้อมูลหรือส่งเข้า SML',
}

const ACTION_LABEL: Record<PermissionAction, string> = {
  can_view: 'เข้าเมนู',
  can_create: 'เพิ่ม',
  can_update: 'แก้ไข',
  can_delete: 'ลบ',
}

const MENU_GROUPS = NAV_GROUPS
  .map((group) => ({
    ...group,
    items: group.items.filter((item) => item.enabled !== false),
  }))
  .filter((group) => group.items.length > 0)

export default function UserSettings() {
  const { user: currentUser, setUser } = useAuth()
  const [users, setUsers] = useState<User[]>([])
  const [selectedUserId, setSelectedUserId] = useState('')
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [permissionDraft, setPermissionDraft] = useState<PermissionDraft>({})
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [savingPermissions, setSavingPermissions] = useState(false)
  const [deletingUser, setDeletingUser] = useState<User | null>(null)

  const editing = Boolean(form.id)
  const sortedUsers = useMemo(
    () => [...users].sort((a, b) => a.role.localeCompare(b.role) || a.email.localeCompare(b.email)),
    [users],
  )
  const selectedUser = sortedUsers.find((u) => u.id === selectedUserId) ?? sortedUsers[0]
  const selectedChanged = selectedUser ? !samePermissions(permissionDraft, permissionsForUser(selectedUser)) : false

  const load = async () => {
    setLoading(true)
    try {
      const res = await client.get<{ data: User[] }>('/api/settings/users')
      const nextUsers = res.data.data ?? []
      setUsers(nextUsers)
      setSelectedUserId((current) => {
        if (current && nextUsers.some((u) => u.id === current)) return current
        return nextUsers[0]?.id ?? ''
      })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  useEffect(() => {
    if (!selectedUser) {
      setPermissionDraft({})
      return
    }
    setPermissionDraft(toPermissionDraft(permissionsForUser(selectedUser)))
  }, [selectedUser?.id, selectedUser?.role, selectedUser?.menu_permissions])

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
      let saved: User
      if (editing) {
        const res = await client.put<User>(`/api/settings/users/${form.id}`, payload)
        saved = res.data
        toast.success('อัปเดตผู้ใช้แล้ว')
      } else {
        const res = await client.post<User>('/api/settings/users', payload)
        saved = res.data
        toast.success('เพิ่มผู้ใช้แล้ว')
      }
      reset()
      setSelectedUserId(saved.id)
      await load()
      if (currentUser?.id === saved.id) setUser({ ...currentUser, ...saved })
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
    setSelectedUserId(u.id)
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

  const updatePermission = (menuKey: string, field: PermissionAction, checked: boolean) => {
    if (!selectedUser) return
    setPermissionDraft((current) => {
      const existing = current[menuKey] ?? permissionForMenu(selectedUser, menuKey) ?? emptyPermission(menuKey)
      const next: UserMenuPermission = { ...existing, [field]: checked }
      if (field === 'can_view' && !checked) {
        next.can_create = false
        next.can_update = false
        next.can_delete = false
      }
      if (selectedUser.role === 'admin' && menuKey === 'settings_users') {
        next.can_view = true
      }
      return { ...current, [menuKey]: next }
    })
  }

  const savePermissions = async () => {
    if (!selectedUser) return
    setSavingPermissions(true)
    try {
      const permissions = permissionsForUser(selectedUser).map((base) => permissionDraft[base.menu_key] ?? base)
      const res = await client.put<{ data: UserMenuPermission[] }>(
        `/api/settings/users/${selectedUser.id}/menu-permissions`,
        { permissions },
      )
      const nextPermissions = res.data.data ?? []
      setUsers((current) => current.map((u) => (
        u.id === selectedUser.id ? { ...u, menu_permissions: nextPermissions } : u
      )))
      setPermissionDraft(toPermissionDraft(nextPermissions))
      if (currentUser?.id === selectedUser.id) {
        setUser({ ...currentUser, menu_permissions: nextPermissions })
      }
      toast.success('บันทึกสิทธิ์เมนูแล้ว')
    } catch (err: any) {
      toast.error(err.response?.data?.error ?? 'บันทึกสิทธิ์เมนูไม่สำเร็จ')
    } finally {
      setSavingPermissions(false)
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
        description="จัดการบัญชีผู้ใช้ และกำหนดเมนูที่แต่ละคนเห็นใน Nexflow"
        actions={
          <Button type="button" variant="outline" onClick={reset}>
            <UserPlus className="mr-2 h-4 w-4" />
            เพิ่มผู้ใช้
          </Button>
        }
      />

      <div className="grid gap-5 xl:grid-cols-[360px_minmax(0,1fr)]">
        <div className="space-y-5">
          <div className="overflow-hidden rounded-lg border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/40">
                  <TableHead>ผู้ใช้</TableHead>
                  <TableHead className="w-[100px]">สิทธิ์</TableHead>
                  <TableHead className="w-[96px] text-right">จัดการ</TableHead>
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
                    <TableRow
                      key={u.id}
                      className={cn('cursor-pointer', selectedUser?.id === u.id && 'bg-primary/5')}
                      onClick={() => setSelectedUserId(u.id)}
                    >
                      <TableCell>
                        <div className="font-medium">{u.name}</div>
                        <div className="truncate text-xs text-muted-foreground">{u.email}</div>
                      </TableCell>
                      <TableCell>
                        <Badge variant={u.role === 'admin' ? 'default' : 'secondary'} className="gap-1">
                          {u.role === 'admin' && <ShieldCheck className="h-3 w-3" />}
                          {ROLE_LABEL[u.role]}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-1">
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            onClick={(event) => {
                              event.stopPropagation()
                              editUser(u)
                            }}
                          >
                            แก้ไข
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            onClick={(event) => {
                              event.stopPropagation()
                              setDeletingUser(u)
                            }}
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
                  <Label>สิทธิ์หลัก</Label>
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
                    {saving ? 'กำลังบันทึก...' : 'บันทึกผู้ใช้'}
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

        <Card className="shadow-none">
          <CardContent className="space-y-4 p-4">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
              <div className="min-w-0">
                <div className="text-base font-semibold text-foreground">สิทธิ์เมนู</div>
                <div className="mt-1 text-sm text-muted-foreground">
                  {selectedUser ? `${selectedUser.name} · ${selectedUser.email}` : 'เลือกผู้ใช้เพื่อกำหนดสิทธิ์'}
                </div>
              </div>
              <Button
                type="button"
                onClick={savePermissions}
                disabled={!selectedUser || savingPermissions || !selectedChanged}
              >
                <Save className="mr-2 h-4 w-4" />
                {savingPermissions ? 'กำลังบันทึก...' : 'บันทึกสิทธิ์'}
              </Button>
            </div>

            <div className="rounded-md border border-warning/25 bg-warning/[0.06] px-3 py-2 text-xs leading-5 text-muted-foreground">
              รอบนี้ระบบใช้จริงที่ช่อง <span className="font-medium text-foreground">เข้าเมนู</span> เพื่อซ่อนเมนูและกันเปิด URL ตรง
              ส่วนช่อง เพิ่ม แก้ไข ลบ เก็บไว้สำหรับล็อกปุ่มและ action ใน phase ถัดไป
            </div>

            {selectedUser ? (
              <PermissionMatrix
                user={selectedUser}
                draft={permissionDraft}
                onChange={updatePermission}
              />
            ) : (
              <div className="rounded-md border border-dashed p-8 text-center text-sm text-muted-foreground">
                ยังไม่มีผู้ใช้ให้กำหนดสิทธิ์
              </div>
            )}
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

function PermissionMatrix({
  user,
  draft,
  onChange,
}: {
  user: User
  draft: PermissionDraft
  onChange: (menuKey: string, field: PermissionAction, checked: boolean) => void
}) {
  return (
    <div className="overflow-hidden rounded-lg border">
      <div className="max-h-[calc(100vh-280px)] overflow-auto">
        <Table>
          <TableHeader className="sticky top-0 z-10 bg-card">
            <TableRow className="bg-muted/40">
              <TableHead className="min-w-[260px]">เมนู</TableHead>
              {Object.entries(ACTION_LABEL).map(([key, label]) => (
                <TableHead key={key} className="w-[92px] text-center">
                  {label}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {MENU_GROUPS.map((group) => (
              <PermissionGroupRows
                key={group.label}
                group={group}
                user={user}
                draft={draft}
                onChange={onChange}
              />
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function PermissionGroupRows({
  group,
  user,
  draft,
  onChange,
}: {
  group: NavGroup
  user: User
  draft: PermissionDraft
  onChange: (menuKey: string, field: PermissionAction, checked: boolean) => void
}) {
  return (
    <>
      <TableRow className="bg-muted/20">
        <TableCell colSpan={5} className="h-8 py-1 text-xs font-semibold text-muted-foreground">
          {group.label}
        </TableCell>
      </TableRow>
      {group.items.map((item) => (
        <PermissionRow
          key={item.menuKey}
          item={item}
          user={user}
          permission={draft[item.menuKey] ?? permissionForMenu(user, item.menuKey) ?? emptyPermission(item.menuKey)}
          onChange={onChange}
        />
      ))}
    </>
  )
}

function PermissionRow({
  item,
  user,
  permission,
  onChange,
}: {
  item: NavItem
  user: User
  permission: UserMenuPermission
  onChange: (menuKey: string, field: PermissionAction, checked: boolean) => void
}) {
  const canEditView = !(user.role === 'admin' && item.menuKey === 'settings_users')
  const actionDisabled = !permission.can_view
  return (
    <TableRow>
      <TableCell>
        <div className="font-medium text-foreground">{item.label}</div>
        <div className="mt-0.5 text-xs text-muted-foreground">{item.hint || item.to}</div>
      </TableCell>
      <PermissionCheckbox
        label={`${item.label}: เข้าเมนู`}
        checked={permission.can_view}
        disabled={!canEditView}
        onCheckedChange={(checked) => onChange(item.menuKey, 'can_view', checked)}
      />
      <PermissionCheckbox
        label={`${item.label}: เพิ่ม`}
        checked={permission.can_create}
        disabled={actionDisabled}
        onCheckedChange={(checked) => onChange(item.menuKey, 'can_create', checked)}
      />
      <PermissionCheckbox
        label={`${item.label}: แก้ไข`}
        checked={permission.can_update}
        disabled={actionDisabled}
        onCheckedChange={(checked) => onChange(item.menuKey, 'can_update', checked)}
      />
      <PermissionCheckbox
        label={`${item.label}: ลบ`}
        checked={permission.can_delete}
        disabled={actionDisabled}
        onCheckedChange={(checked) => onChange(item.menuKey, 'can_delete', checked)}
      />
    </TableRow>
  )
}

function PermissionCheckbox({
  label,
  checked,
  disabled,
  onCheckedChange,
}: {
  label: string
  checked: boolean
  disabled?: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <TableCell className="text-center">
      <Checkbox
        checked={checked}
        disabled={disabled}
        onCheckedChange={(value) => onCheckedChange(value === true)}
        aria-label={label}
        className="mx-auto"
      />
    </TableCell>
  )
}

function permissionsForUser(user: User): UserMenuPermission[] {
  const seen = new Set<string>()
  const permissions: UserMenuPermission[] = []
  for (const group of MENU_GROUPS) {
    for (const item of group.items) {
      if (seen.has(item.menuKey)) continue
      seen.add(item.menuKey)
      permissions.push(permissionForMenu(user, item.menuKey) ?? emptyPermission(item.menuKey))
    }
  }
  return permissions
}

function toPermissionDraft(permissions: UserMenuPermission[]): PermissionDraft {
  return Object.fromEntries(permissions.map((p) => [p.menu_key, p]))
}

function samePermissions(a: PermissionDraft, b: UserMenuPermission[]): boolean {
  for (const permission of b) {
    const current = a[permission.menu_key]
    if (!current) return false
    if (
      current.can_view !== permission.can_view ||
      current.can_create !== permission.can_create ||
      current.can_update !== permission.can_update ||
      current.can_delete !== permission.can_delete
    ) {
      return false
    }
  }
  return true
}

function emptyPermission(menuKey: string): UserMenuPermission {
  return {
    menu_key: menuKey,
    can_view: false,
    can_create: false,
    can_update: false,
    can_delete: false,
  }
}
