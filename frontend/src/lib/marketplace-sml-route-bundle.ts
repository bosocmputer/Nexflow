export type MarketplaceSMLRouteBundleKind = 'shopee' | 'tiktok_shop'
export type MarketplaceCancellationRoute = 'saleinvoicecancel' | 'creditnote' | 'saleordercancel'

export interface MarketplaceSMLRouteBundleConfig {
  apiBase: string
  mainChannel: 'shopee_realtime' | 'tiktok_shop'
  cancelChannel: 'shopee_realtime_cancel' | 'tiktok_shop_cancel'
  platformLabel: string
  title: string
  description: string
  mainHeading: string
  cancelHeading: string
  saveSuccess: string
  saleInvoiceCancellationRoutes: MarketplaceCancellationRoute[]
}

const CONFIG: Record<MarketplaceSMLRouteBundleKind, MarketplaceSMLRouteBundleConfig> = {
  shopee: {
    apiBase: '/api/settings/shopee-sml-route-bundle',
    mainChannel: 'shopee_realtime',
    cancelChannel: 'shopee_realtime_cancel',
    platformLabel: 'Shopee',
    title: 'ตั้งค่าเอกสาร SML สำหรับคำสั่งซื้อ Shopee',
    description: 'ตั้งค่าเอกสารหลักและเอกสารเมื่อยกเลิกพร้อมกัน เพื่อให้ทั้งสองเส้นทางอ้างอิงกันถูกต้อง',
    mainHeading: '1. เมื่อคำสั่งซื้อพร้อมส่ง',
    cancelHeading: '2. เมื่อ Shopee ยกเลิกคำสั่งซื้อ',
    saveSuccess: 'บันทึกเส้นทาง Shopee แล้ว ระบบอัตโนมัติที่เปิดอยู่ถูกพักเพื่อให้ตรวจสอบก่อนเปิดใหม่',
    saleInvoiceCancellationRoutes: ['creditnote', 'saleinvoicecancel'],
  },
  tiktok_shop: {
    apiBase: '/api/settings/tiktok-shop-sml-route-bundle',
    mainChannel: 'tiktok_shop',
    cancelChannel: 'tiktok_shop_cancel',
    platformLabel: 'TikTok Shop',
    title: 'ตั้งค่าเอกสาร SML สำหรับคำสั่งซื้อ TikTok Shop',
    description: 'ตั้งค่าเอกสารหลักและเอกสารเมื่อ TikTok Shop ยกเลิกพร้อมกัน โดยยังไม่เปิดการสร้างเอกสารยกเลิกอัตโนมัติ',
    mainHeading: '1. เมื่อคำสั่งซื้อพร้อมสร้างเอกสาร',
    cancelHeading: '2. เมื่อ TikTok Shop ยกเลิกคำสั่งซื้อ',
    saveSuccess: 'บันทึกเส้นทาง TikTok Shop แล้ว ระบบอัตโนมัติที่เปิดอยู่ถูกพักเพื่อให้ตรวจสอบก่อนเปิดใหม่',
    saleInvoiceCancellationRoutes: ['saleinvoicecancel'],
  },
}

export function marketplaceSMLRouteBundleConfig(kind: MarketplaceSMLRouteBundleKind) {
  return CONFIG[kind]
}
