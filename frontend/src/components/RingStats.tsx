import { Card } from './ui/card'
import type { ReactNode } from 'react'
import { Activity, ArrowDownUp, Cpu, HardDrive, MemoryStick } from 'lucide-react'

interface RingStatProps {
  value: number
  max?: number
  label: string
  subLabel?: ReactNode
  size?: number
  strokeWidth?: number
}

export function RingStat({ value, max = 100, label, subLabel, size = 88, strokeWidth = 4 }: RingStatProps) {
  const radius = (size - strokeWidth) / 2
  const circumference = radius * 2 * Math.PI
  const percentage = max === Infinity ? Math.max(value, 0) : Math.min(Math.max(value / max * 100, 0), 100)
  const strokeDashoffset = circumference - (Math.min(percentage, 100) / 100) * circumference

  const bgStroke = 'hsl(var(--border))'
  const progressStroke = 'hsl(var(--foreground) / 0.8)'
  const displayValue = percentage.toFixed(percentage < 1 ? 2 : 1)
  const Icon = label === 'CPU' ? Cpu : label === '内存' ? MemoryStick : label === 'SWAP' ? ArrowDownUp : label === '负载' ? Activity : HardDrive
  const accessibleLabel = `${label === '/' ? '磁盘 /' : label} ${displayValue}%${typeof subLabel === 'string' ? `，${subLabel}` : ''}`

  return (
    <div className="flex min-w-0 flex-col items-center gap-3" role="group" aria-label={accessibleLabel} data-metric={label}>
      <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        <Icon className="h-3.5 w-3.5" aria-hidden="true" />
        <span>{label}</span>
      </div>
      <div className="relative aspect-square w-full" style={{ maxWidth: size }}>
        <svg viewBox={`0 0 ${size} ${size}`} width={size} height={size} className="h-full w-full -rotate-90" aria-hidden="true">
          {/* Background ring */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            stroke={bgStroke}
            strokeWidth={strokeWidth}
          />
          {/* Progress ring */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            stroke={progressStroke}
            strokeWidth={strokeWidth}
            strokeLinecap="round"
            strokeDasharray={circumference}
            strokeDashoffset={strokeDashoffset}
            className="transition-[stroke-dashoffset] duration-500 ease-out motion-reduce:transition-none"
          />
        </svg>
        {/* Center value */}
        <div className="absolute inset-0 flex items-center justify-center gap-0.5 tabular-nums">
          <span className="text-xl font-semibold tracking-tight text-foreground">{displayValue}</span>
          <span className="self-center pt-1 text-[10px] font-medium text-muted-foreground">%</span>
        </div>
      </div>
      <div className="min-h-4 max-w-full text-center text-[11px] leading-4 tabular-nums text-muted-foreground">
        {subLabel}
      </div>
    </div>
  )
}

interface RingStatsProps {
  cpuPercent: number
  cpuCores: number
  cpuUsed: number
  ramPercent: number
  ramUsed: number
  ramTotal: number
  swapPercent?: number
  swapUsed?: number
  swapTotal?: number
  loadPercent: number
  diskPercent: number
  diskUsed: number
  diskTotal: number
}

export default function RingStats({
  cpuPercent,
  cpuCores,
  cpuUsed,
  ramPercent,
  ramUsed,
  ramTotal,
  swapPercent = 0,
  swapUsed = 0,
  swapTotal = 0,
  loadPercent,
  diskPercent,
  diskUsed,
  diskTotal,
}: RingStatsProps) {
  const formatGB = (mb: number) => {
    if (mb >= 1024) return `${(mb / 1024).toFixed(2)} GB`
    return `${mb} MB`
  }

  const hasSwap = swapTotal > 0

  return (
    <Card className="p-5">
      <h2 className="mb-5 text-sm font-medium text-foreground">状态</h2>
      <div className={`grid grid-cols-2 ${hasSwap ? 'md:grid-cols-3 xl:grid-cols-5' : 'md:grid-cols-4'} gap-x-4 gap-y-6`}>
        <RingStat
          value={cpuPercent}
          label="CPU"
          subLabel={`(${cpuUsed.toFixed(1)} / ${cpuCores} 核)`}
        />
        <RingStat
          value={ramPercent}
          label="内存"
          subLabel={`${formatGB(ramUsed)} / ${formatGB(ramTotal)}`}
        />
        {hasSwap && (
          <RingStat
            value={swapPercent}
            label="SWAP"
            subLabel={`${formatGB(swapUsed)} / ${formatGB(swapTotal)}`}
          />
        )}
        <RingStat
          value={loadPercent}
          max={Infinity}
          label="负载"
        />
        <RingStat
          value={diskPercent}
          label="/"
          subLabel={`${formatGB(diskUsed)} / ${formatGB(diskTotal)}`}
        />
      </div>
    </Card>
  )
}
