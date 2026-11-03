import { Button } from '../components/ui/button'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/table'
import { Card } from '../components/ui/card'
import { Badge } from '../components/ui/badge'
import { ReactNode, useCallback, useEffect, useState } from 'react'
import {
  Activity,
  CheckCircle2,
  Cpu,
  HardDrive,
  MemoryStick,
  RefreshCw,
  XCircle,
} from 'lucide-react'
import { getHostReport, HostProbeReport } from '../services/api'
import { useLanguage, type Language } from '../contexts/LanguageContext'
import { translateText } from '../utils/i18n'

export default function HostReport() {
  const { language } = useLanguage()
  const text = hostReportText[language]
  const [report, setReport] = useState<HostProbeReport | null>(null)
  const [loading, setLoading] = useState(true)

  const fetchReport = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getHostReport()
      setReport(res.data.data || null)
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchReport()
  }, [fetchReport])

  return (
    <div className="space-y-6" data-no-translate>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">{text.title}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{text.subtitle}</p>
        </div>
        <Button variant="outline" size="sm" onClick={fetchReport} disabled={loading} className="inline-flex items-center gap-1.5 px-3 py-2 text-sm">
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          {text.refresh}
        </Button>
      </div>

      {loading && !report ? (
        <Card className="py-14 text-center text-sm text-muted-foreground">{text.loading}</Card>
      ) : !report ? (
        <Card className="py-14 text-center text-sm text-muted-foreground">{text.emptyReport}</Card>
      ) : (
        <div className="space-y-5">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <ProbeMetric icon={<Cpu className="h-4 w-4" />} label="CPU" value={report.cpu.model || 'Unknown'} sub={formatCPUThreads(report.cpu.cores, report.cpu.threads, language)} />
            <ProbeMetric icon={<MemoryStick className="h-4 w-4" />} label="RAM" value={formatMB(report.memory.total_mb)} sub={formatUsedMemory(report.memory.used_mb, language)} />
            <ProbeMetric icon={<HardDrive className="h-4 w-4" />} label="DISK" value={formatDiskCount(report.disks.length, language)} sub={report.disks.map(d => diskTypeLabel(d, language)).filter(Boolean).join(' / ') || 'Unknown'} />
            <ProbeMetric icon={<Activity className="h-4 w-4" />} label={text.runtimeStatus} value={translateDynamic(report.system.uptime_text, language)} sub={formatProcessCount(report.system.process_count, language)} />
          </div>

          <ProbeSection title={text.systemOverview}>
            <ProbeRows rows={[
              [text.hostname, report.hostname],
              [text.os, report.os],
              [text.kernel, report.kernel],
              [text.generatedAt, report.generated_at],
              [text.cpuArch, report.cpu.architecture],
              [text.cpuVirtualization, report.cpu.virtualization ? `${text.supported} (${report.cpu.virtualization_key})` : text.notDetected],
              [text.cpuIntegratedGPU, report.cpu.has_integrated_gpu ? text.detected : text.notDetected],
              [text.gpu, report.gpus.length ? formatItemCount(report.gpus.length, language) : text.notDetected],
              [text.runtimeCapability, runtimeModeLabel(report.runtime.support_mode, language)],
              [text.kvmNested, `${report.runtime.nested_virtualization ? text.supported : text.notDetected} (${translateDynamic(report.runtime.nested_detail || '-', language)})`],
            ]} />
          </ProbeSection>

          <ProbeSection title={text.publicNetwork}>
            <ProbeRows rows={[
              [text.publicIPv4, report.public_ipv4.length ? report.public_ipv4.join('\n') : text.notDetected],
              [text.ipv4Address, report.ipv4_addresses?.length ? report.ipv4_addresses.map(formatIPv4Address).join('\n') : text.notDetected],
              [text.ipv4Prefix, report.ipv4_prefixes?.length ? report.ipv4_prefixes.map(formatIPv4Prefix).join('\n') : text.notDetected],
              [text.ipv6Address, report.ipv6_addresses.length ? report.ipv6_addresses.map(ip => `${ip.address}/${ip.prefix_len} (${ip.interface})`).join('\n') : text.notDetected],
              [text.ipv6Prefix, report.ipv6_prefixes?.length ? report.ipv6_prefixes.map(formatIPv6Prefix).join('\n') : text.notDetected],
              [text.gateway, report.gateways.length ? report.gateways.map(g => `${g.family}: ${g.gateway || '-'} dev ${g.interface || '-'}`).join('\n') : text.notDetected],
            ]} />
          </ProbeSection>

          <ProbeTable
            title={text.memoryModules}
            empty={text.noMemoryModules}
            headers={[text.slot, text.capacity, text.type, text.frequency, text.vendor, text.modelSerial]}
            rows={(report.memory.modules || []).map(m => [
              m.locator || '-',
              m.size || '-',
              m.type || '-',
              m.speed || '-',
              m.manufacturer || '-',
              [m.part_number, m.serial_number].filter(Boolean).join(' / ') || '-',
            ])}
          />

          <ProbeTable
            title={text.disksHealth}
            empty={text.noDisks}
            headers={[text.device, text.model, text.capacity, text.type, text.mountPoint, text.health, text.lifetime, text.powerOn, text.reads, text.writes, text.commands, text.eraseCount]}
            rows={report.disks.map(d => [
              `${d.path || d.name}\n${d.serial || ''}`,
              d.model || '-',
              formatBytes(d.size_bytes),
              diskTypeLabel(d, language),
              d.mountpoints?.length ? d.mountpoints.join('\n') : '-',
              `${diskHealthLabel(d.health, language)}\n${diskHealthDetail(d, language)}`,
              d.virtual ? text.unsupported : formatLifeUsed(d.smart?.life_used_percent, language),
              d.virtual ? text.unsupported : (d.smart?.power_on_hours ? `${d.smart.power_on_hours} ${text.hours}\n${formatPowerOnDays(d.smart.power_on_hours, language)}` : '-'),
              d.virtual ? text.unsupported : formatBytes(d.smart?.read_data_bytes || 0),
              d.virtual ? text.unsupported : formatBytes(d.smart?.written_data_bytes || 0),
              d.virtual ? text.unsupported : formatCommands(d.smart?.read_commands, d.smart?.write_commands, language),
              d.virtual ? text.unsupported : formatWear(d.smart?.wear_leveling_count, d.smart?.erase_count, d.smart?.power_cycle_count, language),
            ])}
          />

          <ProbeTable
            title={text.networkInterfaces}
            empty={text.noNetworkInterfaces}
            headers={[text.nic, text.status, text.driverSpeed, 'MAC', 'IPv4', 'IPv6']}
            rows={report.network_interfaces.map(n => [
              `${n.name}\n${n.model || ''}`,
              n.state || '-',
              `${n.driver || '-'}\n${n.speed_mbps > 0 ? `${n.speed_mbps} Mbps` : '-'}`,
              n.mac || '-',
              n.ipv4?.length ? n.ipv4.map(ip => `${ip.address}/${ip.prefix_len}`).join('\n') : '-',
              n.ipv6?.length ? n.ipv6.map(ip => `${ip.address}/${ip.prefix_len} ${ip.scope}`).join('\n') : '-',
            ])}
          />

          <ProbeTable
            title={text.gpus}
            empty={text.noGPUs}
            headers={[text.name, text.vendor, text.type, text.driver]}
            rows={report.gpus.map(g => [g.name, g.vendor || '-', gpuTypeLabel(g.type, language), g.driver || '-'])}
          />

          <ProbeSection title={text.environmentSupport}>
            <div className="grid gap-2 md:grid-cols-2">
              {report.environment.map(item => (
                <Card key={item.key} className="flex items-start gap-2 px-3 py-2">
                  {item.ok ? <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-green-600" /> : <XCircle className={`mt-0.5 h-4 w-4 shrink-0 ${item.required ? 'text-red-600' : 'text-amber-600'}`} />}
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2 text-xs font-medium text-foreground">
                      <span>{translateDynamic(item.label, language)}</span>
                      <Badge variant={item.required ? 'secondary' : 'outline'} className="text-[10px]">
                        {item.required ? text.required : text.optional}
                      </Badge>
                    </div>
                    <div className="mt-1 break-all font-mono text-[11px] text-muted-foreground">{translateDynamic(item.detail || '-', language)}</div>
                  </div>
                </Card>
              ))}
            </div>
          </ProbeSection>
        </div>
      )}
    </div>
  )
}

function ProbeMetric({ icon, label, value, sub }: { icon: ReactNode; label: string; value: string; sub: string }) {
  return (
    <Card className="flex flex-col px-3 py-3">
      <div className="mb-1 flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded border border-border/60 bg-muted/40">{icon}</span>
        {label}
      </div>
      <div className="line-clamp-2 break-words text-sm font-medium tabular-nums text-foreground" title={value}>{value}</div>
      <div className="mt-auto truncate pt-1 text-xs tabular-nums text-muted-foreground" title={sub}>{sub}</div>
    </Card>
  )
}

const hostReportText = {
  zh: {
    title: '宿主机信息',
    subtitle: '硬件、网络、磁盘健康与运行环境探测报告',
    refresh: '刷新',
    loading: '正在探测宿主机环境...',
    emptyReport: '暂未获取到宿主机信息',
    runtimeStatus: '运行状态',
    systemOverview: '系统概览',
    hostname: '主机名',
    os: '操作系统',
    kernel: '内核',
    generatedAt: '生成时间',
    cpuArch: 'CPU 架构',
    cpuVirtualization: 'CPU 虚拟化指令',
    cpuIntegratedGPU: 'CPU 核显',
    gpu: '显卡',
    runtimeCapability: '运行能力',
    kvmNested: 'KVM 嵌套虚拟化',
    supported: '支持',
    detected: '检测到',
    notDetected: '未检测到',
    publicNetwork: '公网与路由',
    publicIPv4: '公网 IPv4',
    ipv4Address: 'IPv4 地址',
    ipv4Prefix: 'IPv4 段',
    ipv6Address: 'IPv6 地址',
    ipv6Prefix: '可分配 IPv6 前缀',
    gateway: '网关',
    memoryModules: '内存条',
    noMemoryModules: '未检测到内存条明细，可能缺少 dmidecode 或权限受限',
    slot: '插槽',
    capacity: '容量',
    type: '类型',
    frequency: '频率',
    vendor: '厂商',
    modelSerial: '型号/序列号',
    disksHealth: '硬盘与健康',
    noDisks: '未检测到硬盘',
    device: '设备',
    model: '型号',
    mountPoint: '挂载点',
    health: '健康',
    lifetime: '寿命',
    powerOn: '通电',
    reads: '读取',
    writes: '写入',
    commands: '命令数',
    eraseCount: '擦写',
    virtualDisk: '虚拟磁盘',
    virtualDiskDetail: '虚拟磁盘，真实 SMART/寿命/通电数据需在物理宿主机查看',
    unsupported: '不支持',
    hours: '小时',
    used: '已用',
    remaining: '剩余',
    read: '读',
    write: '写',
    wear: '磨损',
    erase: '擦写',
    powerCycles: '启停',
    networkInterfaces: '网卡',
    noNetworkInterfaces: '未检测到网卡',
    nic: '网卡',
    status: '状态',
    driverSpeed: '驱动/速率',
    gpus: '显卡',
    noGPUs: '未检测到显卡',
    name: '名称',
    driver: '驱动',
    environmentSupport: '环境支持',
    required: '必要',
    optional: '可选',
  },
  en: {
    title: 'Host Info',
    subtitle: 'Hardware, network, disk health, and runtime environment report',
    refresh: 'Refresh',
    loading: 'Probing host environment...',
    emptyReport: 'No host information available',
    runtimeStatus: 'Runtime Status',
    systemOverview: 'System Overview',
    hostname: 'Hostname',
    os: 'Operating System',
    kernel: 'Kernel',
    generatedAt: 'Generated At',
    cpuArch: 'CPU Architecture',
    cpuVirtualization: 'CPU Virtualization',
    cpuIntegratedGPU: 'CPU Integrated GPU',
    gpu: 'GPU',
    runtimeCapability: 'Runtime Capability',
    kvmNested: 'KVM Nested Virtualization',
    supported: 'Supported',
    detected: 'Detected',
    notDetected: 'Not detected',
    publicNetwork: 'Public Network & Routing',
    publicIPv4: 'Public IPv4',
    ipv4Address: 'IPv4 Addresses',
    ipv4Prefix: 'IPv4 Prefixes',
    ipv6Address: 'IPv6 Addresses',
    ipv6Prefix: 'Allocatable IPv6 Prefixes',
    gateway: 'Gateway',
    memoryModules: 'Memory Modules',
    noMemoryModules: 'No memory module details detected. dmidecode may be missing or permissions may be limited.',
    slot: 'Slot',
    capacity: 'Capacity',
    type: 'Type',
    frequency: 'Frequency',
    vendor: 'Vendor',
    modelSerial: 'Model / Serial',
    disksHealth: 'Disks & Health',
    noDisks: 'No disks detected',
    device: 'Device',
    model: 'Model',
    mountPoint: 'Mount Point',
    health: 'Health',
    lifetime: 'Lifetime',
    powerOn: 'Power-on',
    reads: 'Reads',
    writes: 'Writes',
    commands: 'Commands',
    eraseCount: 'Erase Count',
    virtualDisk: 'Virtual Disk',
    virtualDiskDetail: 'Virtual disk. Real SMART, lifetime, and power-on data must be checked on the physical host.',
    unsupported: 'Unsupported',
    hours: 'hours',
    used: 'used',
    remaining: 'remaining',
    read: 'Read',
    write: 'Write',
    wear: 'Wear',
    erase: 'Erase',
    powerCycles: 'Power cycles',
    networkInterfaces: 'Network Interfaces',
    noNetworkInterfaces: 'No network interfaces detected',
    nic: 'NIC',
    status: 'Status',
    driverSpeed: 'Driver / Speed',
    gpus: 'GPUs',
    noGPUs: 'No GPUs detected',
    name: 'Name',
    driver: 'Driver',
    environmentSupport: 'Environment Support',
    required: 'Required',
    optional: 'Optional',
  },
} as const

function ProbeSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h2 className="mb-2.5 text-sm font-medium tracking-tight text-foreground">{title}</h2>
      {children}
    </section>
  )
}

function ProbeRows({ rows }: { rows: Array<[string, string]> }) {
  return (
    <Card className="overflow-hidden">
      {rows.map(([label, value]) => (
        <div key={label} className="grid gap-2 border-b border-border px-3 py-2 text-xs last:border-b-0 md:grid-cols-[160px_1fr]">
          <div className="font-medium text-muted-foreground">{label}</div>
          <div className="whitespace-pre-wrap break-words font-mono tabular-nums text-foreground">{value || '-'}</div>
        </div>
      ))}
    </Card>
  )
}

function ProbeTable({ title, headers, rows, empty }: { title: string; headers: string[]; rows: string[][]; empty: string }) {
  return (
    <section>
      <h2 className="mb-2.5 text-sm font-medium tracking-tight text-foreground">{title}</h2>
      {rows.length === 0 ? (
        <Card className="px-3 py-3 text-xs text-muted-foreground">{empty}</Card>
      ) : (
        <Card className="overflow-x-auto">
          <Table className="w-full text-xs">
            <TableHeader>
              <TableRow className="text-left">
                {headers.map(header => <TableHead key={header} className="px-3 py-2 font-medium">{header}</TableHead>)}
              </TableRow>
            </TableHeader>
            <TableBody >
              {rows.map((row, rowIndex) => (
                <TableRow key={rowIndex} className="align-top">
                  {row.map((cell, cellIndex) => (
                    <TableCell key={cellIndex} className="max-w-[280px] whitespace-pre-wrap break-words px-3 py-2 tabular-nums">
                      {cell || '-'}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </section>
  )
}

function formatIPv4Address(ip: HostProbeReport['ipv4_addresses'][number]) {
  return `${ip.address}/${ip.prefix_len} (${ip.interface})`
}

function formatIPv4Prefix(prefix: HostProbeReport['ipv4_prefixes'][number]) {
  const parts = [
    prefix.prefix || '-',
    prefix.subnet_mask ? `mask ${prefix.subnet_mask}` : '',
    prefix.gateway ? `via ${prefix.gateway}` : '',
    prefix.interface ? `dev ${prefix.interface}` : '',
    prefix.source ? `[${prefix.source}]` : '',
  ].filter(Boolean)
  return parts.join(' ')
}

function formatIPv6Prefix(prefix: HostProbeReport['ipv6_prefixes'][number]) {
  const value = prefix.prefix || prefix.address || '-'
  const cidr = value.includes('/') || !prefix.prefix_len ? value : `${value}/${prefix.prefix_len}`
  return `${cidr} via ${prefix.gateway || '-'}`
}

function formatMB(value: number) {
  if (!value) return '-'
  if (value >= 1024) return `${(value / 1024).toFixed(1)} GB`
  return `${value} MB`
}

function formatCPUThreads(cores: number, threads: number, language: Language) {
  return language === 'en' ? `${cores} cores / ${threads} threads` : `${cores} 核 / ${threads} 线程`
}

function formatUsedMemory(usedMB: number, language: Language) {
  return language === 'en' ? `${formatMB(usedMB)} used` : `${formatMB(usedMB)} 已用`
}

function formatDiskCount(count: number, language: Language) {
  return language === 'en' ? `${count} disk${count === 1 ? '' : 's'}` : `${count} 块硬盘`
}

function formatProcessCount(count: number, language: Language) {
  return language === 'en' ? `${count} process${count === 1 ? '' : 'es'}` : `${count} 个进程`
}

function formatItemCount(count: number, language: Language) {
  return language === 'en' ? `${count} item${count === 1 ? '' : 's'}` : `${count} 个`
}

function formatBytes(value: number) {
  if (!value) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let next = value
  let index = 0
  while (next >= 1024 && index < units.length - 1) {
    next /= 1024
    index++
  }
  return `${next.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function formatLifeUsed(value: number | undefined, language: Language) {
  if (value === undefined || value === null) return '-'
  const text = hostReportText[language]
  return `${value}% ${text.used}\n${Math.max(0, 100 - value)}% ${text.remaining}`
}

function formatPowerOnDays(hours: number, language: Language) {
  const days = Math.floor(hours / 24)
  const rest = hours % 24
  return language === 'en'
    ? (days > 0 ? `${days} days ${rest} hours` : `${hours} hours`)
    : (days > 0 ? `${days} 天 ${rest} 小时` : `${hours} 小时`)
}

function formatCommands(read: number | undefined, write: number | undefined, language: Language) {
  if (!read && !write) return '-'
  const text = hostReportText[language]
  return `${text.read} ${formatCount(read || 0)}\n${text.write} ${formatCount(write || 0)}`
}

function formatCount(value: number) {
  if (!value) return '-'
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return `${value}`
}

function formatWear(wear: string | undefined, erase: string | undefined, powerCycles: number | undefined, language: Language) {
  const text = hostReportText[language]
  const rows: string[] = []
  if (wear) rows.push(`${text.wear} ${wear}`)
  if (erase) rows.push(`${text.erase} ${erase}`)
  if (powerCycles) rows.push(`${text.powerCycles} ${powerCycles}`)
  return rows.length ? rows.join('\n') : '-'
}

function runtimeModeLabel(value: string, language: Language) {
  switch (value) {
    case 'kvm_lxc':
      return language === 'en' ? 'KVM + LXC supported' : '支持 KVM + LXC'
    case 'lxc_only':
      return language === 'en' ? 'LXC only' : '仅支持 LXC'
    default:
      return language === 'en' ? 'Runtime requirements not met' : '未满足运行环境'
  }
}

function diskHealthLabel(value: string, language: Language) {
  const text = hostReportText[language]
  switch (value) {
    case 'ok':
      return language === 'en' ? 'Healthy' : '健康'
    case 'failed':
      return language === 'en' ? 'Failed' : '异常'
    case 'virtual':
      return text.virtualDisk
    default:
      return language === 'en' ? 'Unknown' : '未知'
  }
}

function diskHealthDetail(d: { virtual?: boolean; health_detail?: string }, language: Language) {
  if (d.virtual) return hostReportText[language].virtualDiskDetail
  return translateDynamic(d.health_detail || '', language)
}

function diskTypeLabel(d: { type?: string; rotational?: boolean; virtual?: boolean }, language: Language) {
  if (d.virtual || d.type === 'Virtual') return hostReportText[language].virtualDisk
  return d.type || (d.rotational ? 'HDD' : 'SSD')
}

function gpuTypeLabel(value: string, language: Language) {
  if (value === 'virtual') return language === 'en' ? 'Virtual' : '虚拟'
  if (value === 'integrated') return language === 'en' ? 'Integrated' : '核显'
  if (value === 'discrete') return language === 'en' ? 'Discrete' : '独显'
  return value || '-'
}

function translateDynamic(value: string, language: Language) {
  if (language !== 'en' || !value) return value
  return translateText(value)
    .replace(/寿命已用\s*(\d+)%/g, 'Lifetime used $1%')
    .replace(/通电\s*(\d+)h/g, 'Power-on $1h')
    .replace(/写入\s*([^|]+)/g, 'Written $1')
    .replace(/读取\s*([^|]+)/g, 'Read $1')
    .replace(/介质错误\s*(\d+)/g, 'Media errors $1')
}
