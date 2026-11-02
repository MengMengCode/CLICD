import { Card } from './ui/card'
import { Button } from './ui/button'
import { ReactNode, useId } from 'react'
import { RefreshCw } from 'lucide-react'

export type StatsRangeKey = '30m' | '1h' | '1d' | '1w'

export type ChartPoint = {
  ts: number
  value: number
}

export type ResourceChartSeries = {
  label: string
  points: ChartPoint[]
  current?: number
  color?: string
}

export type ResourceChartConfig = {
  title: string
  icon: ReactNode
  points: ChartPoint[]
  current: number
  series?: ResourceChartSeries[]
  detail?: string
  max?: number
  unitLabel?: string
  formatValue: (value: number) => string
}

const chartPalette = ['#2563eb', '#16a34a', '#d97706', '#dc2626']

const rangeLabels: Record<StatsRangeKey, string> = {
  '30m': '30分钟',
  '1h': '1小时',
  '1d': '1天',
  '1w': '1周',
}

export const statsRanges: Record<StatsRangeKey, number> = {
  '30m': 30 * 60 * 1000,
  '1h': 60 * 60 * 1000,
  '1d': 24 * 60 * 60 * 1000,
  '1w': 7 * 24 * 60 * 60 * 1000,
}

export default function ResourceStatsPanel({
  range,
  onRangeChange,
  onRefresh,
  charts,
}: {
  range: StatsRangeKey
  onRangeChange: (range: StatsRangeKey) => void
  onRefresh: () => void
  charts: ResourceChartConfig[]
}) {
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between gap-3 px-4 py-2.5 border-b border-border bg-background">
        <h2 className="text-sm font-semibold text-foreground">统计信息</h2>
        <div className="flex items-center gap-1.5">
          <div className="inline-flex gap-0.5">
            {(Object.keys(rangeLabels) as StatsRangeKey[]).map((item) => (
              <Button variant={range === item ? 'default' : 'ghost'}
                key={item}
                onClick={() => onRangeChange(item)}
                className="h-7 px-3 text-xs"
                aria-pressed={range === item}
              >
                {rangeLabels[item]}
              </Button>
            ))}
          </div>
          <Button variant="outline"
            size="icon"
            onClick={onRefresh}
            className="h-8 w-8"
            title="刷新"
          >
            <RefreshCw className="w-4 h-4" />
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-2">
        {charts.map((chart, index) => (
          <DetailedChart key={chart.title} chart={chart} range={range} className={chartBorderClass(index)} />
        ))}
      </div>
    </Card>
  )
}

function DetailedChart({ chart, range, className }: { chart: ResourceChartConfig; range: StatsRangeKey; className: string }) {
  const series = chart.series?.length
    ? chart.series
    : [{ label: chart.title, points: chart.points, current: chart.current }]
  const primaryStats = getSeriesStats(series[0], chart.current)

  return (
    <div className={`p-4 ${className}`}>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between mb-2">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5 text-sm font-semibold text-foreground">
            <span className="text-muted-foreground">{chart.icon}</span>
            <span>{chart.title}</span>
          </div>
          {chart.detail && <p className="mt-0.5 text-[11px] text-muted-foreground">{chart.detail}</p>}
        </div>
        {series.length > 1 ? (
          <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-right sm:shrink-0">
            {series.map((item, index) => (
              <SeriesStat
                key={item.label}
                color={item.color || chartPalette[index % chartPalette.length]}
                label={item.label}
                stats={getSeriesStats(item, item.current)}
                formatValue={chart.formatValue}
              />
            ))}
          </div>
        ) : (
          <div className="grid grid-cols-3 gap-3 text-right sm:shrink-0">
            <Stat label="当前" value={chart.formatValue(primaryStats.current)} />
            <Stat label="平均" value={chart.formatValue(primaryStats.avg)} />
            <Stat label="峰值" value={chart.formatValue(primaryStats.peak)} />
          </div>
        )}
      </div>
      <LineAreaChart
        series={series}
        range={range}
        max={chart.max}
        formatValue={chart.formatValue}
        unitLabel={chart.unitLabel}
      />
    </div>
  )
}

function SeriesStat({
  color,
  label,
  stats,
  formatValue,
}: {
  color: string
  label: string
  stats: { current: number; avg: number; peak: number }
  formatValue: (value: number) => string
}) {
  return (
    <div className="min-w-[104px]">
      <div className="flex items-center justify-end gap-1 text-[10px] text-muted-foreground">
        <span className="h-2 w-2 rounded-full" style={{ backgroundColor: color }} />
        <span>{label}</span>
      </div>
      <div className="text-xs font-semibold text-foreground tabular-nums whitespace-nowrap">
        {formatValue(stats.current)}
      </div>
      <div className="text-[10px] text-muted-foreground tabular-nums whitespace-nowrap">
        均 {formatValue(stats.avg)} / 峰 {formatValue(stats.peak)}
      </div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className="text-xs font-semibold text-foreground tabular-nums whitespace-nowrap">{value}</div>
    </div>
  )
}

function getSeriesStats(series: ResourceChartSeries, fallbackCurrent = 0) {
  const values = series.points
    .map((point) => point.value)
    .filter((value) => Number.isFinite(value))
  const current = Number.isFinite(series.current) ? Number(series.current) : fallbackCurrent
  const samples = values.length > 0 ? values : [current]
  const avg = samples.reduce((sum, value) => sum + value, 0) / samples.length
  const peak = Math.max(current, ...samples, 0)
  return { current, avg, peak }
}

function LineAreaChart({
  series,
  range,
  max,
  formatValue,
  unitLabel,
}: {
  series: ResourceChartSeries[]
  range: StatsRangeKey
  max?: number
  formatValue: (value: number) => string
  unitLabel?: string
}) {
  const gradientId = `resource-chart-fill-${useId().replace(/:/g, '')}`

  const width = 520
  const height = 150
  const left = 50
  const right = 10
  const top = 8
  const bottom = 28
  const innerWidth = width - left - right
  const innerHeight = height - top - bottom
  const now = Date.now()
  const chartSeries = series.map((item) => {
    const validPoints = item.points.filter((point) => Number.isFinite(point.ts) && Number.isFinite(point.value))
    return {
      ...item,
      points: validPoints.length > 0
        ? validPoints
        : [{ ts: now, value: Number.isFinite(item.current) ? Number(item.current) : 0 }],
    }
  })
  const allPoints = chartSeries.flatMap((item) => item.points)
  const maxValue = Math.max(max || 0, ...allPoints.map((point) => point.value), 1)
  const maxTs = now
  const minTs = now - statsRanges[range]
  const span = Math.max(maxTs - minTs, 1)
  const yTicks = [1, 0.5, 0]
  const xTicks = [0, 0.5, 1]

  const gridStroke = 'hsl(var(--border))'
  const gridStrokeV = 'hsl(var(--muted))'
  const axisStroke = 'hsl(var(--muted-foreground))'
  const lineStroke = 'hsl(var(--foreground))'
  const gradientTop = 'hsl(var(--foreground))'
  const gradientBottom = 'hsl(var(--muted))'
  const primaryLine = buildLine(chartSeries[0]?.points || [{ ts: now, value: 0 }], minTs, span, left, top, innerWidth, innerHeight, maxValue)
  const area = `${left},${top + innerHeight} ${primaryLine} ${left + innerWidth},${top + innerHeight}`

  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="w-full h-[140px]" preserveAspectRatio="none">
      <defs>
        <linearGradient id={gradientId} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" stopColor={gradientTop} stopOpacity="0.25" />
          <stop offset="100%" stopColor={gradientBottom} stopOpacity="0.02" />
        </linearGradient>
      </defs>

      {yTicks.map((tick) => {
        const y = top + (1 - tick) * innerHeight
        return (
          <g key={tick}>
            <line x1={left} y1={y} x2={left + innerWidth} y2={y} stroke={gridStroke} strokeDasharray="3 3" />
            <text x={left - 8} y={y + 3} textAnchor="end" fontSize="10" fill={axisStroke}>
              {formatValue(maxValue * tick)}
            </text>
          </g>
        )
      })}

      {xTicks.map((tick) => {
        const x = left + tick * innerWidth
        const ts = minTs + tick * span
        return (
          <g key={tick}>
            <line x1={x} y1={top} x2={x} y2={top + innerHeight} stroke={gridStrokeV} strokeDasharray="3 3" />
            <text x={x} y={height - 5} textAnchor={tick === 0 ? 'start' : tick === 1 ? 'end' : 'middle'} fontSize="10" fill={axisStroke}>
              {formatTime(ts)}
            </text>
          </g>
        )
      })}

      {unitLabel && (
        <text x={left - 45} y={top + 10} fontSize="10" fill={axisStroke}>
          {unitLabel}
        </text>
      )}

      <line x1={left} y1={top} x2={left} y2={top + innerHeight} stroke={axisStroke} />
      <line x1={left} y1={top + innerHeight} x2={left + innerWidth} y2={top + innerHeight} stroke={axisStroke} />
      {chartSeries.length === 1 && <polygon points={area} fill={`url(#${gradientId})`} />}
      {chartSeries.map((item, index) => (
        <polyline
          key={item.label || index}
          points={buildLine(item.points, minTs, span, left, top, innerWidth, innerHeight, maxValue)}
          fill="none"
          stroke={item.color || (chartSeries.length === 1 ? lineStroke : chartPalette[index % chartPalette.length])}
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      ))}
    </svg>
  )
}

function buildLine(
  points: ChartPoint[],
  minTs: number,
  span: number,
  left: number,
  top: number,
  innerWidth: number,
  innerHeight: number,
  maxValue: number,
) {
  const coords = points.map((point) => {
    const x = left + ((point.ts - minTs) / span) * innerWidth
    const y = top + innerHeight - (point.value / maxValue) * innerHeight
    return `${Number.isFinite(x) ? x : left},${Number.isFinite(y) ? y : top + innerHeight}`
  })
  if (coords.length > 1) return coords.join(' ')

  const [, yText] = (coords[0] || `${left},${top + innerHeight}`).split(',')
  const y = Number(yText)
  const safeY = Number.isFinite(y) ? y : top + innerHeight
  return `${left},${safeY} ${left + innerWidth},${safeY}`
}

function chartBorderClass(index: number) {
  const right = index % 2 === 0 ? 'xl:border-r' : ''
  const top = index > 1 ? 'border-t' : ''
  return `${right} ${top} border-border`
}

function formatTime(ts: number) {
  return new Date(ts).toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}
