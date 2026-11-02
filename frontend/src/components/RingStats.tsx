import { Card } from './ui/card'
import type { ReactNode } from 'react'

interface RingStatProps {
  value: number
  max?: number
  label: string
  subLabel?: ReactNode
  size?: number
  strokeWidth?: number
}

export function RingStat({ value, max = 100, label, subLabel, size = 120, strokeWidth = 8 }: RingStatProps) {
  const radius = (size - strokeWidth) / 2
  const circumference = radius * 2 * Math.PI
  const percentage = max === Infinity ? Math.max(value, 0) : Math.min(Math.max(value / max * 100, 0), 100)
  const strokeDashoffset = circumference - (Math.min(percentage, 100) / 100) * circumference

  const bgStroke = 'hsl(var(--muted))'
  const progressStroke = 'hsl(var(--foreground))'

  return (
    <div className="flex flex-col items-center">
      <div className="relative" style={{ width: size, height: size }}>
        <svg width={size} height={size} className="transform -rotate-90">
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
            style={{ transition: 'stroke-dashoffset 0.5s ease' }}
          />
        </svg>
        {/* Center value */}
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-2xl font-bold text-foreground">{percentage.toFixed(percentage < 1 ? 2 : 1)}%</span>
        </div>
      </div>
      <div className="mt-2 text-center">
        <div className="text-sm font-medium text-foreground">{label}</div>
        {subLabel && <div className="text-xs text-muted-foreground mt-0.5">{subLabel}</div>}
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
      <h2 className="text-sm font-semibold text-foreground mb-4">状态</h2>
      <div className={`grid ${hasSwap ? 'grid-cols-5' : 'grid-cols-4'} gap-3`}>
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
