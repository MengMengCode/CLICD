import { LoaderCircle, Monitor, Server } from 'lucide-react'
import { cn } from '../lib/utils'
import { Badge } from './ui/badge'

export type WorkloadState = 'running' | 'stopped' | 'pending' | 'error' | 'success'

const statusStyles: Record<WorkloadState, string> = {
  running: 'border-emerald-600/20 bg-emerald-500/[0.06] text-emerald-700 dark:border-emerald-400/20 dark:bg-emerald-400/[0.08] dark:text-emerald-400',
  success: 'border-emerald-600/20 bg-emerald-500/[0.06] text-emerald-700 dark:border-emerald-400/20 dark:bg-emerald-400/[0.08] dark:text-emerald-400',
  stopped: 'border-border bg-muted/40 text-muted-foreground',
  pending: 'border-amber-600/20 bg-amber-500/[0.06] text-amber-700 dark:border-amber-400/20 dark:bg-amber-400/[0.08] dark:text-amber-400',
  error: 'border-red-600/20 bg-red-500/[0.06] text-red-700 dark:border-red-400/20 dark:bg-red-400/[0.08] dark:text-red-400',
}

export function WorkloadStatus({ state, label, loading = false, title, className }: {
  state: WorkloadState
  label: string
  loading?: boolean
  title?: string
  className?: string
}) {
  return (
    <Badge
      variant="outline"
      data-workload-status={state}
      aria-label={title && title !== label ? `${label}，${title}` : label}
      title={title || label}
      className={cn('max-w-full gap-1.5 whitespace-nowrap rounded-full px-2 py-0.5 text-[11px] font-medium leading-4 shadow-none', statusStyles[state], className)}
    >
      {loading
        ? <LoaderCircle className="h-3 w-3 shrink-0 animate-spin motion-reduce:animate-none" aria-hidden="true" />
        : <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-current opacity-80" aria-hidden="true" />}
      <span className="truncate">{label}</span>
    </Badge>
  )
}

export function WorkloadIdentity({ runtime = 'lxc', name, size = 'default', className }: {
  runtime?: string
  name?: string
  size?: 'sm' | 'default' | 'lg'
  className?: string
}) {
  const isVM = runtime === 'kvm'
  const title = `${isVM ? 'KVM 虚拟机' : 'LXC 容器'}${name ? ` · ${name}` : ''}`
  const Icon = isVM ? Monitor : Server
  return (
    <span
      role="img"
      aria-label={title}
      title={title}
      data-workload-identity={isVM ? 'kvm' : 'lxc'}
      className={cn('inline-flex shrink-0 items-center justify-center rounded-lg border border-border/80 bg-muted/30 text-muted-foreground', size === 'sm' ? 'h-7 w-7' : size === 'lg' ? 'h-11 w-11' : 'h-9 w-9', className)}
    >
      <Icon className={size === 'sm' ? 'h-3.5 w-3.5' : size === 'lg' ? 'h-5 w-5' : 'h-4 w-4'} aria-hidden="true" />
    </span>
  )
}
