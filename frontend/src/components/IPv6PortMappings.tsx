import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import {
  addIPv6PortMapping,
  deleteIPv6PortMapping,
  getIPv6PortMappings,
  setIPv6NATSync,
  updateIPv6PortMapping,
  type ContainerIdentifier,
  type IPv6PortMappingState,
  type PortMapping,
} from '../services/api'
import { useDialog } from './Dialog'

type Props = {
  containerId: ContainerIdentifier
  isSubUser: boolean
  refreshKey: string
  onUpdated: () => void | Promise<void>
}

type Draft = {
  index: number | null
  hostPort: string
  containerPort: string
  protocol: string
  description: string
}

const inputClass = 'w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-black disabled:bg-gray-100 disabled:text-gray-500'
const buttonClass = 'inline-flex items-center gap-1.5 rounded-md border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50'

function errorMessage(error: unknown, fallback: string) {
  return (error as { response?: { data?: { message?: string } } })?.response?.data?.message || fallback
}

function validPort(value: string) {
  return /^\d+$/.test(value) && Number(value) >= 1 && Number(value) <= 65535
}

function endpoint(address: string, port: number) {
  return address ? `[${address}]:${port}` : `未配置 IPv6:${port}`
}

export default function IPv6PortMappings({ containerId, isSubUser, refreshKey, onUpdated }: Props) {
  const dialog = useDialog()
  const [state, setState] = useState<IPv6PortMappingState | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [draft, setDraft] = useState<Draft | null>(null)
  const stateRef = useRef<IPv6PortMappingState | null>(null)
  const draftRef = useRef<Draft | null>(null)
  const busyRef = useRef(false)
  const requestID = useRef(0)
  draftRef.current = draft

  const acceptState = useCallback((next: IPv6PortMappingState) => {
    const previous = stateRef.current
    if (draftRef.current && previous && (
      previous.sync_ipv4 !== next.sync_ipv4 || previous.enabled !== next.enabled ||
      JSON.stringify(previous.mappings) !== JSON.stringify(next.mappings)
    )) {
      setDraft(null)
      setNotice('映射配置已更新，请重新选择要编辑的映射。')
    }
    stateRef.current = next
    setState(next)
  }, [])

  const load = useCallback(async () => {
    if (busyRef.current) {
      setLoading(false)
      return
    }
    const currentRequest = ++requestID.current
    try {
      const response = await getIPv6PortMappings(containerId)
      if (currentRequest !== requestID.current) return
      if (!response.data.data) throw new Error('missing IPv6 NAT state')
      acceptState(response.data.data)
      setLoadError('')
    } catch (failure) {
      if (currentRequest === requestID.current) setLoadError(errorMessage(failure, '读取 IPv6 NAT 映射失败'))
    } finally {
      if (currentRequest === requestID.current) setLoading(false)
    }
  }, [containerId, acceptState])

  useEffect(() => {
    void load()
    const timer = window.setInterval(() => { void load() }, 5000)
    return () => {
      ++requestID.current
      window.clearInterval(timer)
    }
  }, [load, refreshKey])

  const runMutation = async (operation: () => Promise<void>, success: string) => {
    if (busyRef.current) return
    busyRef.current = true
    ++requestID.current
    setBusy(true)
    setLoading(false)
    setError('')
    setNotice('')
    try {
      await operation()
      setDraft(null)
      setNotice(success)
      await onUpdated()
    } catch (failure) {
      setError(errorMessage(failure, '保存 IPv6 NAT 映射失败'))
    } finally {
      busyRef.current = false
      setBusy(false)
      await load()
    }
  }

  const toggleSync = async () => {
    if (!state?.enabled || isSubUser || busyRef.current) return
    const enabled = !state.sync_ipv4
    if (enabled && !await dialog.confirm('同步 IPv4 映射', '启用后，IPv6 NAT 将跟随 IPv4 端口映射，不能独立编辑。')) return
    await runMutation(async () => {
      const response = await setIPv6NATSync(containerId, enabled)
      if (response.data.data) acceptState(response.data.data)
    }, enabled ? '已启用 IPv4 映射同步' : '已切换到独立 IPv6 NAT 映射')
  }

  const saveMapping = async (event: FormEvent) => {
    event.preventDefault()
    if (!draft || !state?.enabled || state.sync_ipv4) return
    const existing = draft.index === null ? undefined : state.mappings[draft.index]
    if (isSubUser && !existing) return
    if (draft.index === null && state.mappings.length >= state.limit) {
      setError('已达到管理员分配的 IPv6 NAT 端口配额')
      return
    }
    if (!validPort(draft.containerPort) || (!isSubUser && !validPort(draft.hostPort))) {
      setError('端口必须是 1 到 65535 之间的整数')
      return
    }
    const mapping: PortMapping = isSubUser && existing
      ? { ...existing, container_port: Number(draft.containerPort) }
      : {
        host_port: Number(draft.hostPort),
        container_port: Number(draft.containerPort),
        protocol: draft.protocol,
        description: draft.description.trim(),
        ...(existing?.host_ip ? { host_ip: existing.host_ip } : {}),
      }
    await runMutation(async () => {
      const response = draft.index === null
        ? await addIPv6PortMapping(containerId, mapping)
        : await updateIPv6PortMapping(containerId, draft.index, mapping)
      if (response.data.data) acceptState({ ...state, mappings: response.data.data })
    }, 'IPv6 NAT 映射已保存')
  }

  const removeMapping = async (index: number) => {
    if (!state?.enabled || state.sync_ipv4 || isSubUser || busyRef.current) return
    const target = state.mappings[index]
    if (!await dialog.confirm('删除 IPv6 NAT 映射', '确认删除这条 IPv6 NAT 端口映射？')) return
    if (JSON.stringify(target) !== JSON.stringify(stateRef.current?.mappings[index])) {
      setError('映射配置已更新，请重新选择要删除的映射。')
      return
    }
    await runMutation(async () => {
      const response = await deleteIPv6PortMapping(containerId, index)
      if (response.data.data) acceptState({ ...state, mappings: response.data.data })
    }, 'IPv6 NAT 映射已删除')
  }

  const ready = !!state?.enabled && !!state.host_ipv6 && !!state.guest_ipv6
  const canEdit = ready && !!state && !state.sync_ipv4 && !busy
  const canAdd = canEdit && !isSubUser && !!state && state.mappings.length < state.limit
  const canSave = canEdit && (draft?.index !== null || canAdd)

  return (
    <section className="space-y-4 rounded-lg border border-gray-200 bg-white p-5" aria-label="IPv6 NAT 端口映射">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-black">IPv6 NAT 端口映射</h2>
        <div className="flex items-center gap-2">
          <button type="button" className={buttonClass} onClick={() => { void load() }} disabled={busy} title="刷新 IPv6 NAT 映射">
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />刷新
          </button>
          {!isSubUser && (
            <button type="button" className={buttonClass} disabled={!canAdd} onClick={() => {
              setError('')
              setDraft({ index: null, hostPort: '', containerPort: '', protocol: 'tcp', description: '' })
            }}>
              <Plus className="h-3.5 w-3.5" />添加映射
            </button>
          )}
        </div>
      </div>
      {loading && !state ? (
        <p className="text-sm text-gray-500">正在读取 IPv6 NAT 映射...</p>
      ) : state && (
        <>
          <div className="flex flex-wrap items-start justify-between gap-3 rounded-md bg-gray-50 p-3 text-xs text-gray-600">
            <div className="space-y-1">
              <p>模式：<span className="font-medium text-gray-900">{state.sync_ipv4 ? '同步 IPv4 映射（只读）' : '独立 IPv6 NAT 映射'}</span></p>
              <p>端口配额：<span className="font-mono">{state.mappings.length}/{state.limit}</span></p>
              <p className="break-all">宿主机 IPv6：<span className="font-mono">{state.host_ipv6 || '未配置'}</span></p>
              <p className="break-all">容器内网 IPv6（ULA）：<span className="font-mono">{state.guest_ipv6 || '未分配'}</span></p>
            </div>
            {!isSubUser && (
              <label className="inline-flex cursor-pointer items-center gap-2 text-gray-800">
                <input type="checkbox" checked={state.sync_ipv4} disabled={busy || !state.enabled} onChange={() => { void toggleSync() }} />
                同步 IPv4 映射
              </label>
            )}
          </div>
          {!state.enabled && <p className="text-xs text-amber-700">宿主机尚未启用 IPv6 NAT，当前无法管理映射，请联系管理员。</p>}
          {state.enabled && !ready && <p className="text-xs text-amber-700">IPv6 NAT 地址尚未就绪，请管理员检查宿主机 IPv6 和容器内网地址。</p>}
          {state.sync_ipv4 ? (
            <p className="text-xs text-gray-500">映射由 IPv4 配置派生；管理员关闭同步后可独立配置 IPv6 端口。</p>
          ) : isSubUser ? (
            <p className="text-xs text-gray-500">可修改已分配映射的容器端口；宿主机端口、协议及映射数量由管理员管理。</p>
          ) : (
            <p className="text-xs text-gray-500">独立映射不受 IPv4 端口调整影响。{state.limit === 0 ? '当前未分配 IPv6 NAT 端口配额。' : state.mappings.length >= state.limit ? '已达到 IPv6 NAT 端口配额。' : ''}</p>
          )}
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="border-b border-gray-200 text-gray-500"><tr><th className="px-2 py-2 font-medium">IPv6 访问端点 → 容器端点</th><th className="px-2 py-2 font-medium">协议</th><th className="px-2 py-2 font-medium">描述</th><th className="px-2 py-2 text-right font-medium">操作</th></tr></thead>
              <tbody className="divide-y divide-gray-100">
                {state.mappings.map((mapping, index) => (
                  <tr key={`${mapping.host_port}-${mapping.protocol}-${index}`}>
                    <td className="whitespace-nowrap px-2 py-3 font-mono">{endpoint(mapping.host_ip || state.host_ipv6, mapping.host_port)} → {endpoint(state.guest_ipv6, mapping.container_port)}</td>
                    <td className="px-2 py-3">{mapping.protocol === 'all' ? 'TCP + UDP' : mapping.protocol.toUpperCase()}</td>
                    <td className="px-2 py-3">{mapping.description || '-'}</td>
                    <td className="whitespace-nowrap px-2 py-3 text-right">
                      {state.sync_ipv4 ? <span className="text-gray-400">同步只读</span> : (
                        <div className="inline-flex gap-1">
                          <button type="button" disabled={!canEdit} className={buttonClass} aria-label={`编辑 IPv6 映射 ${mapping.host_port}`} onClick={() => {
                            setError('')
                            setDraft({ index, hostPort: String(mapping.host_port), containerPort: String(mapping.container_port), protocol: mapping.protocol, description: mapping.description })
                          }}><Pencil className="h-3.5 w-3.5" />编辑</button>
                          {!isSubUser && <button type="button" disabled={!canEdit} className={buttonClass} aria-label={`删除 IPv6 映射 ${mapping.host_port}`} onClick={() => { void removeMapping(index) }}><Trash2 className="h-3.5 w-3.5" /></button>}
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
                {state.mappings.length === 0 && <tr><td colSpan={4} className="px-2 py-5 text-center text-gray-400">暂无 IPv6 NAT 端口映射</td></tr>}
              </tbody>
            </table>
          </div>
          {draft && (
            <form onSubmit={(event) => { void saveMapping(event) }} className="space-y-3 rounded-md border border-gray-200 p-4" aria-label={draft.index === null ? '添加 IPv6 映射' : '编辑 IPv6 映射'}>
              <div className="text-sm font-medium">{draft.index === null ? '添加 IPv6 NAT 映射' : '编辑 IPv6 NAT 映射'}</div>
              <div className="grid gap-3 sm:grid-cols-2">
                <label className="space-y-1 text-xs text-gray-600"><span>宿主机端口</span><input type="number" min={1} max={65535} required disabled={isSubUser || !canEdit} value={draft.hostPort} onChange={(event) => setDraft({ ...draft, hostPort: event.target.value })} className={inputClass} /></label>
                <label className="space-y-1 text-xs text-gray-600"><span>容器端口</span><input type="number" min={1} max={65535} required disabled={!canEdit} value={draft.containerPort} onChange={(event) => setDraft({ ...draft, containerPort: event.target.value })} className={inputClass} /></label>
                <label className="space-y-1 text-xs text-gray-600"><span>协议</span><select disabled={isSubUser || !canEdit} value={draft.protocol} onChange={(event) => setDraft({ ...draft, protocol: event.target.value })} className={inputClass}><option value="tcp">TCP</option><option value="udp">UDP</option></select></label>
                <label className="space-y-1 text-xs text-gray-600"><span>描述</span><input disabled={isSubUser || !canEdit} value={draft.description} onChange={(event) => setDraft({ ...draft, description: event.target.value })} className={inputClass} /></label>
              </div>
              <div className="flex justify-end gap-2"><button type="button" disabled={busy} className={buttonClass} onClick={() => setDraft(null)}>取消</button><button type="submit" disabled={!canSave} className="rounded-md bg-black px-4 py-2 text-xs text-white disabled:opacity-50">{busy ? '保存中...' : '保存映射'}</button></div>
            </form>
          )}
        </>
      )}
      {(error || loadError) && <p role="alert" className="rounded-md bg-red-50 p-3 text-xs text-red-700">{error || loadError}</p>}
      {notice && <p role="status" className="rounded-md bg-blue-50 p-3 text-xs text-blue-800">{notice}</p>}
    </section>
  )
}
