import { Card } from './ui/card'
import { Button } from './ui/button'
import { ReactNode, useId, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { calculateChartLayout, type ChartLayout } from '../lib/chart-layout'

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
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-2.5 border-b border-border bg-background">
        <h2 className="text-sm font-semibold text-foreground">统计信息</h2>
        <div className="flex min-w-0 max-w-full flex-wrap items-center gap-1.5">
          <div className="inline-flex flex-wrap gap-0.5">
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
    <div className={`min-w-0 p-4 ${className}`} data-resource-chart={chart.title}>
      <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-start sm:justify-between mb-3">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5 text-sm font-semibold text-foreground">
            <span className="text-muted-foreground">{chart.icon}</span>
            <span>{chart.title}</span>
          </div>
          {chart.detail && <p className="mt-0.5 text-[11px] text-muted-foreground [overflow-wrap:anywhere]">{chart.detail}</p>}
        </div>
        {series.length > 1 ? (
          <div className="grid min-w-0 max-w-full grid-cols-2 gap-x-4 gap-y-1 text-right sm:shrink-0">
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
          <div className="grid min-w-0 max-w-full grid-cols-3 gap-3 text-right sm:shrink-0">
            <Stat label="当前" value={chart.formatValue(primaryStats.current)} />
            <Stat label="平均" value={chart.formatValue(primaryStats.avg)} />
            <Stat label="峰值" value={chart.formatValue(primaryStats.peak)} />
          </div>
        )}
      </div>
      <LineAreaChart
        label={chart.title}
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
    <div className="min-w-0">
      <div className="flex items-center justify-end gap-1 text-[10px] text-muted-foreground">
        <span className="h-0.5 w-3 shrink-0 rounded-full" style={{ backgroundColor: color }} />
        <span className="[overflow-wrap:anywhere]">{label}</span>
      </div>
      <div className="text-xs font-medium text-foreground tabular-nums [overflow-wrap:anywhere]">
        {formatValue(stats.current)}
      </div>
      <div className="text-[10px] text-muted-foreground tabular-nums [overflow-wrap:anywhere]">
        均 {formatValue(stats.avg)} / 峰 {formatValue(stats.peak)}
      </div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className="text-xs font-medium text-foreground tabular-nums [overflow-wrap:anywhere]">{value}</div>
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
  label,
  series,
  range,
  max,
  formatValue,
  unitLabel,
}: {
  label: string
  series: ResourceChartSeries[]
  range: StatsRangeKey
  max?: number
  formatValue: (value: number) => string
  unitLabel?: string
}) {
  const chartId = `resource-chart-${useId().replace(/:/g, '')}`
  const containerRef = useRef<HTMLDivElement>(null)
  const measureRef = useRef<SVGTextElement>(null)
  const [layout, setLayout] = useState<ChartLayout | null>(null)
  const now = useMemo(() => Date.now(), [series, range])
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
  const minTs = now - statsRanges[range]
  const span = Math.max(statsRanges[range], 1)
  const yLabelKey = JSON.stringify([1, 0.5, 0].map((tick) => formatValue(maxValue * tick)))
  const xLabelKey = JSON.stringify([0, 0.5, 1].map((tick) => formatTime(minTs + tick * span)))

  useLayoutEffect(() => {
    const container = containerRef.current
    const textNode = measureRef.current
    if (!container || !textNode) return
    let disposed = false
    let frame = 0
    const measure = (text: string) => {
      textNode.textContent = text
      return textNode.getComputedTextLength()
    }
    const update = () => {
      if (disposed) return
      const width = container.getBoundingClientRect().width
      if (width <= 0) return
      const yLabels: string[] = JSON.parse(yLabelKey)
      const xLabels: string[] = JSON.parse(xLabelKey)
      let fontTop = 0
      let fontBottom = 0
      // Real SVG metrics include tabular figures and any translated fallback font.
      for (const text of [...yLabels, ...xLabels, unitLabel || '', '0123456789Mg']) {
        measure(text)
        const box = textNode.getBBox()
        fontTop = Math.min(fontTop, box.y)
        fontBottom = Math.max(fontBottom, box.y + box.height)
      }
      const next = calculateChartLayout({
        width, yLabels, xLabels, unitLabel, measure, fontTop,
        fontHeight: Math.max(1, fontBottom - fontTop),
      })
      setLayout((previous) => JSON.stringify(previous) === JSON.stringify(next) ? previous : next)
    }
    const schedule = () => {
      if (disposed) return
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(update)
    }
    const observer = new ResizeObserver(schedule)
    observer.observe(container)
    document.fonts.ready.then(schedule)
    document.fonts.addEventListener('loadingdone', schedule)
    update()
    return () => {
      disposed = true
      cancelAnimationFrame(frame)
      observer.disconnect()
      document.fonts.removeEventListener('loadingdone', schedule)
    }
  }, [yLabelKey, xLabelKey, unitLabel])

  const gridStroke = 'hsl(var(--border))'
  const axisStroke = 'hsl(var(--muted-foreground))'
  const lineStroke = 'hsl(var(--foreground))'
  const plot = layout?.plot
  const primaryLine = plot
    ? buildLine(chartSeries[0]?.points || [], minTs, span, plot.left, plot.top, plot.width, plot.height, maxValue)
    : ''
  const area = plot ? `${plot.left},${plot.top + plot.height} ${primaryLine} ${plot.left + plot.width},${plot.top + plot.height}` : ''
  const textBaseline = (center: number, lines: string[]) => layout
    ? center - (layout.fontHeight + (lines.length - 1) * layout.lineHeight) / 2 - layout.fontTop
    : center

  return (
    <div ref={containerRef} className="min-w-0 w-full" data-chart-container>
      <svg
        viewBox={`0 0 ${layout?.width || 1} ${layout?.height || 140}`}
        className="block w-full overflow-hidden text-[10px] tabular-nums"
        style={{ height: layout?.height || 140 }}
        role="img"
        aria-label={label}
      >
        <text ref={measureRef} visibility="hidden" aria-hidden="true" />
        {layout && plot && <>
          <defs>
            <linearGradient id={`${chartId}-fill`} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor={lineStroke} stopOpacity="0.07" />
              <stop offset="100%" stopColor={lineStroke} stopOpacity="0.005" />
            </linearGradient>
            <clipPath id={`${chartId}-clip`} clipPathUnits="userSpaceOnUse">
              <rect x={plot.left} y={plot.top} width={plot.width} height={plot.height} data-chart-plot />
            </clipPath>
          </defs>

          {layout.yLabels.map(({ tick, lines }) => {
            const y = plot.top + (1 - tick) * plot.height
            return (
              <g key={tick}>
                <line x1={plot.left} y1={y} x2={plot.left + plot.width} y2={y} stroke={gridStroke} strokeWidth="0.75" strokeOpacity="0.7" />
                <text x={plot.left - 8} y={textBaseline(y, lines)} textAnchor="end" fill={axisStroke} data-chart-y-label>
                  {lines.map((line, index) => <tspan key={index} x={plot.left - 8} dy={index === 0 ? 0 : layout.lineHeight}>{line}</tspan>)}
                </text>
              </g>
            )
          })}

          {layout.xLabels.map(({ tick, lines }) => {
            const x = plot.left + tick * plot.width
            return (
              <g key={tick}>
                <line x1={x} y1={plot.top} x2={x} y2={plot.top + plot.height} stroke={gridStroke} strokeWidth="0.75" strokeOpacity="0.35" />
                <text x={x} y={plot.top + plot.height + 8 - layout.fontTop} textAnchor={tick === 0 ? 'start' : tick === 1 ? 'end' : 'middle'} fill={axisStroke} data-chart-x-label>
                  {lines.map((line, index) => <tspan key={index} x={x} dy={index === 0 ? 0 : layout.lineHeight}>{line}</tspan>)}
                </text>
              </g>
            )
          })}

          {layout.unitLines.length > 0 && (
            <text x={4} y={4 - layout.fontTop} fill={axisStroke} data-chart-unit-label>
              {layout.unitLines.map((line, index) => <tspan key={index} x={4} dy={index === 0 ? 0 : layout.lineHeight}>{line}</tspan>)}
            </text>
          )}

          <g clipPath={`url(#${chartId}-clip)`} data-chart-series>
            {chartSeries.length === 1 && <polygon points={area} fill={`url(#${chartId}-fill)`} />}
            {chartSeries.map((item, index) => (
              <polyline
                key={item.label || index}
                points={buildLine(item.points, minTs, span, plot.left, plot.top, plot.width, plot.height, maxValue)}
                fill="none"
                stroke={item.color || (chartSeries.length === 1 ? lineStroke : chartPalette[index % chartPalette.length])}
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            ))}
          </g>
        </>}
      </svg>
    </div>
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
