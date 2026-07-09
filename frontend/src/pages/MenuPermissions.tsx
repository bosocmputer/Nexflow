import { useEffect, useMemo, useState } from 'react'
import { Save, ShieldCheck, UsersRound } from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { PageHeader } from '@/components/common/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
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

type PermissionDraft = Record<string, UserMenuPermission>

const ADMIN_LOCKED_MENU_KEYS = new Set(['settings_users', 'settings_menu_permissions'])
const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

const ROLE_LABEL: Record<User['role'], string> = {
  admin: 'ผู้ดูแลระบบ',
  staff: 'พนักงาน',
  viewer: 'ดูข้อมูลอย่างเดียว',
}

const MENU_GROUPS = NAV_GROUPS
  .map((group) => ({
    ...group,
    items: group.items.filter((item) => item.enabled !== false && (!item.minPhase || PHASE >= item.minPhase)),
  }))
  .filter((group) => group.items.length > 0)

export default function MenuPermissions() {
  const { user: currentUser, setUser } = useAuth()
  const [users, setUsers] = useState<User[]>([])
  const [selectedUserId, setSelectedUserId] = useState('')
  const [draft, setDraft] = useState<PermissionDraft>({})
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  const sortedUsers = useMemo(
    () => [...users].sort((a, b) => a.role.localeCompare(b.role) || a.email.localeCompare(b.email)),
    [users],
  )
  const selectedUser = sortedUsers.find((u) => u.id === selectedUserId) ?? sortedUsers[0]
  const permissions = selectedUser ? permissionsForUser(selectedUser) : []
  const selectedChanged = selectedUser ? !sameViewPermissions(draft, permissions) : false
  const allVisible = permissions.length > 0 && permissions.every((p) => (draft[p.menu_key] ?? p).can_view)
  const someVisible = permissions.some((p) => (draft[p.menu_key] ?? p).can_view)
  const visibleCount = permissions.filter((p) => (draft[p.menu_key] ?? p).can_view).length

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
      setDraft({})
      return
    }
    setDraft(toPermissionDraft(permissionsForUser(selectedUser)))
  }, [selectedUser?.id, selectedUser?.role, selectedUser?.menu_permissions])

  const updateMenu = (menuKey: string, checked: boolean) => {
    if (!selectedUser) return
    setDraft((current) => {
      const existing = current[menuKey] ?? permissionForMenu(selectedUser, menuKey) ?? emptyPermission(menuKey)
      const next = withView(existing, selectedUser, menuKey, checked)
      return { ...current, [menuKey]: next }
    })
  }

  const updateGroup = (group: NavGroup, checked: boolean) => {
    if (!selectedUser) return
    setDraft((current) => {
      const next = { ...current }
      for (const item of group.items) {
        const existing = current[item.menuKey] ?? permissionForMenu(selectedUser, item.menuKey) ?? emptyPermission(item.menuKey)
        next[item.menuKey] = withView(existing, selectedUser, item.menuKey, checked)
      }
      return next
    })
  }

  const updateAll = (checked: boolean) => {
    if (!selectedUser) return
    setDraft((current) => {
      const next = { ...current }
      for (const p of permissionsForUser(selectedUser)) {
        next[p.menu_key] = withView(current[p.menu_key] ?? p, selectedUser, p.menu_key, checked)
      }
      return next
    })
  }

  const save = async () => {
    if (!selectedUser) return
    setSaving(true)
    try {
      const nextPermissions = permissionsForUser(selectedUser).map((base) => draft[base.menu_key] ?? base)
      const res = await client.put<{ data: UserMenuPermission[] }>(
        `/api/settings/users/${selectedUser.id}/menu-permissions`,
        { permissions: nextPermissions },
      )
      const saved = res.data.data ?? []
      setUsers((current) => current.map((u) => (
        u.id === selectedUser.id ? { ...u, menu_permissions: saved } : u
      )))
      setDraft(toPermissionDraft(saved))
      if (currentUser?.id === selectedUser.id) {
        setUser({ ...currentUser, menu_permissions: saved })
      }
      toast.success('บันทึกสิทธิ์เมนูแล้ว')
    } catch (err: any) {
      toast.error(err.response?.data?.error ?? 'บันทึกสิทธิ์เมนูไม่สำเร็จ')
    } finally {
      setSaving(false)
    }
  }

  if (currentUser?.role !== 'admin') {
    return (
      <div className="p-6">
        <PageHeader title="สิทธิ์เมนู" description="เฉพาะผู้ดูแลระบบเท่านั้น" />
      </div>
    )
  }

  return (
    <div className="space-y-5 p-6">
      <PageHeader
        title="สิทธิ์เมนู"
        description="เลือก user แล้วกำหนดว่าแต่ละคนเห็นเมนูไหนใน Sidebar, Command Palette และ URL ตรง"
        actions={
          <Button type="button" onClick={save} disabled={!selectedUser || saving || !selectedChanged}>
            <Save className="mr-2 h-4 w-4" />
            {saving ? 'กำลังบันทึก...' : 'บันทึกสิทธิ์'}
          </Button>
        }
      />

      <div className="grid gap-5 xl:grid-cols-[360px_minmax(0,1fr)]">
        <div className="overflow-hidden rounded-lg border bg-card">
          <div className="border-b px-4 py-3">
            <div className="flex items-center gap-2 font-semibold">
              <UsersRound className="h-4 w-4 text-accent-strong" />
              เลือกผู้ใช้
            </div>
          </div>
          <div className="max-h-[calc(100vh-220px)] overflow-auto">
            {loading ? (
              <div className="p-6 text-center text-sm text-muted-foreground">กำลังโหลด...</div>
            ) : sortedUsers.length === 0 ? (
              <div className="p-6 text-center text-sm text-muted-foreground">ยังไม่มีผู้ใช้</div>
            ) : (
              sortedUsers.map((u) => (
                <button
                  key={u.id}
                  type="button"
                  onClick={() => setSelectedUserId(u.id)}
                  className={cn(
                    'flex w-full items-center justify-between gap-3 border-b px-4 py-3 text-left last:border-b-0 hover:bg-muted/45',
                    selectedUser?.id === u.id && 'bg-primary/5',
                  )}
                >
                  <span className="min-w-0">
                    <span className="block truncate font-medium text-foreground">{u.name}</span>
                    <span className="block truncate text-xs text-muted-foreground">{u.email}</span>
                  </span>
                  <Badge variant={u.role === 'admin' ? 'default' : 'secondary'} className="shrink-0 gap-1">
                    {u.role === 'admin' && <ShieldCheck className="h-3 w-3" />}
                    {ROLE_LABEL[u.role]}
                  </Badge>
                </button>
              ))
            )}
          </div>
        </div>

        <Card className="shadow-none">
          <CardContent className="space-y-4 p-4">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
              <div className="min-w-0">
                <div className="text-base font-semibold text-foreground">
                  {selectedUser ? selectedUser.name : 'เลือกผู้ใช้'}
                </div>
                <div className="mt-1 text-sm text-muted-foreground">
                  {selectedUser ? `${selectedUser.email} · เปิด ${visibleCount}/${permissions.length} เมนู` : 'ยังไม่มีผู้ใช้ให้กำหนดสิทธิ์'}
                </div>
              </div>
              {selectedUser && (
                <label className="flex select-none items-center gap-2 rounded-md border bg-background px-3 py-2 text-sm">
                  <Checkbox
                    checked={allVisible ? true : someVisible ? 'indeterminate' : false}
                    onCheckedChange={(value) => updateAll(value === true)}
                    aria-label="เลือกเมนูทั้งหมด"
                  />
                  เลือกทั้งหมด
                </label>
              )}
            </div>

            <div className="rounded-md border border-info/20 bg-info/[0.04] px-3 py-2 text-xs leading-5 text-muted-foreground">
              ตารางนี้แสดงเฉพาะสิทธิ์ <span className="font-medium text-foreground">เข้าเมนู</span> ก่อน
              ส่วนสิทธิ์ เพิ่ม แก้ไข ลบ ยังเก็บอยู่ในระบบสำหรับ phase ถัดไป แต่ไม่แสดงให้ user สับสนในรอบนี้
            </div>

            {selectedUser ? (
              <PermissionTable
                user={selectedUser}
                draft={draft}
                onMenuChange={updateMenu}
                onGroupChange={updateGroup}
              />
            ) : (
              <div className="rounded-md border border-dashed p-8 text-center text-sm text-muted-foreground">
                ยังไม่มีผู้ใช้ให้กำหนดสิทธิ์
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function PermissionTable({
  user,
  draft,
  onMenuChange,
  onGroupChange,
}: {
  user: User
  draft: PermissionDraft
  onMenuChange: (menuKey: string, checked: boolean) => void
  onGroupChange: (group: NavGroup, checked: boolean) => void
}) {
  return (
    <div className="overflow-hidden rounded-lg border">
      <div className="max-h-[calc(100vh-330px)] overflow-auto">
        <Table>
          <TableHeader className="sticky top-0 z-10 bg-card">
            <TableRow className="bg-muted/40">
              <TableHead>เมนู</TableHead>
              <TableHead className="w-[132px] text-center">เข้าเมนู</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {MENU_GROUPS.map((group) => (
              <PermissionGroupRows
                key={group.label}
                group={group}
                user={user}
                draft={draft}
                onMenuChange={onMenuChange}
                onGroupChange={onGroupChange}
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
  onMenuChange,
  onGroupChange,
}: {
  group: NavGroup
  user: User
  draft: PermissionDraft
  onMenuChange: (menuKey: string, checked: boolean) => void
  onGroupChange: (group: NavGroup, checked: boolean) => void
}) {
  const groupPermissions = group.items.map((item) => (
    draft[item.menuKey] ?? permissionForMenu(user, item.menuKey) ?? emptyPermission(item.menuKey)
  ))
  const allGroupVisible = groupPermissions.every((p) => p.can_view)
  const someGroupVisible = groupPermissions.some((p) => p.can_view)

  return (
    <>
      <TableRow className="bg-muted/20">
        <TableCell className="h-9 py-1 text-xs font-semibold text-muted-foreground">
          {group.label}
        </TableCell>
        <TableCell className="text-center">
          <Checkbox
            checked={allGroupVisible ? true : someGroupVisible ? 'indeterminate' : false}
            onCheckedChange={(value) => onGroupChange(group, value === true)}
            aria-label={`เลือกทั้งหมดในหมวด ${group.label}`}
            className="mx-auto"
          />
        </TableCell>
      </TableRow>
      {group.items.map((item) => (
        <PermissionRow
          key={item.menuKey}
          item={item}
          user={user}
          permission={draft[item.menuKey] ?? permissionForMenu(user, item.menuKey) ?? emptyPermission(item.menuKey)}
          onChange={onMenuChange}
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
  onChange: (menuKey: string, checked: boolean) => void
}) {
  const locked = user.role === 'admin' && ADMIN_LOCKED_MENU_KEYS.has(item.menuKey)
  return (
    <TableRow>
      <TableCell>
        <div className="font-medium text-foreground">{item.label}</div>
        <div className="mt-0.5 text-xs text-muted-foreground">{item.hint || item.to}</div>
      </TableCell>
      <TableCell className="text-center">
        <Checkbox
          checked={permission.can_view}
          disabled={locked}
          onCheckedChange={(value) => onChange(item.menuKey, value === true)}
          aria-label={`${item.label}: เข้าเมนู`}
          className="mx-auto"
        />
      </TableCell>
    </TableRow>
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

function sameViewPermissions(a: PermissionDraft, b: UserMenuPermission[]): boolean {
  for (const permission of b) {
    const current = a[permission.menu_key]
    if (!current || current.can_view !== permission.can_view) return false
  }
  return true
}

function withView(permission: UserMenuPermission, user: User, menuKey: string, canView: boolean): UserMenuPermission {
  const next: UserMenuPermission = { ...permission, menu_key: menuKey, can_view: canView }
  if (user.role === 'admin' && ADMIN_LOCKED_MENU_KEYS.has(menuKey)) {
    next.can_view = true
  }
  if (!next.can_view) {
    next.can_create = false
    next.can_update = false
    next.can_delete = false
  }
  return next
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
