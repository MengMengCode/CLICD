import { useState, useEffect, useCallback, useRef, type ReactNode } from 'react'
import { useParams, useNavigate } from 'react-router'
import {
  ArrowLeft,
  AlertTriangle,
  Camera,
  Clock,
  Copy,
  Cpu,
  HardDrive,
  Key,
  Maximize2,
  MemoryStick,
  Minimize2,
  Monitor,
  Network,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Save,
  Settings,
  Square,
  TerminalSquare,
  Trash2,
  UserPlus,
  X,
} from 'lucide-react'
import {
  default as api,
  addPortMapping,
  assignIPv6,
  APIResponse,
  Container,
  ContainerMetricPoint as ContainerMetricSample,
  ContainerUsage,
  createSubUser,
  createContainerSnapshot,
  deleteContainer,
  deleteContainerSnapshot,
  deletePortMapping,
  getContainer,
  getContainerHistory,
  getContainerSnapshots,
  getContainerUsage,
  getHostInfo,
  getStorageInfo,
  getTrafficInfo,
  HostInfo,
  TrafficInfo,
  getEnabledImages,
  getFirewall,
  PortMapping,
  PublicIPv4Info,
  FirewallRule,
  updatePublicIPv4Assignments,
  updateIPv6Assignments,
  reinstallContainer,
  resetSSHPassword,
  restartContainer,
  startContainer,
  stopContainer,
  Snapshot,
  SnapshotSchedule,
  StorageInfo,
  Template,
  updateContainerExpiry,
  updateFirewall,
  updateSnapshotQuota,
  updateSnapshotSchedule,
  restoreContainerSnapshot,
  resetTraffic,
  updateTrafficLimit,
  updateResourceLimit,
  updatePortMapping,
  SubUser,
} from '../services/api'
import { useDialog } from '../components/Dialog'
import { useAuth } from '../contexts/AuthContext'
import { useLanguage } from '../contexts/LanguageContext'
import WebSSHViewer from '../components/WebSSHViewer'
import WebVNCViewer from '../components/WebVNCViewer'
import { RingStat } from '../components/RingStats'
import { copyToClipboard } from '../utils/clipboard'
import ResourceStatsPanel, {
  ChartPoint,
  ResourceChartConfig,
  StatsRangeKey,
  statsRanges,
} from '../components/ResourceStatsPanel'
import { generateSSHPassword, sshPasswordError, sshPublicKeyError, type ReinstallSSHAuthMode } from '../utils/sshAuth'

const PUBLIC_HOST = window.location.hostname
const inputClass = 'w-full px-3 py-2 border border-gray-300 rounded-md text-sm text-black bg-white focus:outline-none focus:ring-2 focus:ring-black focus:border-black'

type MetricPoint = {
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
type MappingDraft = {
  index: number | null
  description: string
  host_port: string
  host_ip: string
  container_port: string
  protocol: string
}

type IPAssignMode = 'clear' | 'random' | 'custom'

const emptyDraft: MappingDraft = {
  index: null,
  description: '',
  host_port: '',
  host_ip: '',
  container_port: '',
  protocol: 'all',
}

export default function ContainerDetail() {
  const { id: paramId } = useParams<{ id: string }>()
  const containerIdentifier = paramId || ''
  const navigate = useNavigate()
  const dialog = useDialog()
  const { isSubUser } = useAuth()
  const { t } = useLanguage()
  const [container, setContainer] = useState<Container | null>(null)
  const [hostInfo, setHostInfo] = useState<HostInfo | null>(null)
  const [usage, setUsage] = useState<ContainerUsage | null>(null)
  const [history, setHistory] = useState<MetricPoint[]>(() => readHistory(containerIdentifier))
  const [range, setRange] = useState<StatsRangeKey>('30m')
  const [loading, setLoading] = useState(true)
  const [actionLoading, setActionLoading] = useState<string | null>(null)
  const [taskStatus, setTaskStatus] = useState('') // current task type for this container
  const [showSSH, setShowSSH] = useState(false)
  const [showVNC, setShowVNC] = useState(false)
  const vncFullscreenRef = useRef<HTMLDivElement>(null)
  const [vncFullscreen, setVncFullscreen] = useState(false)
  const [showNat, setShowNat] = useState(false)
  const [showIPAssign, setShowIPAssign] = useState(false)
  const [savingIPAssign, setSavingIPAssign] = useState(false)
  const [ipv4AssignMode, setIPv4AssignMode] = useState<IPAssignMode>('clear')
  const [ipv4AssignCount, setIPv4AssignCount] = useState(1)
  const [ipv4Selected, setIPv4Selected] = useState<string[]>([])
  const [ipv6AssignMode, setIPv6AssignMode] = useState<IPAssignMode>('clear')
  const [ipv6AssignCount, setIPv6AssignCount] = useState(1)
  const [ipv6DraftText, setIPv6DraftText] = useState('')
  const [showMappingEditor, setShowMappingEditor] = useState(false)
  const [showExpiryEdit, setShowExpiryEdit] = useState(false)
  const [editExpiry, setEditExpiry] = useState('')
  const [savingExpiry, setSavingExpiry] = useState(false)
  const [draft, setDraft] = useState<MappingDraft>(emptyDraft)
  const [savingMapping, setSavingMapping] = useState(false)
  const [showReinstall, setShowReinstall] = useState(false)
  const [templates, setTemplates] = useState<Template[]>([])
  const [selectedTemplate, setSelectedTemplate] = useState('')
  const [reinstallAuthMode, setReinstallAuthMode] = useState<ReinstallSSHAuthMode>('keep')
  const [reinstallPasswordDraft, setReinstallPasswordDraft] = useState('')
  const [reinstallPublicKeyDraft, setReinstallPublicKeyDraft] = useState('')
  const [reinstalling, setReinstalling] = useState(false)
  const [traffic, setTraffic] = useState<TrafficInfo | null>(null)
  const [subUser, setSubUser] = useState<SubUser | null>(null)
  const [showSubUser, setShowSubUser] = useState(false)
  const [showTrafficEdit, setShowTrafficEdit] = useState(false)
  const [trafficEdit, setTrafficEdit] = useState({ mode: 'total', monthly: 0, inGB: 0, outGB: 0 })
  const [savingTraffic, setSavingTraffic] = useState(false)
  const [showResourceEdit, setShowResourceEdit] = useState(false)
  const [resourceEdit, setResourceEdit] = useState({ vcpu: 1, ramMb: 512, networkDownMbps: 0, networkUpMbps: 0, ioReadMbps: 0, ioWriteMbps: 0 })
  const [savingResource, setSavingResource] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [showResetPassword, setShowResetPassword] = useState(false)
  const [resetPasswordDraft, setResetPasswordDraft] = useState('')
  const [resetPasswordResult, setResetPasswordResult] = useState('')
  const [resetPasswordSaving, setResetPasswordSaving] = useState(false)
  const [showSnapshots, setShowSnapshots] = useState(false)
  const [snapshots, setSnapshots] = useState<Snapshot[]>([])
  const [snapshotQuota, setSnapshotQuota] = useState(3)
  const [snapshotQuotaDraft, setSnapshotQuotaDraft] = useState(3)
  const [editingSnapshotQuota, setEditingSnapshotQuota] = useState(false)
  const [snapshotSchedule, setSnapshotSchedule] = useState<SnapshotSchedule | null>(null)
  const [snapshotBusy, setSnapshotBusy] = useState('')
  const [storageInfo, setStorageInfo] = useState<StorageInfo | null>(null)
  const [storageLoading, setStorageLoading] = useState(!isSubUser)
  const [snapshotStoragePoolID, setSnapshotStoragePoolID] = useState('')
  const [showSnapshotSchedule, setShowSnapshotSchedule] = useState(false)
  const [snapshotScheduleDraft, setSnapshotScheduleDraft] = useState({ intervalHours: 24, time: '03:00' })
  const [showFirewall, setShowFirewall] = useState(false)
  const [firewallEnabled, setFirewallEnabled] = useState(false)
  const [firewallDefaultAction, setFirewallDefaultAction] = useState<'ACCEPT' | 'DROP'>('DROP')
  const [firewallRules, setFirewallRules] = useState<FirewallRule[]>([])
  const [firewallSaving, setFirewallSaving] = useState(false)
  const [firewallMessage, setFirewallMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [editingFirewallRule, setEditingFirewallRule] = useState<FirewallRule | null>(null)
  const [showFirewallEditor, setShowFirewallEditor] = useState(false)

  const fetchContainer = useCallback(async () => {
    if (!containerIdentifier) return
    try {
      const [res, hostRes] = await Promise.all([
        getContainer(containerIdentifier),
        isSubUser ? Promise.resolve(null) : getHostInfo().catch(() => null),
      ])
      if (res.data.data) setContainer(res.data.data)
      if (hostRes?.data.data) setHostInfo(hostRes.data.data)
    } catch (err) {
      console.error('Failed to fetch container:', err)
    } finally {
      setLoading(false)
    }
  }, [containerIdentifier, isSubUser])

  const fetchSnapshots = useCallback(async () => {
    if (!containerIdentifier) return
    try {
      const res = await getContainerSnapshots(containerIdentifier)
      const data = res.data.data
      const quota = data?.quota || container?.snapshot_limit || 3
      setSnapshots(data?.snapshots || [])
      setSnapshotQuota(quota)
      setSnapshotQuotaDraft(quota)
      setSnapshotSchedule(data?.schedule || null)
    } catch (err) {
      console.error('Failed to fetch snapshots:', err)
    }
  }, [containerIdentifier, container?.snapshot_limit])

  const fetchStorage = useCallback(async () => {
    if (isSubUser) {
      setStorageLoading(false)
      return
    }
    setStorageLoading(true)
    try {
      const res = await getStorageInfo()
      setStorageInfo(res.data.data || null)
    } catch (err) {
      console.error('Failed to fetch storage:', err)
      setStorageInfo(null)
    } finally {
      setStorageLoading(false)
    }
  }, [isSubUser])

  const fetchMetricHistory = useCallback(async () => {
    if (!containerIdentifier) return
    try {
      const res = await getContainerHistory(containerIdentifier)
      const points = (res.data.data || []).map(normalizeContainerMetricSample)
      if (points.length > 0) {
        setHistory(points)
        localStorage.setItem(historyKey(container?.uuid || containerIdentifier), JSON.stringify(points))
      }
    } catch (err) {
      console.error('Failed to fetch metric history:', err)
    }
  }, [containerIdentifier, container?.uuid])

  const fetchUsage = useCallback(async () => {
    if (!containerIdentifier) return
    try {
      const res = await getContainerUsage(containerIdentifier)
      if (res.data.data) {
        setUsage(res.data.data)
      }
    } catch (err) {
      console.error('Failed to fetch usage:', err)
    }
  }, [containerIdentifier])

  useEffect(() => {
    if (!containerIdentifier) return
    setHistory(readHistory(container?.uuid || containerIdentifier))
  }, [containerIdentifier, container?.uuid])

  useEffect(() => {
    fetchContainer()
    // Auto-refresh container status every 5s (silent, no spinner)
    const timer = window.setInterval(fetchContainer, 5000)
    return () => window.clearInterval(timer)
  }, [fetchContainer])

  useEffect(() => {
    const handleFullscreenChange = () => {
      setVncFullscreen(document.fullscreenElement === vncFullscreenRef.current)
    }
    document.addEventListener('fullscreenchange', handleFullscreenChange)
    return () => document.removeEventListener('fullscreenchange', handleFullscreenChange)
  }, [])

  const toggleVNCFullscreen = async () => {
    const target = vncFullscreenRef.current
    if (!target) return
    try {
      if (document.fullscreenElement === target) {
        await document.exitFullscreen()
      } else {
        await target.requestFullscreen()
      }
    } catch {
      setVncFullscreen((value) => !value)
    }
  }

  useEffect(() => {
    fetchUsage()
    const timer = window.setInterval(fetchUsage, 5000)
    return () => window.clearInterval(timer)
  }, [fetchUsage])

  useEffect(() => {
    fetchMetricHistory()
    const timer = window.setInterval(fetchMetricHistory, 30000)
    return () => window.clearInterval(timer)
  }, [fetchMetricHistory])

  useEffect(() => {
    if (showSnapshots) {
      fetchSnapshots()
      fetchStorage()
    }
  }, [showSnapshots, fetchSnapshots, fetchStorage])

  // Poll task status for this container
  useEffect(() => {
    if (!containerIdentifier) return
    const check = async () => {
      try {
        const { getTasks } = await import('../services/api')
        const res = await getTasks()
        if (res.data.data) {
          for (const t of res.data.data) {
            const matchesContainer = container
              ? t.container_id === container.id || t.container_name === container.name
              : false
            if (matchesContainer && (t.status === 'pending' || t.status === 'running')) {
              setTaskStatus(t.type)
              return
            }
          }
        }
        setTaskStatus('')
      } catch { /* ignore */ }
    }
    check()
    const t = setInterval(check, 2000)
    return () => clearInterval(t)
  }, [containerIdentifier, container?.id])

  const taskActionLabels: Record<string, string> = {
    start: '开机中...', stop: '关机中...', restart: '重启中...', delete: '删除中...', reinstall: '重装中...',
  }

  const ensureSubUserCanOperate = async () => {
    if (isSubUser && container?.policy_blocked) {
      await dialog.alert('策略临时封禁', container.policy_blocked_reason || '虚拟机被策略临时封禁，暂不能执行操作。')
      return false
    }
    return true
  }

  const handleAction = async (action: string) => {
    if (!containerIdentifier) return
    if (!(await ensureSubUserCanOperate())) return
    setActionLoading(action)
    try {
      switch (action) {
        case 'start':
          await startContainer(containerIdentifier)
          break
        case 'stop':
          await stopContainer(containerIdentifier)
          setShowSSH(false)
          setShowVNC(false)
          break
        case 'restart':
          await restartContainer(containerIdentifier)
          break
        case 'delete':
          if (!(await dialog.confirm('删除容器', `确定要删除容器 ${container?.name} 吗？此操作不可撤销。`))) return
          await deleteContainer(containerIdentifier)
          navigate('/containers')
          return
      }
      await fetchContainer()
    } catch (err) {
      console.error('Action failed:', err)
      dialog.alert('操作失败', (err as Error).message || '请稍后重试')
    } finally {
      setActionLoading(null)
    }
  }

  const openTrafficEdit = () => {
    if (!container) return
    setTrafficEdit({
      mode: container.traffic_mode || 'total',
      monthly: container.monthly_traffic_gb || 0,
      inGB: container.traffic_in_gb || 0,
      outGB: container.traffic_out_gb || 0,
    })
    setShowTrafficEdit(true)
  }

  const saveTrafficLimit = async () => {
    if (!container) return
    setSavingTraffic(true)
    try {
      await updateTrafficLimit(container.id, {
        traffic_mode: trafficEdit.mode,
        monthly_traffic_gb: trafficEdit.monthly,
        traffic_in_gb: trafficEdit.inGB,
        traffic_out_gb: trafficEdit.outGB,
      })
      setShowTrafficEdit(false)
      fetchContainer()
    } catch (err) {
      dialog.alert('错误', '保存失败')
    } finally {
      setSavingTraffic(false)
    }
  }

  const openResourceEdit = () => {
    if (!container) return
    setResourceEdit({
      vcpu: container.vcpu,
      ramMb: container.ram_mb,
      networkDownMbps: resourceLimitValue(container.network_down_mbps, container.network_bw_mbps),
      networkUpMbps: resourceLimitValue(container.network_up_mbps, container.network_bw_mbps),
      ioReadMbps: resourceLimitValue(container.io_read_mbps, container.io_speed_mbps),
      ioWriteMbps: resourceLimitValue(container.io_write_mbps, container.io_speed_mbps),
    })
    setShowResourceEdit(true)
  }

  const saveResourceLimit = async () => {
    if (!container) return
    setSavingResource(true)
    try {
      await updateResourceLimit(container.id, {
        vcpu: resourceEdit.vcpu,
        ram_mb: resourceEdit.ramMb,
        network_down_mbps: resourceEdit.networkDownMbps,
        network_up_mbps: resourceEdit.networkUpMbps,
        network_bw_mbps: symmetricLimit(resourceEdit.networkDownMbps, resourceEdit.networkUpMbps),
        io_read_mbps: resourceEdit.ioReadMbps,
        io_write_mbps: resourceEdit.ioWriteMbps,
        io_speed_mbps: symmetricLimit(resourceEdit.ioReadMbps, resourceEdit.ioWriteMbps),
      })
      setShowResourceEdit(false)
      fetchContainer()
    } catch (err) {
      dialog.alert('错误', '保存失败')
    } finally {
      setSavingResource(false)
    }
  }

  const syncFirewallState = (enabled: boolean, defaultAction: 'ACCEPT' | 'DROP', rules: FirewallRule[]) => {
    const nextRules = rules.map(r => ({ ...r }))
    setFirewallEnabled(enabled)
    setFirewallDefaultAction(defaultAction)
    setFirewallRules(nextRules)
    setContainer(prev => prev ? {
      ...prev,
      firewall_enabled: enabled,
      firewall_default_action: defaultAction,
      firewall_rules: nextRules.map(r => ({ ...r })),
    } : prev)
  }

  const openFirewall = async () => {
    if (!container) return
    syncFirewallState(container.firewall_enabled || false, container.firewall_default_action || 'DROP', container.firewall_rules || [])
    setFirewallMessage(null)
    setShowFirewall(true)
    try {
      const res = await getFirewall(container.id)
      const data = res.data.data
      if (data) syncFirewallState(data.enabled, data.default_action || 'DROP', data.rules || [])
    } catch (err) {
      console.error('Failed to load firewall:', err)
    }
  }

  const saveFirewall = async () => {
    if (!container) return
    setFirewallSaving(true)
    try {
      const res = await updateFirewall(container.id, { enabled: firewallEnabled, default_action: firewallDefaultAction, rules: firewallRules })
      const data = res.data.data
      if (data) {
        syncFirewallState(data.enabled, data.default_action || 'DROP', data.rules || [])
      }
      setFirewallMessage({ type: 'success', text: '防火墙设置已保存并应用' })
      fetchContainer()
    } catch (err: any) {
      const message = err?.response?.data?.message || '保存防火墙设置失败'
      setFirewallMessage({ type: 'error', text: message })
      dialog.alert('错误', message)
    } finally {
      setFirewallSaving(false)
    }
  }

  const addFirewallRule = () => {
    const hasIPv4Firewall = (container?.public_ipv4s?.length || 0) > 0 || Math.max(container?.port_mapping_limit || 0, container?.port_mappings?.length || 0) > 0
    const hasIPv6Firewall = !!container?.ipv6 || (container?.ipv6_addresses?.length || 0) > 0
    setEditingFirewallRule({
      id: '',
      network: hasIPv4Firewall ? 'ipv4' : hasIPv6Firewall ? 'ipv6' : 'ipv4',
      direction: 'in',
      protocol: 'tcp',
      port: '',
      source_ip: '',
      action: 'DROP',
      description: '',
      enabled: true,
    })
    setShowFirewallEditor(true)
  }

  const saveFirewallRule = (rule: FirewallRule) => {
    if (rule.id) {
      // Update existing
      setFirewallRules(firewallRules.map(r => r.id === rule.id ? rule : r))
    } else {
      // Add new with temporary ID
      const newRule = { ...rule, id: `tmp-${Date.now()}` }
      setFirewallRules([...firewallRules, newRule])
    }
    setShowFirewallEditor(false)
    setEditingFirewallRule(null)
  }

  const deleteFirewallRule = (ruleId: string) => {
    setFirewallRules(firewallRules.filter(r => r.id !== ruleId))
  }

  const toggleFirewallRule = (ruleId: string) => {
    setFirewallRules(firewallRules.map(r => r.id === ruleId ? { ...r, enabled: !r.enabled } : r))
  }

  const openReinstall = async () => {
    try {
      const res = await getEnabledImages(container?.virtualization || 'lxc', containerIdentifier)
      if (res.data.data) {
        const data = res.data.data
        setTemplates(data)
        const currentTemplate = container?.template || ''
        setSelectedTemplate(data.some((template) => template.id === currentTemplate) ? currentTemplate : (data[0]?.id || ''))
      }
      setReinstallAuthMode('keep')
      setReinstallPasswordDraft('')
      setReinstallPublicKeyDraft('')
      setShowReinstall(true)
    } catch (err) {
      console.error(err)
    }
  }

  const handleCreateSubUser = async () => {
    if (!container?.uuid) return
    try {
      const res = await createSubUser(container.uuid)
      if (res.data.success && res.data.data) {
        setSubUser(res.data.data)
        setShowSubUser(true)
      }
    } catch (err) {
      console.error(err)
    }
  }

  const handleReinstall = async () => {
    if (!containerIdentifier || !selectedTemplate) return
    const linuxTemplate = !isWindowsTemplate(selectedTemplate)
    if (linuxTemplate && reinstallAuthMode === 'password') {
      const validationError = sshPasswordError(reinstallPasswordDraft.trim())
      if (validationError) {
        await dialog.alert('密码格式不正确', validationError)
        return
      }
    }
    if (linuxTemplate && reinstallAuthMode === 'key') {
      const validationError = sshPublicKeyError(reinstallPublicKeyDraft)
      if (validationError) {
        await dialog.alert('SSH Key 格式不正确', validationError)
        return
      }
    }
    setReinstalling(true)
    try {
      await reinstallContainer(containerIdentifier, selectedTemplate, linuxTemplate ? {
        ssh_auth_mode: reinstallAuthMode,
        ssh_password: reinstallAuthMode === 'password' ? reinstallPasswordDraft.trim() : '',
        ssh_public_key: reinstallAuthMode === 'key' ? reinstallPublicKeyDraft.trim() : '',
      } : undefined)
      setShowReinstall(false)
      setShowSSH(false)
      setShowVNC(false)
      await fetchContainer()
    } catch (err) {
      console.error('Reinstall failed:', err)
      dialog.alert('重装失败', '请稍后重试')
    } finally {
      setReinstalling(false)
    }
  }

  const generateResetPassword = () => {
    setResetPasswordDraft(generateSSHPassword())
    setResetPasswordResult('')
  }

  const resetPasswordError = (password: string) => {
    return sshPasswordError(password)
  }

  const handleResetPassword = async () => {
    if (!containerIdentifier) return
    const password = resetPasswordDraft.trim()
    const validationError = resetPasswordError(password)
    if (validationError) {
      await dialog.alert('密码格式不正确', validationError)
      return
    }
    setResetPasswordSaving(true)
    try {
      const res = await resetSSHPassword(containerIdentifier, password)
      if (res.data.success) {
        const nextPassword = (res.data.data as { password: string })?.password || password
        setResetPasswordResult(nextPassword)
        setResetPasswordDraft(nextPassword)
        await fetchContainer()
      }
    } catch (err: unknown) {
      console.error(err)
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('密码重置失败', error.response?.data?.message || '请稍后重试')
    } finally {
      setResetPasswordSaving(false)
    }
  }

  const openResetPassword = () => {
    setResetPasswordDraft('')
    setResetPasswordResult('')
    setShowResetPassword(true)
  }

  const handleAssignIPv6 = async () => {
    if (!containerIdentifier) return
    setActionLoading('ipv6')
    try {
      await assignIPv6(containerIdentifier)
      await fetchContainer()
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('IPv6 allocation failed', error.response?.data?.message || 'Please try again later')
    } finally {
      setActionLoading(null)
    }
  }

  const openIPAssign = () => {
    const currentIPv4 = (container?.public_ipv4s || []).map((item) => item.address).filter(Boolean)
    const currentIPv6 = (container?.ipv6_addresses || []).map((item) => item.address).filter(Boolean)
    setIPv4Selected(currentIPv4)
    setIPv4AssignMode(currentIPv4.length > 0 ? 'custom' : 'clear')
    setIPv4AssignCount(Math.max(1, currentIPv4.length || 1))
    setIPv6DraftText(currentIPv6.join('\n'))
    setIPv6AssignMode(currentIPv6.length > 0 ? 'custom' : 'clear')
    setIPv6AssignCount(Math.max(1, currentIPv6.length || 1))
    setShowIPAssign(true)
  }

  const submitIPAssign = async () => {
    if (!containerIdentifier) return
    setSavingIPAssign(true)
    try {
      await updatePublicIPv4Assignments(containerIdentifier, {
        mode: ipv4AssignMode,
        count: Math.max(1, Math.round(ipv4AssignCount || 1)),
        addresses: ipv4AssignMode === 'custom' ? ipv4Selected : [],
      })
      await updateIPv6Assignments(containerIdentifier, {
        mode: ipv6AssignMode,
        count: Math.max(1, Math.round(ipv6AssignCount || 1)),
        addresses: ipv6AssignMode === 'custom' ? splitAddressLines(ipv6DraftText) : [],
      })
      await fetchContainer()
      setShowIPAssign(false)
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('公网 IP 分配失败', error.response?.data?.message || '请检查地址是否可用或已被占用')
    } finally {
      setSavingIPAssign(false)
    }
  }

  const openAddMapping = () => {
    if (isSubUser && container?.policy_blocked) return
    setDraft(emptyDraft)
    setShowNat(true)
    setShowMappingEditor(true)
  }

  const openEditMapping = (pm: PortMapping, index: number) => {
    if (isSubUser && container?.policy_blocked) return
    setDraft({
      index,
      description: pm.description,
      host_port: String(pm.host_port),
      host_ip: pm.host_ip || '',
      container_port: String(pm.container_port),
      protocol: pm.protocol || 'all',
    })
    setShowNat(true)
    setShowMappingEditor(true)
  }

  const submitMapping = async (): Promise<boolean> => {
    if (!containerIdentifier) return false
    if (!(await ensureSubUserCanOperate())) return false
    if (draft.index === null && container) {
      const currentCount = container.port_mappings?.length || 0
      const limit = Math.max(container.port_mapping_limit || 0, currentCount)
      if (limit <= 0) {
        dialog.alert('未分配 IPv4 NAT', '该容器未分配 IPv4 NAT 端口配额。')
        return false
      }
      if (currentCount >= limit) {
        dialog.alert('端口配额已满', '已达到管理员分配的 NAT 端口配额。')
        return false
      }
    }
    const containerPort = parseInt(draft.container_port, 10)
    const hostPort = isSubUser ? 0 : (draft.host_port.trim() ? parseInt(draft.host_port, 10) : 0)
    if (!containerPort || containerPort < 1 || containerPort > 65535) {
      dialog.alert('输入错误', '请输入有效的内部端口')
      return false
    }

    // For sub-users editing existing mappings: use original host_port and protocol
    let hostPortVal = hostPort
    let protocolVal = draft.protocol
    if (isSubUser && draft.index !== null && container?.port_mappings?.[draft.index]) {
      const orig = container.port_mappings[draft.index]
      hostPortVal = orig.host_port
      protocolVal = orig.protocol || 'all'
    }

    const payload: PortMapping = {
      container_port: containerPort,
      host_port: hostPortVal,
      host_ip: isSubUser ? undefined : (draft.host_ip || undefined),
      protocol: protocolVal,
      description: draft.description.trim() || `Port-${containerPort}`,
    }

    setSavingMapping(true)
    try {
      if (draft.index === null) {
        await addPortMapping(containerIdentifier, payload)
      } else {
        await updatePortMapping(containerIdentifier, draft.index, payload)
      }
      setDraft(emptyDraft)
      await fetchContainer()
      return true
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('操作失败', error.response?.data?.message || '保存端口映射失败')
      return false
    } finally {
      setSavingMapping(false)
    }
  }

  const removeMapping = async (index: number) => {
    if (!containerIdentifier || !(await dialog.confirm('删除映射', '确定要删除这条映射规则吗？'))) return
    if (!(await ensureSubUserCanOperate())) return
    try {
      await deletePortMapping(containerIdentifier, index)
      await fetchContainer()
      if (draft.index === index) setDraft(emptyDraft)
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('操作失败', error.response?.data?.message || '删除端口映射失败')
    }
  }

  const handleCreateSnapshot = async () => {
    if (!containerIdentifier) return
    if (!(await ensureSubUserCanOperate())) return
    if (!snapshotStorageReady) {
      await dialog.alert('未配置快照存储', '请先在存储管理中为快照开启至少一块存储磁盘。')
      return
    }
    if (isSubUser && snapshots.length >= snapshotQuota) {
      await dialog.alert('快照配额已满', '已达到管理员设置的快照配额，请先删除旧快照。')
      return
    }
    if (container?.status === 'running') {
      const confirmed = await dialog.confirm(
        '拍摄快照',
        `拍摄快照需要先关机，完成后会自动重启容器 ${container.name}。是否继续？`
      )
      if (!confirmed) return
    }
    setSnapshotBusy('create')
    try {
      await createContainerSnapshot(containerIdentifier, { storage_pool_id: snapshotStoragePoolID || undefined })
      await Promise.all([fetchSnapshots(), fetchContainer()])
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      await dialog.alert('创建快照失败', error.response?.data?.message || '请稍后重试。')
    } finally {
      setSnapshotBusy('')
    }
  }

  const openSnapshotSchedule = () => {
    if (isSubUser && container?.policy_blocked) return
    if (!snapshotStorageReady) {
      dialog.alert('未配置快照存储', '请先在存储管理中为快照开启至少一块存储磁盘。')
      return
    }
    setSnapshotScheduleDraft({
      intervalHours: Math.max(snapshotSchedule?.interval_hours || 24, 24),
      time: snapshotSchedule?.time || '03:00',
    })
    setShowSnapshotSchedule(true)
  }

  const saveSnapshotSchedule = async (enabled: boolean) => {
    if (!containerIdentifier) return
    if (!(await ensureSubUserCanOperate())) return
    const intervalHours = snapshotScheduleDraft.intervalHours
    const scheduleTime = snapshotScheduleDraft.time || '03:00'
    if (enabled && intervalHours < 24) {
      await dialog.alert('参数错误', '自动快照周期最低是 1 天一次。')
      return
    }
    setSnapshotBusy('schedule')
    try {
      await updateSnapshotSchedule(containerIdentifier, enabled, intervalHours, scheduleTime)
      await Promise.all([fetchSnapshots(), fetchContainer()])
      setShowSnapshotSchedule(false)
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      await dialog.alert('定时快照失败', error.response?.data?.message || '请稍后重试。')
    } finally {
      setSnapshotBusy('')
    }
  }

  const saveSnapshotQuota = async () => {
    if (!containerIdentifier || isSubUser) return
    const nextQuota = Math.max(1, Math.round(snapshotQuotaDraft || 1))
    setSnapshotBusy('quota')
    try {
      await updateSnapshotQuota(containerIdentifier, nextQuota)
      setSnapshotQuota(nextQuota)
      setSnapshotQuotaDraft(nextQuota)
      setEditingSnapshotQuota(false)
      await Promise.all([fetchSnapshots(), fetchContainer()])
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      await dialog.alert('保存快照配额失败', error.response?.data?.message || '请稍后重试。')
    } finally {
      setSnapshotBusy('')
    }
  }

  const handleDeleteSnapshot = async (snapshot: Snapshot) => {
    if (!containerIdentifier) return
    if (!(await ensureSubUserCanOperate())) return
    if (!(await dialog.confirm('删除快照', `确定删除 ${snapshot.created_at} 的快照吗？`))) return
    setSnapshotBusy(snapshot.id)
    try {
      await deleteContainerSnapshot(containerIdentifier, snapshot.id)
      await fetchSnapshots()
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      await dialog.alert('删除快照失败', error.response?.data?.message || '请稍后重试。')
    } finally {
      setSnapshotBusy('')
    }
  }

  const handleRestoreSnapshot = async (snapshot: Snapshot) => {
    if (!containerIdentifier) return
    if (!(await ensureSubUserCanOperate())) return
    if (!(await dialog.confirm('恢复快照', `确定恢复到 ${snapshot.created_at} 的快照吗？当前容器数据会被覆盖。`))) return
    setSnapshotBusy(snapshot.id)
    try {
      await restoreContainerSnapshot(containerIdentifier, snapshot.id)
      await Promise.all([fetchSnapshots(), fetchContainer()])
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      await dialog.alert('恢复快照失败', error.response?.data?.message || '请稍后重试。')
    } finally {
      setSnapshotBusy('')
    }
  }

  const copyText = async (text: string) => {
    await copyToClipboard(text)
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-black"></div>
      </div>
    )
  }

  if (!container) {
    return (
      <div className="text-center py-20">
        <p className="text-gray-500">容器不存在</p>
        <button onClick={() => navigate('/containers')} className="mt-4 text-sm text-black underline">
          返回列表
        </button>
      </div>
    )
  }

  const isRunning = container.status === 'running'
  const isInitializing = container.status === 'initializing'
  const isKVM = (container.virtualization || 'lxc') === 'kvm'
  const isWindows = container.template?.includes('windows')
  const reinstallLinuxTemplate = !isWindowsTemplate(selectedTemplate)
  const canOpenVNC = isKVM && isRunning
  const isExpired = container.expires_at ? new Date(container.expires_at) < new Date() : false
  const isPolicyBlocked = !!container.policy_blocked
  const isSubUserPolicyBlocked = isSubUser && isPolicyBlocked
  const policyBlockedText = container.policy_blocked_reason || '虚拟机被策略临时封禁'
  const publicIPv4s = container.public_ipv4s || []
  const assignedIPv4List = publicIPv4s.map((item) => item.address).filter(Boolean)
  const allocatableIPv4s = mergeIPv4Choices(hostInfo?.network.public_ipv4_addresses || [], publicIPv4s)
  const publicHost = assignedIPv4List[0] || hostInfo?.network.public_ipv4 || PUBLIC_HOST
  const ipv6List = (container.ipv6_addresses || [])
    .map((item) => item.address)
    .filter(Boolean)
  if (ipv6List.length === 0 && container.ipv6) ipv6List.push(container.ipv6)
  const maxVCPU = hostInfo?.cpu.cores || 64
  const maxRAMMB = hostInfo?.ram.total_mb ? Number(hostInfo.ram.total_mb) : undefined
  const hasIndependentIPv4 = assignedIPv4List.length > 0
  const hasIndependentIPv6 = ipv6List.length > 0
  const defaultConnPort = isWindows ? 3389 : 22
  const snapshotStoragePools = (storageInfo?.pools || []).filter((pool) =>
    pool.enabled !== false && pool.available !== false && (pool.content_types || []).includes('snapshots')
  )
  const snapshotStorageReady = isSubUser || snapshotStoragePools.length > 0

  let publicEndpoint = '-'
  let sshCommand = ''

  if (hasIndependentIPv4) {
    // Direct connection via independent IPv4 — all ports forwarded
    publicEndpoint = `${assignedIPv4List[0]}:${defaultConnPort}`
    if (!isWindows) {
      sshCommand = `ssh root@${assignedIPv4List[0]}`
    }
  } else if (hasIndependentIPv6) {
    publicEndpoint = `[${ipv6List[0]}]:${defaultConnPort}`
    if (!isWindows) {
      sshCommand = `ssh root@[${ipv6List[0]}]`
    }
  } else if (container.ssh_port > 0) {
    // NAT port mapping mode
    publicEndpoint = `${publicHost}:${container.ssh_port}`
    sshCommand = `ssh -p ${container.ssh_port} root@${publicHost}`
  }
  const editingSSH = draft.index !== null && !!container.port_mappings?.[draft.index] && (
    container.port_mappings[draft.index].description === 'SSH' || container.port_mappings[draft.index].container_port === 22 ||
    container.port_mappings[draft.index].description === 'RDP' || container.port_mappings[draft.index].container_port === 3389
  )
  const filtered = filterHistory(history, range)
  const cpuPct = clamp(((usage?.cpu_usage_pct || 0) / (container.vcpu || 1)))
  const ramTotalBytes = usage?.memory_total_bytes && usage.memory_total_bytes > 0
    ? usage.memory_total_bytes
    : container.ram_mb * 1024 * 1024
  const ramPct = ramTotalBytes > 0 ? clamp(((usage?.memory_usage_bytes || 0) / ramTotalBytes) * 100) : 0
  const loadPct = container.vcpu > 0 ? ((usage?.load1 || 0) / container.vcpu) * 100 : 0
  const diskPct = container.disk_gb > 0 ? clamp(((usage?.disk_usage_bytes || 0) / (container.disk_gb * 1024 * 1024 * 1024)) * 100) : 0
  const networkRxBps = usage?.network_rx_bps || 0
  const networkTxBps = usage?.network_tx_bps || 0
  const networkBps = networkRxBps + networkTxBps
  const networkDownLimit = resourceLimitValue(container.network_down_mbps, container.network_bw_mbps)
  const networkUpLimit = resourceLimitValue(container.network_up_mbps, container.network_bw_mbps)
  const netPct = Math.max(
    directionUsagePercent(networkRxBps, networkDownLimit, 125000, 125000000),
    directionUsagePercent(networkTxBps, networkUpLimit, 125000, 125000000),
  )
  const diskReadBps = usage?.disk_read_bps || 0
  const diskWriteBps = usage?.disk_write_bps || 0
  const diskIOBps = diskReadBps + diskWriteBps
  const ioReadLimit = resourceLimitValue(container.io_read_mbps, container.io_speed_mbps)
  const ioWriteLimit = resourceLimitValue(container.io_write_mbps, container.io_speed_mbps)
  const diskIOPct = Math.max(
    directionUsagePercent(diskReadBps, ioReadLimit, 1024 * 1024, 1024 * 1024 * 1024),
    directionUsagePercent(diskWriteBps, ioWriteLimit, 1024 * 1024, 1024 * 1024 * 1024),
  )
  const mappingCount = container.port_mappings?.length || 0
  const mappingLimit = Math.max(container.port_mapping_limit || 0, mappingCount)
  const hasNATQuota = mappingLimit > 0
  const canAddMapping = hasNATQuota && mappingCount < mappingLimit && !isSubUserPolicyBlocked
  const hasFirewallIPv4 = hasIndependentIPv4 || hasNATQuota
  const firewallNetworkOptions: Array<{ value: NonNullable<FirewallRule['network']>; label: string }> = []
  if (hasFirewallIPv4) {
    firewallNetworkOptions.push({
      value: 'ipv4',
      label: hasIndependentIPv4 ? 'IPv4（公网 IPv4）' : 'IPv4（NAT）',
    })
  }
  if (hasIndependentIPv6) {
    firewallNetworkOptions.push({ value: 'ipv6', label: 'IPv6' })
  }
  if (hasFirewallIPv4 && hasIndependentIPv6) {
    firewallNetworkOptions.push({ value: 'all', label: '全部网络' })
  }
  const managementUrl = subUser?.access_code
    ? `${window.location.origin}/login?code=${encodeURIComponent(subUser.access_code)}`
    : ''
  const charts: ResourceChartConfig[] = [
    {
      title: 'CPU 使用率',
      icon: <Cpu className="w-5 h-5" />,
      current: clamp(((usage?.cpu_usage_pct || 0) / (container.vcpu || 1))),
      points: toChartPoints(filtered, 'cpu'),
      max: 100,
      formatValue: formatPercent,
      detail: `${container.vcpu} 核`,
    },
    {
      title: '内存使用',
      icon: <MemoryStick className="w-5 h-5" />,
      current: ramPct,
      points: toChartPoints(filtered, 'memory'),
      max: 100,
      formatValue: formatPercent,
      detail: `${formatBytes(usage?.memory_usage_bytes || 0)} / ${formatBytes(ramTotalBytes)}`,
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
      detail: `入 ${formatRate(networkRxBps)} / 出 ${formatRate(networkTxBps)}，限速占用 ${netPct.toFixed(1)}%，累计 ${formatBytes((usage?.network_rx_bytes || 0) + (usage?.network_tx_bytes || 0))}`,
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
      detail: `读 ${formatRate(diskReadBps)} / 写 ${formatRate(diskWriteBps)}，限速占用 ${diskIOPct.toFixed(1)}%，累计 ${formatBytes((usage?.disk_read_bytes || 0) + (usage?.disk_write_bytes || 0))}，容量 ${diskPct.toFixed(1)}%`,
    },
  ]

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <button
          onClick={() => navigate('/containers')}
          className="flex items-center gap-1.5 text-sm text-gray-600 hover:text-black transition-colors"
        >
          <ArrowLeft className="w-4 h-4" />
          返回列表
        </button>
        <div />
      </div>

      <div className="bg-white border border-gray-200 rounded-lg p-5">
        <div className="flex items-start justify-between gap-4">
          <div className="flex items-start gap-4">
            <div className="w-14 h-14 flex items-center justify-center">
              {getTemplateIcon(container.template || '') || <Cpu className="w-7 h-7 text-slate-700" />}
            </div>
            <div>
              <div className="flex items-center gap-2 flex-wrap">
                <h1 className="text-xl font-bold text-black">{container.name}</h1>
                <StatusBadge running={isRunning} initializing={isInitializing} />
              </div>
              <div className="flex items-center gap-2 flex-wrap mt-2">
                <InfoTag color="blue">系统 {container.template}</InfoTag>
                <InfoTag color="slate">类型 {(container.virtualization || 'lxc').toUpperCase()}</InfoTag>
                <InfoTag color="emerald">内网 {container.ip || '-'}</InfoTag>
                {hasIndependentIPv4 ? (
                  <InfoTag color="amber">独立 IPv4 {assignedIPv4List[0]}</InfoTag>
                ) : (
                  <InfoTag color="amber">IPv4 NAT {hasNATQuota ? `${mappingCount} 条` : '未分配'}</InfoTag>
                )}
                <InfoTag color="violet">{isWindows ? 'RDP' : 'SSH'} {publicEndpoint}</InfoTag>
                {isPolicyBlocked && <InfoTag color="red">策略封禁</InfoTag>}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-1.5 flex-wrap justify-end">
            {!isRunning ? (
              <ActionButton dark disabled={!!taskStatus || isExpired || isSubUserPolicyBlocked} onClick={() => handleAction('start')}>
                <Play className="w-3.5 h-3.5" />
                {isSubUserPolicyBlocked ? '已封禁' : isExpired ? '已到期' : taskStatus === 'start' ? taskActionLabels['start'] : '开机'}
              </ActionButton>
            ) : (
              <>
                <ActionButton disabled={!!taskStatus || isExpired || isSubUserPolicyBlocked} onClick={() => handleAction('stop')}>
                  <Square className="w-3.5 h-3.5" />
                  {isSubUserPolicyBlocked ? '已封禁' : isExpired ? '已到期' : taskStatus === 'stop' ? taskActionLabels['stop'] : '关机'}
                </ActionButton>
                <ActionButton disabled={!!taskStatus || isExpired || isSubUserPolicyBlocked} onClick={() => handleAction('restart')}>
                  <RefreshCw className="w-3.5 h-3.5" />
                  {isSubUserPolicyBlocked ? '已封禁' : isExpired ? '已到期' : taskStatus === 'restart' ? taskActionLabels['restart'] : '重启'}
                </ActionButton>
                {!isWindows && (
                  <ActionButton dark disabled={isSubUserPolicyBlocked} onClick={() => setShowSSH(true)}>
                    <TerminalSquare className="w-3.5 h-3.5" />
                    WebSSH
                  </ActionButton>
                )}
                {isKVM && (
                  <ActionButton dark disabled={!canOpenVNC || isSubUserPolicyBlocked} onClick={() => setShowVNC(true)}>
                    <Monitor className="w-3.5 h-3.5" />
                    WebVNC
                  </ActionButton>
                )}
              </>
            )}
            {!isSubUser && (
              <ActionButton onClick={handleCreateSubUser} disabled={!!taskStatus || isExpired}>
                <UserPlus className="w-3.5 h-3.5" />
                管理链接
              </ActionButton>
            )}
            {!hasIndependentIPv4 && hasNATQuota && (
              <ActionButton disabled={isSubUserPolicyBlocked} onClick={() => setShowNat(true)}>
                <Settings className="w-3.5 h-3.5" />
                IPv4 NAT 管理
              </ActionButton>
            )}
            <ActionButton onClick={openFirewall} disabled={isSubUserPolicyBlocked}>
              <FirewallIcon className="w-3.5 h-3.5" />
              防火墙
            </ActionButton>
            <ActionButton onClick={() => setShowSnapshots(true)} disabled={!!taskStatus || !!snapshotBusy || isSubUserPolicyBlocked}>
              <Camera className="w-3.5 h-3.5" />
              快照
            </ActionButton>
            <ActionButton onClick={openReinstall} disabled={!!taskStatus || isExpired || isSubUserPolicyBlocked}>
              <RefreshCw className="w-3.5 h-3.5" />
              {isExpired ? '已到期' : taskStatus === 'reinstall' ? taskActionLabels['reinstall'] : '重装'}
            </ActionButton>
            {!isSubUser && (
              <ActionButton disabled={!!taskStatus} onClick={() => handleAction('delete')}>
                <Trash2 className="w-3.5 h-3.5" />
                {taskStatus === 'delete' ? taskActionLabels['delete'] : '删除'}
              </ActionButton>
            )}
          </div>
        </div>
      </div>

      {isSubUserPolicyBlocked && (
        <div className="flex items-start gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <div>
            <div className="font-medium">虚拟机被策略临时封禁</div>
            <div className="mt-1 text-xs text-red-600">{policyBlockedText}</div>
          </div>
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        <Panel
          title="连接信息"
          extra={!isWindows && !isSubUserPolicyBlocked ? (
            <button
              onClick={openResetPassword}
              className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs text-gray-600 hover:bg-gray-100 hover:text-black"
            >
              <Key className="w-3.5 h-3.5" />
              重置 SSH 密码
            </button>
          ) : undefined}
        >
          {isSubUserPolicyBlocked ? (
            <div className="rounded-md border border-red-100 bg-red-50 px-3 py-2 text-sm text-red-700">
              虚拟机被策略临时封禁，连接信息暂不可用。
            </div>
          ) : isWindows ? (
            <>
              <PlainRow label="RDP 地址" value={publicEndpoint} mono />
              <PlainRow label="用户名" value="Administrator" mono />
              <div className="flex items-center justify-between gap-3">
                <span className="text-gray-500">管理员密码</span>
                <div className="flex items-center gap-1 min-w-0">
                  <span
                    className={`font-mono text-xs cursor-pointer select-none ${showPassword ? 'text-black' : 'text-gray-400 tracking-[0.25em]'}`}
                    onClick={() => setShowPassword(!showPassword)}
                    title={showPassword ? '点击隐藏' : '点击显示'}
                  >
                    {container.ssh_password ? (showPassword ? container.ssh_password : '••••••••') : '-'}
                  </span>
                  {container.ssh_password && (
                    <button onClick={() => copyText(container.ssh_password)} className="p-0.5 text-gray-400 hover:text-black rounded" title="复制">
                      <Copy className="w-3 h-3" />
                    </button>
                  )}
                </div>
              </div>
              {container.vnc_port > 0 && (
                <PlainRow label="VNC 端口" value={`127.0.0.1:${container.vnc_port}`} mono />
              )}
            </>
          ) : (
            <>
              <PlainRow label="SSH 地址" value={publicEndpoint} mono copyValue={sshCommand} onCopy={copyText} />
              <PlainRow label="用户名" value="root" mono />
              <div className="flex items-center justify-between gap-3">
                <span className="text-gray-500">SSH 密码</span>
                <div className="flex items-center gap-1 min-w-0">
                  <span
                    className={`font-mono text-xs cursor-pointer select-none ${showPassword ? 'text-black' : 'text-gray-400 tracking-[0.25em]'}`}
                    onClick={() => setShowPassword(!showPassword)}
                    title={showPassword ? '点击隐藏' : '点击显示'}
                  >
                    {container.ssh_password ? (showPassword ? container.ssh_password : '••••••••') : '-'}
                  </span>
                  {container.ssh_password && (
                    <button onClick={() => copyText(container.ssh_password)} className="p-0.5 text-gray-400 hover:text-black rounded" title="复制">
                      <Copy className="w-3 h-3" />
                    </button>
                  )}
                </div>
              </div>
            </>
          )}
        </Panel>

        <Panel title="资源配置" extra={!isSubUser ? (
          <button onClick={openResourceEdit} className="text-gray-400 hover:text-black transition-colors" title="编辑资源限制">
            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
              <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
            </svg>
          </button>
        ) : undefined}>
          <PlainRow label="vCPU" value={`${container.vcpu} 核`} />
          <PlainRow label="内存" value={`${container.ram_mb} MB`} />
          <PlainRow label="磁盘" value={`${container.disk_gb} GB`} />
          <PlainRow label="网络速率" value={formatDirectionalLimit(t('下行'), networkDownLimit, t('上行'), networkUpLimit, 'Mbps')} />
          <PlainRow label="IO 速度" value={formatDirectionalLimit(t('读取'), ioReadLimit, t('写入'), ioWriteLimit, 'MB/s')} />
        </Panel>

        <Panel title="实时状态">
          <PlainRow label="识别码" value={container.uuid || '-'} mono copyValue={container.uuid} onCopy={copyText} />
          <PlainRow label="状态" value={isRunning ? '运行中' : '已停止'} />
          <PlainRow label="内网 IP" value={container.ip || '-'} mono />
          <PlainRow label="Public IPv4" value={assignedIPv4List.length ? assignedIPv4List.join(', ') : '-'} mono copyValue={assignedIPv4List[0]} onCopy={copyText}>
            {!isSubUser && (
              <button onClick={openIPAssign} className="ml-1 p-0.5 text-gray-400 hover:text-black rounded" title="修改公网 IP 分配">
                <Pencil className="w-3 h-3" />
              </button>
            )}
          </PlainRow>
          <PlainRow label="IPv6" value={ipv6List.length ? ipv6List.join(', ') : '-'} mono copyValue={ipv6List[0]} onCopy={copyText}>
            {!isSubUser && (
              <>
                {ipv6List.length === 0 && (
                  <button onClick={handleAssignIPv6} disabled={actionLoading === 'ipv6'} className="ml-1 px-1.5 py-0.5 text-[10px] text-gray-600 border border-gray-200 rounded hover:bg-gray-50 disabled:opacity-50">
                    Assign
                  </button>
                )}
                <button onClick={openIPAssign} className="ml-1 p-0.5 text-gray-400 hover:text-black rounded" title="修改公网 IP 分配">
                  <Pencil className="w-3 h-3" />
                </button>
              </>
            )}
          </PlainRow>
          <PlainRow label="CPU 累计时间" value={formatCPU(usage?.cpu_usage_usec || 0)} />
          <PlainRow label="创建时间" value={container.created_at} />
          <PlainRow label="到期时间" value={formatExpiration(container.expires_at)}>
            {!isSubUser && (
              <button onClick={() => { setEditExpiry(container.expires_at ? container.expires_at.slice(0, 10) : ''); setShowExpiryEdit(true) }} className="ml-1 p-0.5 text-gray-400 hover:text-black rounded" title="修改到期时间">
                <Pencil className="w-3 h-3" />
              </button>
            )}
          </PlainRow>
        </Panel>
      </div>

      {/* Container resource ring stats (matching host dashboard style) */}
      {container && (
        <div className="bg-white border border-gray-200 rounded-lg p-5">
          <h2 className="text-sm font-semibold text-black mb-4">状态</h2>
          <div className="grid grid-cols-4 gap-4">
            <RingStat
              value={cpuPct}
              label="CPU"
              subLabel={`(${(cpuPct * container.vcpu / 100).toFixed(1)} / ${container.vcpu} 核)`}
            />
            <RingStat
              value={ramPct}
              label="内存"
              subLabel={`${formatMB(usage?.memory_usage_bytes || 0)} / ${formatMB(ramTotalBytes)}`}
            />
            <RingStat
              value={loadPct}
              max={Infinity}
              label="负载"
            />
            <RingStat
              value={diskPct}
              label="磁盘"
              subLabel={`${formatGB(usage?.disk_usage_bytes || 0)} / ${container.disk_gb} GB`}
            />
          </div>
        </div>
      )}

      {/* Traffic usage bar */}
      {container && (
        <div className="bg-white border border-gray-200 rounded-lg p-5">
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-semibold text-black">月流量</h2>
            {!isSubUser && (
              <button
                onClick={openTrafficEdit}
                className="text-gray-400 hover:text-black transition-colors"
                title="编辑流量限制"
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
                  <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
                </svg>
              </button>
            )}
          </div>
          <TrafficBar container={container} />
        </div>
      )}
      {/* Traffic limit edit modal */}
      {showTrafficEdit && (
        <Modal title="编辑月流量限制" onClose={() => setShowTrafficEdit(false)}>
          <div className="space-y-3">
            <div>
              <label className="block text-xs text-gray-500 mb-1">流量统计模式</label>
              <select
                value={trafficEdit.mode}
                onChange={(e) => setTrafficEdit({ ...trafficEdit, mode: e.target.value })}
                className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm"
              >
                <option value="total">双向合并统计</option>
                <option value="in_out">入站/出站分开统计</option>
              </select>
            </div>
            {trafficEdit.mode === 'total' ? (
              <div>
                <label className="block text-xs text-gray-500 mb-1">月流量上限 (GB，0=不限制)</label>
                <input
                  type="number"
                  min="0"
                  value={trafficEdit.monthly}
                  onChange={(e) => setTrafficEdit({ ...trafficEdit, monthly: Math.max(0, Number(e.target.value)) })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm"
                />
              </div>
            ) : (
              <>
                <div>
                  <label className="block text-xs text-gray-500 mb-1">入站上限 (GB，0=不限制)</label>
                  <input
                    type="number"
                    min="0"
                    value={trafficEdit.inGB}
                    onChange={(e) => setTrafficEdit({ ...trafficEdit, inGB: Math.max(0, Number(e.target.value)) })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm"
                  />
                </div>
                <div>
                  <label className="block text-xs text-gray-500 mb-1">出站上限 (GB，0=不限制)</label>
                  <input
                    type="number"
                    min="0"
                    value={trafficEdit.outGB}
                    onChange={(e) => setTrafficEdit({ ...trafficEdit, outGB: Math.max(0, Number(e.target.value)) })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm"
                  />
                </div>
              </>
            )}
            <div className="flex justify-end gap-2 pt-2">
              <button onClick={() => setShowTrafficEdit(false)} className="px-4 py-2 text-sm text-gray-600 border border-gray-200 rounded-md hover:bg-gray-50">取消</button>
              <button onClick={saveTrafficLimit} disabled={savingTraffic} className="px-4 py-2 text-sm bg-black text-white rounded-md hover:bg-gray-800 disabled:opacity-50">{savingTraffic ? '保存中...' : '保存'}</button>
            </div>
          </div>
        </Modal>
      )}

      <ResourceStatsPanel range={range} onRangeChange={setRange} onRefresh={() => { fetchContainer(); fetchUsage() }} charts={charts} />

      {showResetPassword && (
        <Modal title="重置 SSH 密码" onClose={() => setShowResetPassword(false)}>
          <div className="space-y-4">
            <div>
              <label className="block text-xs text-gray-500 mb-1">新 SSH 密码</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={resetPasswordDraft}
                  onChange={(e) => { setResetPasswordDraft(e.target.value); setResetPasswordResult('') }}
                  placeholder="请输入 8-64 位，至少包含字母和数字"
                  className={inputClass}
                />
                <button
                  type="button"
                  onClick={generateResetPassword}
                  className="px-3 py-2 border border-gray-300 rounded-md text-gray-600 hover:bg-gray-50 hover:text-black"
                  title="生成随机密码"
                >
                  <RefreshCw className="w-4 h-4" />
                </button>
              </div>
              {resetPasswordDraft && resetPasswordError(resetPasswordDraft) && (
                <p className="mt-1 text-xs text-red-600">{resetPasswordError(resetPasswordDraft)}</p>
              )}
            </div>
            {resetPasswordResult && (
              <div className="p-3 bg-green-50 border border-green-200 rounded-md">
                <div className="text-xs text-green-700 mb-1">密码已修改成功</div>
                <div className="flex items-center justify-between gap-2">
                  <span className="font-mono text-sm text-green-900 break-all">{resetPasswordResult}</span>
                  <button onClick={() => copyText(resetPasswordResult)} className="p-1 text-green-700 hover:text-green-900 rounded" title="复制">
                    <Copy className="w-4 h-4" />
                  </button>
                </div>
              </div>
            )}
            <p className="text-xs text-gray-500 leading-relaxed">
              Linux LXC/KVM 修改 root SSH 密码通常无需重启；KVM 需要虚拟机运行且 guest agent 或 SSH 可用。
            </p>
            <div className="flex justify-end gap-2 pt-2">
              <button onClick={() => setShowResetPassword(false)} className="px-4 py-2 text-sm text-gray-600 border border-gray-200 rounded-md hover:bg-gray-50">取消</button>
              <button
                onClick={handleResetPassword}
                disabled={resetPasswordSaving || !resetPasswordDraft || !!resetPasswordError(resetPasswordDraft)}
                className="px-4 py-2 text-sm bg-black text-white rounded-md hover:bg-gray-800 disabled:opacity-50"
              >
                {resetPasswordSaving ? '修改中...' : '确认修改'}
              </button>
            </div>
          </div>
        </Modal>
      )}

      {showSSH && (
        <Modal title={`WebSSH - ${container.name}`} onClose={() => setShowSSH(false)} wide>
          <div className="h-[70vh] min-h-[520px]">
            {isRunning ? (
              <WebSSHViewer containerName={container.name} onClose={() => setShowSSH(false)} />
            ) : (
              <div className="h-full flex items-center justify-center bg-gray-950 text-gray-400 rounded-md">容器未运行，请先开机</div>
            )}
          </div>
        </Modal>
      )}

      {showVNC && (
        <Modal
          title={`WebVNC - ${container.name}`}
          onClose={() => setShowVNC(false)}
          wide
          flush
          extra={
            <button
              onClick={toggleVNCFullscreen}
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs text-gray-600 hover:text-black hover:bg-gray-100 rounded"
              title={vncFullscreen ? '退出全屏' : '全屏显示'}
            >
              {vncFullscreen ? <Minimize2 className="w-3.5 h-3.5" /> : <Maximize2 className="w-3.5 h-3.5" />}
              {vncFullscreen ? '退出全屏' : '全屏'}
            </button>
          }
        >
          <div
            ref={vncFullscreenRef}
            className={`bg-white ${vncFullscreen ? 'fixed inset-0 z-[70] p-3' : 'h-[calc(92vh-112px)] min-h-[420px] p-5'}`}
          >
            <div className="h-full">
            {canOpenVNC ? (
              <WebVNCViewer containerName={container.name} onClose={() => setShowVNC(false)} />
            ) : (
              <div className="h-full flex items-center justify-center bg-gray-950 text-gray-400 rounded-md">VNC 控制台暂不可用，请确认 KVM 虚拟机已开机并刷新页面</div>
            )}
            </div>
          </div>
        </Modal>
      )}

      {showSnapshots && (
        <Modal
          title="快照"
          onClose={() => {
            setShowSnapshots(false)
            setEditingSnapshotQuota(false)
          }}
          wide
          extra={
            <div className="flex items-center gap-2">
              <button
                onClick={openSnapshotSchedule}
                disabled={!!snapshotBusy || storageLoading || !snapshotStorageReady}
                className={`inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs ${
                  snapshotSchedule?.enabled
                    ? 'border border-blue-200 bg-blue-50 text-blue-700 hover:bg-blue-100'
                    : 'border border-gray-300 text-gray-700 hover:bg-gray-50'
                } disabled:opacity-50`}
              >
                <Clock className="w-3.5 h-3.5" />
                {snapshotBusy === 'schedule' ? '处理中...' : snapshotSchedule?.enabled ? '定时设置' : '定时快照'}
              </button>
              <button
                onClick={handleCreateSnapshot}
                disabled={!!snapshotBusy || storageLoading || !snapshotStorageReady || (isSubUser && snapshots.length >= snapshotQuota)}
                className="inline-flex items-center gap-1.5 rounded-md bg-black px-3 py-1.5 text-xs text-white hover:bg-gray-800 disabled:opacity-50"
              >
                <Camera className="w-3.5 h-3.5" />
                {snapshotBusy === 'create' ? '创建中...' : '新建快照'}
              </button>
            </div>
          }
        >
          <div className="space-y-4">
            {storageLoading && !isSubUser && (
              <div className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-sm text-gray-600">
                <RefreshCw className="h-4 w-4 animate-spin" />
                正在检查存储配置...
              </div>
            )}
            {!storageLoading && !snapshotStorageReady && (
              <div className="flex items-center justify-between gap-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
                <span>尚未开启快照存储，无法新建或启用定时快照。</span>
                <button onClick={() => { setShowSnapshots(false); navigate('/storage') }} className="shrink-0 rounded-md border border-amber-300 bg-white px-3 py-1.5 text-xs font-medium hover:bg-amber-100">
                  去开启
                </button>
              </div>
            )}
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-xs text-gray-600">
              <div>
                快照数量：
                <span className="font-mono text-gray-900">
                  {snapshots.length}
                </span>
              </div>
              <div className="flex items-center gap-2">
                <span>子用户配额：</span>
                <span className="font-mono text-gray-900">{snapshotQuota}</span>
                {!isSubUser && (
                  <button
                    onClick={() => {
                      setSnapshotQuotaDraft(snapshotQuota)
                      setEditingSnapshotQuota((value) => !value)
                    }}
                    className="inline-flex items-center gap-1 rounded border border-gray-300 bg-white px-2 py-1 text-[11px] text-gray-700 hover:bg-gray-50"
                    disabled={snapshotBusy === 'quota'}
                  >
                    <Pencil className="w-3 h-3" />
                    修改
                  </button>
                )}
              </div>
              <div>
                定时状态：
                <span className="text-gray-900">
                  {snapshotSchedule?.enabled ? `已开启，每 ${formatScheduleInterval(snapshotSchedule.interval_hours || 24)}，${snapshotSchedule.time || '03:00'} 执行` : '未开启'}
                </span>
              </div>
              {snapshotSchedule?.next_run && (
                <div>下次执行：<span className="font-mono text-gray-900">{formatDateTime(snapshotSchedule.next_run)}</span></div>
              )}
            </div>

            {!isSubUser && snapshotStoragePools.length > 0 && (
              <div className="flex flex-wrap items-end gap-3 rounded-lg border border-gray-200 bg-white px-4 py-3">
                <Field label="新建快照存储磁盘">
                  <select
                    value={snapshotStoragePoolID}
                    onChange={(event) => setSnapshotStoragePoolID(event.target.value)}
                    className="w-72 px-3 py-2 border border-gray-300 rounded-md text-sm text-black bg-white focus:outline-none focus:ring-2 focus:ring-black focus:border-black"
                  >
                    <option value="">自动选择（默认盘优先，空间不足自动切换）</option>
                    {snapshotStoragePools.map((pool) => (
                      <option key={pool.id} value={pool.id}>
                        {pool.name} · {pool.mount_point || pool.path}
                      </option>
                    ))}
                  </select>
                </Field>
                <div className="pb-2 text-xs text-gray-400">仅影响手动新建快照；定时快照使用默认磁盘。</div>
              </div>
            )}

            {editingSnapshotQuota && !isSubUser && (
              <div className="flex flex-wrap items-end gap-3 rounded-lg border border-gray-200 bg-white px-4 py-3">
                <Field label="子用户每台容器快照上限">
                  <input
                    type="number"
                    min={1}
                    max={999}
                    value={snapshotQuotaDraft}
                    onChange={(event) => setSnapshotQuotaDraft(Math.max(1, Math.round(Number(event.target.value) || 1)))}
                    className="w-44 px-3 py-2 border border-gray-300 rounded-md text-sm text-black bg-white focus:outline-none focus:ring-2 focus:ring-black focus:border-black"
                  />
                </Field>
                <div className="flex gap-2 pb-0.5">
                  <button
                    onClick={() => {
                      setEditingSnapshotQuota(false)
                      setSnapshotQuotaDraft(snapshotQuota)
                    }}
                    className="px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 rounded-md"
                    disabled={snapshotBusy === 'quota'}
                  >
                    取消
                  </button>
                  <button
                    onClick={saveSnapshotQuota}
                    disabled={snapshotBusy === 'quota'}
                    className="inline-flex items-center gap-1.5 px-3 py-2 text-sm bg-black text-white rounded-md hover:bg-gray-800 disabled:opacity-50"
                  >
                    <Save className="w-4 h-4" />
                    {snapshotBusy === 'quota' ? '保存中...' : '保存'}
                  </button>
                </div>
              </div>
            )}

            <SnapshotTable
              snapshots={snapshots}
              busy={snapshotBusy}
              onRestore={handleRestoreSnapshot}
              onDelete={handleDeleteSnapshot}
            />
          </div>
        </Modal>
      )}

      {showSnapshotSchedule && (
        <Modal title="定时快照" onClose={() => setShowSnapshotSchedule(false)}>
          <div className="space-y-4">
            <Field label="自动快照周期">
              <select
                value={snapshotScheduleDraft.intervalHours}
                onChange={(e) => setSnapshotScheduleDraft({ ...snapshotScheduleDraft, intervalHours: Number(e.target.value) })}
                className={inputClass}
              >
                <option value={24}>1 天</option>
                <option value={72}>3 天</option>
                <option value={168}>7 天</option>
                <option value={336}>14 天</option>
              </select>
            </Field>
            <Field label="执行时间">
              <input
                type="time"
                value={snapshotScheduleDraft.time}
                onChange={(e) => setSnapshotScheduleDraft({ ...snapshotScheduleDraft, time: e.target.value || '03:00' })}
                className={inputClass}
              />
            </Field>
            <div className="rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-xs text-gray-500">
              {`每 ${formatScheduleInterval(snapshotScheduleDraft.intervalHours)} 在 ${snapshotScheduleDraft.time || '03:00'} 执行。`}
            </div>
            <div className="flex justify-between gap-3 pt-2">
              {snapshotSchedule?.enabled ? (
                <button
                  onClick={() => saveSnapshotSchedule(false)}
                  disabled={snapshotBusy === 'schedule'}
                  className="px-4 py-2 text-sm text-red-600 border border-red-200 rounded-md hover:bg-red-50 disabled:opacity-50"
                >
                  关闭定时
                </button>
              ) : <div />}
              <div className="flex gap-2">
                <button onClick={() => setShowSnapshotSchedule(false)} className="px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 rounded-md">取消</button>
                <button
                  onClick={() => saveSnapshotSchedule(true)}
                  disabled={snapshotBusy === 'schedule'}
                  className="px-4 py-2 text-sm bg-black text-white rounded-md hover:bg-gray-800 disabled:opacity-50"
                >
                  {snapshotBusy === 'schedule' ? '保存中...' : '保存'}
                </button>
              </div>
            </div>
          </div>
        </Modal>
      )}

      {showFirewall && (
        <Modal title="防火墙设置" onClose={() => { setShowFirewall(false); setShowFirewallEditor(false); setEditingFirewallRule(null) }} wide extra={
          !isSubUser && (
            <button
              onClick={addFirewallRule}
              disabled={firewallNetworkOptions.length === 0}
              title={firewallNetworkOptions.length === 0 ? '当前容器没有可配置的 NAT、公网 IPv4 或 IPv6' : undefined}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 bg-black text-white rounded-md text-xs hover:bg-gray-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Plus className="w-3.5 h-3.5" />添加规则
            </button>
          )
        }>
          <div className="space-y-5">
            {/* Global toggle */}
            <div className="flex items-center justify-between gap-4">
              <div>
                <div className="text-sm font-medium text-gray-800">防火墙</div>
                <div className="text-xs text-gray-500">
                  {firewallEnabled
                    ? (firewallDefaultAction === 'DROP' ? '已启用，未匹配规则的流量将被拒绝' : '已启用，未匹配规则的流量将被放行')
                    : '未启用时不接管该容器流量'}
                </div>
              </div>
              <button
                onClick={() => setFirewallEnabled(!firewallEnabled)}
                className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${firewallEnabled ? 'bg-emerald-500' : 'bg-gray-300'}`}
              >
                <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${firewallEnabled ? 'translate-x-6' : 'translate-x-1'}`} />
              </button>
            </div>

            <div className="flex items-center justify-between gap-4 rounded-md border border-gray-200 px-3 py-2">
              <div>
                <div className="text-sm font-medium text-gray-800">默认动作</div>
                <div className="text-xs text-gray-500">没有命中下方规则时如何处理</div>
              </div>
              <select
                value={firewallDefaultAction}
                onChange={(e) => setFirewallDefaultAction(e.target.value as 'ACCEPT' | 'DROP')}
                disabled={isSubUser}
                className="rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-xs text-gray-800 focus:border-black focus:outline-none focus:ring-2 focus:ring-black disabled:opacity-60"
              >
                <option value="DROP">未匹配拒绝</option>
                <option value="ACCEPT">未匹配放行</option>
              </select>
            </div>

            <div className="rounded-md border border-blue-100 bg-blue-50 px-3 py-2 text-xs text-blue-800">
              <div className="font-medium text-blue-900">网络范围</div>
              <div className="mt-1">
                {firewallNetworkOptions.length > 0
                  ? `可配置：${firewallNetworkOptions.filter((option) => option.value !== 'all').map((option) => option.label).join('、')}。`
                  : '当前容器未分配 IPv4 NAT、独立公网 IPv4 或 IPv6，暂无可配置网络。'}
                {hasFirewallIPv4 ? ` IPv4 规则覆盖${hasIndependentIPv4 ? '独立公网 IPv4' : 'IPv4 NAT 端口映射'}。` : ''}
                {hasNATQuota && !hasIndependentIPv4 ? ' NAT 入站端口按容器内部端口匹配，不是宿主机公网端口。' : ''}
                {hasIndependentIPv6 ? ' IPv6 规则覆盖该容器已分配的 IPv6 地址。' : ''}
              </div>
            </div>

            {firewallMessage && (
              <div className={`rounded-md px-3 py-2 text-xs ${firewallMessage.type === 'success' ? 'border border-emerald-100 bg-emerald-50 text-emerald-700' : 'border border-red-100 bg-red-50 text-red-700'}`}>
                {firewallMessage.text}
              </div>
            )}

            {/* Rules table */}
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="border-b border-gray-200 bg-gray-50 text-xs text-gray-500">
                  <tr>
                    <th className="px-3 py-2 text-left font-medium">状态</th>
                    <th className="px-3 py-2 text-left font-medium">网络</th>
                    <th className="px-3 py-2 text-left font-medium">方向</th>
                    <th className="px-3 py-2 text-left font-medium">协议</th>
                    <th className="px-3 py-2 text-left font-medium">端口</th>
                    <th className="px-3 py-2 text-left font-medium">来源/目标 IP</th>
                    <th className="px-3 py-2 text-left font-medium">动作</th>
                    <th className="px-3 py-2 text-left font-medium">描述</th>
                    {!isSubUser && <th className="px-3 py-2 text-right font-medium">操作</th>}
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {firewallRules.map((rule) => (
                    <tr key={rule.id} className={!rule.enabled ? 'opacity-50' : ''}>
                      <td className="px-3 py-2">
                        <button onClick={() => toggleFirewallRule(rule.id)} className={`inline-flex h-4 w-7 items-center rounded-full transition-colors ${rule.enabled ? 'bg-emerald-500' : 'bg-gray-300'}`}>
                          <span className={`inline-block h-3 w-3 transform rounded-full bg-white transition-transform ${rule.enabled ? 'translate-x-3.5' : 'translate-x-0.5'}`} />
                        </button>
                      </td>
                      <td className="px-3 py-2">
                        <span className="inline-flex rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-700">
                          {(rule.network || 'ipv4') === 'ipv6' ? 'IPv6' : (rule.network || 'ipv4') === 'all' ? '全部' : 'IPv4'}
                        </span>
                      </td>
                      <td className="px-3 py-2">
                        <span className={`inline-flex px-1.5 py-0.5 rounded text-xs font-medium ${rule.direction === 'in' ? 'bg-blue-50 text-blue-700' : 'bg-orange-50 text-orange-700'}`}>
                          {rule.direction === 'in' ? '入站' : '出站'}
                        </span>
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">{rule.protocol.toUpperCase()}</td>
                      <td className="px-3 py-2 font-mono text-xs">{rule.port || '全部'}</td>
                      <td className="px-3 py-2 font-mono text-xs">{rule.source_ip || '任意'}</td>
                      <td className="px-3 py-2">
                        <span className={`inline-flex px-1.5 py-0.5 rounded text-xs font-medium ${rule.action === 'ACCEPT' ? 'bg-emerald-50 text-emerald-700' : 'bg-red-50 text-red-700'}`}>
                          {rule.action === 'ACCEPT' ? '放行' : '拒绝'}
                        </span>
                      </td>
                      <td className="px-3 py-2 text-xs text-gray-600 max-w-32 truncate">{rule.description || '-'}</td>
                      {!isSubUser && (
                        <td className="px-3 py-2 text-right">
                          <div className="inline-flex items-center gap-1">
                            <button onClick={() => {
                              const currentNetwork = (rule.network || 'ipv4') as NonNullable<FirewallRule['network']>
                              const network = firewallNetworkOptions.some((option) => option.value === currentNetwork)
                                ? currentNetwork
                                : (firewallNetworkOptions[0]?.value || currentNetwork)
                              setEditingFirewallRule({ ...rule, network })
                              setShowFirewallEditor(true)
                            }} className="p-1.5 text-gray-400 hover:text-gray-700 rounded hover:bg-gray-100">
                              <Pencil className="h-3.5 w-3.5" />
                            </button>
                            <button onClick={() => deleteFirewallRule(rule.id)} className="p-1.5 text-gray-400 hover:text-red-600 rounded hover:bg-red-50">
                              <Trash2 className="h-3.5 w-3.5" />
                            </button>
                          </div>
                        </td>
                      )}
                    </tr>
                  ))}
                  {firewallRules.length === 0 && (
                    <tr><td colSpan={isSubUser ? 8 : 9} className="px-3 py-6 text-center text-xs text-gray-400">暂无防火墙规则</td></tr>
                  )}
                </tbody>
              </table>
            </div>

            {/* Save button */}
            {!isSubUser && (
              <div className="flex justify-end">
                <button onClick={saveFirewall} disabled={firewallSaving} className="inline-flex items-center gap-1.5 px-4 py-2 bg-black text-white rounded-md text-sm hover:bg-gray-800 disabled:opacity-50">
                  <Save className="w-3.5 h-3.5" />
                  {firewallSaving ? '保存中...' : '保存'}
                </button>
              </div>
            )}
          </div>
        </Modal>
      )}

      {showFirewallEditor && editingFirewallRule && (
        <Modal title={editingFirewallRule.id ? '编辑规则' : '添加规则'} onClose={() => { setShowFirewallEditor(false); setEditingFirewallRule(null) }}>
          <div className="space-y-4">
            <Field label="网络">
              {firewallNetworkOptions.length > 0 ? (
                <select value={editingFirewallRule.network || firewallNetworkOptions[0].value} onChange={(e) => setEditingFirewallRule({ ...editingFirewallRule, network: e.target.value as FirewallRule['network'] })} className={inputClass}>
                  {firewallNetworkOptions.map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
              ) : (
                <input value="当前容器没有可配置网络" disabled className={`${inputClass} bg-gray-100 text-gray-400`} />
              )}
            </Field>
            <Field label="方向">
              <select value={editingFirewallRule.direction} onChange={(e) => setEditingFirewallRule({ ...editingFirewallRule, direction: e.target.value as 'in' | 'out' })} className={inputClass}>
                <option value="in">入站 (Inbound)</option>
                <option value="out">出站 (Outbound)</option>
              </select>
            </Field>
            <Field label="协议">
              <select
                value={editingFirewallRule.protocol}
                onChange={(e) => {
                  const protocol = e.target.value as FirewallRule['protocol']
                  setEditingFirewallRule({
                    ...editingFirewallRule,
                    protocol,
                    port: protocol === 'tcp' || protocol === 'udp' ? editingFirewallRule.port : '',
                  })
                }}
                className={inputClass}
              >
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
                <option value="icmp">ICMP</option>
                <option value="all">全部</option>
              </select>
            </Field>
            <Field
              label="端口"
              hint={editingFirewallRule.protocol === 'tcp' || editingFirewallRule.protocol === 'udp'
                ? (editingFirewallRule.direction === 'in'
                  ? ((editingFirewallRule.network || 'ipv4') === 'ipv4' && hasNATQuota && !hasIndependentIPv4
                    ? 'NAT 入站填容器内部端口，例如公网 22023 -> 容器 22，这里填 22'
                    : '入站填容器服务端口；留空为全部端口，支持: 22 | 80,443 | 8000-9000')
                  : '出站填远端目标端口；留空为全部端口，支持: 22 | 80,443 | 8000-9000')
                : '端口仅适用于 TCP/UDP'}
            >
              <input
                value={editingFirewallRule.port}
                onChange={(e) => setEditingFirewallRule({ ...editingFirewallRule, port: e.target.value })}
                placeholder={editingFirewallRule.protocol === 'tcp' || editingFirewallRule.protocol === 'udp' ? '如: 22 或 80,443 或 8000-9000' : '当前协议不使用端口'}
                disabled={editingFirewallRule.protocol !== 'tcp' && editingFirewallRule.protocol !== 'udp'}
                className={`${inputClass} disabled:bg-gray-100 disabled:text-gray-400`}
              />
            </Field>
            <Field
              label={editingFirewallRule.direction === 'in' ? '来源 IP' : '目标 IP'}
              hint={(editingFirewallRule.network || 'ipv4') === 'ipv6' ? '留空为任意 IPv6，支持 CIDR: 2001:db8::/64' : (editingFirewallRule.network || 'ipv4') === 'all' ? '留空为任意 IP，支持 IPv4/IPv6 CIDR' : '留空为任意 IPv4，支持 CIDR: 192.168.1.0/24'}
            >
              <input
                value={editingFirewallRule.source_ip}
                onChange={(e) => setEditingFirewallRule({ ...editingFirewallRule, source_ip: e.target.value })}
                placeholder={(editingFirewallRule.network || 'ipv4') === 'ipv6' ? '如: 2001:db8::/64' : (editingFirewallRule.network || 'ipv4') === 'all' ? '如: 192.168.1.0/24 或 2001:db8::/64' : '如: 192.168.1.0/24'}
                className={inputClass}
              />
            </Field>
            <Field label="动作">
              <select value={editingFirewallRule.action} onChange={(e) => setEditingFirewallRule({ ...editingFirewallRule, action: e.target.value as 'ACCEPT' | 'DROP' })} className={inputClass}>
                <option value="ACCEPT">放行 (ACCEPT)</option>
                <option value="DROP">拒绝 (DROP)</option>
              </select>
            </Field>
            <Field label="描述">
              <input value={editingFirewallRule.description} onChange={(e) => setEditingFirewallRule({ ...editingFirewallRule, description: e.target.value })} placeholder="规则描述" className={inputClass} />
            </Field>
            <div className="flex justify-end gap-2 pt-2">
              <button onClick={() => { setShowFirewallEditor(false); setEditingFirewallRule(null) }} className="px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 rounded-md">取消</button>
              <button onClick={() => saveFirewallRule(editingFirewallRule)} className="px-4 py-2 text-sm bg-black text-white rounded-md hover:bg-gray-800">确定</button>
            </div>
          </div>
        </Modal>
      )}

      {showIPAssign && (
        <Modal title="公网 IP 分配" onClose={() => setShowIPAssign(false)} wide>
          <div className="grid gap-5 md:grid-cols-2">
            <div className="space-y-3">
              <div>
                <h3 className="text-sm font-medium text-gray-900">独立 IPv4</h3>
                <p className="mt-1 text-xs text-gray-500">修改后会重放端口映射、SNAT 和防火墙规则。</p>
              </div>
              <Segmented value={ipv4AssignMode} onChange={setIPv4AssignMode} />
              {ipv4AssignMode === 'random' && (
                <Field label="随机数量">
                  <input type="number" min={1} max={64} value={ipv4AssignCount} onChange={(e) => setIPv4AssignCount(parseInt(e.target.value || '1', 10))} className={inputClass} />
                </Field>
              )}
              {ipv4AssignMode === 'custom' && (
                <div className="space-y-2">
                  {allocatableIPv4s.length === 0 ? (
                    <div className="rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-xs text-gray-500">没有可选择的公网 IPv4，请先到路由管理配置 IPv4 池。</div>
                  ) : (
                    <div className="grid gap-2">
                      {allocatableIPv4s.map((ip) => (
                        <label key={`${ip.interface}-${ip.address}`} className="flex min-w-0 items-center gap-2 rounded-md border border-gray-200 px-3 py-2 text-xs text-gray-700">
                          <input
                            type="checkbox"
                            checked={ipv4Selected.includes(ip.address)}
                            onChange={(event) => {
                              const next = event.target.checked
                                ? Array.from(new Set([...ipv4Selected, ip.address]))
                                : ipv4Selected.filter((value) => value !== ip.address)
                              setIPv4Selected(next)
                              setIPv4AssignCount(Math.max(1, next.length || 1))
                            }}
                          />
                          <span className="truncate font-mono">{ip.address}</span>
                          <span className="shrink-0 text-gray-400">{ip.interface}</span>
                          {ip.gateway && <span className="shrink-0 text-gray-400">gw {ip.gateway}</span>}
                        </label>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>

            <div className="space-y-3">
              <div>
                <h3 className="text-sm font-medium text-gray-900">独立 IPv6</h3>
                <p className="mt-1 text-xs text-gray-500">自定义地址必须落在路由管理配置的 IPv6 前缀内。</p>
              </div>
              <Segmented value={ipv6AssignMode} onChange={setIPv6AssignMode} />
              {ipv6AssignMode === 'random' && (
                <Field label="随机数量">
                  <input type="number" min={1} max={64} value={ipv6AssignCount} onChange={(e) => setIPv6AssignCount(parseInt(e.target.value || '1', 10))} className={inputClass} />
                </Field>
              )}
              {ipv6AssignMode === 'custom' && (
                <Field label="IPv6 地址">
                  <textarea
                    value={ipv6DraftText}
                    onChange={(e) => {
                      setIPv6DraftText(e.target.value)
                      setIPv6AssignCount(Math.max(1, splitAddressLines(e.target.value).length || 1))
                    }}
                    className={`${inputClass} min-h-32 font-mono text-xs`}
                    placeholder="2001:db8:100::100&#10;2001:db8:100::101"
                  />
                </Field>
              )}
            </div>
          </div>
          <div className="mt-5 flex justify-end gap-2 border-t border-gray-200 pt-4">
            <button onClick={() => setShowIPAssign(false)} disabled={savingIPAssign} className="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">
              取消
            </button>
            <button onClick={submitIPAssign} disabled={savingIPAssign} className="inline-flex items-center gap-1.5 rounded-md bg-black px-3 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50">
              <Save className="h-4 w-4" />
              {savingIPAssign ? '保存中...' : '保存'}
            </button>
          </div>
        </Modal>
      )}

      {showNat && !hasIndependentIPv4 && (
        <Modal title="IPv4 NAT 端口管理" onClose={() => { setShowNat(false); setDraft(emptyDraft); setShowMappingEditor(false) }} wide extra={
          !isSubUser && canAddMapping && (
            <button onClick={openAddMapping} className="inline-flex items-center gap-1.5 px-3 py-1.5 bg-black text-white rounded-md text-xs hover:bg-gray-800">
              <Plus className="w-3.5 h-3.5" />添加映射
            </button>
          )
        }>
          <div className="space-y-5">
            <div className="flex items-center justify-between gap-4">
              <div className="text-xs text-gray-500">
                {hasNATQuota ? (
                  <>端口配额：<span className="font-mono text-gray-800">{mappingCount}/{mappingLimit}</span></>
                ) : (
                  <span>未分配 IPv4 NAT 端口配额</span>
                )}
              </div>
              {!isSubUser && hasNATQuota && !canAddMapping && (
                <div className="text-xs text-amber-600">已达到管理员分配的 IPv4 NAT 端口配额</div>
              )}
            </div>
            <MappingTable mappings={container.port_mappings || []} publicHost={publicHost} onEdit={openEditMapping} onDelete={isSubUser ? () => {} : removeMapping} isSubUser={isSubUser} />
          </div>
        </Modal>
      )}

      {showMappingEditor && (
        <Modal
          title={draft.index === null ? '添加端口映射' : '修改端口映射'}
          onClose={() => { setShowMappingEditor(false); setDraft(emptyDraft) }}
        >
          <MappingEditor
            draft={draft}
            setDraft={setDraft}
            isSubUser={isSubUser}
            canAddMapping={canAddMapping}
            saving={savingMapping}
            containerIdentifier={containerIdentifier}
            publicIPv4s={publicIPv4s}
            onCancel={() => { setShowMappingEditor(false); setDraft(emptyDraft) }}
            onSubmit={async () => {
              if (await submitMapping()) {
                setShowMappingEditor(false)
                setDraft(emptyDraft)
              }
            }}
          />
        </Modal>
      )}

      {showSubUser && subUser && (
        <Modal title="管理链接" onClose={() => setShowSubUser(false)}>
          <div className="bg-gray-50 dark:bg-gray-800 rounded-lg p-4 text-sm space-y-3">
            <div className="flex items-start justify-between gap-3">
              <span className="shrink-0 text-gray-500 dark:text-gray-400">地址</span>
              <div className="flex min-w-0 items-center gap-1">
                <span className="font-mono text-xs text-black dark:text-white break-all">{managementUrl}</span>
                <button onClick={() => copyText(managementUrl)} className="shrink-0 p-0.5 text-gray-400 hover:text-black dark:hover:text-white rounded"><Copy className="w-3 h-3" /></button>
              </div>
            </div>
            <div className="flex items-start justify-between gap-3">
              <span className="shrink-0 text-gray-500 dark:text-gray-400">密码</span>
              <div className="flex min-w-0 items-center gap-1">
                <span className="font-mono text-xs text-black dark:text-white">{subUser.password || ''}</span>
                <button onClick={() => copyText(subUser.password || '')} className="shrink-0 p-0.5 text-gray-400 hover:text-black dark:hover:text-white rounded"><Copy className="w-3 h-3" /></button>
              </div>
            </div>
          </div>
        </Modal>
      )}

      {showReinstall && (
        <Modal title="重装系统" onClose={() => setShowReinstall(false)}>
          <div className="space-y-4">
            <p className="text-sm text-gray-600">重装系统会删除容器内所有数据，请谨慎操作。</p>
            <Field label="选择新系统模板">
              <select value={selectedTemplate} onChange={(e) => setSelectedTemplate(e.target.value)} className={inputClass}>
                {templates.map((template) => (
                  <option key={template.id} value={template.id}>{template.name}</option>
                ))}
              </select>
            </Field>
            {reinstallLinuxTemplate && (
              <div className="rounded-md border border-gray-200 bg-white px-3 py-3 text-sm">
                <div className="mb-2 font-medium text-gray-800">登录方式</div>
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                  {([
                    ['keep', '保留当前密码'],
                    ['auto_password', '生成新密码'],
                    ['password', '自定义密码'],
                    ['key', 'SSH Key'],
                  ] as Array<[ReinstallSSHAuthMode, string]>).map(([mode, label]) => (
                    <button
                      key={mode}
                      type="button"
                      onClick={() => setReinstallAuthMode(mode)}
                      className={`rounded-md border px-3 py-2 text-xs font-medium transition-colors ${reinstallAuthMode === mode ? 'border-black bg-black text-white' : 'border-gray-300 text-gray-700 hover:bg-gray-50'}`}
                    >
                      {label}
                    </button>
                  ))}
                </div>
                {reinstallAuthMode === 'password' && (
                  <div className="mt-3 flex gap-2">
                    <input
                      type="text"
                      value={reinstallPasswordDraft}
                      onChange={(event) => setReinstallPasswordDraft(event.target.value)}
                      className={inputClass}
                      placeholder="RootPass123"
                    />
                    <button
                      type="button"
                      onClick={() => setReinstallPasswordDraft(generateSSHPassword())}
                      className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-gray-300 text-gray-600 hover:bg-gray-50"
                      title="生成密码"
                    >
                      <RefreshCw className="h-4 w-4" />
                    </button>
                  </div>
                )}
                {reinstallAuthMode === 'key' && (
                  <textarea
                    value={reinstallPublicKeyDraft}
                    onChange={(event) => setReinstallPublicKeyDraft(event.target.value)}
                    className={`${inputClass} mt-3 min-h-20 resize-y font-mono text-xs`}
                    placeholder="ssh-ed25519 AAAA..."
                  />
                )}
              </div>
            )}
            <div className="flex justify-end gap-3">
              <button onClick={() => setShowReinstall(false)} className="px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 rounded-md">取消</button>
              <button onClick={handleReinstall} disabled={reinstalling} className="px-4 py-2 text-sm bg-black text-white rounded-md hover:bg-gray-800 disabled:opacity-50">
                {reinstalling ? '重装中...' : '确认重装'}
              </button>
            </div>
          </div>
        </Modal>
      )}

      {showExpiryEdit && (
        <Modal title="修改到期时间" onClose={() => setShowExpiryEdit(false)}>
          <div className="space-y-4">
            <p className="text-sm text-gray-500">当前：{formatExpiration(container.expires_at)}</p>
            <div>
              <label className="block text-xs text-gray-500 mb-1">新到期日期（留空为长期有效）</label>
              <input
                type="date"
                value={editExpiry}
                onChange={(e) => setEditExpiry(e.target.value)}
                min={new Date().toISOString().slice(0, 10)}
                className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm text-black bg-white"
              />
            </div>
            <div className="flex justify-end gap-3">
              <button onClick={() => setShowExpiryEdit(false)} className="px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 rounded-md">取消</button>
              <button onClick={async () => {
                setSavingExpiry(true)
                try {
                  const expVal = editExpiry ? editExpiry + ' 23:59:59' : ''
                  await updateContainerExpiry(container.id, expVal)
                  setShowExpiryEdit(false)
                  fetchContainer()
                } catch { /* ignore */ }
                finally { setSavingExpiry(false) }
              }} disabled={savingExpiry} className="px-4 py-2 text-sm bg-black text-white rounded-md hover:bg-gray-800 disabled:opacity-50">
                {savingExpiry ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </Modal>
      )}

      {/* Resource limit edit modal */}
      {showResourceEdit && (
        <Modal title="编辑资源限制" onClose={() => setShowResourceEdit(false)}>
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-xs text-gray-500 mb-1">vCPU 核数</label>
                <input type="number" min={0.25} max={maxVCPU} step={0.25} value={resourceEdit.vcpu}
                  onChange={(e) => setResourceEdit({ ...resourceEdit, vcpu: clampVCPU(Number(e.target.value), maxVCPU) })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm" />
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">内存 (MB)</label>
                <input type="number" min={128} max={maxRAMMB} step={128} value={resourceEdit.ramMb}
                  onChange={(e) => setResourceEdit({ ...resourceEdit, ramMb: clampResourceInt(Number(e.target.value), 128, maxRAMMB, 128) })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm" />
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">下行带宽 (Mbps，0=不限制)</label>
                <input type="number" min={0} value={resourceEdit.networkDownMbps}
                  onChange={(e) => setResourceEdit({ ...resourceEdit, networkDownMbps: Math.max(0, Number(e.target.value) || 0) })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm" />
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">上行带宽 (Mbps，0=不限制)</label>
                <input type="number" min={0} value={resourceEdit.networkUpMbps}
                  onChange={(e) => setResourceEdit({ ...resourceEdit, networkUpMbps: Math.max(0, Number(e.target.value) || 0) })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm" />
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">读取 IO (MB/s，0=不限制)</label>
                <input type="number" min={0} value={resourceEdit.ioReadMbps}
                  onChange={(e) => setResourceEdit({ ...resourceEdit, ioReadMbps: Math.max(0, Number(e.target.value) || 0) })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm" />
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">写入 IO (MB/s，0=不限制)</label>
                <input type="number" min={0} value={resourceEdit.ioWriteMbps}
                  onChange={(e) => setResourceEdit({ ...resourceEdit, ioWriteMbps: Math.max(0, Number(e.target.value) || 0) })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm" />
              </div>
            </div>
            <p className="text-[11px] text-gray-400">磁盘容量不支持动态修改。修改后运行中的容器会立即应用新的 cgroup 限制。</p>
            <div className="flex justify-end gap-2 pt-2">
              <button onClick={() => setShowResourceEdit(false)} className="px-4 py-2 text-sm text-gray-600 border border-gray-200 rounded-md hover:bg-gray-50">取消</button>
              <button onClick={saveResourceLimit} disabled={savingResource} className="px-4 py-2 text-sm bg-black text-white rounded-md hover:bg-gray-800 disabled:opacity-50">
                {savingResource ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  )
}

function RangeSwitch({ value, onChange }: { value: StatsRangeKey; onChange: (value: StatsRangeKey) => void }) {
  return (
    <div className="inline-flex border border-gray-200 rounded-md overflow-hidden bg-white">
      {(['30m', '1h', '1d', '1w'] as StatsRangeKey[]).map((item) => (
        <button
          key={item}
          onClick={() => onChange(item)}
          className={`px-3 py-1.5 text-xs ${value === item ? 'bg-black text-white' : 'text-gray-600 hover:bg-gray-50'}`}
        >
          {item === '30m' ? '30分钟' : item === '1h' ? '1小时' : item === '1d' ? '1天' : '1周'}
        </button>
      ))}
    </div>
  )
}

function FirewallIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 1024 1024" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
      <path d="M979.989543 469.308394H757.450516c4.519899-21.887511 7.247838-45.094992 7.247838-69.798441 0-137.428929-116.773391-270.417958-121.72528-276.001833a21.415521 21.415521 0 0 0-21.887511-6.319858 21.287524 21.287524 0 0 0-15.103663 16.98362l-12.583719 75.438315C571.854663 148.115571 533.535519 69.229333 467.241 5.910748A21.46352 21.46352 0 0 0 441.585573 2.982813a21.295524 21.295524 0 0 0-9.727782 23.935466c15.703649 58.366696-2.815937 152.996581-22.911488 226.978928-5.591875-35.7912-15.615651-66.214521-32.935264-76.414293a21.351523 21.351523 0 0 0-32.167282 18.399589c0 31.359299-15.999643 60.278653-34.519228 93.813904-24.703448 44.759-52.734822 95.525866-52.734822 167.972247 0 4.055909 0.599987 7.727827 0.767983 11.64774H41.346516A21.343523 21.343523 0 0 0 20.010993 490.651917v511.98856a21.343523 21.343523 0 0 0 21.335523 21.335524H979.989543a21.343523 21.343523 0 0 0 21.335524-21.335524v-511.98856A21.343523 21.343523 0 0 0 979.989543 469.308394z m-149.332663 42.663047v127.99714H660.380685c33.879243-29.183348 65.878528-72.702376 85.334093-127.99714h84.942102zM346.699693 310.255948c7.559831-13.599696 14.895667-26.919399 21.167527-40.399098 3.495922 28.543362 5.503877 64.510559 5.071887 100.26176a21.311524 21.311524 0 0 0 17.367612 21.199526 21.255525 21.255525 0 0 0 23.935465-13.351701c3.071931-8.191817 63.918572-169.980202 65.958527-293.241448 78.462247 104.493665 96.429845 228.906885 96.63784 230.354853a21.279525 21.279525 0 0 0 20.823535 18.431588c9.85578-0.255994 19.631561-7.383835 21.335523-17.791602L640.077138 189.29865c32.895265 46.422963 81.958169 129.277111 81.958169 210.219303 0 157.772475-113.837456 240.458627-153.212577 240.458628H455.241268c-19.023575-5.247883-155.940516-47.742933-155.940516-182.347926 0-61.486626 24.111461-105.133651 47.398941-147.372707zM659.996693 682.647627v127.99714H361.339366v-127.99714H659.996693zM190.67118 511.971441h72.750374c15.311658 60.974638 54.910773 101.717727 93.693907 127.99714H190.67118v-127.99714z m-127.99714 0H148.008133v127.99714H62.67404v-127.99714z m0 170.668186h255.99428v127.99714h-255.99428v-127.99714zM148.008133 981.296954H62.67404v-127.99714H148.008133v127.99714z m341.328373 0H190.67118v-127.99714h298.665326v127.99714z m341.320374 0H531.999553v-127.99714h298.657327v127.99714z m127.99714 0h-85.326093v-127.99714h85.326093v127.99714z m0-170.660187h-255.99428v-127.99714h255.99428v127.99714z m0-170.668186h-85.326093v-127.99714h85.326093v127.99714z" />
    </svg>
  )
}

function StatusBadge({ running, initializing }: { running: boolean; initializing?: boolean }) {
  if (initializing) {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium whitespace-nowrap bg-amber-50 text-amber-700">
        <span className="w-1.5 h-1.5 rounded-full flex-shrink-0 bg-amber-500 animate-pulse"></span>
        正在初始化
      </span>
    )
  }
  return (
    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium whitespace-nowrap ${running ? 'bg-emerald-100 text-emerald-700' : 'bg-rose-100 text-rose-700'}`}>
      <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${running ? 'bg-emerald-500' : 'bg-rose-500'}`}></span>
      {running ? '运行中' : '已停止'}
    </span>
  )
}

function InfoTag({ color, children }: { color: 'blue' | 'emerald' | 'amber' | 'violet' | 'slate' | 'red'; children: ReactNode }) {
  const classes = {
    blue: 'bg-blue-50 text-blue-700 border-blue-100',
    emerald: 'bg-emerald-50 text-emerald-700 border-emerald-100',
    amber: 'bg-amber-50 text-amber-700 border-amber-100',
    violet: 'bg-violet-50 text-violet-700 border-violet-100',
    slate: 'bg-slate-50 text-slate-700 border-slate-100',
    red: 'bg-red-50 text-red-700 border-red-100',
  }
  return <span className={`px-1.5 py-0.5 border rounded text-[11px] whitespace-nowrap ${classes[color]}`}>{children}</span>
}

function ActionButton({ children, onClick, disabled, dark = false }: { children: ReactNode; onClick: () => void; disabled?: boolean; dark?: boolean }) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex items-center gap-1 px-2.5 py-1.5 rounded-md text-xs transition-colors disabled:opacity-50 disabled:cursor-not-allowed whitespace-nowrap ${dark ? 'bg-black text-white hover:bg-gray-800' : 'border border-gray-300 text-gray-700 hover:bg-gray-50'}`}
    >
      {children}
    </button>
  )
}

function MetricChart({ icon, title, value, detail, points, percent, color }: { icon: ReactNode; title: string; value: string; detail: string; points: number[]; percent: number; color: 'blue' | 'emerald' | 'amber' | 'violet' }) {
  const colors = {
    blue: { text: 'text-blue-700', bg: 'bg-blue-50', bar: 'bg-blue-500', stroke: '#3b82f6', fill: '#dbeafe' },
    emerald: { text: 'text-emerald-700', bg: 'bg-emerald-50', bar: 'bg-emerald-500', stroke: '#10b981', fill: '#d1fae5' },
    amber: { text: 'text-amber-700', bg: 'bg-amber-50', bar: 'bg-amber-500', stroke: '#f59e0b', fill: '#fef3c7' },
    violet: { text: 'text-violet-700', bg: 'bg-violet-50', bar: 'bg-violet-500', stroke: '#8b5cf6', fill: '#ede9fe' },
  }
  const palette = colors[color]
  return (
    <div className="bg-white border border-gray-200 rounded-lg p-4">
      <div className="flex items-center justify-between">
        <div className={`w-9 h-9 rounded-md ${palette.bg} ${palette.text} flex items-center justify-center`}>{icon}</div>
        <Cpu className="w-4 h-4 text-gray-300" />
      </div>
      <div className="mt-3">
        <div className="text-xs text-gray-500">{title}</div>
        <div className="text-lg font-semibold text-black mt-0.5">{value}</div>
        <div className="text-xs text-gray-400 mt-0.5">{detail}</div>
      </div>
      <MiniChart points={points} stroke={palette.stroke} fill={palette.fill} />
      <div className="mt-3 h-2 bg-gray-100 rounded-full overflow-hidden">
        <div className={`h-full ${palette.bar}`} style={{ width: `${Math.max(3, Math.min(percent, 100))}%` }} />
      </div>
    </div>
  )
}

function MiniChart({ points, stroke, fill }: { points: number[]; stroke: string; fill: string }) {
  const width = 320
  const height = 76
  const values = points.length > 1 ? points : [0, ...points]
  const polyline = values.map((value, index) => {
    const x = values.length === 1 ? 0 : (index / (values.length - 1)) * width
    const y = height - (clamp(value) / 100) * height
    return `${x},${y}`
  }).join(' ')
  const area = `0,${height} ${polyline} ${width},${height}`

  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="w-full h-20 mt-3" preserveAspectRatio="none">
      <polygon points={area} fill={fill} opacity="0.75" />
      <polyline points={polyline} fill="none" stroke={stroke} strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function Panel({ title, children, extra }: { title: string; children: ReactNode; extra?: ReactNode }) {
  return (
    <div className="bg-white border border-gray-200 rounded-lg p-5">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-sm font-semibold text-black">{title}</h2>
        {extra}
      </div>
      <div className="space-y-3 text-sm">{children}</div>
    </div>
  )
}

function PlainRow({ label, value, mono = false, copyValue, onCopy, children }: { label: string; value: string; mono?: boolean; copyValue?: string; onCopy?: (value: string) => void; children?: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-gray-500">{label}</span>
      <div className="flex items-center gap-1 min-w-0">
        <span className={`text-black truncate ${mono ? 'font-mono text-xs' : ''}`}>{value}</span>
        {children}
        {copyValue && onCopy && (
          <button onClick={() => onCopy(copyValue)} className="p-0.5 text-gray-400 hover:text-black rounded" title="复制">
            <Copy className="w-3 h-3" />
          </button>
        )}
      </div>
    </div>
  )
}

function SnapshotTable({ snapshots, busy, onRestore, onDelete }: {
  snapshots: Snapshot[]
  busy: string
  onRestore: (snapshot: Snapshot) => void
  onDelete: (snapshot: Snapshot) => void
}) {
  if (snapshots.length === 0) {
    return <p className="rounded-lg border border-dashed border-gray-200 px-4 py-8 text-center text-sm text-gray-400">暂无快照</p>
  }

  return (
    <div className="overflow-x-auto rounded-lg border border-gray-200">
      <table className="w-full min-w-[760px] text-sm">
        <thead className="border-b border-gray-200 bg-gray-50 text-xs text-gray-500">
          <tr>
            <TableHead>快照时间</TableHead>
            <TableHead>类型</TableHead>
            <TableHead>创建者</TableHead>
            <TableHead>大小</TableHead>
            <th className="px-3 py-2 text-right text-xs font-medium text-gray-500">操作</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100">
          {snapshots.map((snapshot) => (
            <tr key={snapshot.id}>
              <td className="px-3 py-2 font-mono text-xs text-gray-800">{snapshot.created_at}</td>
              <td className="px-3 py-2">
                <span className={`rounded px-2 py-1 text-xs ${snapshot.scheduled ? 'bg-blue-50 text-blue-700' : 'bg-gray-100 text-gray-700'}`}>
                  {snapshot.scheduled ? '定时' : '手动'}
                </span>
              </td>
              <td className="px-3 py-2 text-xs text-gray-600">{snapshot.created_by || '-'}</td>
              <td className="px-3 py-2 font-mono text-xs text-gray-600">{formatBytes(snapshot.size_bytes || 0)}</td>
              <td className="px-3 py-2">
                <div className="flex justify-end gap-1.5">
                  <button
                    onClick={() => onRestore(snapshot)}
                    disabled={!!busy}
                    className="rounded border border-gray-300 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                  >
                    {busy === snapshot.id ? '处理中...' : '恢复'}
                  </button>
                  <button
                    onClick={() => onDelete(snapshot)}
                    disabled={!!busy}
                    className="rounded border border-red-200 px-2.5 py-1 text-xs text-red-600 hover:bg-red-50 disabled:opacity-50"
                  >
                    删除
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function MappingEditor({
  draft,
  setDraft,
  isSubUser,
  canAddMapping,
  saving,
  containerIdentifier,
  publicIPv4s,
  onCancel,
  onSubmit,
}: {
  draft: MappingDraft
  setDraft: (draft: MappingDraft) => void
  isSubUser: boolean
  canAddMapping: boolean
  saving: boolean
  containerIdentifier: string
  publicIPv4s: { address: string; interface?: string }[]
  onCancel: () => void
  onSubmit: () => void
}) {
  const isEditing = draft.index !== null
  const updateDraft = (patch: Partial<MappingDraft>) => setDraft({ ...draft, ...patch })
  const disabledInputClass = 'w-full px-3 py-2 border border-gray-200 rounded-md text-sm text-gray-400 bg-gray-50'

  const fillRandomPort = async () => {
    try {
      const params = draft.host_ip ? { host_ip: draft.host_ip } : undefined
      const res = await api.get<APIResponse<{ port: number }>>(`/containers/${containerIdentifier}/random-port`, { params })
      const port = res.data.data?.port || 0
      if (port > 0) updateDraft({ host_port: String(port) })
    } catch {
      // keep manual input available if random port lookup fails
    }
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label="名称">
          <input
            value={draft.description}
            disabled={isSubUser}
            onChange={(e) => updateDraft({ description: e.target.value })}
            className={isSubUser ? disabledInputClass : inputClass}
            placeholder="Web / API"
          />
        </Field>

        <Field label="协议">
          {isSubUser ? (
            <input value={draft.protocol.toUpperCase()} disabled className={disabledInputClass} />
          ) : (
            <select value={draft.protocol} onChange={(e) => updateDraft({ protocol: e.target.value })} className={inputClass}>
              <option value="all">全部 (ALL)</option>
              <option value="tcp">TCP</option>
              <option value="udp">UDP</option>
              <option value="tcp+udp">TCP+UDP</option>
              <option value="icmp">ICMP</option>
            </select>
          )}
        </Field>

        <Field label="外部端口">
          {isSubUser ? (
            <input value={draft.host_port} disabled className={disabledInputClass} />
          ) : (
            <div className="flex gap-1">
              <input
                value={draft.host_port}
                onChange={(e) => updateDraft({ host_port: e.target.value })}
                className={`${inputClass} flex-1`}
                placeholder="默认同内部"
              />
              <button
                type="button"
                onClick={fillRandomPort}
                className="rounded-md border border-gray-300 px-2 py-2 text-xs text-gray-500 hover:bg-gray-50"
                title="随机空闲端口"
              >
                随机
              </button>
            </div>
          )}
        </Field>

        <Field label="Host IPv4">
          {isSubUser ? (
            <input value={draft.host_ip || 'All IPv4'} disabled className={disabledInputClass} />
          ) : (
            <select value={draft.host_ip} onChange={(e) => updateDraft({ host_ip: e.target.value })} className={inputClass}>
              <option value="">All assigned IPv4</option>
              {publicIPv4s.map((ip) => (
                <option key={`${ip.interface}-${ip.address}`} value={ip.address}>
                  {ip.address}{ip.interface ? ` (${ip.interface})` : ''}
                </option>
              ))}
            </select>
          )}
        </Field>

        <Field label="内部端口">
          <input
            value={draft.container_port}
            onChange={(e) => updateDraft({ container_port: e.target.value })}
            className={inputClass}
            placeholder="例如 80"
          />
        </Field>
      </div>

      <div className="flex justify-end gap-2 border-t border-gray-100 pt-4">
        <button onClick={onCancel} className="px-4 py-2 text-sm text-gray-600 border border-gray-200 rounded-md hover:bg-gray-50">
          取消
        </button>
        <button
          onClick={onSubmit}
          disabled={saving || (!isEditing && !canAddMapping)}
          className="inline-flex items-center justify-center gap-1.5 px-4 py-2 text-sm bg-black text-white rounded-md hover:bg-gray-800 disabled:opacity-50"
        >
          <Save className="w-4 h-4" />
          {saving ? '保存中...' : '保存'}
        </button>
      </div>
    </div>
  )
}

function MappingTable({ mappings, publicHost, onEdit, onDelete, compact = false, isSubUser = false }: { mappings: PortMapping[]; publicHost: string; onEdit: (pm: PortMapping, index: number) => void; onDelete: (index: number) => void; compact?: boolean; isSubUser?: boolean }) {
  if (mappings.length === 0) {
    return <p className="text-sm text-gray-400">暂无端口映射</p>
  }

  return (
    <div className="overflow-x-auto border border-gray-200 rounded-lg">
      <table className="w-full min-w-[680px]">
        <thead className="bg-gray-50 border-b border-gray-200">
          <tr>
            <TableHead>名称</TableHead>
            <TableHead>协议</TableHead>
            <TableHead>Host IPv4</TableHead>
            <TableHead>外部端口</TableHead>
            <TableHead>内部端口</TableHead>
            {!compact && <th className="text-right px-3 py-2 text-xs font-medium text-gray-500">操作</th>}
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100">
          {mappings.map((pm, index) => {
            const isSSH = pm.description === 'SSH' || pm.container_port === 22 || pm.description === 'RDP' || pm.container_port === 3389
            return (
              <tr key={`${pm.host_port}-${pm.container_port}-${index}`}>
                <td className="px-3 py-2 text-sm">
                  <span className="font-medium text-gray-800">{pm.description}</span>
                  {isSSH && <span className="ml-2 px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-700 text-xs">默认</span>}
                </td>
                <td className="px-3 py-2 text-xs text-gray-500">{pm.protocol.toUpperCase()}</td>
                <td className="px-3 py-2 font-mono text-xs text-gray-800">{pm.host_ip || publicHost || 'All IPv4'}</td>
                <td className="px-3 py-2 font-mono text-xs text-gray-800">{pm.host_port}</td>
                <td className="px-3 py-2 font-mono text-xs text-gray-800">{pm.container_port}</td>
                {!compact && (
                  <td className="px-3 py-2">
                    <div className="flex justify-end gap-1">
                      <button onClick={() => onEdit(pm, index)} className="p-1.5 text-gray-500 hover:text-black hover:bg-gray-100 rounded" title="修改">
                        <Pencil className="w-3.5 h-3.5" />
                      </button>
                      {!isSubUser && (
                        <button disabled={isSSH} onClick={() => onDelete(index)} className="p-1.5 text-gray-500 hover:text-red-600 hover:bg-red-50 rounded disabled:opacity-30 disabled:cursor-not-allowed" title={isSSH ? '默认 SSH 映射不能删除' : '删除'}>
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      )}
                    </div>
                  </td>
                )}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function TableHead({ children }: { children: ReactNode }) {
  return <th className="text-left px-3 py-2 text-xs font-medium text-gray-500">{children}</th>
}

function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return (
    <label className="block">
      <span className="block text-xs font-medium text-gray-600 mb-1.5">{label}</span>
      {children}
      {hint && <span className="block text-[11px] text-gray-400 mt-1">{hint}</span>}
    </label>
  )
}

function Segmented({ value, onChange }: { value: IPAssignMode; onChange: (value: IPAssignMode) => void }) {
  const items: Array<{ value: IPAssignMode; label: string }> = [
    { value: 'clear', label: '不分配' },
    { value: 'random', label: '随机分配' },
    { value: 'custom', label: '自定义' },
  ]
  return (
    <div className="grid grid-cols-3 gap-1 rounded-md bg-gray-100 p-1">
      {items.map((item) => (
        <button
          key={item.value}
          type="button"
          onClick={() => onChange(item.value)}
          className={`rounded px-2 py-1.5 text-xs font-medium ${value === item.value ? 'bg-white text-black shadow-sm' : 'text-gray-600 hover:text-black'}`}
        >
          {item.label}
        </button>
      ))}
    </div>
  )
}

function Modal({ title, children, onClose, wide = false, extra, flush = false }: { title: string; children: ReactNode; onClose: () => void; wide?: boolean; extra?: ReactNode; flush?: boolean }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className={`bg-white rounded-lg shadow-xl border border-gray-200 w-full ${wide ? 'max-w-5xl' : 'max-w-md'} max-h-[92vh] overflow-hidden flex flex-col`}>
        <div className="flex items-center justify-between px-5 py-3 border-b border-gray-200">
          <h2 className="text-sm font-semibold text-black">{title}</h2>
          <div className="flex items-center gap-2">
            {extra}
            <button onClick={onClose} className="p-1.5 text-gray-500 hover:text-black hover:bg-gray-100 rounded" title="关闭">
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>
        <div className={flush ? "overflow-hidden" : "p-5 overflow-y-auto"}>{children}</div>
      </div>
    </div>
  )
}

function filterHistory(history: MetricPoint[], range: StatsRangeKey) {
  const cutoff = Date.now() - statsRanges[range]
  return history.filter((point) => point.ts >= cutoff)
}

function readHistory(containerName: string): MetricPoint[] {
  if (!containerName) return []
  try {
    const raw = localStorage.getItem(historyKey(containerName))
    if (!raw) return []
    const parsed = JSON.parse(raw) as MetricPoint[]
    const cutoff = Date.now() - statsRanges['1w']
    return parsed.filter((point) => point.ts >= cutoff)
  } catch {
    return []
  }
}

function normalizeContainerMetricSample(point: ContainerMetricSample): MetricPoint {
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

function splitAddressLines(value: string) {
  return value
    .split(/[\n,，\s]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function mergeIPv4Choices(candidates: PublicIPv4Info[], assigned: { address: string; interface?: string; prefix_len?: number; gateway?: string }[]) {
  const byAddress = new Map<string, PublicIPv4Info>()
  for (const item of candidates) {
    if (item.address) byAddress.set(item.address, item)
  }
  for (const item of assigned) {
    if (!item.address || byAddress.has(item.address)) continue
    byAddress.set(item.address, {
      address: item.address,
      interface: item.interface || '',
      prefix: item.prefix_len ? `${item.address}/${item.prefix_len}` : item.address,
      prefix_len: item.prefix_len,
      gateway: item.gateway,
    })
  }
  return Array.from(byAddress.values()).sort((a, b) => a.address.localeCompare(b.address, undefined, { numeric: true }))
}

function historyKey(containerName: string) {
  return `clicd_container_metric_history:${containerName}`
}

function clamp(value: number) {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(value, 100))
}

function clampVCPU(value: number, max: number) {
  const rounded = Math.round((Number.isFinite(value) ? value : 1) * 4) / 4
  return Number(Math.min(Math.max(rounded, 0.25), max).toFixed(2))
}

function clampResourceInt(value: number, min: number, max?: number, fallback = min) {
  const next = Math.round(Number.isFinite(value) ? value : fallback)
  return Math.min(Math.max(next, min), max ?? next)
}

function resourceLimitValue(value?: number, fallback?: number) {
  return Math.max(0, Number(value || fallback || 0))
}

function symmetricLimit(a: number, b: number) {
  const left = resourceLimitValue(a)
  const right = resourceLimitValue(b)
  if (left === right) return left
  if (left === 0) return right
  if (right === 0) return left
  return Math.min(left, right)
}

function directionUsagePercent(bytesPerSecond: number, limit: number, bytesPerLimitUnit: number, fallbackBytesPerSecond: number) {
  const denominator = limit > 0 ? limit * bytesPerLimitUnit : fallbackBytesPerSecond
  return denominator > 0 ? clamp((bytesPerSecond / denominator) * 100) : 0
}

function formatLimit(value: number, unit: string) {
  return value > 0 ? `${value} ${unit}` : '不限制'
}

function formatDirectionalLimit(firstLabel: string, firstValue: number, secondLabel: string, secondValue: number, unit: string) {
  return `${firstLabel} ${formatLimit(firstValue, unit)} / ${secondLabel} ${formatLimit(secondValue, unit)}`
}

function toChartPoints(history: MetricPoint[], key: keyof Omit<MetricPoint, 'ts'>): ChartPoint[] {
  return history.flatMap((point) => {
    const value = Number(point[key])
    return Number.isFinite(value) ? [{ ts: point.ts, value }] : []
  })
}

function formatPercent(value: number): string {
  return `${value.toFixed(1)}%`
}

function formatMB(bytes: number): string {
  if (bytes === 0) return '0 B'
  const mb = bytes / (1024 * 1024)
  if (mb >= 1024) return `${(mb / 1024).toFixed(2)} GB`
  return `${Math.round(mb)} MB`
}

function formatGB(bytes: number): string {
  if (bytes === 0) return '0 B'
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1024) return `${(gb / 1024).toFixed(2)} TB`
  return `${gb.toFixed(2)} GB`
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  if (bytes < 1024) return `${bytes.toFixed(1)} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

function formatCPU(usec: number): string {
  return `${(usec / 1000000).toFixed(2)}s`
}

function formatExpiration(value?: string): string {
  if (!value) return '长期有效'
  return value.length >= 10 ? value.slice(0, 10) : value
}

function formatDateTime(value?: string): string {
  if (!value) return '-'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString()
}

function formatScheduleInterval(hours: number): string {
  if (hours === 24) return '1 天'
  if (hours % 24 === 0) return `${hours / 24} 天`
  return `${hours} 小时`
}

function formatRate(bytesPerSecond: number): string {
  if (bytesPerSecond < 1024) return `${bytesPerSecond.toFixed(0)} B/s`
  if (bytesPerSecond < 1024 * 1024) return `${(bytesPerSecond / 1024).toFixed(1)} KB/s`
  return `${(bytesPerSecond / (1024 * 1024)).toFixed(1)} MB/s`
}

function TrafficBar({ container }: { container: Container }) {
  const rx = container.traffic_used_rx || 0
  const tx = container.traffic_used_tx || 0
  const total = rx + tx
  const mode = container.traffic_mode || 'total'

  const totalLimit = mode === 'in_out'
    ? (container.traffic_in_gb || 0) + (container.traffic_out_gb || 0)
    : container.monthly_traffic_gb || 0

  const rxLimit = mode === 'in_out' ? (container.traffic_in_gb || 0) * 1073741824 : 0
  const txLimit = mode === 'in_out' ? (container.traffic_out_gb || 0) * 1073741824 : 0

  const totalPct = totalLimit > 0 ? Math.min((total / (totalLimit * 1073741824)) * 100, 100) : 0
  const rxPct = rxLimit > 0 ? Math.min((rx / rxLimit) * 100, 100) : 0
  const txPct = txLimit > 0 ? Math.min((tx / txLimit) * 100, 100) : 0

  if (totalLimit === 0 && rxLimit === 0 && txLimit === 0) {
    return <div className="text-sm text-gray-400">未设置流量限制</div>
  }

  if (mode === 'in_out') {
    return (
      <div className="space-y-3">
        <div>
          <div className="flex justify-between text-xs text-gray-500 mb-1">
            <span>入站 (RX)</span>
            <span>{formatBytes(rx)} {rxLimit > 0 ? `/ ${formatBytes(rxLimit)}` : '(不限制)'}</span>
          </div>
          {rxLimit > 0 && (
            <div className="w-full bg-gray-200 rounded-full h-2.5">
              <div className="bg-black h-2.5 rounded-full transition-all" style={{ width: `${Math.min(rxPct, 100)}%` }} />
            </div>
          )}
        </div>
        <div>
          <div className="flex justify-between text-xs text-gray-500 mb-1">
            <span>出站 (TX)</span>
            <span>{formatBytes(tx)} {txLimit > 0 ? `/ ${formatBytes(txLimit)}` : '(不限制)'}</span>
          </div>
          {txLimit > 0 && (
            <div className="w-full bg-gray-200 rounded-full h-2.5">
              <div className="bg-black h-2.5 rounded-full transition-all" style={{ width: `${Math.min(txPct, 100)}%` }} />
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div>
      <div className="flex justify-between text-xs text-gray-500 mb-1">
        <span>已用</span>
        <div className="flex items-center gap-2">
          <span>{formatBytes(total)} / {totalLimit} GB</span>
          {(container.traffic_used_rx > 0 || container.traffic_used_tx > 0) && (
            <button onClick={async () => {
              try {
                await resetTraffic(container.id)
              } catch { /* ignore */ }
            }} className="text-xs text-gray-400 hover:text-black underline">重置</button>
          )}
        </div>
      </div>
      <div className="w-full bg-gray-200 rounded-full h-2.5">
        <div className="bg-black h-2.5 rounded-full transition-all" style={{ width: `${Math.min(totalPct, 100)}%` }} />
      </div>
    </div>
  )
}

function isWindowsTemplate(templateID: string) {
  return templateID.toLowerCase().includes('windows')
}

function getTemplateIcon(id: string): ReactNode {
  const size = 'w-6 h-6'
  id = id.startsWith('kvm-') ? id.slice(4) : id
  if (id.startsWith('debian')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M935.473 375.359a558.602 558.602 0 0 0-22.351-114.655l13.308 4.436c-35.66-81.385-90.086-163.623-153.556-199.282-8.701-5.118-35.147 4.948-26.616-12.113s-37.536-8.19-56.816-4.778c-26.275 4.266-30.028-29.175-75.071-35.83-25.593-3.582-32.247 18.427-44.702 13.309-23.545-9.384-20.816-27.64-57.669-9.384-18.427 9.042 11.602-26.105-49.138-4.607L457.744 0C349.23 41.63 318.69 76.266 288.15 79.337c-6.996 0-34.124 32.759-53.574 53.062-17.062 17.062-26.275 36.512-49.138 39.583l-17.062 70.636A136.494 136.494 0 0 0 119.41 339.7a66.711 66.711 0 0 1 4.436-52.892c-17.062 6.825-45.896 17.062-29.687 96.91 12.796 63.13-5.29 135.13 10.066 204.742 4.777 20.986 0 40.095 6.142 51.185 107.66 235.794 208.836 392.08 472.44 384.06l4.436-8.872c-28.152-6.825-55.11-17.062-111.584-30.711-18.597-4.436-23.033-34.124-40.265-44.19-9.384-5.46-28.323-4.095-37.195-9.896s4.266-21.668-19.962-14.332c-8.531 2.56-13.82-10.92-20.133-17.061s0-23.716-23.375-24.74-18.426-29.687-19.791-44.702c-12.114 1.536-1.195-1.535-13.308 4.436a63.64 63.64 0 0 1-23.887-31.735c-10.237-48.967-10.578-21.497-15.014-32.417a322.297 322.297 0 0 0-19.28-42.142l26.787 8.872h4.436l4.436-13.309-26.616-8.701h31.223c-7.678 13.99 2.047 5.29-13.479 8.872v13.308l22.35-8.872v-13.308c-20.644-10.237-28.663-13.308-49.137-22.01l9.043 8.872v4.436h-49.138c-22.01-14.843-13.99-31.734-17.915-53.062 17.062 0 9.213 6.655 17.062-13.137l-17.062 8.872 13.308-33.953-13.308 13.138c-29.176-38.73-16.209-97.764-11.943-152.02A180.684 180.684 0 0 1 211.2 372.97c8.872-10.067 5.119-25.251 5.46-37.195l31.223-26.445H265.8c7.678 17.061 4.777 5.46 0 22.01l8.872 4.435c7.166-8.701 6.142-5.971 8.872-22.01-10.066-10.578-6.995-9.895-26.616-13.308A119.432 119.432 0 0 1 368.51 243.13l4.436-13.308-17.915 9.043-4.436-13.138a109.536 109.536 0 0 1 76.095-27.128c6.313 0 6.996-17.062 12.797-19.45 161.574-60.57 309.33 9.383 371.093 147.413a324.173 324.173 0 0 1 8.19 34.123c17.061 56.987-7.167 121.48 9.725 155.604-7.849 36-36.683 13.82-40.266 30.881-8.531 41.29-14.844 59.717-40.778 78.826a196.38 196.38 0 0 1-30.711 22.35 84.285 84.285 0 0 0 22.35-39.753c-106.294 111.584-262.58 63.981-290.049-105.954a101.176 101.176 0 0 1 35.147-93.157c92.987-87.527 150.144-52.38 205.765-20.474l-8.872-30.711c-32.93-24.398-17.062-19.792-9.043-57.328v-4.436l-17.915-13.137c2.56 10.066 1.024 5.289 9.043 17.061-4.436 16.039 0 9.043-8.872 17.062-15.014 9.725-23.716 7.337-44.702 4.436l4.436-13.308-13.308-13.308c0 11.773-4.095 2.73 0 17.062-126.086 9.896-218.05 80.02-178.636 260.191a220.608 220.608 0 0 0 8.872 44.19l-8.872 8.702-4.436-26.446h-13.48l-4.435 13.308c-12.626-25.763-0.853 10.75 40.265 52.892a149.29 149.29 0 0 0 12.797 12.625c47.773 34.124 113.29 81.385 201.328 49.138h9.043v-4.436l-102.37-13.308-4.436-8.701c106.806 24.74 176.93-8.531 236.646-48.456 13.138-17.062 11.431-24.057 22.18-9.043 19.28-17.061 3.925-26.786 13.48-44.019 6.483-11.772 32.587-17.062 44.7-35.318l40.096-136.494h-17.062c3.071-14.332 22.522-34.123-4.436-48.455-2.559-1.536 9.043-1.365 8.872-4.266a145.537 145.537 0 0 0-22.18-66.37c33.1 21.669 36.342 68.247 53.574 105.783v8.872h4.436V375.36zM453.308 595.455l-9.555-26.446 62.446 57.328zM146.196 211.736l-23.204-4.436v39.754c16.72-10.578 18.939-10.407 22.35-35.318z m574.981 176.419a57.498 57.498 0 0 0-17.062 44.19l13.48 8.701a37.877 37.877 0 0 0 4.435-52.891zM868.42 555.872c26.275-11.602 54.598-58.01 35.83-97.081l-35.83 96.91z m-174.03-79.508c-15.697 11.773-19.791 13.308-22.35 39.754l13.307 8.872 17.915-8.872a60.228 60.228 0 0 0 4.436-48.455c-8.36 13.478-2.559 20.644-13.308 8.701z m-67.053 79.508c15.868-10.92 11.944-14.844 17.915-22.18v-4.778a292.097 292.097 0 0 1-62.446 0c-13.137-13.99-13.308-29.346-31.223-39.583 17.062 35.147 3.242 38.218 31.223 61.764a158.162 158.162 0 0 0 40.095 4.436c1.536 0-6.824-1.024 4.436 0zM207.79 520.554H194.31l-8.872 8.702c9.555 10.237 5.46 7.166 13.308-4.436L212.225 547l4.436-17.062-8.872-8.701z m17.062 57.328l4.436-8.873c-10.067-8.701 0-3.583-13.308 0l-13.309-17.061 4.436 17.061v8.873h17.062z" fill="#CE0C48"/></svg>
  if (id.startsWith('ubuntu')) return <svg className={size} viewBox="0 0 1024 1024"><circle cx="512" cy="512" r="511" fill="#DD4814"/><path d="M164.532 442.532c-37.676 0-68.2 30.524-68.2 68.2 0 37.656 30.524 68.184 68.2 68.184 37.66 0 68.184-30.528 68.184-68.184 0-37.676-30.524-68.2-68.184-68.2z m486.86 309.912c-32.612 18.84-43.8 60.52-24.96 93.116 18.82 32.616 60.5 43.796 93.116 24.96 32.612-18.82 43.796-60.5 24.96-93.12-18.82-32.592-60.524-43.772-93.116-24.956z m-338.744-241.712c0-67.384 33.472-126.92 84.684-162.968L347.48 264.268c-59.656 39.88-104.048 100.816-122.496 172.188 21.528 17.56 35.304 44.3 35.304 74.272 0 29.956-13.776 56.696-35.304 74.26C243.408 656.376 287.8 717.32 347.48 757.2l49.852-83.52c-51.212-36.028-84.684-95.56-84.684-162.948z m199.168-199.188c104.052 0 189.42 79.776 198.38 181.52l97.16-1.432c-4.776-75.112-37.592-142.544-88.008-192.128-25.928 9.796-55.88 8.296-81.76-6.624-25.932-14.964-42.192-40.208-46.636-67.608a297.04 297.04 0 0 0-79.14-10.76 295.148 295.148 0 0 0-131.276 30.652l47.38 84.908a198.384 198.384 0 0 1 83.9-18.528z m0 398.36a198.404 198.404 0 0 1-83.896-18.528l-47.38 84.9a294.848 294.848 0 0 0 131.28 30.684 296.16 296.16 0 0 0 79.136-10.788c4.444-27.4 20.708-52.62 46.632-67.608 25.904-14.948 55.836-16.42 81.76-6.624 50.42-49.584 83.232-117.016 88.016-192.128l-97.188-1.432c-8.94 101.772-94.304 181.52-198.36 181.52z m139.552-440.924c32.616 18.832 74.3 7.68 93.116-24.936 18.84-32.616 7.68-74.3-24.936-93.14-32.616-18.816-74.296-7.64-93.14 24.976-18.812 32.6-7.632 74.28 24.96 93.1z" fill="#FFF"/></svg>
  if (id.startsWith('alpine')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M255.914667 68.565333L0 512l255.914667 443.434667h512.170666L1024 512 768.085333 68.565333H255.914667zM425.173333 303.786667L540.16 422.4l68.181333 68.053333 0.085334-0.085333 102.826666 100.821333c-8.533333 5.973333-16.469333 10.752-24.021333 14.677334a160.256 160.256 0 0 1-21.162667 9.258666 115.285333 115.285333 0 0 1-18.133333 4.736c-5.589333 0.981333-10.666667 1.450667-15.274667 1.450667-5.546667 0-10.325333-0.597333-14.421333-1.450667a56.192 56.192 0 0 1-10.24-3.072 40.533333 40.533333 0 0 1-8.533333-4.821333l-45.312-46.592-129.664-129.749333-46.933334 44.928-130.986666 131.072a41.557333 41.557333 0 0 1-8.533334 4.736 54.357333 54.357333 0 0 1-10.112 3.114666 70.826667 70.826667 0 0 1-14.421333 1.408c-4.608 0-9.685333-0.384-15.274667-1.322666a115.2 115.2 0 0 1-18.133333-4.864 159.914667 159.914667 0 0 1-21.162667-9.258667 223.061333 223.061333 0 0 1-24.021333-14.634667L425.173333 303.786667z m201.386667 33.493333l195.370667 196.181333 58.965333 57.728a223.573333 223.573333 0 0 1-24.064 14.677334 159.146667 159.146667 0 0 1-21.077333 9.258666 115.072 115.072 0 0 1-18.176 4.736c-5.546667 0.981333-10.709333 1.450667-15.36 1.450667-5.504 0-10.282667-0.597333-14.378667-1.450667a54.826667 54.826667 0 0 1-16.426667-6.229333 10.197333 10.197333 0 0 1-2.261333-1.706667l-52.565333-51.968-90.069334-90.069333-14.250666 14.250667L545.706667 418.133333l80.896-80.938666z m-254.549333 175.786667v107.904a90.794667 90.794667 0 0 1-15.189334-1.493334 117.973333 117.973333 0 0 1-18.005333-4.949333 158.208 158.208 0 0 1-20.821333-9.130667 222.592 222.592 0 0 1-23.68-14.506666l77.653333-77.866667z" fill="#0D597F"/></svg>
  if (id.startsWith('centos')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M153.650377 358.349623v112.005247h-3.694326v-108.310921l3.694326-3.694326z" fill="#932279"/><path d="M453.058708 512l-29.554608 29.554608H137.86553v108.310922L0 512l137.86553-137.86553v108.310922h285.63857l29.554608 29.554608zM738.529354 226.529354L553.64513 411.413578V149.956051h108.310921l3.694326 3.694326 72.878977 72.878977z" fill="#932279"/><path d="M649.86553 137.86553h-108.310922v285.63857l-29.554608 29.554608-29.554608-29.554608V137.86553h-108.310922L512 0l137.86553 137.86553zM874.043949 553.64513v108.310921l-3.694326 3.694326-72.878977 72.878977-184.884224-184.884224h261.457527z" fill="#EFA724"/><path d="M886.13447 361.036405v13.098065l-6.04526-6.04526-6.045261-6.045261v108.310921h-3.694326v-125.103312l3.694326 3.694326 6.045261 6.045261 6.04526 6.04526z" fill="#262577"/><path d="M886.13447 649.86553v-108.310922H600.4959L570.941292 512l29.554608-29.554608h285.63857v-108.310922l137.86553 137.86553-137.86553 137.86553z" fill="#262577"/><path d="M411.413578 470.35487H149.956051v-108.310921l3.694326-3.694326L226.529354 285.470646 411.413578 470.35487zM470.35487 149.956051V411.413578L285.470646 226.529354l72.878977-72.878977 3.694326-3.694326h108.310921z" fill="#9CCD2A"/><path d="M738.529354 797.470646L553.64513 612.586422v261.457527h108.310921l3.694326-3.694326 72.878977-72.878977z" fill="#EFA724"/><path d="M649.86553 886.13447h-108.310922V600.4959L512 570.941292l-29.554608 29.554608v285.63857h-108.310922l137.86553 137.86553 137.86553-137.86553z" fill="#9CCD2A"/><path d="M470.35487 874.043949V612.586422L285.470646 797.470646l72.878977 72.878977 3.694326 3.694326h108.310921z" fill="#262577"/><path d="M470.35487 428.541817v41.813053h-41.813053L226.529354 268.342407l-76.573303 76.573303V149.956051h194.959659l-76.573303 76.573303 202.012463 202.012463z" fill="#9CCD2A"/><path d="M880.08921 143.91079v224.17842l-6.045261-6.045261v108.310921H612.586422L797.470646 285.470646l72.878977 72.878977v-13.098065l3.694326 3.694326v-4.030174l-76.573303-76.573303-202.012463 202.012463h-41.813053v-41.813053L755.657593 226.529354l-82.618564-82.618564h207.050181z" fill="#932279"/><path d="M666.993768 137.86553l12.090522 12.090521h194.959659v212.087898l6.045261 6.045261 6.04526 6.04526V137.86553z" fill="#FFF"/><path d="M874.043949 679.08429v194.959659H679.08429L755.657593 797.470646 553.64513 595.458183v-41.813053h41.813053L797.470646 755.657593l76.573303-76.573303z" fill="#EFA724"/><path d="M411.413578 553.64513L226.529354 738.529354l-72.878977-72.878977-3.694326-3.694326v-108.310921H411.413578z" fill="#262577"/><path d="M470.35487 595.458183L268.342407 797.470646l76.573303 76.573303H149.956051V679.08429L226.529354 755.657593l202.012463-202.012463h41.813053v41.813053z" fill="#262577"/><path d="M874.043949 344.91571v4.030174l-3.694326-3.694326v13.098065l3.694326 3.694326 6.045261 6.045261 6.04526 6.04526v-16.792391z" fill="#FFF"/></svg>
  if (id.startsWith('archlinux')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M504.149333 7.850667c-44.373333 108.544-70.997333 179.2-120.149333 284.330666 30.037333 32.085333 67.242667 69.290667 127.317333 111.274667-64.512-26.624-108.544-53.248-141.653333-80.896-63.146667 131.413333-161.792 318.464-361.813333 678.229333 157.696-90.794667 279.552-146.773333 393.216-168.277333-4.778667-21.162667-7.509333-43.690667-7.509334-67.584l0.341334-5.12c2.389333-100.693333 54.954667-178.517333 117.077333-173.056s110.592 91.477333 107.861333 192.170667c-0.341333 18.090667-2.389333 36.522667-6.485333 54.272 112.64 21.845333 233.130667 77.824 388.437333 167.594666l-83.968-155.648c-40.96-31.744-83.968-73.386667-171.349333-118.101333 60.074667 15.701333 103.082667 33.792 136.533333 53.930667-265.557333-493.909333-287.061333-559.786667-377.856-773.12z" fill="#1793D1"/></svg>
  if (id.startsWith('fedora')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M512 0C229.344 0 0.224 229.024 0 511.648V907.84a116.384 116.384 0 0 0 116.384 116.128h395.808c282.656-0.128 511.776-229.28 511.776-512 0-282.752-229.248-512-512-512z m196.064 237.952c-16.16 0-22.016-3.104-45.728-3.104a126.848 126.848 0 0 0-126.848 126.624v110.208c0 9.888 8.032 17.92 17.92 17.92h83.328c31.072 0 56.16 24.736 56.16 55.904 0 31.328-25.344 55.968-56.736 55.968h-100.608v127.36a240.32 240.32 0 0 1-240.288 240.288h-1.248a190.944 190.944 0 0 1-53.216-7.52l1.344 0.32c-27.168-7.072-49.376-29.408-49.376-55.296 0-31.328 22.752-54.112 56.736-54.112 16.128 0 22.016 3.072 45.696 3.072a126.848 126.848 0 0 0 126.848-126.624v-110.208a17.92 17.92 0 0 0-17.92-17.888h-83.328a55.808 55.808 0 0 1-56.096-55.904c0-31.328 25.344-55.968 56.736-55.968h100.576v-127.36a240.32 240.32 0 0 1 240.288-240.288c20.128 0 34.432 2.272 53.088 7.136 27.168 7.136 49.408 29.44 49.408 55.296 0 31.36-22.752 54.144-56.736 54.144z" fill="#294172"/></svg>
  if (id.startsWith('nixos')) return <svg className={size} viewBox="0 0 60 60"><g fillRule="evenodd"><path d="M23.58 20.214L8.964 45.528 5.55 39.743l3.94-6.78-7.823-.02L0 30.052l1.703-2.956 11.135.035 4.002-6.9zM24.7 40.45h29.23l-3.302 5.85-7.84-.022 3.894 6.785-1.67 2.9-3.412.004-5.537-9.66-7.976-.016zm17.014-11.092L27.1 4.043l6.716-.063 3.902 6.8 3.93-6.765 3.337.002 1.7 2.953-5.598 9.626 3.974 6.916z" fill="#7ebae4"/><path d="M35.28 19.486l-29.23-.002 3.303-5.848 7.84.022L13.3 6.873l1.67-2.9 3.412-.004 5.537 9.66 7.976.016zm1.14 20.294l14.616-25.313 3.413 5.785-3.94 6.78 7.823.02 1.668 2.9-1.703 2.956-11.135-.035-4.002 6.9z" fill="#5277c3"/></g><defs><path id="B" d="M18.305 30.642L32.92 55.956l-6.716.063-3.902-6.8-3.93 6.765-3.337-.002-1.71-2.953 5.598-9.626-3.974-6.916z"/></defs></svg>
  if (id.startsWith('kali')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M545.194667 253.568s-84.053333-5.546667-227.285334 39.253333c-145.92 45.653333-228.693333 110.378667-228.693333 110.378667s217.514667-121.472 463.018667-128.341333z m313.642666 132.053333l10.965334-0.725333s-62.634667-75.946667-182.528-112.981333c67.413333 27.392 126.037333 63.701333 171.562666 113.706666z m17.92 31.573334c1.664-2.901333 7.082667 9.258667 11.221334 14.378666 0.170667 1.024 0.426667 1.664-1.92 1.152-0.213333-1.066667-0.554667-1.365333-0.554667-1.365333s-5.76-3.413333-7.552-5.845333c-1.749333-2.432-2.090667-6.698667-1.194667-8.32z m147.114667 361.770666s13.312-152.661333-226.56-187.861333a779.818667 779.818667 0 0 0-107.690667-7.978667c-192.256 2.56-199.253333-221.738667-54.4-233.045333 60.032-4.949333 131.712 27.434667 201.813334 60.074667-0.298667 8.704 0.085333 16.426667 5.802666 23.552 5.717333 7.168 27.648 14.933333 34.688 18.986666 6.997333 4.010667 29.482667 18.346667 43.264 36.266667 2.986667-5.589333 27.904-21.845333 27.904-21.845333s-5.973333 0.128-19.84-5.077334c-13.909333-5.205333-30.421333-20.906667-30.805333-21.802666-0.426667-0.938667-0.64-2.346667 2.56-2.986667 2.517333-2.090667-3.072-8.832-5.546667-11.306667-2.474667-2.474667-18.986667-30.549333-19.370666-31.146666-0.384-0.682667-0.512-1.322667-1.706667-2.133334-3.626667-1.152-19.626667 1.706667-19.626667 1.706667s-24.533333-12.074667-33.024-38.101333c0.128 4.565333-4.224 9.557333 0 20.010666-12.8-5.418667-23.808-14.677333-32.512-37.546666-5.12 13.013333 0 21.290667 0 21.290666s-30.165333-8.448-34.986666-36.266666c-5.290667 12.501333 0 20.010667 0 20.010666s-49.194667-25.685333-130.944-26.026666c-54.741333-5.034667-66.133333-101.290667-61.013334-117.504 0 0-78.933333-41.6-234.368-59.989334-155.392-18.346667-282.794667-2.773333-282.794666-2.773333s275.2-13.226667 495.658666 76.074667c7.509333 33.493333 30.037333 89.344 42.197334 116.181333-34.773333 24.021333-73.941333 46.592-80.042667 126.72-6.101333 80.128 62.805333 150.613333 148.224 152.746667 81.066667 4.352 137.130667 4.949333 205.056 40.192 64.853333 35.84 118.016 145.066667 123.306667 243.328 5.632-72.917333-21.717333-229.674667-149.333334-277.248 178.389333 31.232 194.090667 163.498667 194.090667 163.498666zM541.013333 241.621333l-6.4-20.693333s-105.984-18.816-248.405333-8.704C143.786667 222.336 0 272.213333 0 272.213333s294.229333-74.026667 541.013333-30.592z" fill="#557C94"/></svg>
  if (id.startsWith('rockylinux')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M995.498667 680.362667c18.474667-52.778667 28.501333-109.568 28.501333-168.704C1024 229.077333 794.752 0 512 0S0 229.077333 0 511.658667c0 139.818667 56.106667 266.496 147.114667 358.826666L666.453333 351.530667l128.213334 128.170666 200.832 200.704z m-93.525334 162.816l-235.52-235.349334-368.896 368.597334A510.506667 510.506667 0 0 0 512 1023.274667c156.16 0 296.106667-69.888 389.973333-180.053334h0.042667z" fill="#10B981"/></svg>
  if (id.startsWith('windows')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M56.888889 227.555556l398.222222-70.542223V512H56.888889V227.555556z m0 625.777777l398.222222 70.542223V568.888889H56.888889v284.444444zM512 147.342222L1024 56.888889v455.111111H512V147.342222z m0 786.204445L1024 1024v-455.111111H512v364.657778z" fill="#16C6FE"/></svg>
  return null
}
