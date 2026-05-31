import { useState, useEffect } from 'react'
import api from '@/api/client'
import type { BillItem } from '@/types'

// ── Module-level cache — shared across ALL ItemRow instances ───────────────────
// Must remain module-scoped (not inside the hook) so rows sharing the same
// item_code only fetch once and subsequent renders get instant results.
export const catalogMetaCache = new Map<
  string,
  { item_name: string; unit_code?: string }
>()

export interface MatchInfo {
  itemName: string | null
  score: number | null   // 0..1, null = user-picked code outside candidates
}

export function useMatchInfo(item: BillItem): MatchInfo {
  const code = item.item_code ?? ''
  const candidate = (item.candidates ?? []).find((c) => c.item_code === code)

  const [fetched, setFetched] = useState<{
    item_name: string
  } | null>(() =>
    code && catalogMetaCache.has(code) ? (catalogMetaCache.get(code) ?? null) : null,
  )

  useEffect(() => {
    if (!code || candidate) return
    if (catalogMetaCache.has(code)) {
      setFetched(catalogMetaCache.get(code) ?? null)
      return
    }
    let cancelled = false
    api
      .get<{ item_name: string }>(
        `/api/catalog/${encodeURIComponent(code)}`,
      )
      .then((res) => {
        if (cancelled) return
        const meta = { item_name: res.data.item_name }
        catalogMetaCache.set(code, meta)
        setFetched(meta)
      })
      .catch(() => {
        /* code not in catalog (user-typed?) — leave blank */
      })
    return () => {
      cancelled = true
    }
  }, [code, candidate])

  if (candidate) {
    return {
      itemName: candidate.item_name,
      score: candidate.score,
    }
  }

  return {
    itemName: fetched?.item_name ?? null,
    score: null,
  }
}
