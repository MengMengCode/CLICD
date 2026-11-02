import { Alert } from './ui/alert'
import { Card } from './ui/card'
import { Button } from './ui/button'
import { Label } from './ui/label'
import { Switch } from './ui/switch'
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from './ui/table'
import { Input } from './ui/input'
import { NativeSelect } from './ui/native-select'
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

const inputClass = 'w-full px-3 py-2 text-sm'
const buttonClass = 'inline-flex items-center gap-1.5 px-3 py-1.5 text-xs'

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
    <Card role="region" className="space-y-4 p-5" aria-label="IPv6 NAT 端口映射">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-foreground">IPv6 NAT 端口映射</h2>
        <div className="flex items-center gap-2">
          <Button variant="ghost" type="button" className={`h-auto ${buttonClass}`} onClick={() => { void load() }} disabled={busy} title="刷新 IPv6 NAT 映射">
            <RefreshCw className={`h-3.5 w-3.5  ${loading ? "animate-spin" : ""}`} />刷新
          </Button>
          {!isSubUser && (
            <Button variant="ghost" type="button" className={`h-auto ${buttonClass}`} disabled={!canAdd} onClick={() => {
              setError('')
              setDraft({ index: null, hostPort: '', containerPort: '', protocol: 'tcp', description: '' })
            }}>
              <Plus className="h-3.5 w-3.5" />添加映射
            </Button>
          )}
        </div>
      </div>
      {loading && !state ? (
        <p className="text-sm text-muted-foreground">正在读取 IPv6 NAT 映射...</p>
      ) : state && (
        <>
          <div className="flex flex-wrap items-start justify-between gap-3 rounded-md bg-muted p-3 text-xs text-muted-foreground">
            <div className="space-y-1">
              <p>模式：<span className="font-medium text-foreground">{state.sync_ipv4 ? '同步 IPv4 映射（只读）' : '独立 IPv6 NAT 映射'}</span></p>
              <p>端口配额：<span className="font-mono">{state.mappings.length}/{state.limit}</span></p>
              <p className="break-all">宿主机 IPv6：<span className="font-mono">{state.host_ipv6 || '未配置'}</span></p>
              <p className="break-all">容器内网 IPv6（ULA）：<span className="font-mono">{state.guest_ipv6 || '未分配'}</span></p>
            </div>
            {!isSubUser && (
              <Label className="inline-flex cursor-pointer items-center gap-2 text-foreground">
                <Switch  checked={state.sync_ipv4} disabled={busy || !state.enabled} onCheckedChange={() => { void toggleSync() }} />
                同步 IPv4 映射
              </Label>
            )}
          </div>
          {!state.enabled && <p className="text-xs text-amber-700">宿主机尚未启用 IPv6 NAT，当前无法管理映射，请联系管理员。</p>}
          {state.enabled && !ready && <p className="text-xs text-amber-700">IPv6 NAT 地址尚未就绪，请管理员检查宿主机 IPv6 和容器内网地址。</p>}
          {state.sync_ipv4 ? (
            <p className="text-xs text-muted-foreground">映射由 IPv4 配置派生；管理员关闭同步后可独立配置 IPv6 端口。</p>
          ) : isSubUser ? (
            <p className="text-xs text-muted-foreground">可修改已分配映射的容器端口；宿主机端口、协议及映射数量由管理员管理。</p>
          ) : (
            <p className="text-xs text-muted-foreground">独立映射不受 IPv4 端口调整影响。{state.limit === 0 ? '当前未分配 IPv6 NAT 端口配额。' : state.mappings.length >= state.limit ? '已达到 IPv6 NAT 端口配额。' : ''}</p>
          )}
          <div className="overflow-x-auto">
            <Table className="w-full text-left text-xs">
              <TableHeader className="text-muted-foreground"><TableRow><TableHead className="px-2 py-2 font-medium">IPv6 访问端点 → 容器端点</TableHead><TableHead className="px-2 py-2 font-medium">协议</TableHead><TableHead className="px-2 py-2 font-medium">描述</TableHead><TableHead className="px-2 py-2 text-right font-medium">操作</TableHead></TableRow></TableHeader>
              <TableBody className="divide-y divide-border">
                {state.mappings.map((mapping, index) => (
                  <TableRow key={`${mapping.host_port}-${mapping.protocol}-${index}`}>
                    <TableCell className="whitespace-nowrap px-2 py-3 font-mono">{endpoint(mapping.host_ip || state.host_ipv6, mapping.host_port)} → {endpoint(state.guest_ipv6, mapping.container_port)}</TableCell>
                    <TableCell className="px-2 py-3">{mapping.protocol === 'all' ? 'TCP + UDP' : mapping.protocol.toUpperCase()}</TableCell>
                    <TableCell className="px-2 py-3">{mapping.description || '-'}</TableCell>
                    <TableCell className="whitespace-nowrap px-2 py-3 text-right">
                      {state.sync_ipv4 ? <span className="text-muted-foreground">同步只读</span> : (
                        <div className="inline-flex gap-1">
                          <Button variant="ghost" type="button" disabled={!canEdit} className={`h-auto ${buttonClass}`} aria-label={`编辑 IPv6 映射 ${mapping.host_port}`} onClick={() => {
                            setError('')
                            setDraft({ index, hostPort: String(mapping.host_port), containerPort: String(mapping.container_port), protocol: mapping.protocol, description: mapping.description })
                          }}><Pencil className="h-3.5 w-3.5" />编辑</Button>
                          {!isSubUser && <Button variant="ghost" type="button" disabled={!canEdit} className={`h-auto ${buttonClass}`} aria-label={`删除 IPv6 映射 ${mapping.host_port}`} onClick={() => { void removeMapping(index) }}><Trash2 className="h-3.5 w-3.5" /></Button>}
                        </div>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
                {state.mappings.length === 0 && <TableRow><TableCell colSpan={4} className="px-2 py-5 text-center text-muted-foreground">暂无 IPv6 NAT 端口映射</TableCell></TableRow>}
              </TableBody>
            </Table>
          </div>
          {draft && (
            <Card className="p-4"><form onSubmit={(event) => { void saveMapping(event) }} className="space-y-3" aria-label={draft.index === null ? '添加 IPv6 映射' : '编辑 IPv6 映射'}>
              <div className="text-sm font-medium">{draft.index === null ? '添加 IPv6 NAT 映射' : '编辑 IPv6 NAT 映射'}</div>
              <div className="grid gap-3 sm:grid-cols-2">
                <Label className="space-y-1 text-xs text-muted-foreground"><span>宿主机端口</span><Input type="number" min={1} max={65535} required disabled={isSubUser || !canEdit} value={draft.hostPort} onChange={(event) => setDraft({ ...draft, hostPort: event.target.value })} className={`${inputClass} h-auto`} /></Label>
                <Label className="space-y-1 text-xs text-muted-foreground"><span>容器端口</span><Input type="number" min={1} max={65535} required disabled={!canEdit} value={draft.containerPort} onChange={(event) => setDraft({ ...draft, containerPort: event.target.value })} className={`${inputClass} h-auto`} /></Label>
                <Label className="space-y-1 text-xs text-muted-foreground"><span>协议</span><NativeSelect wrapperClassName="w-full" disabled={isSubUser || !canEdit} value={draft.protocol} onChange={(event) => setDraft({ ...draft, protocol: event.target.value })} className={`${inputClass} h-auto pr-9`}><option value="tcp">TCP</option><option value="udp">UDP</option></NativeSelect></Label>
                <Label className="space-y-1 text-xs text-muted-foreground"><span>描述</span><Input disabled={isSubUser || !canEdit} value={draft.description} onChange={(event) => setDraft({ ...draft, description: event.target.value })} className={`${inputClass} h-auto`} /></Label>
              </div>
              <div className="flex justify-end gap-2"><Button variant="ghost" type="button" disabled={busy} className={`h-auto ${buttonClass}`} onClick={() => setDraft(null)}>取消</Button><Button variant="default" type="submit" disabled={!canSave} className="h-auto px-4 py-2 text-xs">{busy ? '保存中...' : '保存映射'}</Button></div>
            </form></Card>
          )}
        </>
      )}
      {(error || loadError) && <Alert variant="destructive" role="alert" className="p-3 text-xs">{error || loadError}</Alert>}
      {notice && <Alert variant="default" role="status" className="p-3 text-xs">{notice}</Alert>}
    </Card>
  )
}
