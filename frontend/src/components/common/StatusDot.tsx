import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const dotVariants = cva('inline-block rounded-full', {
  variants: {
    variant: {
      success: 'bg-success',
      warning: 'bg-warning',
      danger: 'bg-destructive',
      info: 'bg-info',
      muted: 'bg-muted-foreground',
      primary: 'bg-primary',
    },
    size: {
      sm: 'h-1.5 w-1.5',
      md: 'h-2 w-2',
      lg: 'h-2.5 w-2.5',
    },
  },
  defaultVariants: { variant: 'muted', size: 'md' },
})

const containerVariants = cva('inline-flex items-center gap-2 text-sm', {
  variants: {
    variant: {
      success: 'text-success',
      warning: 'text-warning',
      danger: 'text-destructive',
      info: 'text-info',
      muted: 'text-muted-foreground',
      primary: 'text-foreground',
    },
  },
  defaultVariants: { variant: 'muted' },
})

export interface StatusDotProps
  extends VariantProps<typeof dotVariants> {
  label?: string
  className?: string
  pulse?: boolean
}

export function StatusDot({ variant, size, label, className, pulse }: StatusDotProps) {
  return (
    <span className={cn(containerVariants({ variant }), className)}>
      <span className="relative inline-flex">
        <span className={cn(dotVariants({ variant, size }))} />
        {pulse && (
          <span
            className={cn(
              dotVariants({ variant, size }),
              'absolute inset-0 animate-ping opacity-60',
            )}
          />
        )}
      </span>
      {label && <span className="leading-none">{label}</span>}
    </span>
  )
}
