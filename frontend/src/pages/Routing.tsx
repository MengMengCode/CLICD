import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Globe2, Network, Pencil, Plus, RefreshCw, Router, Save, Search, Server, Trash2, X } from 'lucide-react'
import { useNavigate } from 'react-router'
import { useLanguage, type Language } from '../contexts/LanguageContext'
import {
  getRoutingInfo,
  updateRoutingIPv6Prefixes,
  updateRoutingIPv4Pool,
  updateRoutingPools,
  type IPv4Route,
  type IPv6Route,
  type LANDHCPRoute,
  type IPv6PrefixInfo,
  type NAT4PortRange,
  type NAT4Route,
  type PublicIPv4Info,
  type RoutingInfo,
} from '../services/api'

export default function Routing() {
  const navigate = useNavigate()
  const { language } = useLanguage()
  const text = routingText[language]
  const [routing, setRouting] = useState<RoutingInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [editingIPv4, setEditingIPv4] = useState(false)
  const [ipv4EditMode, setIPv4EditMode] = useState<'pool' | 'address'>('pool')
  const [editingIPv4Address, setEditingIPv4Address] = useState('')
  const [savingIPv4, setSavingIPv4] = useState(false)
  const [ipv4Draft, setIPv4Draft] = useState<(PublicIPv4Info & { _id: number })[]>([])
  const nextDraftId = useRef(0)
  const [editingNAT4, setEditingNAT4] = useState(false)
  const [savingNAT4, setSavingNAT4] = useState(false)
  const [nat4Draft, setNAT4Draft] = useState<NAT4PortRange>({ start: 20000, end: 65535 })
  const [editingIPv6, setEditingIPv6] = useState(false)
  const [savingIPv6, setSavingIPv6] = useState(false)
  const [ipv6Draft, setIPv6Draft] = useState<(IPv6PrefixInfo & { _id: number })[]>([])
  const [nat4Page, setNat4Page] = useState(1)
  const [ipv6Page, setIPv6Page] = useState(1)
  const [nat4Search, setNat4Search] = useState('')
  const [ipv6Search, setIPv6Search] = useState('')

  const fetchData = useCallback(async () => {
    try {
      const res = await getRoutingInfo()
      setRouting(res.data.data || null)
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const publicIPv4s = routing?.public_ipv4_addresses || []
  const ipv4Assignments = routing?.ipv4_assignments || []
  const lanDHCPAssignments = routing?.lan_dhcp_assignments || []
  const nat4Mappings = routing?.nat4_mappings || []
  const ipv6Prefixes = routing?.ipv6_prefixes || []
  const ipv6Assignments = routing?.ipv6_assignments || []
  const nat4Range = routing?.nat4_port_range || { start: 20000, end: 65535 }
  const nat4Networks = routing?.nat4_networks
  const defaultIPv4Interface = routing?.host_public_ipv4?.interface || publicIPv4s[0]?.interface || 'eth0'
  const defaultIPv4Gateway = routing?.host_public_ipv4?.gateway || publicIPv4s[0]?.gateway || ''
  const defaultIPv4PrefixLen = routing?.host_public_ipv4?.prefix_len || publicIPv4s[0]?.prefix_len || 32
  const defaultIPv6Interface = ipv6Prefixes[0]?.interface || defaultIPv4Interface
  const defaultIPv6Gateway = ipv6Prefixes[0]?.gateway || ''

  useEffect(() => {
    if (!editingIPv4) {
      setIPv4Draft(publicIPv4s.map((ip) => ({ ...ip, _id: nextDraftId.current++ })))
    }
  }, [editingIPv4, publicIPv4s])

  const assignedIPv4 = useMemo(() => {
    const byAddress = new Map<string, IPv4Route>()
    ipv4Assignments.forEach((item) => byAddress.set(item.address, item))
    return byAddress
  }, [ipv4Assignments])

  const startEditIPv4 = () => {
    setIPv4Draft(publicIPv4s.map((ip) => ({ ...ip, _id: nextDraftId.current++ })))
    setIPv4EditMode('pool')
    setEditingIPv4Address('')
    setEditingIPv4(true)
  }

  const startEditIPv4Address = (ip: PublicIPv4Info) => {
    setIPv4Draft([{ ...ip, _id: nextDraftId.current++ }])
    setIPv4EditMode('address')
    setEditingIPv4Address(ip.address)
    setEditingIPv4(true)
  }

  const closeEditIPv4 = () => {
    setEditingIPv4(false)
    setIPv4EditMode('pool')
    setEditingIPv4Address('')
    setIPv4Draft([])
  }

  const addIPv4Row = () => {
    setIPv4Draft((items) => [
      ...items,
      {
        _id: nextDraftId.current++,
        address: '',
        interface: defaultIPv4Interface,
        prefix: '',
        prefix_len: defaultIPv4PrefixLen,
        subnet_mask: subnetMaskFromPrefixLen(defaultIPv4PrefixLen),
        gateway: defaultIPv4Gateway,
        source: 'manual',
      },
    ])
  }

  const updateIPv4Draft = (index: number, patch: Partial<PublicIPv4Info>) => {
    setIPv4Draft((items) => items.map((item, i) => (i === index ? { ...item, ...patch } : item)))
  }

  const saveIPv4Pool = async () => {
    setSavingIPv4(true)
    try {
      const draftItems = ipv4Draft
        .map(({ _id, ...item }) => ({
          ...item,
          address: (item.address || '').trim(),
          interface: (item.interface || defaultIPv4Interface).trim(),
          gateway: (item.gateway || defaultIPv4Gateway).trim(),
          prefix_len: Number(item.prefix_len || defaultIPv4PrefixLen),
        }))
        .filter((item) => item.address)
      if (draftItems.some((item) => !item.gateway)) {
        alert(text.ipv4GatewayRequired)
        return
      }
      if (ipv4EditMode === 'address' && draftItems.length === 0) {
        alert(text.ipv4AddressRequired)
        return
      }
      const items = ipv4EditMode === 'address'
        ? mergeIPv4PoolItem(publicIPv4s, editingIPv4Address, draftItems[0])
        : draftItems
      const res = await updateRoutingIPv4Pool(items)
      setRouting(res.data.data || null)
      setEditingIPv4(false)
    } catch (err: any) {
      alert(err?.response?.data?.message || text.saveIPv4PoolFailed)
    } finally {
      setSavingIPv4(false)
    }
  }

  const startEditNAT4 = () => {
    setNAT4Draft({ start: nat4Range.start || 20000, end: nat4Range.end || 65535 })
    setEditingNAT4(true)
  }

  const saveNAT4Range = async () => {
    const start = Math.round(Number(nat4Draft.start || 0))
    const end = Math.round(Number(nat4Draft.end || 0))
    if (start < 1 || start > 65535 || end < 1 || end > 65535 || start > end) {
      alert(text.nat4RangeInvalid)
      return
    }
    setSavingNAT4(true)
    try {
      const res = await updateRoutingPools({ nat4_port_range: { start, end } })
      setRouting(res.data.data || null)
      setEditingNAT4(false)
    } catch (err: any) {
      alert(err?.response?.data?.message || text.saveNAT4RangeFailed)
    } finally {
      setSavingNAT4(false)
    }
  }

  const startEditIPv6 = () => {
    setIPv6Draft(ipv6Prefixes.map((prefix) => ({ ...prefix, _id: nextDraftId.current++ })))
    setEditingIPv6(true)
  }

  const addIPv6Row = () => {
    setIPv6Draft((items) => [
      ...items,
      {
        _id: nextDraftId.current++,
        prefix: '',
        address: '',
        prefix_len: 64,
        interface: defaultIPv6Interface,
        gateway: defaultIPv6Gateway,
        source: 'manual',
      },
    ])
  }

  const updateIPv6Draft = (index: number, patch: Partial<IPv6PrefixInfo>) => {
    setIPv6Draft((items) => items.map((item, i) => (i === index ? { ...item, ...patch } : item)))
  }

  const saveIPv6Prefixes = async () => {
    setSavingIPv6(true)
    try {
      const items = ipv6Draft
        .map(({ _id, ...item }) => ({
          ...item,
          prefix: (item.prefix || '').trim(),
          address: (item.address || '').trim(),
          interface: (item.interface || defaultIPv6Interface).trim(),
          gateway: (item.gateway || '').trim(),
          prefix_len: Number(item.prefix_len || 0),
        }))
        .filter((item) => item.prefix || item.address)
      if (items.some((item) => !item.interface)) {
        alert(text.ipv6InterfaceRequired)
        return
      }
      const res = await updateRoutingIPv6Prefixes(items)
      setRouting(res.data.data || null)
      setEditingIPv6(false)
    } catch (err: any) {
      alert(err?.response?.data?.message || text.saveIPv6PrefixesFailed)
    } finally {
      setSavingIPv6(false)
    }
  }

  const filteredNat4 = useMemo(() => {
    const q = nat4Search.toLowerCase().trim()
    if (!q) return nat4Mappings
    return nat4Mappings.filter((item) => matchesNat4(item, q))
  }, [nat4Mappings, nat4Search])

  const filteredIPv6 = useMemo(() => {
    const q = ipv6Search.toLowerCase().trim()
    if (!q) return ipv6Assignments
    return ipv6Assignments.filter((item) => matchesIPv6(item, q))
  }, [ipv6Assignments, ipv6Search])

  useEffect(() => { setNat4Page(1) }, [nat4Search])
  useEffect(() => { setIPv6Page(1) }, [ipv6Search])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-black" />
      </div>
    )
  }

  const pageSize = 10
  const nat4TotalPages = Math.max(1, Math.ceil(filteredNat4.length / pageSize))
  const ipv6TotalPages = Math.max(1, Math.ceil(filteredIPv6.length / pageSize))
  const currentNat4Page = Math.min(nat4Page, nat4TotalPages)
  const currentIPv6Page = Math.min(ipv6Page, ipv6TotalPages)
  const pagedNat4Mappings = filteredNat4.slice((currentNat4Page - 1) * pageSize, currentNat4Page * pageSize)
  const pagedIPv6Assignments = filteredIPv6.slice((currentIPv6Page - 1) * pageSize, currentIPv6Page * pageSize)
  const editingIPv4Assignment = editingIPv4Address ? assignedIPv4.get(editingIPv4Address) : undefined

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold text-black">{text.pageTitle}</h1>
          <p className="mt-1 text-sm text-gray-500">{text.pageSubtitle}</p>
        </div>
        <button
          onClick={() => { setRefreshing(true); fetchData() }}
          disabled={refreshing}
          className="inline-flex items-center gap-2 rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50"
        >
          <RefreshCw className={`h-4 w-4 ${refreshing ? 'animate-spin' : ''}`} />
          {text.refresh}
        </button>
      </div>

      <div className="grid gap-4 md:grid-cols-4">
        <CapacityCard
          title={text.nat4Ports}
          watermark="NAT4"
          remaining={routing?.nat4.remaining || '0'}
          total={routing?.nat4.total || '0'}
          used={routing?.nat4.used || 0}
          label={text.remainingTotal}
          usedLabel={text.used}
          detail={[
            formatNATRange(nat4Range, language),
            nat4Networks ? `LXC ${nat4Networks.lxc.subnet} · KVM ${nat4Networks.kvm.subnet}` : '',
          ].filter(Boolean).join(' · ')}
          action={
            <button onClick={startEditNAT4} className="rounded p-1.5 text-gray-500 hover:bg-gray-100 hover:text-black" title={text.editNAT4Range}>
              <Pencil className="h-4 w-4" />
            </button>
          }
        />
        <CapacityCard title={text.publicIPv4} watermark="IPv4" remaining={routing?.ipv4.remaining || '0'} total={routing?.ipv4.total || '0'} used={routing?.ipv4.used || 0} label={formatPoolCount(publicIPv4s.length, language)} usedLabel={text.used} />
        <CapacityCard title={text.lanDHCP} watermark="LAN" remaining={String(routing?.lan_dhcp.used || 0)} total={routing?.lan_dhcp.total || 'DHCP'} used={routing?.lan_dhcp.used || 0} label={text.dhcpManagedByLAN} usedLabel={text.used} />
        <CapacityCard title="IPv6" watermark="IPv6" remaining={formatCapacity(routing?.ipv6.remaining || '0', language)} total={formatCapacity(routing?.ipv6.total || '0', language)} used={routing?.ipv6.used || 0} label={formatDetectedPrefixCount(ipv6Prefixes.length, language)} usedLabel={text.used} />
      </div>

      {editingNAT4 && (
        <RouteModal title={text.editNAT4Range} onClose={() => setEditingNAT4(false)}>
          <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <LabeledNumberInput label={text.rangeStart} value={nat4Draft.start} onChange={(value) => setNAT4Draft((draft) => ({ ...draft, start: value }))} min={1} max={65535} />
              <LabeledNumberInput label={text.rangeEnd} value={nat4Draft.end} onChange={(value) => setNAT4Draft((draft) => ({ ...draft, end: value }))} min={1} max={65535} />
            </div>
            <div className="flex items-center justify-end gap-2">
              <button onClick={() => setEditingNAT4(false)} disabled={savingNAT4} className="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-50">
                {text.cancel}
              </button>
              <button onClick={saveNAT4Range} disabled={savingNAT4} className="inline-flex items-center gap-1.5 rounded-md bg-black px-3 py-1.5 text-xs text-white hover:bg-gray-800 disabled:opacity-50">
                <Save className="h-3.5 w-3.5" />
                {savingNAT4 ? text.saving : text.save}
              </button>
            </div>
          </div>
        </RouteModal>
      )}

      <Panel
        title={text.publicIPv4Pool}
        subtitle={formatIPv4PoolSubtitle(publicIPv4s.length, ipv4Assignments.length, language)}
        action={
          <button onClick={startEditIPv4} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50">
            <Plus className="h-3.5 w-3.5" />
            {text.editPool}
          </button>
        }
      >
        {publicIPv4s.length === 0 ? (
          <EmptyState text={text.noPublicIPv4Pool} icon={<Globe2 className="h-7 w-7" />} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[980px] text-sm">
              <thead className="border-b border-gray-200 bg-gray-50 text-xs text-gray-500">
                <tr>
                  <th className="px-4 py-3 text-left font-medium">IPv4</th>
                  <th className="px-4 py-3 text-left font-medium">{text.gateway}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.interface}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.mask}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.assignedTo}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.status}</th>
                  <th className="px-4 py-3 text-right font-medium">{text.action}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {publicIPv4s.map((ip) => {
                  const assigned = assignedIPv4.get(ip.address)
                  return (
                    <tr key={`${ip.interface}-${ip.address}`} className="hover:bg-gray-50">
                      <td className="px-4 py-3 font-mono text-xs text-gray-700">{ip.address}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-600">{ip.gateway || '-'}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-600">{ip.interface || '-'}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-600">{ip.subnet_mask || (ip.prefix_len ? subnetMaskFromPrefixLen(ip.prefix_len) : '-')}</td>
                      <td className="px-4 py-3">
                        {assigned ? (
                          <button onClick={() => navigate(`/container/${assigned.container_id}`)} className="font-medium text-black hover:underline">
                            {assigned.container_name}
                          </button>
                        ) : (
                          <span className="text-gray-400">{text.available}</span>
                        )}
                      </td>
                      <td className="px-4 py-3">{assigned ? <StatusBadge status={assigned.status} language={language} /> : <span className="text-xs text-gray-400">{text.free}</span>}</td>
                      <td className="px-4 py-3 text-right">
                        <button onClick={() => startEditIPv4Address(ip)} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-2.5 py-1.5 text-xs text-gray-700 hover:bg-gray-50">
                          <Pencil className="h-3.5 w-3.5" />
                          {text.edit}
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Panel>

      {editingIPv4 && (
        <RouteModal title={ipv4EditMode === 'pool' ? text.editIPv4Pool : text.editIPv4} onClose={closeEditIPv4} wide>
          <div className="space-y-3">
            {ipv4EditMode === 'address' && (
              <div className="rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <div className="text-xs font-medium uppercase text-gray-400">{text.container}</div>
                    <div className="mt-1 text-sm text-gray-700">{editingIPv4Assignment?.container_name || text.available}</div>
                  </div>
                  {editingIPv4Assignment && (
                    <button onClick={() => navigate(`/container/${editingIPv4Assignment.container_id}`)} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 hover:bg-white">
                      <Server className="h-3.5 w-3.5" />
                      {text.openContainer}
                    </button>
                  )}
                </div>
              </div>
            )}

            <div className="overflow-x-auto">
              <table className="w-full min-w-[860px] text-sm">
                <thead className="border-b border-gray-200 bg-gray-50 text-xs text-gray-500">
                  <tr>
                    <th className="px-3 py-2 text-left font-medium">{text.ipv4CIDR}</th>
                    <th className="px-3 py-2 text-left font-medium">{text.gateway}</th>
                    <th className="px-3 py-2 text-left font-medium">{text.interface}</th>
                    <th className="px-3 py-2 text-left font-medium">{text.mask}</th>
                    {ipv4EditMode === 'pool' && <th className="px-3 py-2 text-right font-medium">{text.action}</th>}
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {ipv4Draft.map((item, index) => (
                    <tr key={item._id}>
                      <td className="px-3 py-2"><input value={item.address || ''} onChange={(e) => updateIPv4Draft(index, { address: e.target.value })} placeholder={text.ipv4CIDR} className={smallInputClass} /></td>
                      <td className="px-3 py-2"><input value={item.gateway || ''} onChange={(e) => updateIPv4Draft(index, { gateway: e.target.value })} placeholder={defaultIPv4Gateway || text.gateway} className={smallInputClass} /></td>
                      <td className="px-3 py-2"><input value={item.interface || ''} onChange={(e) => updateIPv4Draft(index, { interface: e.target.value })} placeholder={defaultIPv4Interface} className={smallInputClass} /></td>
                      <td className="px-3 py-2 font-mono text-xs text-gray-500">{item.subnet_mask || (item.prefix_len ? subnetMaskFromPrefixLen(item.prefix_len) : text.auto)}</td>
                      {ipv4EditMode === 'pool' && (
                        <td className="px-3 py-2 text-right">
                          <button onClick={() => setIPv4Draft((items) => items.filter((_, i) => i !== index))} className="inline-flex items-center justify-center rounded p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600">
                            <Trash2 className="h-4 w-4" />
                          </button>
                        </td>
                      )}
                    </tr>
                  ))}
                  {ipv4Draft.length === 0 && <EmptyRow colSpan={ipv4EditMode === 'pool' ? 5 : 4} text={text.noIPv4InPool} />}
                </tbody>
              </table>
            </div>
            <div className="flex flex-wrap items-center justify-between gap-3">
              {ipv4EditMode === 'pool' ? (
                <button onClick={addIPv4Row} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50">
                  <Plus className="h-3.5 w-3.5" />
                  {text.addIPv4}
                </button>
              ) : (
                <span />
              )}
              <div className="flex items-center gap-2">
                <button onClick={closeEditIPv4} disabled={savingIPv4} className="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-50">
                  {text.cancel}
                </button>
                <button onClick={saveIPv4Pool} disabled={savingIPv4} className="inline-flex items-center gap-1.5 rounded-md bg-black px-3 py-1.5 text-xs text-white hover:bg-gray-800 disabled:opacity-50">
                  <Save className="h-3.5 w-3.5" />
                  {savingIPv4 ? text.saving : text.save}
                </button>
              </div>
            </div>
          </div>
        </RouteModal>
      )}

      <Panel
        title={text.detectedIPv6Prefixes}
        subtitle={formatPrefixCount(ipv6Prefixes.length, language)}
        action={
          <button onClick={startEditIPv6} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50">
            <Pencil className="h-3.5 w-3.5" />
            {text.editPrefixes}
          </button>
        }
      >
        {ipv6Prefixes.length === 0 ? (
          <EmptyState text={text.noIPv6Prefixes} icon={<Router className="h-7 w-7" />} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] text-sm">
              <thead className="border-b border-gray-200 bg-gray-50 text-xs text-gray-500">
                <tr>
                  <th className="px-4 py-3 text-left font-medium">{text.prefix}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.hostAddress}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.interface}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.gateway}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.source}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {ipv6Prefixes.map((prefix) => (
                  <tr key={`${prefix.interface}-${prefix.prefix}`} className="hover:bg-gray-50">
                    <td className="px-4 py-3 font-mono text-xs text-gray-700">{prefix.prefix || `${prefix.address}/${prefix.prefix_len}`}</td>
                    <td className="px-4 py-3 font-mono text-xs text-gray-600">{prefix.address || '-'}</td>
                    <td className="px-4 py-3 font-mono text-xs text-gray-600">{prefix.interface || '-'}</td>
                    <td className="px-4 py-3 font-mono text-xs text-gray-600">{prefix.gateway || '-'}</td>
                    <td className="px-4 py-3 text-xs text-gray-500">{formatSource(prefix.source, language)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>

      {editingIPv6 && (
        <RouteModal title={text.editIPv6Prefixes} onClose={() => setEditingIPv6(false)} wide>
          <div className="space-y-3">
            <div className="overflow-x-auto">
              <table className="w-full min-w-[860px] text-sm">
                <thead className="border-b border-gray-200 bg-gray-50 text-xs text-gray-500">
                  <tr>
                    <th className="px-3 py-2 text-left font-medium">{text.prefix}</th>
                    <th className="px-3 py-2 text-left font-medium">{text.hostAddress}</th>
                    <th className="px-3 py-2 text-left font-medium">{text.interface}</th>
                    <th className="px-3 py-2 text-left font-medium">{text.gateway}</th>
                    <th className="px-3 py-2 text-right font-medium">{text.action}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {ipv6Draft.map((item, index) => (
                    <tr key={item._id}>
                      <td className="px-3 py-2"><input value={item.prefix || ''} onChange={(e) => updateIPv6Draft(index, { prefix: e.target.value })} placeholder="2001:db8:100::/64" className={smallInputClass} /></td>
                      <td className="px-3 py-2"><input value={item.address || ''} onChange={(e) => updateIPv6Draft(index, { address: e.target.value })} placeholder="2001:db8:100::1" className={smallInputClass} /></td>
                      <td className="px-3 py-2"><input value={item.interface || ''} onChange={(e) => updateIPv6Draft(index, { interface: e.target.value })} placeholder={defaultIPv6Interface} className={smallInputClass} /></td>
                      <td className="px-3 py-2"><input value={item.gateway || ''} onChange={(e) => updateIPv6Draft(index, { gateway: e.target.value })} placeholder={text.gateway} className={smallInputClass} /></td>
                      <td className="px-3 py-2 text-right">
                        <button onClick={() => setIPv6Draft((items) => items.filter((_, i) => i !== index))} className="inline-flex items-center justify-center rounded p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600">
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </td>
                    </tr>
                  ))}
                  {ipv6Draft.length === 0 && <EmptyRow colSpan={5} text={text.noIPv6Prefixes} />}
                </tbody>
              </table>
            </div>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <button onClick={addIPv6Row} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50">
                <Plus className="h-3.5 w-3.5" />
                {text.addIPv6Prefix}
              </button>
              <div className="flex items-center gap-2">
                <button onClick={() => setEditingIPv6(false)} disabled={savingIPv6} className="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-50">
                  {text.cancel}
                </button>
                <button onClick={saveIPv6Prefixes} disabled={savingIPv6} className="inline-flex items-center gap-1.5 rounded-md bg-black px-3 py-1.5 text-xs text-white hover:bg-gray-800 disabled:opacity-50">
                  <Save className="h-3.5 w-3.5" />
                  {savingIPv6 ? text.saving : text.save}
                </button>
              </div>
            </div>
          </div>
        </RouteModal>
      )}

      <Panel title={text.lanDHCPAssignments} subtitle={formatAddressSubtitle(lanDHCPAssignments.length, lanDHCPAssignments.length, language)}>
        {lanDHCPAssignments.length === 0 ? (
          <EmptyState text={text.noLANDHCPAssignments} icon={<Network className="h-7 w-7" />} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[980px] text-sm">
              <thead className="border-b border-gray-200 bg-gray-50 text-xs text-gray-500">
                <tr>
                  <th className="px-4 py-3 text-left font-medium">{text.container}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.runtimeName}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.guestIPv4}</th>
                  <th className="px-4 py-3 text-left font-medium">模式</th>
                  <th className="px-4 py-3 text-left font-medium">{text.gateway}</th>
                  <th className="px-4 py-3 text-left font-medium">MAC</th>
                  <th className="px-4 py-3 text-left font-medium">{text.interface}</th>
                  <th className="px-4 py-3 text-left font-medium">{text.status}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {lanDHCPAssignments.map((item: LANDHCPRoute) => (
                  <tr key={`${item.container_id}-${item.interface}-${item.mac_address || item.address}`} className="hover:bg-gray-50">
                    <td className="px-4 py-3">
                      <button onClick={() => navigate(`/container/${item.container_id}`)} className="inline-flex items-center gap-2 text-left font-medium text-black hover:underline">
                        <Server className="h-4 w-4 text-gray-400" />
                        {item.container_name}
                      </button>
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-gray-600">{item.lxc_name}</td>
                    <td className="px-4 py-3 font-mono text-xs text-gray-700">{item.address ? `${item.address}${item.prefix_len ? `/${item.prefix_len}` : ''}` : '-'}</td>
                    <td className="px-4 py-3 text-xs text-gray-600">{item.mode === 'static' ? '手动' : 'DHCP'}</td>
                    <td className="px-4 py-3 font-mono text-xs text-gray-600">{item.gateway || '-'}</td>
                    <td className="px-4 py-3 font-mono text-xs text-gray-600">{item.mac_address || '-'}</td>
                    <td className="px-4 py-3 font-mono text-xs text-gray-600">{item.interface || '-'}</td>
                    <td className="px-4 py-3"><StatusBadge status={item.status} language={language} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>

      <Panel title={text.ipv4NAT} subtitle={formatMappingSubtitle(filteredNat4.length, nat4Mappings.length, language)} action={<SearchBox value={nat4Search} onChange={setNat4Search} placeholder={text.searchNAT} />}>
        {nat4Mappings.length === 0 ? (
          <EmptyState text={text.noIPv4NATMappings} icon={<Network className="h-7 w-7" />} />
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[940px] text-sm">
                <thead className="border-b border-gray-200 bg-gray-50 text-xs text-gray-500">
                  <tr>
                    <th className="px-4 py-3 text-left font-medium">{text.container}</th>
                    <th className="px-4 py-3 text-left font-medium">{text.runtimeName}</th>
                    <th className="px-4 py-3 text-left font-medium">{text.guestIPv4}</th>
                    <th className="px-4 py-3 text-left font-medium">{text.hostIPv4}</th>
                    <th className="px-4 py-3 text-left font-medium">{text.hostPort}</th>
                    <th className="px-4 py-3 text-left font-medium">{text.guestPort}</th>
                    <th className="px-4 py-3 text-left font-medium">{text.protocol}</th>
                    <th className="px-4 py-3 text-left font-medium">{text.status}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {pagedNat4Mappings.map((mapping, index) => (
                    <tr key={`${mapping.container_id}-${mapping.host_ip}-${mapping.host_port}-${mapping.protocol}-${index}`} className="hover:bg-gray-50">
                      <td className="px-4 py-3">
                        <button onClick={() => navigate(`/container/${mapping.container_id}`)} className="inline-flex items-center gap-2 text-left font-medium text-black hover:underline">
                          <Server className="h-4 w-4 text-gray-400" />
                          {mapping.container_name}
                        </button>
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-600">{mapping.lxc_name}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-600">{mapping.ip || '-'}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-700">{mapping.host_ip || text.allIPv4}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-700">{mapping.host_port}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-700">{mapping.container_port}</td>
                      <td className="px-4 py-3 uppercase text-gray-600">{mapping.protocol || '-'}</td>
                      <td className="px-4 py-3"><StatusBadge status={mapping.status} language={language} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pagination page={currentNat4Page} totalPages={nat4TotalPages} totalItems={filteredNat4.length} pageSize={pageSize} onPageChange={setNat4Page} language={language} />
          </>
        )}
      </Panel>

      <Panel title={text.ipv6Assignments} subtitle={formatAddressSubtitle(filteredIPv6.length, ipv6Assignments.length, language)} action={<SearchBox value={ipv6Search} onChange={setIPv6Search} placeholder={text.searchIPv6} />}>
        {ipv6Assignments.length === 0 ? (
          <EmptyState text={text.noIPv6Assignments} icon={<Router className="h-7 w-7" />} />
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[820px] text-sm">
                <thead className="border-b border-gray-200 bg-gray-50 text-xs text-gray-500">
                  <tr>
                    <th className="px-4 py-3 text-left font-medium">{text.container}</th>
                    <th className="px-4 py-3 text-left font-medium">{text.runtimeName}</th>
                    <th className="px-4 py-3 text-left font-medium">IPv6</th>
                    <th className="px-4 py-3 text-left font-medium">{text.prefix}</th>
                    <th className="px-4 py-3 text-left font-medium">{text.interface}</th>
                    <th className="px-4 py-3 text-left font-medium">{text.status}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {pagedIPv6Assignments.map((item) => (
                    <tr key={`${item.container_id}-${item.address}`} className="hover:bg-gray-50">
                      <td className="px-4 py-3">
                        <button onClick={() => navigate(`/container/${item.container_id}`)} className="inline-flex items-center gap-2 text-left font-medium text-black hover:underline">
                          <Server className="h-4 w-4 text-gray-400" />
                          {item.container_name}
                        </button>
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-600">{item.lxc_name}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-700">{item.address}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-600">/{item.prefix_len || '-'}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-600">{item.interface || '-'}</td>
                      <td className="px-4 py-3"><StatusBadge status={item.status} language={language} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pagination page={currentIPv6Page} totalPages={ipv6TotalPages} totalItems={filteredIPv6.length} pageSize={pageSize} onPageChange={setIPv6Page} language={language} />
          </>
        )}
      </Panel>
    </div>
  )
}

function Panel({ title, subtitle, action, children }: { title: string; subtitle?: string; action?: ReactNode; children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-lg border border-gray-200 bg-white">
      <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-4 py-3">
        <div>
          <div className="text-sm font-medium text-black">{title}</div>
          {subtitle && <div className="mt-1 text-xs text-gray-500">{subtitle}</div>}
        </div>
        {action}
      </div>
      {children}
    </div>
  )
}

function RouteModal({ title, onClose, wide, children }: { title: string; onClose: () => void; wide?: boolean; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className={`flex max-h-[88vh] w-full flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl ${wide ? 'max-w-5xl' : 'max-w-xl'}`}>
        <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-5 py-4">
          <div className="text-base font-semibold text-black">{title}</div>
          <button onClick={onClose} className="rounded p-1 text-gray-500 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>
        <div className="overflow-y-auto p-4">
          {children}
        </div>
      </div>
    </div>
  )
}

function SearchBox({ value, onChange, placeholder }: { value: string; onChange: (value: string) => void; placeholder: string }) {
  return (
    <div className="relative w-48">
      <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-400" />
      <input
        type="text"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className="w-full rounded-md border border-gray-300 bg-white py-1.5 pl-8 pr-7 text-xs text-black focus:outline-none focus:ring-1 focus:ring-black"
      />
      {value && (
        <button onClick={() => onChange('')} className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600">
          <X className="h-3 w-3" />
        </button>
      )}
    </div>
  )
}

function Pagination({ page, totalPages, totalItems, pageSize, onPageChange, language }: {
  page: number
  totalPages: number
  totalItems: number
  pageSize: number
  onPageChange: (page: number) => void
  language: Language
}) {
  if (totalPages <= 1) return null
  const text = routingText[language]
  const start = (page - 1) * pageSize + 1
  const end = Math.min(page * pageSize, totalItems)
  return (
    <div className="flex items-center justify-between gap-3 border-t border-gray-200 px-4 py-3 text-sm">
      <div className="text-xs text-gray-500">{formatShowingRange(start, end, totalItems, language)}</div>
      <div className="flex items-center gap-2">
        <button onClick={() => onPageChange(Math.max(1, page - 1))} disabled={page <= 1} className="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-50">
          {text.previous}
        </button>
        <span className="min-w-16 text-center text-xs text-gray-500">{page} / {totalPages}</span>
        <button onClick={() => onPageChange(Math.min(totalPages, page + 1))} disabled={page >= totalPages} className="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-50">
          {text.next}
        </button>
      </div>
    </div>
  )
}

function CapacityCard({ title, watermark, remaining, total, used, label, usedLabel, detail, action }: {
  title: string
  watermark: string
  remaining: string
  total: string
  used: number
  label: string
  usedLabel: string
  detail?: string
  action?: ReactNode
}) {
  return (
    <div className="relative overflow-hidden rounded-lg border border-gray-200 bg-white p-4">
      <div className="pointer-events-none absolute bottom-1 right-3 select-none bg-gradient-to-br from-black via-gray-600 to-gray-300 bg-clip-text text-[44px] font-black italic tracking-wide text-transparent opacity-25 -skew-x-12">
        {watermark}
      </div>
      <div className="relative z-10">
        <div className="flex items-start justify-between gap-3">
          <div>
          <div className="text-sm font-medium text-gray-700">{title}</div>
          <div className="mt-2 flex items-end gap-2">
            <span className="text-2xl font-semibold text-black">{remaining}</span>
            <span className="pb-1 text-sm text-gray-400">/ {total}</span>
          </div>
          </div>
          {action}
        </div>
      </div>
      <div className="relative z-10 mt-3 text-xs text-gray-500">{label}</div>
      <div className="relative z-10 mt-1 text-xs text-gray-400">{usedLabel} {used}</div>
      {detail && <div className="relative z-10 mt-1 font-mono text-xs text-gray-400">{detail}</div>}
    </div>
  )
}

function LabeledNumberInput({ label, value, onChange, min, max }: {
  label: string
  value: number
  onChange: (value: number) => void
  min: number
  max: number
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-gray-500">{label}</span>
      <input
        type="number"
        min={min}
        max={max}
        value={value || ''}
        onChange={(event) => onChange(Number(event.target.value))}
        className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-800 focus:outline-none focus:ring-1 focus:ring-black"
      />
    </label>
  )
}

function EmptyState({ icon, text }: { icon: ReactNode; text: string }) {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
      <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-lg bg-gray-100 text-gray-500">{icon}</div>
      <div className="text-sm font-medium text-gray-700">{text}</div>
    </div>
  )
}

function EmptyRow({ colSpan, text }: { colSpan: number; text: string }) {
  return (
    <tr>
      <td colSpan={colSpan} className="px-3 py-8 text-center text-sm text-gray-400">{text}</td>
    </tr>
  )
}

function StatusBadge({ status, language }: { status: string; language: Language }) {
  const running = status === 'running'
  return (
    <span className={`rounded px-2 py-1 text-xs ${running ? 'bg-green-50 text-green-700' : 'bg-gray-100 text-gray-700'}`}>
      {formatContainerStatus(status, language)}
    </span>
  )
}

function matchesNat4(item: NAT4Route, query: string) {
  return (
    String(item.host_port).includes(query) ||
    String(item.container_port).includes(query) ||
    item.container_name.toLowerCase().includes(query) ||
    item.lxc_name.toLowerCase().includes(query) ||
    (item.ip || '').toLowerCase().includes(query) ||
    (item.host_ip || '').toLowerCase().includes(query)
  )
}

function matchesIPv6(item: IPv6Route, query: string) {
  return (
    (item.address || '').toLowerCase().includes(query) ||
    item.container_name.toLowerCase().includes(query) ||
    item.lxc_name.toLowerCase().includes(query) ||
    (item.interface || '').toLowerCase().includes(query)
  )
}

function formatCapacity(value: string, language: Language): string {
  if (value === 'large') return routingText[language].large
  return value
}

function subnetMaskFromPrefixLen(prefixLen: number): string {
  if (!Number.isFinite(prefixLen) || prefixLen < 0 || prefixLen > 32) return '-'
  const mask = prefixLen === 0 ? 0 : (0xffffffff << (32 - prefixLen)) >>> 0
  return [24, 16, 8, 0].map((shift) => (mask >>> shift) & 255).join('.')
}

function mergeIPv4PoolItem(pool: PublicIPv4Info[], originalAddress: string, replacement: PublicIPv4Info): PublicIPv4Info[] {
  let replaced = false
  const next = pool.map((item) => {
    if (item.address !== originalAddress) return item
    replaced = true
    return replacement
  })
  if (!replaced) {
    next.push(replacement)
  }
  return next
}

const routingText = {
  zh: {
    pageTitle: '路由管理',
    pageSubtitle: 'NAT4、公网 IPv4 池和 IPv6 地址分配',
    refresh: '刷新',
    nat4Ports: 'NAT4 端口',
    editNAT4Range: '编辑 NAT4 范围',
    rangeStart: '起始端口',
    rangeEnd: '结束端口',
    nat4RangeInvalid: 'NAT4 范围必须是 1-65535，且起始端口不能大于结束端口',
    saveNAT4RangeFailed: '保存 NAT4 范围失败',
    remainingTotal: '剩余 / 总数',
    publicIPv4: '公网 IPv4',
    lanDHCP: '局域网 DHCP',
    dhcpManagedByLAN: '由局域网 DHCP 分配',
    lanDHCPAssignments: '局域网 DHCP 分配',
    noLANDHCPAssignments: '暂无局域网 DHCP 分配',
    publicIPv4Pool: '公网 IPv4 池',
    editPool: '编辑 IP 池',
    noPublicIPv4Pool: '暂未配置公网 IPv4 池',
    gateway: '网关',
    interface: '网卡',
    mask: '掩码',
    assignedTo: '分配给',
    status: '状态',
    action: '操作',
    available: '可用',
    free: '空闲',
    edit: '修改',
    editIPv4Pool: '编辑 IPv4 池',
    editIPv4: '修改 IPv4',
    ipv4GatewayRequired: 'IPv4 网关不能为空',
    ipv4AddressRequired: 'IPv4 地址不能为空',
    saveIPv4PoolFailed: '保存 IPv4 池失败',
    ipv4CIDR: 'IPv4 / CIDR',
    container: '容器',
    openContainer: '打开容器',
    auto: '自动',
    noIPv4InPool: 'IPv4 池内暂无地址',
    addIPv4: '添加 IPv4',
    cancel: '取消',
    save: '保存',
    saving: '保存中...',
    detectedIPv6Prefixes: '检测到的 IPv6 前缀',
    editPrefixes: '编辑前缀',
    editIPv6Prefixes: '编辑 IPv6 前缀',
    addIPv6Prefix: '添加 IPv6 前缀',
    noIPv6Prefixes: '暂无 IPv6 前缀',
    ipv6InterfaceRequired: 'IPv6 网卡不能为空',
    saveIPv6PrefixesFailed: '保存 IPv6 前缀失败',
    prefix: '前缀',
    hostAddress: '宿主地址',
    source: '来源',
    local: '本机',
    manual: '手动',
    ipv4NAT: 'IPv4 NAT',
    searchNAT: '搜索 NAT...',
    noIPv4NATMappings: '暂无 IPv4 NAT 映射',
    runtimeName: '运行时名称',
    guestIPv4: '客户机 IPv4',
    hostIPv4: '宿主 IPv4',
    hostPort: '宿主端口',
    guestPort: '客户机端口',
    protocol: '协议',
    allIPv4: '全部 IPv4',
    ipv6Assignments: 'IPv6 地址分配',
    searchIPv6: '搜索 IPv6...',
    noIPv6Assignments: '暂无 IPv6 地址分配',
    previous: '上一页',
    next: '下一页',
    used: '已用',
    running: '运行中',
    stopped: '已停止',
    unknown: '未知',
    large: '大量',
  },
  en: {
    pageTitle: 'Routing',
    pageSubtitle: 'NAT4, public IPv4 pool, and IPv6 assignments',
    refresh: 'Refresh',
    nat4Ports: 'NAT4 ports',
    editNAT4Range: 'Edit NAT4 range',
    rangeStart: 'Start port',
    rangeEnd: 'End port',
    nat4RangeInvalid: 'NAT4 range must be 1-65535, and start cannot be greater than end',
    saveNAT4RangeFailed: 'Save NAT4 range failed',
    remainingTotal: 'remaining / total',
    publicIPv4: 'Public IPv4',
    lanDHCP: 'LAN DHCP',
    dhcpManagedByLAN: 'Managed by LAN DHCP',
    lanDHCPAssignments: 'LAN DHCP assignments',
    noLANDHCPAssignments: 'No LAN DHCP assignments',
    publicIPv4Pool: 'Public IPv4 pool',
    editPool: 'Edit pool',
    noPublicIPv4Pool: 'No public IPv4 pool configured',
    gateway: 'Gateway',
    interface: 'Interface',
    mask: 'Mask',
    assignedTo: 'Assigned to',
    status: 'Status',
    action: 'Action',
    available: 'Available',
    free: 'Free',
    edit: 'Edit',
    editIPv4Pool: 'Edit IPv4 pool',
    editIPv4: 'Edit IPv4',
    ipv4GatewayRequired: 'IPv4 gateway is required',
    ipv4AddressRequired: 'IPv4 address is required',
    saveIPv4PoolFailed: 'Save IPv4 pool failed',
    ipv4CIDR: 'IPv4 / CIDR',
    container: 'Container',
    openContainer: 'Open container',
    auto: 'Auto',
    noIPv4InPool: 'No IPv4 addresses in the pool',
    addIPv4: 'Add IPv4',
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving...',
    detectedIPv6Prefixes: 'Detected IPv6 prefixes',
    editPrefixes: 'Edit prefixes',
    editIPv6Prefixes: 'Edit IPv6 prefixes',
    addIPv6Prefix: 'Add IPv6 prefix',
    noIPv6Prefixes: 'No IPv6 prefixes',
    ipv6InterfaceRequired: 'IPv6 interface is required',
    saveIPv6PrefixesFailed: 'Save IPv6 prefixes failed',
    prefix: 'Prefix',
    hostAddress: 'Host address',
    source: 'Source',
    local: 'local',
    manual: 'manual',
    ipv4NAT: 'IPv4 NAT',
    searchNAT: 'Search NAT...',
    noIPv4NATMappings: 'No IPv4 NAT mappings',
    runtimeName: 'Runtime name',
    guestIPv4: 'Guest IPv4',
    hostIPv4: 'Host IPv4',
    hostPort: 'Host port',
    guestPort: 'Guest port',
    protocol: 'Protocol',
    allIPv4: 'All IPv4',
    ipv6Assignments: 'IPv6 assignments',
    searchIPv6: 'Search IPv6...',
    noIPv6Assignments: 'No IPv6 assignments',
    previous: 'Previous',
    next: 'Next',
    used: 'Used',
    running: 'Running',
    stopped: 'Stopped',
    unknown: 'Unknown',
    large: 'large',
  },
} as const

function formatPoolCount(count: number, language: Language) {
  return language === 'en' ? `${count} in pool` : `池内 ${count} 个`
}

function formatIPv4PoolSubtitle(total: number, assigned: number, language: Language) {
  return language === 'en'
    ? `${formatAddressCount(total, language)} in pool, ${assigned} assigned`
    : `池内 ${total} 个地址，已分配 ${assigned} 个`
}

function formatDetectedPrefixCount(count: number, language: Language) {
  return language === 'en'
    ? `${count} detected ${count === 1 ? 'prefix' : 'prefixes'}`
    : `检测到 ${count} 个前缀`
}

function formatNATRange(range: NAT4PortRange, language: Language) {
  return language === 'en' ? `range ${range.start}-${range.end}` : `范围 ${range.start}-${range.end}`
}

function formatPrefixCount(count: number, language: Language) {
  return language === 'en' ? `${count} ${count === 1 ? 'prefix' : 'prefixes'}` : `${count} 个前缀`
}

function formatMappingSubtitle(filtered: number, total: number, language: Language) {
  return language === 'en'
    ? `${filtered} of ${total} ${total === 1 ? 'mapping' : 'mappings'}`
    : `显示 ${filtered} 条，共 ${total} 条映射`
}

function formatAddressSubtitle(filtered: number, total: number, language: Language) {
  return language === 'en'
    ? `${filtered} of ${total} ${total === 1 ? 'address' : 'addresses'}`
    : `显示 ${filtered} 个，共 ${total} 个地址`
}

function formatAddressCount(count: number, language: Language) {
  return language === 'en' ? `${count} ${count === 1 ? 'address' : 'addresses'}` : `${count} 个地址`
}

function formatShowingRange(start: number, end: number, total: number, language: Language) {
  return language === 'en' ? `Showing ${start}-${end} of ${total}` : `显示 ${start}-${end}，共 ${total} 条`
}

function formatContainerStatus(status: string, language: Language) {
  const text = routingText[language]
  switch ((status || '').toLowerCase()) {
    case 'running':
      return text.running
    case 'stopped':
      return text.stopped
    default:
      return status || text.unknown
  }
}

function formatSource(source: string | undefined, language: Language) {
  if (!source || source === 'local') return routingText[language].local
  if (source === 'manual') return routingText[language].manual
  return source
}

const smallInputClass = 'w-full rounded border border-gray-300 px-2 py-1.5 font-mono text-xs text-gray-800 focus:outline-none focus:ring-1 focus:ring-black'
