import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import api from '@/api/client'
import { cn } from '@/lib/utils'

interface AuthImageProps {
  src?: string
  alt?: string
  className?: string
  imgClassName?: string
  fallback?: ReactNode
  children?: ReactNode
}

export function AuthImage({
  src,
  alt = '',
  className,
  imgClassName,
  fallback,
  children,
}: AuthImageProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const [visible, setVisible] = useState(false)
  const [blobURL, setBlobURL] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    setBlobURL((current) => {
      if (current) URL.revokeObjectURL(current)
      return null
    })
    setFailed(false)
    setVisible(false)
  }, [src])

  useEffect(() => {
    if (!src) return
    const node = containerRef.current
    if (!node || typeof IntersectionObserver === 'undefined') {
      setVisible(true)
      return
    }
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) {
          setVisible(true)
          observer.disconnect()
        }
      },
      { rootMargin: '160px' },
    )
    observer.observe(node)
    return () => observer.disconnect()
  }, [src])

  useEffect(() => {
    if (!src || !visible || blobURL || failed) return
    let cancelled = false
    api
      .get(src, { responseType: 'blob' })
      .then((res) => {
        if (cancelled) return
        const contentType = (res.headers['content-type'] ?? '').toString()
        const blob = new Blob([res.data as Blob], { type: contentType || 'application/octet-stream' })
        setBlobURL(URL.createObjectURL(blob))
      })
      .catch(() => {
        if (!cancelled) setFailed(true)
      })
    return () => {
      cancelled = true
    }
  }, [src, visible, blobURL, failed])

  useEffect(() => {
    return () => {
      if (blobURL) URL.revokeObjectURL(blobURL)
    }
  }, [blobURL])

  return (
    <div ref={containerRef} className={cn('relative overflow-hidden', className)}>
      {blobURL && !failed ? (
        <img
          src={blobURL}
          alt={alt}
          className={cn('h-full w-full object-cover', imgClassName)}
          loading="lazy"
          onError={() => setFailed(true)}
        />
      ) : (
        fallback
      )}
      {children}
    </div>
  )
}
