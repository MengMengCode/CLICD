import { type ReactNode, useId, useMemo } from 'react'
import { RefreshCw } from 'lucide-react'
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { useLanguage } from '../contexts/LanguageContext'
import { Button } from './ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from './ui/chart'

export type StatsRangeKey = '30m' | '1h' | '1d' | '1w'
export type ChartPoint = { ts: number; value: number }
export type ResourceChartSeries = { label: string; points: ChartPoint[]; current?: number; color?: string }
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
const rangeLabels: Record<StatsRangeKey, string> = { '30m': '30分钟', '1h': '1小时', '1d': '1天', '1w': '1周' }
export const statsRanges: Record<StatsRangeKey, number> = {
  '30m': 30 * 60 * 1000, '1h': 60 * 60 * 1000, '1d': 24 * 60 * 60 * 1000, '1w': 7 * 24 * 60 * 60 * 1000,
}
const palette = [1, 2, 3, 4, 5].map(n => `hsl(var(--chart-${n}))`)

export default function ResourceStatsPanel({ range, onRangeChange, onRefresh, charts }: {
  range: StatsRangeKey; onRangeChange: (range: StatsRangeKey) => void; onRefresh: () => void; charts: ResourceChartConfig[]
}) {
  const { t } = useLanguage()
  return (
    <section className="min-w-0 space-y-4" aria-label={t('统计信息')}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-foreground">{t('统计信息')}</h2>
        <div className="flex min-w-0 max-w-full flex-wrap items-center gap-2">
          <div className="inline-flex flex-wrap items-center rounded-lg border bg-muted/30 p-1" role="group" aria-label={t('统计信息')}>
            {(Object.keys(rangeLabels) as StatsRangeKey[]).map(item => (
              <Button key={item} variant={range === item ? 'secondary' : 'ghost'} size="sm"
                onClick={() => onRangeChange(item)} aria-pressed={range === item}
                className="h-7 whitespace-nowrap px-2.5 text-xs">{t(rangeLabels[item])}</Button>
            ))}
          </div>
          <Button variant="outline" size="icon" onClick={onRefresh} className="h-9 w-9 shrink-0" title={t('刷新')} aria-label={t('刷新')}>
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      </div>
      <div className="grid min-w-0 grid-cols-1 gap-4 xl:grid-cols-2">
        {charts.map(chart => <ResourceChart key={chart.title} chart={chart} range={range} />)}
      </div>
    </section>
  )
}

function ResourceChart({ chart, range }: { chart: ResourceChartConfig; range: StatsRangeKey }) {
  const { t, language } = useLanguage()
  const id = useId().replace(/[^a-zA-Z0-9_-]/g, '')
  const now = useMemo(() => Date.now(), [chart, range])
  const start = now - statsRanges[range]
  const locale = language === 'en' ? 'en-US' : 'zh-CN'
  const series = useMemo(() => {
    const source = chart.series?.length ? chart.series : [{ label: chart.title, points: chart.points, current: chart.current }]
    return source.map((item, index) => {
      const samples = new Map<number, number>()
      for (const point of item.points) {
        if (Number.isFinite(point.ts) && Number.isFinite(point.value) && point.ts >= start && point.ts <= now) samples.set(point.ts, point.value)
      }
      const points = Array.from(samples, ([ts, value]) => ({ ts, value })).sort((a, b) => a.ts - b.ts)
      const current = Number.isFinite(item.current) ? Number(item.current) : source.length === 1 ? finite(chart.current) : 0
      return { ...item, points, current, key: `metric${index}`, color: item.color || palette[index % palette.length] }
    })
  }, [chart, start, now])
  const config = Object.fromEntries(series.map(item => [item.key, { label: t(item.label), color: item.color }])) satisfies ChartConfig
  const statistics = series.map(item => getSeriesStats(item))
  const { data, ceiling, sampleCount } = useMemo(() => {
    const rows = new Map<number, ChartDatum>()
    let ceiling = Math.max(finite(chart.max), 1)
    let sampleCount = 0
    for (const item of series) {
      for (const point of item.points) {
        const row = rows.get(point.ts) || { ts: point.ts }
        row[item.key] = point.value
        rows.set(point.ts, row)
        ceiling = Math.max(ceiling, point.value)
        sampleCount++
      }
    }
    // Timestamp anchors keep the selected window without inventing samples.
    if (!rows.has(start)) rows.set(start, { ts: start })
    if (!rows.has(now)) rows.set(now, { ts: now })
    const data = sampleChartRows(Array.from(rows.values()).sort((a, b) => a.ts - b.ts), series.map(item => item.key))
    return { data, ceiling, sampleCount }
  }, [chart.max, series, start, now])
  const timeFormat = useMemo(() => new Intl.DateTimeFormat(locale, range === '1w'
    ? { month: 'numeric', day: 'numeric' } : { hour: '2-digit', minute: '2-digit', hour12: false }), [locale, range])
  const tooltipFormat = useMemo(() => new Intl.DateTimeFormat(locale, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  }), [locale])
  // Integer byte-rate formatting can give 0.5 B/s and 1 B/s the same label.
  const yTicks = [0, ceiling / 2, ceiling].filter((tick, index, ticks) =>
    !ticks.slice(index + 1).some(next => chart.formatValue(next) === chart.formatValue(tick)))

  return (
    <Card className="min-w-0" data-resource-chart={chart.title}>
      <CardHeader className="space-y-2 p-4 pb-4">
        <CardTitle className="flex min-w-0 items-center gap-2 text-sm leading-5" id={`resource-title-${id}`}>
          <span className="shrink-0 text-muted-foreground [&_svg]:h-4 [&_svg]:w-4">{chart.icon}</span>
          <span className="min-w-0 break-words">{t(chart.title)}</span>
        </CardTitle>
        {chart.detail && <CardDescription className="text-xs leading-5">{t(chart.detail)}</CardDescription>}
      </CardHeader>
      <CardContent className="min-w-0 space-y-5 p-4 pt-0">
        {series.length === 1 ? (
          <div className="grid min-w-0 grid-cols-3 gap-2" data-chart-summary>
            <Stat label={t('当前')} value={chart.formatValue(statistics[0].current)} prominent />
            <Stat label={t('平均')} value={chart.formatValue(statistics[0].avg)} />
            <Stat label={t('峰值')} value={chart.formatValue(statistics[0].peak)} />
          </div>
        ) : (
          <div className="grid min-w-0 grid-cols-[repeat(auto-fit,minmax(min(100%,8rem),1fr))] gap-x-4 gap-y-4" data-chart-summary>
            {series.map((item, index) => (
              <div key={item.key} className="min-w-0 space-y-2" data-chart-series-summary={item.label}>
                <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
                  <span className="h-2.5 w-2.5 shrink-0 rounded-[2px]" style={{ backgroundColor: item.color }} />
                  <span className="min-w-0 break-words">{t(item.label)}</span>
                </div>
                <div className="whitespace-nowrap text-base font-semibold leading-6 tracking-tight tabular-nums" data-chart-stat="current">
                  {chart.formatValue(statistics[index].current)}
                </div>
                <dl className="grid gap-1 text-[11px] leading-4 text-muted-foreground">
                  <SummaryRow label={t('平均')} value={chart.formatValue(statistics[index].avg)} kind="average" />
                  <SummaryRow label={t('峰值')} value={chart.formatValue(statistics[index].peak)} kind="peak" />
                </dl>
              </div>
            ))}
          </div>
        )}
        <div className="relative min-w-0">
          <ChartContainer config={config} className="aspect-auto h-[200px] min-h-[200px] w-full min-w-0"
            aria-labelledby={`resource-title-${id}`} data-chart-container>
            <AreaChart accessibilityLayer data={data} margin={{ top: 8, right: 20, left: 12, bottom: 0 }}>
              <defs>
                {series.map(item => (
                  <linearGradient key={item.key} id={`resource-fill-${id}-${item.key}`} x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={`var(--color-${item.key})`} stopOpacity={0.2} />
                    <stop offset="100%" stopColor={`var(--color-${item.key})`} stopOpacity={0.02} />
                  </linearGradient>
                ))}
              </defs>
              <CartesianGrid vertical={false} />
              <XAxis dataKey="ts" type="number" scale="time" domain={[start, now]} allowDataOverflow
                ticks={[start, start + statsRanges[range] / 2, now]} tickLine={false} axisLine={false}
                tickMargin={10} minTickGap={24} interval="preserveStartEnd" height={32}
                tick={{ fontSize: 11 }} tickFormatter={ts => timeFormat.format(Number(ts))} />
              <YAxis width="auto" domain={[0, ceiling]} ticks={yTicks} allowDataOverflow
                tickLine={false} axisLine={false} tickMargin={8} tick={{ fontSize: 11 }} tickFormatter={chart.formatValue} />
              <ChartTooltip cursor={false} isAnimationActive={false} content={
                <ChartTooltipContent className="min-w-48 max-w-[calc(100vw-2rem)]" indicator="dot"
                  labelFormatter={(_, payload) => tooltipFormat.format(Number(payload[0]?.payload.ts))}
                  formatter={(value, name, item) => <>
                    <span className="h-2.5 w-2.5 shrink-0 rounded-[2px]" style={{ backgroundColor: item.color }} />
                    <span className="min-w-0 flex-1 text-muted-foreground">{config[String(name)]?.label || String(name)}</span>
                    <span className="shrink-0 whitespace-nowrap font-mono font-medium text-foreground tabular-nums">{chart.formatValue(Number(value))}</span>
                  </>} />
              } />
              {series.map(item => (
                <Area key={item.key} dataKey={item.key} type="monotone" connectNulls
                  stroke={`var(--color-${item.key})`} strokeWidth={2} fill={`url(#resource-fill-${id}-${item.key})`}
                  dot={item.points.length === 1 ? { r: 3, fill: `var(--color-${item.key})`, strokeWidth: 0 } : false}
                  activeDot={{ r: 4 }} isAnimationActive={false} />
              ))}
            </AreaChart>
          </ChartContainer>
          {sampleCount === 0 && <div className="pointer-events-none absolute inset-0 flex items-center justify-center pb-8 text-xs text-muted-foreground">{t('暂无数据')}</div>}
        </div>
      </CardContent>
    </Card>
  )
}

function Stat({ label, value, prominent }: { label: string; value: string; prominent?: boolean }) {
  return <div className="min-w-0 space-y-1">
    <div className="text-xs text-muted-foreground">{label}</div>
    <div className={`${prominent ? 'text-lg font-semibold' : 'text-base font-medium'} whitespace-nowrap leading-7 tracking-tight tabular-nums text-foreground`} data-chart-stat={label}>{value}</div>
  </div>
}
function SummaryRow({ label, value, kind }: { label: string; value: string; kind: string }) {
  return <div className="flex min-w-0 items-baseline justify-between gap-x-2">
    <dt className="shrink-0">{label}</dt><dd className="shrink-0 whitespace-nowrap tabular-nums" data-chart-stat={kind}>{value}</dd>
  </div>
}
function finite(value?: number) { return Number.isFinite(value) ? Number(value) : 0 }
function getSeriesStats(series: ResourceChartSeries) {
  const current = finite(series.current)
  let total = 0; let peak = Math.max(0, current)
  for (const point of series.points) { total += point.value; peak = Math.max(peak, point.value) }
  return { current, avg: series.points.length ? total / series.points.length : current, peak }
}

type ChartDatum = { ts: number; [key: string]: number | null }
// Only drawing is sampled: retain real extrema/endpoints from each bucket.
// Current/average/peak above continue to use every original history sample.
function sampleChartRows(rows: ChartDatum[], keys: string[], limit = 1200): ChartDatum[] {
  if (rows.length <= limit) return rows
  const bucketCount = Math.max(1, Math.floor((limit - 2) / (keys.length * 2 + 2)))
  const selected = new Set<number>([0, rows.length - 1])
  for (let bucket = 0; bucket < bucketCount; bucket++) {
    const first = Math.floor(bucket * rows.length / bucketCount)
    const end = Math.floor((bucket + 1) * rows.length / bucketCount)
    selected.add(first); selected.add(end - 1)
    for (const key of keys) {
      let minIndex = -1; let maxIndex = -1
      for (let index = first; index < end; index++) {
        const value = rows[index][key]
        if (typeof value !== 'number') continue
        if (minIndex < 0 || value < Number(rows[minIndex][key])) minIndex = index
        if (maxIndex < 0 || value > Number(rows[maxIndex][key])) maxIndex = index
      }
      if (minIndex >= 0) selected.add(minIndex)
      if (maxIndex >= 0) selected.add(maxIndex)
    }
  }
  return Array.from(selected).sort((a, b) => a - b).map(index => rows[index])
}
