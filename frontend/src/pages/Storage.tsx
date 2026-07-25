import { useCallback, useEffect, useMemo, useState } from 'react'
import { AlertCircle, CheckCircle2, HardDrive, RefreshCw, Save } from 'lucide-react'
import { getStorageInfo, updateStoragePools, StorageDisk, StorageInfo, StoragePool } from '../services/api'
import { useLanguage } from '../contexts/LanguageContext'

const contentOptions = [
  ['lxc', 'LXC 容器'],
  ['kvm', 'KVM 磁盘'],
  ['images', '镜像缓存'],
  ['snapshots', '快照'],
  ['backups', '备份'],
] as const

const contentLabels = Object.fromEntries(contentOptions)

const contentColors: Record<string, string> = {
  lxc: '#2563eb',
  kvm: '#7c3aed',
  images: '#d97706',
  snapshots: '#059669',
  backups: '#0891b2',
}

export default function Storage() {
  const { t } = useLanguage()
  const [info, setInfo] = useState<StorageInfo | null>(null)
  const [pools, setPools] = useState<StoragePool[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saveMessage, setSaveMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getStorageInfo()
      const data = res.data.data || { pools: [], disks: [], content_types: [] }
      setInfo(data)
      setPools(data.pools || [])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  useEffect(() => {
    if (!saveMessage) return
    const timer = window.setTimeout(() => setSaveMessage(null), 3500)
    return () => window.clearTimeout(timer)
  }, [saveMessage])

  const mountedDisks = useMemo(() => (info?.disks || []).filter((disk) => !!disk.mount_point), [info?.disks])

  const save = async () => {
    setSaveMessage(null)
    setSaving(true)
    try {
      const normalized = pools
        .map((pool) => ({
          ...pool,
          id: (pool.id || pool.name || '').trim(),
          name: (pool.name || '').trim(),
          path: (pool.path || '').trim(),
          content_types: pool.content_types || [],
          default_contents: (pool.default_contents || []).filter((item) => (pool.content_types || []).includes(item)),
          enabled: pool.enabled !== false,
        }))
      const res = await updateStoragePools(normalized)
      const data = res.data.data
      if (data) {
        setInfo(data)
        setPools(data.pools || [])
      }
      setSaveMessage({ type: 'success', text: '存储配置已保存' })
    } catch (err: any) {
      setSaveMessage({ type: 'error', text: err?.response?.data?.message || '保存存储配置失败' })
    } finally {
      setSaving(false)
    }
  }

  const updateDiskPool = (disk: StorageDisk, updater: (pool: StoragePool) => StoragePool) => {
    setPools((current) => {
      const index = current.findIndex((pool) => poolForDisk(pool, disk))
      const base = index >= 0 ? current[index] : defaultPoolForDisk(disk)
      const nextPool = updater(base)
      if (index >= 0) {
        return current.map((item, i) => i === index ? nextPool : item)
      }
      return [...current, nextPool]
    })
  }

  const toggleContent = (disk: StorageDisk, content: string) => {
    updateDiskPool(disk, (pool) => {
      const current = pool.content_types || []
      const enabled = current.includes(content)
      const contentTypes = enabled ? current.filter((item) => item !== content) : [...current, content]
      return {
        ...pool,
        enabled: true,
        content_types: contentTypes,
        default_contents: (pool.default_contents || []).filter((item) => contentTypes.includes(item)),
      }
    })
  }

  const toggleDefault = (disk: StorageDisk, content: string) => {
    setPools((current) => {
      const index = current.findIndex((pool) => poolForDisk(pool, disk))
      const base = index >= 0 ? current[index] : defaultPoolForDisk(disk)
      if (!(base.content_types || []).includes(content)) return current
      const hasDefault = (base.default_contents || []).includes(content)
      const baseDefaults = (base.default_contents || []).filter((value) => value !== content)
      const cleared = current.map((item) => ({
        ...item,
        default_contents: (item.default_contents || []).filter((value) => value !== content),
      }))
      const nextPool = {
        ...base,
        default_contents: hasDefault ? baseDefaults : [...baseDefaults, content],
      }
      if (index >= 0) {
        return cleared.map((item, i) => i === index ? nextPool : item)
      }
      return [...cleared, nextPool]
    })
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-black"></div>
      </div>
    )
  }

  return (
    <div className="min-w-0 space-y-5">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold text-black dark:text-white">{t('存储管理')}</h1>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">{t('只显示已挂载磁盘；勾选后，对应功能可以选择该磁盘保存数据。')}</p>
        </div>
        <div className="flex shrink-0 gap-2">
          <button onClick={fetchData} className="inline-flex items-center gap-2 rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50">
            <RefreshCw className="h-4 w-4" />{t('刷新')}
          </button>
          <button onClick={save} disabled={saving} className="inline-flex items-center gap-2 rounded-md bg-black px-3 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50">
            <Save className="h-4 w-4" />{t(saving ? '保存中...' : '保存')}
          </button>
        </div>
      </div>

      {saveMessage && (
        <div
          role="status"
          aria-live="polite"
          className={`flex items-center gap-2 rounded-md border px-3 py-2 text-sm ${
            saveMessage.type === 'success'
              ? 'border-emerald-200 bg-emerald-50 text-emerald-800'
              : 'border-red-200 bg-red-50 text-red-700'
          }`}
        >
          {saveMessage.type === 'success'
            ? <CheckCircle2 className="h-4 w-4 shrink-0" />
            : <AlertCircle className="h-4 w-4 shrink-0" />}
          <span>{t(saveMessage.text)}</span>
        </div>
      )}

      <div className="overflow-hidden rounded-lg border border-gray-200 bg-white text-sm">
        <div className="hidden grid-cols-[minmax(170px,0.65fr)_minmax(320px,1.2fr)_minmax(480px,1.8fr)] gap-4 border-b border-gray-200 bg-gray-50 px-4 py-3 text-xs text-gray-500 2xl:grid">
          <div className="font-medium">{t('磁盘')}</div>
          <div className="font-medium">{t('空间分布')}</div>
          <div className="font-medium">{t('用于存储')}</div>
        </div>
        {mountedDisks.length === 0 ? (
          <div className="px-4 py-10 text-center text-gray-400">{t('未检测到已挂载磁盘')}</div>
        ) : (
          <div className="divide-y divide-gray-100">
            {mountedDisks.map((disk) => {
              const pool = pools.find((item) => poolForDisk(item, disk))
              const contentUsage = contentUsageMap(pool?.content_usage || disk.content_usage || [])
              const clicdUsed = pool?.clicd_used_bytes || disk.clicd_used_bytes || 0
              return (
                <section
                  key={`${disk.path}-${disk.mount_point}`}
                  className="grid min-w-0 grid-cols-1 gap-4 px-4 py-4 hover:bg-gray-50/70 2xl:grid-cols-[minmax(170px,0.65fr)_minmax(320px,1.2fr)_minmax(480px,1.8fr)]"
                >
                  <div className="min-w-0">
                    <div className="mb-2 text-xs font-medium text-gray-500 2xl:hidden">{t('磁盘')}</div>
                    <div className="flex min-w-0 items-start gap-3">
                      <div className="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-gray-100 text-gray-600">
                        <HardDrive className="h-5 w-5" />
                      </div>
                      <div className="min-w-0">
                        <div className="truncate font-mono text-xs font-medium text-gray-900" title={disk.path || disk.name}>{disk.path || disk.name}</div>
                        <div className="mt-1 truncate text-xs text-gray-500" title={disk.model || disk.fstype || disk.type || '-'}>{disk.model || disk.fstype || disk.type || '-'}</div>
                        <div className="mt-1 truncate font-mono text-xs text-gray-400" title={disk.mount_point}>{disk.mount_point}</div>
                      </div>
                    </div>
                  </div>
                  <div className="min-w-0">
                    <div className="mb-2 text-xs font-medium text-gray-500 2xl:hidden">{t('空间分布')}</div>
                    <DiskUsageBar disk={disk} contentUsage={contentUsage} clicdUsed={clicdUsed} />
                  </div>
                  <div className="min-w-0">
                    <div className="mb-2 text-xs font-medium text-gray-500 2xl:hidden">{t('用于存储')}</div>
                    <div className="grid min-w-0 grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
                      {contentOptions.map(([value, label]) => {
                        const checked = (pool?.content_types || []).includes(value)
                        const isDefault = (pool?.default_contents || []).includes(value)
                        return (
                          <div key={value} className={`min-w-0 rounded-md border px-2.5 py-2 ${checked ? 'border-gray-300 bg-white' : 'border-gray-200 bg-gray-50'}`}>
                            <label className="flex cursor-pointer items-center gap-2 text-xs text-gray-700">
                              <input className="shrink-0" type="checkbox" checked={checked} onChange={() => toggleContent(disk, value)} />
                              <span className="truncate" title={t(label)}>{t(label)}</span>
                            </label>
                            {checked && (
                              <div className="mt-1.5 flex items-center justify-between gap-2 border-t border-gray-100 pt-1.5">
                                <span className="truncate text-[11px] text-gray-500">{t('默认盘')}</span>
                                <button
                                  type="button"
                                  role="switch"
                                  aria-checked={isDefault}
                                  title={isDefault ? `${t('关闭')} ${t(label)} ${t('默认盘')}` : `${t('设为')} ${t(label)} ${t('默认盘')}`}
                                  onClick={() => toggleDefault(disk, value)}
                                  className={`relative inline-flex h-5 w-9 shrink-0 appearance-none items-center rounded-full border p-0 transition-colors focus:outline-none focus:ring-2 focus:ring-black focus:ring-offset-1 ${isDefault ? 'border-black bg-black' : 'border-gray-300 bg-gray-200'}`}
                                >
                                  <span className={`pointer-events-none absolute left-0.5 top-0.5 block h-4 w-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${isDefault ? 'translate-x-4' : 'translate-x-0'}`} />
                                </button>
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  </div>
                </section>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

function DiskUsageBar({
  disk,
  contentUsage,
  clicdUsed,
}: {
  disk: StorageDisk
  contentUsage: Record<string, number>
  clicdUsed: number
}) {
  const { t } = useLanguage()
  const total = Math.max(0, disk.size_bytes || 0)
  const free = Math.max(0, Math.min(total, disk.free_bytes || 0))
  const used = Math.max(0, total - free)
  const rawContentSegments = contentOptions.map(([value, label]) => ({
      key: value,
      label,
      size: Math.max(0, contentUsage[value] || 0),
      color: contentColors[value],
    }))
  const rawContentTotal = rawContentSegments.reduce((sum, segment) => sum + segment.size, 0)
  const normalizedClicdUsed = Math.max(0, Math.min(used, Math.max(clicdUsed || 0, rawContentTotal)))
  const contentScale = rawContentTotal > normalizedClicdUsed && rawContentTotal > 0
    ? normalizedClicdUsed / rawContentTotal
    : 1
  const contentSegments = rawContentSegments.map((segment) => ({ ...segment, size: segment.size * contentScale }))
  const categorizedClicdUsed = contentSegments.reduce((sum, segment) => sum + segment.size, 0)
  const unclassifiedClicdUsed = Math.max(0, normalizedClicdUsed - categorizedClicdUsed)
  const nonClicdUsed = Math.max(0, used - normalizedClicdUsed)
  const segments = [
    ...contentSegments,
    { key: 'clicd-other', label: 'CLICD 其他', size: unclassifiedClicdUsed, color: '#111827' },
    { key: 'other', label: '非 CLICD', size: nonClicdUsed, color: '#4b5563' },
    { key: 'free', label: '可用空间', size: free, color: '#e5e7eb' },
  ].filter((segment) => segment.size > 0)

  return (
    <div className="w-full min-w-0">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-xs text-gray-600">
        <span>{t('已用')} {formatBytes(used)} / {formatBytes(total)}</span>
        <span>{usagePct(used, total).toFixed(1)}% · {t('可用')} {formatBytes(free)}</span>
      </div>
      <div className="mt-2 flex h-8 w-full overflow-hidden rounded-md border border-gray-300 bg-gray-100">
        {segments.map((segment) => {
          const pct = usagePct(segment.size, total)
          return (
            <div
              key={segment.key}
              title={`${t(segment.label)}: ${formatBytes(segment.size)} (${pct.toFixed(2)}%)`}
              className="flex h-full items-center justify-center overflow-hidden border-r border-white/70 text-[10px] font-medium text-white last:border-r-0"
              style={{ width: `${pct}%`, minWidth: pct > 0 && pct < 0.6 ? '3px' : undefined, backgroundColor: segment.color }}
            >
              {pct >= 9 && <span className={segment.key === 'free' ? 'text-gray-600' : ''}>{t(segment.label)}</span>}
            </div>
          )
        })}
      </div>
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1.5">
        {segments.map((segment) => (
          <div key={segment.key} className="flex items-center gap-1.5 text-[11px] text-gray-600">
            <span className="h-2.5 w-2.5 shrink-0 rounded-sm border border-black/5" style={{ backgroundColor: segment.color }} />
            <span>{t(segment.label)}</span>
            <span className="font-medium text-gray-800">{formatBytes(segment.size)}</span>
            <span className="text-gray-400">{usagePct(segment.size, total).toFixed(1)}%</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function poolForDisk(pool: StoragePool, disk: StorageDisk) {
  if (!disk.mount_point) return false
  const mount = cleanPath(disk.mount_point)
  const poolMount = cleanPath(pool.mount_point || '')
  const poolPath = cleanPath(pool.path || '')
  return poolMount === mount || poolPath === mount || poolPath.startsWith(`${mount}/`)
}

function defaultPoolForDisk(disk: StorageDisk): StoragePool {
  const mount = cleanPath(disk.mount_point || '/')
  const baseName = mount === '/' ? 'system' : mount.split('/').filter(Boolean).pop() || disk.name || 'disk'
  const primaryContents = mount === '/' ? contentOptions.map(([value]) => value) : []
  return {
    id: `disk-${slugID(mount === '/' ? 'root' : baseName)}`,
    name: `${baseName} (${disk.path || disk.name})`,
    path: mount === '/' ? '/var/lib/clicd' : `${mount}/clicd`,
    content_types: primaryContents,
    default_contents: [...primaryContents],
    enabled: true,
    mount_point: disk.mount_point,
  }
}

function cleanPath(value: string) {
  return value.replace(/\\/g, '/').replace(/\/+$/g, '') || '/'
}

function slugID(value: string) {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'storage'
}

function contentUsageMap(items: Array<{ content_type: string; size_bytes: number }>) {
  return items.reduce<Record<string, number>>((acc, item) => {
    acc[item.content_type] = (acc[item.content_type] || 0) + (item.size_bytes || 0)
    return acc
  }, {})
}

function usagePct(used: number, total: number) {
  if (!total || total <= 0) return 0
  return Math.max(0, Math.min(100, (used / total) * 100))
}

function formatBytes(bytes: number) {
  if (!bytes) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index++
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}
