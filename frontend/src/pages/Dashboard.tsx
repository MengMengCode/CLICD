import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Cpu, HardDrive, MemoryStick, Network, Server } from 'lucide-react'
import RingStats from '../components/RingStats'
import ResourceStatsPanel, {
  ChartPoint,
  ResourceChartConfig,
  StatsRangeKey,
  statsRanges,
} from '../components/ResourceStatsPanel'
import { DashboardStats, getDashboard, getHostHistory, getHostInfo, HostInfo, HostMetricPoint as HostMetricSample } from '../services/api'

type HostMetricPoint = {
  ts: number
  cpu: number
  memory: number
  network?: number
  networkRx?: number
  networkTx?: number
  diskIO?: number
  diskRead?: number
  diskWrite?: number
}

const hostHistoryKey = 'clicd_host_metric_history_v2'

export default function Dashboard() {
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [host, setHost] = useState<HostInfo | null>(null)
  const [history, setHistory] = useState<HostMetricPoint[]>(readHostHistory)
  const [range, setRange] = useState<StatsRangeKey>('30m')
  const [loading, setLoading] = useState(true)

  const fetchHistory = useCallback(async () => {
    try {
      const res = await getHostHistory()
      const points = (res.data.data || []).map(normalizeHostMetricSample)
      if (points.length > 0) {
        setHistory(points)
        localStorage.setItem(hostHistoryKey, JSON.stringify(points))
      }
    } catch (err) {
      console.error(err)
    }
  }, [])

  const fetchData = useCallback(async () => {
    try {
      const [dashRes, hostRes] = await Promise.all([getDashboard(), getHostInfo()])
      if (dashRes.data.data) setStats(dashRes.data.data)
      if (hostRes.data.data) {
        const nextHost = hostRes.data.data
        setHost(nextHost)
      }
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchHistory()
    fetchData()
    const interval = window.setInterval(fetchData, 5000)
    const historyInterval = window.setInterval(fetchHistory, 30000)
    return () => {
      window.clearInterval(interval)
      window.clearInterval(historyInterval)
    }
  }, [fetchData, fetchHistory])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-black"></div>
      </div>
    )
  }

  const filtered = filterHistory(history, range)
  const memoryPct = host && host.ram.total_mb > 0 ? (host.ram.used_mb / host.ram.total_mb) * 100 : 0
  const networkRxBps = host?.network.rx_bps || 0
  const networkTxBps = host?.network.tx_bps || 0
  const diskReadBps = host?.disk_io.read_bps || 0
  const diskWriteBps = host?.disk_io.write_bps || 0
  const networkBps = (host?.network.rx_bps || 0) + (host?.network.tx_bps || 0)
  const diskIOBps = (host?.disk_io.read_bps || 0) + (host?.disk_io.write_bps || 0)

  const charts: ResourceChartConfig[] = [
    {
      title: 'CPU 使用率',
      icon: <Cpu className="w-5 h-5" />,
      current: host?.cpu.usage_pct || 0,
      points: toChartPoints(filtered, 'cpu'),
      max: 100,
      formatValue: formatPercent,
      detail: `${host?.cpu.cores || 0} 核`,
    },
    {
      title: '内存使用',
      icon: <MemoryStick className="w-5 h-5" />,
      current: memoryPct,
      points: toChartPoints(filtered, 'memory'),
      max: 100,
      formatValue: formatPercent,
      detail: `${formatMB(host?.ram.used_mb || 0)} / ${formatMB(host?.ram.total_mb || 0)}`,
    },
    {
      title: '网络流量',
      icon: <Network className="w-5 h-5" />,
      current: networkBps,
      points: toChartPoints(filtered, 'network'),
      series: [
        { label: '入', points: toChartPoints(filtered, 'networkRx'), current: networkRxBps, color: '#2563eb' },
        { label: '出', points: toChartPoints(filtered, 'networkTx'), current: networkTxBps, color: '#16a34a' },
      ],
      formatValue: formatRate,
      detail: `入 ${formatRate(networkRxBps)} / 出 ${formatRate(networkTxBps)}`,
    },
    {
      title: '磁盘IO',
      icon: <HardDrive className="w-5 h-5" />,
      current: diskIOBps,
      points: toChartPoints(filtered, 'diskIO'),
      series: [
        { label: '读', points: toChartPoints(filtered, 'diskRead'), current: diskReadBps, color: '#d97706' },
        { label: '写', points: toChartPoints(filtered, 'diskWrite'), current: diskWriteBps, color: '#dc2626' },
      ],
      formatValue: formatRate,
      detail: `读 ${formatRate(diskReadBps)} / 写 ${formatRate(diskWriteBps)}`,
    },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-black">控制面板</h1>
        <p className="text-sm text-gray-500 mt-1">宿主机资源状态与容器概览</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <SummaryCard icon={<Server className="w-5 h-5" />} title="容器总数" value={stats?.total_containers || 0} />
        <SummaryCard dot="bg-green-500" title="运行中" value={stats?.running || 0} />
        <SummaryCard dot="bg-red-500" title="已停止" value={stats?.stopped || 0} muted />
      </div>

      {host && (
        <RingStats
          cpuPercent={host.cpu.usage_pct}
          cpuCores={host.cpu.cores}
          cpuUsed={host.cpu.usage_pct * host.cpu.cores / 100}
          ramPercent={host.ram.total_mb > 0 ? (host.ram.used_mb / host.ram.total_mb) * 100 : 0}
          ramUsed={host.ram.used_mb}
          ramTotal={host.ram.total_mb}
          loadPercent={(host.load.load1 / Math.max(host.cpu.cores, 1)) * 100}
          diskPercent={host.disk.total_gb > 0 ? (host.disk.used_gb / host.disk.total_gb) * 100 : 0}
          diskUsed={host.disk.used_gb * 1024}
          diskTotal={host.disk.total_gb * 1024}
        />
      )}

      <ResourceStatsPanel range={range} onRangeChange={setRange} onRefresh={fetchData} charts={charts} />
    </div>
  )
}

function SummaryCard({
  icon,
  dot,
  title,
  value,
  muted = false,
}: {
  icon?: ReactNode
  dot?: string
  title: string
  value: number
  muted?: boolean
}) {
  return (
    <div className="bg-white border border-gray-200 rounded-lg p-5">
      <div className="flex items-center gap-2 text-sm text-gray-500 mb-2">
        {icon}
        {dot && <span className={`w-2 h-2 rounded-full ${dot}`}></span>}
        {title}
      </div>
      <div className={`text-3xl font-bold ${muted ? 'text-gray-600' : 'text-black'}`}>{value}</div>
    </div>
  )
}

function readHostHistory(): HostMetricPoint[] {
  try {
    const raw = localStorage.getItem(hostHistoryKey)
    if (!raw) return []
    const parsed = JSON.parse(raw) as HostMetricPoint[]
    const cutoff = Date.now() - statsRanges['1w']
    return parsed.filter((item) => item.ts >= cutoff)
  } catch {
    return []
  }
}

function filterHistory(history: HostMetricPoint[], range: StatsRangeKey) {
  const cutoff = Date.now() - statsRanges[range]
  return history.filter((point) => point.ts >= cutoff)
}

function toChartPoints(history: HostMetricPoint[], key: keyof Omit<HostMetricPoint, 'ts'>): ChartPoint[] {
  return history.flatMap((point) => {
    const value = Number(point[key])
    return Number.isFinite(value) ? [{ ts: point.ts, value }] : []
  })
}

function normalizeHostMetricSample(point: HostMetricSample): HostMetricPoint {
  return {
    ts: point.ts,
    cpu: clamp(point.cpu),
    memory: clamp(point.memory),
    network: point.network || 0,
    networkRx: point.network_rx || 0,
    networkTx: point.network_tx || 0,
    diskIO: point.disk_io || 0,
    diskRead: point.disk_read || 0,
    diskWrite: point.disk_write || 0,
  }
}

function clamp(value: number) {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(value, 100))
}

function formatPercent(value: number) {
  return `${value.toFixed(1)}%`
}

function formatMB(mb: number) {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${Math.round(mb)} MB`
}

function formatBytes(value: number) {
  if (value < 1024) return `${value.toFixed(0)} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(2)} KB`
  return `${(value / 1024 / 1024).toFixed(2)} MB`
}

function formatRate(value: number) {
  return `${formatBytes(value)}/s`
}
