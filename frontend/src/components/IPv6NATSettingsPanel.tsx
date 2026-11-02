import { useEffect, useState } from 'react'
import { RefreshCw, Save } from 'lucide-react'
import { useLanguage } from '../contexts/LanguageContext'
import { getIPv6NATSettings, updateIPv6NATSettings, type IPv6NATSettings, type IPv6NATStatus } from '../services/api'

export default function IPv6NATSettingsPanel({ onSaved }: { onSaved?: () => void }) {
  const { language } = useLanguage()
  const en = language === 'en'
  const [status, setStatus] = useState<IPv6NATStatus | null>(null)
  const [draft, setDraft] = useState<IPv6NATSettings>({ enabled: false, subnet: 'fd42:636c:6963::/64', interface: '' })
  const [saving, setSaving] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const load = async () => {
    setLoading(true)
    setError('')
    try {
      const result = (await getIPv6NATSettings()).data.data
      if (!result) throw new Error('Missing IPv6 NAT settings')
      setStatus(result)
      setDraft(result.settings)
    } catch (err: unknown) {
      setStatus(null)
      setError(apiError(err, en ? 'Could not load IPv6 NAT settings' : '无法读取 IPv6 NAT 设置'))
    } finally { setLoading(false) }
  }
  useEffect(() => { void load() }, [])
  const save = async () => {
    if (loading || saving || !status) return
    const subnet = draft.subnet.trim()
    const uplink = (draft.interface || '').trim()
    if (!isULASubnet(subnet)) {
      setError(en ? 'Enter a valid private IPv6 ULA /64 subnet' : '请输入有效的 IPv6 ULA /64 内网网段')
      return
    }
    if (uplink && (!/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,14}$/.test(uplink) || uplink === 'lo' || uplink === 'lxcbr0')) {
      setError(en ? 'Enter a valid external interface name, or leave it blank' : '请输入有效的出口网卡名称，或留空自动检测')
      return
    }
    setSaving(true)
    setError('')
    try {
      const result = (await updateIPv6NATSettings({ ...draft, subnet, interface: uplink })).data.data
      if (result) { setStatus(result); setDraft(result.settings) }
      onSaved?.()
    } catch (err: unknown) {
      setError(apiError(err, en ? 'Could not save IPv6 NAT settings' : 'IPv6 NAT 设置保存失败'))
    } finally { setSaving(false) }
  }
  const fieldClass = 'mt-1 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-black focus:outline-none focus:ring-2 focus:ring-black'
  return (
    <section className="rounded-lg border border-gray-200 bg-white">
      <div className="flex items-center justify-between gap-3 border-b border-gray-100 px-5 py-4">
        <div>
          <h2 className="font-medium text-gray-900">{en ? 'LXC IPv6 NAT' : 'LXC IPv6 NAT'}</h2>
          <p className="mt-1 text-xs text-gray-500">{en ? 'Share the host IPv6 address, including a single /128, using private container addresses.' : '为容器分配内网 IPv6，通过宿主机共享出网，支持只有单个 /128 地址的宿主机。'}</p>
        </div>
        <button type="button" onClick={() => void load()} disabled={loading || saving} className="rounded-md p-2 text-gray-500 hover:bg-gray-100 disabled:opacity-50" aria-label={en ? 'Refresh IPv6 NAT' : '刷新 IPv6 NAT'}><RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /></button>
      </div>
      <div className="space-y-4 p-5">
        {error && <p role="alert" className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>}
        {status && <div className="text-xs text-gray-500">{status.host_address ? `${status.interface} · ${status.host_address} · ${status.bridge} ${status.gateway}` : status.reason}</div>}
        <label className="flex items-center gap-2 text-sm text-gray-800">
          <input type="checkbox" checked={draft.enabled} disabled={loading || saving} onChange={event => setDraft(value => ({ ...value, enabled: event.target.checked }))} />
          {en ? 'Enable IPv6 NAT bridge' : '启用 IPv6 NAT 网桥'}
        </label>
        <div className="grid gap-4 sm:grid-cols-2">
          <label className="text-xs font-medium text-gray-600">{en ? 'Private IPv6 subnet (ULA /64)' : '内网 IPv6 网段（ULA /64）'}<input className={fieldClass} value={draft.subnet} disabled={loading || saving} onChange={event => setDraft(value => ({ ...value, subnet: event.target.value }))} placeholder="fd42:636c:6963::/64" /></label>
          <label className="text-xs font-medium text-gray-600">{en ? 'Uplink interface (blank = automatic)' : '出口网卡（留空自动检测）'}<input className={fieldClass} value={draft.interface || ''} disabled={loading || saving} onChange={event => setDraft(value => ({ ...value, interface: event.target.value }))} placeholder={status?.interface || 'eth0'} /></label>
        </div>
        <p className="text-xs leading-5 text-gray-500">{en ? 'IPv6 ports follow IPv4 mappings by default. Each container can switch to independent IPv6 port management. Changes apply to managed LXC bridge networks; routed public IPv6 and KVM remain separate.' : 'IPv6 端口默认同步 IPv4 映射，可在容器详情中关闭同步并独立管理。此设置作用于受管理的 LXC 网桥网络；公网 IPv6 地址池和 KVM 使用各自的网络配置。'}</p>
        <div className="flex justify-end"><button type="button" onClick={() => void save()} disabled={loading || saving || !status} className="inline-flex items-center gap-2 rounded-md bg-black px-4 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50"><Save className="h-4 w-4" />{saving ? (en ? 'Saving…' : '保存中…') : (en ? 'Save' : '保存')}</button></div>
      </div>
    </section>
  )
}

function isULASubnet(value: string) {
  const match = /^([a-fA-F0-9:]+)\/64$/.exec(value)
  if (!match || !/^f[cd][a-fA-F0-9]{2}$/i.test(match[1].split(':')[0])) return false
  try {
    new URL(`http://[${match[1]}]/`)
    return true
  } catch {
    return false
  }
}

function apiError(error: unknown, fallback: string) {
  return (error as { response?: { data?: { message?: string } } })?.response?.data?.message || fallback
}
