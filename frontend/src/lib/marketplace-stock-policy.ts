export type MarketplaceStockPolicy = 'managed' | 'zeroing' | 'disabled_zero' | 'manual_unmanaged' | 'blocked'

// A new or safety-blocked mapping follows the normal managed workflow. Policies
// explicitly chosen by a user are preserved and only changed from Advanced.
export function editableMarketplaceStockPolicy(policy?: MarketplaceStockPolicy): MarketplaceStockPolicy {
  return !policy || policy === 'blocked' ? 'managed' : policy
}

type PauseCopy = { title: string; description: string }

const PAUSE_REASON_COPY: Record<string, PauseCopy> = {
  catalog_sync_in_progress: {
    title: 'กำลังอัปเดตข้อมูลสินค้า',
    description: 'รอให้อัปเดตเสร็จ แล้วกดตรวจสอบสต๊อกอีกครั้ง',
  },
  catalog_generation_reconcile: {
    title: 'ข้อมูลสินค้าเปลี่ยนแล้ว',
    description: 'กดตรวจสอบสต๊อกเพื่อยืนยันข้อมูลล่าสุดก่อนเปิดซิงก์',
  },
  marketplace_mapping_reconcile: {
    title: 'กำลังเตรียมการจับคู่สินค้า',
    description: 'รอให้งานเสร็จ แล้วกดตรวจสอบสต๊อกอีกครั้ง',
  },
  set_definition_changed: {
    title: 'ส่วนประกอบสินค้าเปลี่ยนแล้ว',
    description: 'กดตรวจสอบสต๊อกเพื่อคำนวณจากส่วนประกอบล่าสุด',
  },
  single_location_required: {
    title: 'กรุณาเลือกคลังและพื้นที่เก็บ',
    description: 'เลือก 1 คลังและ 1 พื้นที่เก็บที่ต้องการใช้คำนวณสต๊อก',
  },
  warehouse_scope_required: {
    title: 'กรุณาเลือกขอบเขตสต๊อก',
    description: 'เลือกคลังและพื้นที่เก็บก่อนตรวจสอบสต๊อก',
  },
  mass_zero_drop: {
    title: 'ยอดสต๊อกลดลงเป็นศูนย์หลายรายการ',
    description: 'ระบบหยุดไว้ให้ตรวจรายการและยอดจาก SML ก่อนส่งไป Shopee',
  },
}

export function marketplaceStockPauseCopy(reason?: string): PauseCopy {
  const key = reason?.trim() ?? ''
  return PAUSE_REASON_COPY[key] ?? {
    title: 'รอตรวจสอบก่อนซิงก์',
    description: 'กดตรวจสอบสต๊อกเพื่อยืนยันข้อมูลล่าสุด หากยังไม่ผ่านให้ตรวจรายการในแท็บ “ต้องแก้”',
  }
}
