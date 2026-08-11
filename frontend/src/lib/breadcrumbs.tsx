import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { useLocation, matchPath } from 'react-router-dom'

export interface Crumb {
  label: string
  href?: string
}

interface BreadcrumbDef {
  label: string
  href?: string
  dynamic?: boolean
}

const ROUTES: Array<{ pattern: string; crumbs: BreadcrumbDef[] }> = [
  { pattern: '/dashboard', crumbs: [{ label: 'ภาพรวมแพลตฟอร์ม' }, { label: 'ยอดขายตามแพลตฟอร์ม' }] },
  { pattern: '/nextstep-marketplace', crumbs: [{ label: 'ภาพรวมแพลตฟอร์ม', href: '/dashboard' }, { label: 'NextStep Marketplace' }] },
  { pattern: '/setup', crumbs: [{ label: 'เชื่อมต่อแพลตฟอร์ม' }, { label: 'สถานะพร้อมใช้งาน' }] },
  { pattern: '/sales-orders', crumbs: [{ label: 'ออเดอร์และเอกสาร' }, { label: 'ใบสั่งขาย' }] },
  { pattern: '/sale-invoices', crumbs: [{ label: 'ออเดอร์และเอกสาร' }, { label: 'ขายสินค้าและบริการ' }] },
  {
    pattern: '/sales-orders/:id',
    crumbs: [{ label: 'ออเดอร์และเอกสาร' }, { label: 'ใบสั่งขาย', href: '/sales-orders' }, { label: ':id', dynamic: true }],
  },
  {
    pattern: '/sale-invoices/:id',
    crumbs: [{ label: 'ออเดอร์และเอกสาร' }, { label: 'ขายสินค้าและบริการ', href: '/sale-invoices' }, { label: ':id', dynamic: true }],
  },
  {
    pattern: '/import',
    crumbs: [{ label: 'นำเข้าและรับชำระ' }, { label: 'นำเข้า Marketplace' }],
  },
  {
    pattern: '/import/lazada',
    crumbs: [{ label: 'นำเข้าและรับชำระ' }, { label: 'Lazada Excel' }],
  },
  {
    pattern: '/import/shopee',
    crumbs: [{ label: 'นำเข้าและรับชำระ' }, { label: 'นำเข้า Shopee' }],
  },
  {
    pattern: '/shopee-operations',
    crumbs: [{ label: 'ออเดอร์และเอกสาร' }, { label: 'คำสั่งซื้อ Shopee' }],
  },
  {
    pattern: '/import/tiktok',
    crumbs: [{ label: 'นำเข้าและรับชำระ' }, { label: 'TikTok Excel' }],
  },
  {
    pattern: '/messages',
    crumbs: [{ label: 'ลูกค้าและ LINE' }, { label: 'ข้อความลูกค้า' }],
  },
  { pattern: '/mappings', crumbs: [{ label: 'สินค้าและการจับคู่' }, { label: 'ตารางจับคู่สินค้า' }] },
  {
    pattern: '/marketplace-aliases',
    crumbs: [{ label: 'สินค้าและการจับคู่' }, { label: 'การจับคู่สินค้า' }],
  },
  { pattern: '/settings', crumbs: [{ label: 'เชื่อมต่อแพลตฟอร์ม' }, { label: 'ตั้งค่าทั่วไป' }] },
  {
    pattern: '/settings/catalog',
    crumbs: [{ label: 'สินค้าและการจับคู่' }, { label: 'สินค้าใน SML' }],
  },
  {
    pattern: '/settings/channels',
    crumbs: [{ label: 'เชื่อมต่อแพลตฟอร์ม' }, { label: 'เส้นทางเอกสาร SML' }],
  },
  {
    pattern: '/settings/shopee-connections',
    crumbs: [{ label: 'เชื่อมต่อแพลตฟอร์ม' }, { label: 'ร้าน Shopee' }],
  },
  {
    pattern: '/settings/shopee-stock',
    crumbs: [{ label: 'สินค้าและการจับคู่' }, { label: 'ซิงก์สต๊อก Shopee' }],
  },
  {
    pattern: '/settings/instance',
    crumbs: [{ label: 'เชื่อมต่อแพลตฟอร์ม' }, { label: 'ข้อมูลร้านและการเชื่อมต่อ' }],
  },
  {
    pattern: '/settings/line-notifications',
    crumbs: [{ label: 'ลูกค้าและ LINE' }, { label: 'LINE แจ้งเตือน' }],
  },
  {
    pattern: '/settings/line-myshop',
    crumbs: [{ label: 'ลูกค้าและ LINE' }, { label: 'LINE MyShop' }],
  },
  {
    pattern: '/settings/line-oa',
    crumbs: [{ label: 'ลูกค้าและ LINE' }, { label: 'บัญชี LINE OA' }],
  },
  {
    pattern: '/settings/quick-replies',
    crumbs: [{ label: 'ลูกค้าและ LINE' }, { label: 'ข้อความสำเร็จรูป' }],
  },
  {
    pattern: '/settings/chat-tags',
    crumbs: [{ label: 'ลูกค้าและ LINE' }, { label: 'ป้ายลูกค้า' }],
  },
  {
    pattern: '/settings/old-data',
    crumbs: [{ label: 'ดูแลระบบ' }, { label: 'จัดการข้อมูลเก่า' }],
  },
  {
    pattern: '/settings/users',
    crumbs: [{ label: 'ดูแลระบบ' }, { label: 'ผู้ใช้ระบบ' }],
  },
  { pattern: '/logs', crumbs: [{ label: 'ดูแลระบบ' }, { label: 'ประวัติการทำงาน' }] },
  { pattern: '/bulk-send-jobs', crumbs: [{ label: 'ออเดอร์และเอกสาร' }, { label: 'งานส่งเข้า SML' }] },
  { pattern: '/shopee-settlements', crumbs: [{ label: 'นำเข้าและรับชำระ' }, { label: 'รับชำระ Shopee' }] },
]

interface CtxValue {
  dynamic: Record<string, string>
  setDynamicLabel: (key: string, label: string) => void
}

const Ctx = createContext<CtxValue | null>(null)

export function BreadcrumbProvider({ children }: { children: React.ReactNode }) {
  const [dynamic, setDynamic] = useState<Record<string, string>>({})
  const setDynamicLabel = (key: string, label: string) =>
    setDynamic((p) => (p[key] === label ? p : { ...p, [key]: label }))
  return (
    <Ctx.Provider value={{ dynamic, setDynamicLabel }}>{children}</Ctx.Provider>
  )
}

export function useDynamicCrumb(key: string, label: string | undefined | null) {
  const ctx = useContext(Ctx)
  useEffect(() => {
    if (label && ctx) ctx.setDynamicLabel(key, label)
  }, [ctx, key, label])
}

export function useCrumbs(): Crumb[] {
  const { pathname } = useLocation()
  const ctx = useContext(Ctx)

  return useMemo(() => {
    for (const r of ROUTES) {
      const match = matchPath(r.pattern, pathname)
      if (!match) continue
      return r.crumbs.map((c) => {
        if (!c.dynamic) return { label: c.label, href: c.href }
        const key = c.label.replace(':', '')
        const dynLabel =
          (ctx?.dynamic[key]) ?? match.params[key]?.slice(0, 8) ?? key
        return { label: dynLabel }
      })
    }
    return []
  }, [pathname, ctx])
}
