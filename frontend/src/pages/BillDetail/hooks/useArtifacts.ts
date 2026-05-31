import { useState, useEffect } from 'react'
import api from '@/api/client'
import type { EmailPrintEvent } from '@/types'

export interface BillArtifact {
  id: string
  bill_id: string
  kind: string
  filename: string
  content_type?: string
  size_bytes: number
  sha256?: string
  source_meta?: Record<string, unknown>
  created_at: string
}

export function useArtifacts(billId: string) {
  const [items, setItems] = useState<BillArtifact[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true
    api
      .get<{ data: BillArtifact[] }>(`/api/bills/${billId}/artifacts`)
      .then((r) => {
        if (alive) setItems(r.data.data ?? [])
      })
      .catch(() => {
        if (alive) setItems([])
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [billId])

  return { items, loading }
}

async function fetchArtifactBlob(
  billID: string,
  artID: string,
  filename: string,
  mode: 'preview' | 'download',
): Promise<Blob> {
  const res = await api.get(
    `/api/bills/${billID}/artifacts/${artID}/${mode}`,
    { responseType: 'blob' },
  )
  // Some browsers / axios builds drop the `charset=utf-8` parameter when
  // building the Blob from the response, leaving plain `text/html` —
  // that's enough to make the new tab default to Latin-1 and mangle Thai.
  // Reconstruct the Blob with the full Content-Type from the response
  // header (or fall back to a UTF-8 default for text-y files).
  const original = res.data as Blob
  const headerCT = (res.headers['content-type'] ?? '').toString()
  const fallbackCT =
    original.type ||
    (filename.endsWith('.html')
      ? 'text/html; charset=utf-8'
      : filename.endsWith('.json')
        ? 'application/json; charset=utf-8'
        : filename.endsWith('.txt')
          ? 'text/plain; charset=utf-8'
          : 'application/octet-stream')
  return new Blob([original], { type: headerCT || fallbackCT })
}

// Fetch artifact through the authenticated axios client and hand the result off
// as a blob URL — needed because <a target="_blank"> can't attach Authorization
// headers, and we don't want to leak the JWT into query strings.
export async function openArtifact(
  billID: string,
  artID: string,
  filename: string,
  mode: 'preview' | 'download',
): Promise<void> {
  try {
    const blob = await fetchArtifactBlob(billID, artID, filename, mode)
    const blobURL = URL.createObjectURL(blob)
    if (mode === 'download') {
      const a = document.createElement('a')
      a.href = blobURL
      a.download = filename
      document.body.appendChild(a)
      a.click()
      a.remove()
      // Tab is still alive so it's safe to revoke once download has started.
      setTimeout(() => URL.revokeObjectURL(blobURL), 2000)
    } else {
      window.open(blobURL, '_blank', 'noopener')
      // Don't revoke immediately — the new tab needs the URL alive.
      setTimeout(() => URL.revokeObjectURL(blobURL), 60_000)
    }
  } catch (err) {
    console.error('artifact open failed', err)
  }
}

export async function printArtifact(
  billID: string,
  artID: string,
  filename: string,
): Promise<void> {
  const blob = await fetchArtifactBlob(billID, artID, filename, 'preview')
  const blobURL = URL.createObjectURL(blob)
  const iframe = document.createElement('iframe')
  iframe.src = blobURL
  iframe.title = filename
  iframe.style.position = 'fixed'
  iframe.style.left = '-10000px'
  iframe.style.top = '0'
  iframe.style.width = '1px'
  iframe.style.height = '1px'
  iframe.style.border = '0'
  iframe.style.opacity = '0'

  await new Promise<void>((resolve, reject) => {
    let cleaned = false
    const cleanup = () => {
      if (cleaned) return
      cleaned = true
      URL.revokeObjectURL(blobURL)
      iframe.remove()
    }
    iframe.onload = () => {
      const win = iframe.contentWindow
      if (!win) {
        cleanup()
        reject(new Error('ไม่สามารถเปิดหน้าต่างพิมพ์ได้'))
        return
      }
      win.onafterprint = cleanup
      setTimeout(cleanup, 60_000)
      setTimeout(() => {
        try {
          win.focus()
          win.print()
          resolve()
        } catch (err) {
          cleanup()
          reject(err)
        }
      }, 100)
    }
    iframe.onerror = () => {
      cleanup()
      reject(new Error('โหลดไฟล์สำหรับพิมพ์ไม่สำเร็จ'))
    }
    document.body.appendChild(iframe)
  })
}

export async function recordArtifactPrint(
  billID: string,
  artID: string,
): Promise<EmailPrintEvent> {
  const res = await api.post<{ data: EmailPrintEvent }>(
    `/api/bills/${billID}/artifacts/${artID}/print-events`,
  )
  return res.data.data
}
