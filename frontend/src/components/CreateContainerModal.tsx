import { Alert } from './ui/alert'
import { Modal } from './ui/modal'
import { RadioGroup, RadioGroupItem } from './ui/radio-group'
import { Badge } from './ui/badge'
import { Card } from './ui/card'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { NativeSelect } from './ui/native-select'
import { Label } from './ui/label'
import { Checkbox } from './ui/checkbox'
import { Textarea } from './ui/textarea'
import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react'
import { ArrowLeft, ArrowRight, CalendarClock, Check, Plus, RefreshCw, Trash2, X } from 'lucide-react'
import { useNavigate } from 'react-router'
import { batchCreate, getIPv6Status, getIPv6NATSettings, getEnabledImages, getHostInfo, getHostReport, getRoutingInfo, getStorageInfo, CreateContainerRequest, HostInfo, HostProbeReport, IPv6Status, IPv6NATStatus, PortMapping, RoutingInfo, StorageInfo, Template } from '../services/api'
import { useDialog } from './Dialog'
import { useLanguage, type Language } from '../contexts/LanguageContext'
import { generateSSHPassword, sshPasswordError, sshPublicKeyError, type SSHAuthMode } from '../utils/sshAuth'

interface CreateContainerModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: (containers: CreateContainerRequest[]) => void | Promise<void>
  existingNames?: string[]
}

const defaultForm: CreateContainerRequest = {
  name: '',
  virtualization: 'lxc',
  template_id: '',
  storage_pool_id: '',
  vcpu: 1,
  cpu_percent: 100,
  ram_mb: 512,
  disk_gb: 10,
  network_bw_mbps: 0,
  network_down_mbps: 0,
  network_up_mbps: 0,
  monthly_traffic_gb: 0,
  traffic_mode: 'total',
  traffic_in_gb: 0,
  traffic_out_gb: 0,
  io_speed_mbps: 0,
  io_read_mbps: 0,
  io_write_mbps: 0,
  extra_ports: [],
  nat_port_mappings: [],
  management_port: 0,
  port_mapping_count: 2,
  ipv6_port_mapping_count: 0,
  ipv6_nat_sync_ipv4: true,
  assign_nat: true,
  lan_ipv4_mode: '',
  lan_interface: '',
  lan_ipv4_address: '',
  lan_ipv4_prefix_len: 24,
  lan_ipv4_gateway: '',
  snapshot_limit: 1,
  assign_ipv4: false,
  ipv4_count: 1,
  public_ipv4s: [],
  assign_ipv6: false,
  ipv6_count: 1,
  ipv6_addresses: [],
  ssh_auth_mode: 'auto_password',
  ssh_password: '',
  ssh_public_key: '',
  allowed_image_ids: [],
  image_limit_configured: false,
  expires_at: '',
}

export default function CreateContainerModal({ isOpen, onClose, onSuccess, existingNames = [] }: CreateContainerModalProps) {
  const navigate = useNavigate()
  const dialog = useDialog()
  const { language, t } = useLanguage()
  const networkText = createNetworkText[language]
  const wizardSteps = [t('基础信息'), t('镜像选择'), t('网络配置'), t('预览清单')]
  const [currentStep, setCurrentStep] = useState(0)
  const formBodyRef = useRef<HTMLDivElement>(null)
  const [templates, setTemplates] = useState<Template[]>([])
  const [loading, setLoading] = useState(false)
  const [batchCount, setBatchCount] = useState(1)
  const [form, setForm] = useState<CreateContainerRequest>(defaultForm)
  const [hostInfo, setHostInfo] = useState<HostInfo | null>(null)
  const [hostReport, setHostReport] = useState<HostProbeReport | null>(null)
  const [storageInfo, setStorageInfo] = useState<StorageInfo | null>(null)
  const [storageLoading, setStorageLoading] = useState(true)
  const [routingInfo, setRoutingInfo] = useState<RoutingInfo | null>(null)
  const [ipv6Status, setIPv6Status] = useState<IPv6Status | null>(null)
  const [ipv6NATStatus, setIPv6NATStatus] = useState<IPv6NATStatus | null>(null)
  const [nameError, setNameError] = useState('')

  useEffect(() => {
    if (isOpen) setCurrentStep(0)
  }, [isOpen])

  useEffect(() => {
    if (formBodyRef.current) formBodyRef.current.scrollTop = 0
  }, [currentStep, isOpen])

  useEffect(() => {
    if (!isOpen) return

    getEnabledImages(form.virtualization)
      .then((res) => {
        const data = res.data.data || []
        setTemplates(data)
        setForm((prev) => {
          const templateID = data.some((item) => item.id === prev.template_id) ? prev.template_id : (data[0]?.id || '')
          const allowed = new Set(data.map((item) => item.id))
          const selectedAllowedIDs = (prev.allowed_image_ids || []).filter((id) => allowed.has(id))
          return applyTemplateDefaults({
            ...prev,
            template_id: templateID,
            allowed_image_ids: prev.image_limit_configured ? selectedAllowedIDs : (templateID ? [templateID] : []),
            image_limit_configured: true,
          }, data.find((item) => item.id === templateID))
        })
      })
      .catch(console.error)

    getIPv6NATSettings().then(res => setIPv6NATStatus(res.data.data || null)).catch(() => setIPv6NATStatus(null))
    getIPv6Status()
      .then((res) => {
        const status = res.data.data || null
        setIPv6Status(status)
        if (!status?.available) {
          setForm((prev) => ({ ...prev, assign_ipv6: false }))
        }
      })
      .catch(() => {
        setIPv6Status({ available: false, reachable: false, reason: 'IPv6 status check failed', prefixes: [] })
        setForm((prev) => ({ ...prev, assign_ipv6: false }))
      })

    getHostInfo()
      .then((res) => setHostInfo(res.data.data || null))
      .catch(() => setHostInfo(null))

    getHostReport()
      .then((res) => setHostReport(res.data.data || null))
      .catch(() => setHostReport(null))

  }, [isOpen, form.virtualization])

  useEffect(() => {
    if (templates.length > 0 && (!form.template_id || !templates.some((item) => item.id === form.template_id))) {
      const defaultID = templates[0].id
      setForm((prev) => {
        if (prev.template_id && templates.some((item) => item.id === prev.template_id)) {
          return prev
        }
        const allowed = new Set(prev.allowed_image_ids || [])
        allowed.add(defaultID)
        return applyTemplateDefaults({
          ...prev,
          template_id: defaultID,
          allowed_image_ids: prev.image_limit_configured ? prev.allowed_image_ids : [defaultID],
          image_limit_configured: prev.image_limit_configured,
        }, templates.find((item) => item.id === defaultID))
      })
    }
  }, [templates, form.template_id])

  useEffect(() => {
    if (!isOpen) return
    let active = true
    setStorageLoading(true)
    getStorageInfo()
      .then((res) => {
        if (active) setStorageInfo(res.data.data || null)
      })
      .catch(() => {
        if (active) setStorageInfo(null)
      })
      .finally(() => {
        if (active) setStorageLoading(false)
      })
    return () => { active = false }
  }, [isOpen])

  useEffect(() => {
    if (!isOpen) return
    let active = true
    getRoutingInfo()
      .then((res) => {
        if (active) setRoutingInfo(res.data.data || null)
      })
      .catch(() => {
        if (active) setRoutingInfo(null)
      })
    return () => { active = false }
  }, [isOpen])

  const ipv6Available = !!ipv6Status?.available
  const ipv6NATAvailable = form.virtualization === 'lxc' && !form.lan_ipv4_mode && !!ipv6NATStatus?.settings.enabled && !!ipv6NATStatus.available
  const ipv6Prefixes = ipv6Status?.prefixes || []
  const ipv6Prefix = ipv6Prefixes.length > 1 ? `${ipv6Prefixes.length} prefixes configured` : (ipv6Prefixes[0]?.prefix || '')
  const publicIPv4s = hostInfo?.network.public_ipv4_addresses || []
  const ipv4Available = publicIPv4s.length > 0
  const manualIPv4s = form.public_ipv4s || []
  const selectedTemplate = templates.find((template) => template.id === form.template_id)
  const manualISO = form.virtualization === 'kvm' && isManualISOTemplate(selectedTemplate)
  const manualDisk = selectedTemplate?.provisioner === 'manual-disk'
  const manualMessage = manualDisk ? '预装磁盘保留原系统账号和配置，请通过 VNC 检查网络，不会注入新密码。' : '此 ISO 将通过 VNC 手动安装系统，磁盘分区、账号和网络需要自行设置，不会注入 SSH 密码或云初始化。'
  const maxVCPU = hostInfo?.cpu.cores || 64
  const maxRAMMB = hostInfo?.ram.total_mb ? Number(hostInfo.ram.total_mb) : undefined
  const kvmAvailable = !!hostInfo?.runtime?.kvm_available
  const storagePools = useMemo(() => {
    const content = form.virtualization === 'kvm' ? 'kvm' : 'lxc'
    return (storageInfo?.pools || []).filter((pool) => pool.enabled && pool.available !== false && (pool.content_types || []).includes(content))
  }, [storageInfo, form.virtualization])
  const storageReady = storagePools.length > 0

  useEffect(() => {
    if (hostInfo && !kvmAvailable && form.virtualization === 'kvm') {
      setForm((prev) => applyTemplateDefaults({ ...prev, virtualization: 'lxc', template_id: '' }))
    }
  }, [hostInfo, kvmAvailable, form.virtualization])
  const maxDiskGB = hostInfo?.disk.total_gb ? Math.max(1, Math.floor(hostInfo.disk.total_gb)) : undefined
  const resourceErrors = validateResourceInputs(form, maxVCPU, maxRAMMB, maxDiskGB, manualISO, selectedTemplate?.min_disk_gb)
  const lanIPv4Enabled = form.lan_ipv4_mode === 'dhcp' || form.lan_ipv4_mode === 'static'
  const lanStaticEnabled = form.lan_ipv4_mode === 'static'
  const natEnabled = form.assign_nat !== false && !lanIPv4Enabled
  const lanInterfaces = useMemo(() => getLANDHCPInterfaces(hostReport), [hostReport])
  const defaultLANInterface = lanInterfaces[0]?.name || ''
  const customNATMappings = form.nat_port_mappings || []
  const natPortCount = natEnabled
    ? (customNATMappings.length > 0 ? customNATMappings.length + 1 : Math.max(2, form.port_mapping_count || 2))
    : 0
  const linuxTemplate = !manualISO && !isWindowsTemplate(form.template_id)
  const sshAuthMode = (form.ssh_auth_mode || 'auto_password') as SSHAuthMode
  const managementPort = Math.round(Number(form.management_port) || 0)
  const natAllocationPreview = useMemo(
    () => previewNATAllocation(
      routingInfo,
      customNATMappings,
      managementPort,
      natEnabled ? natPortCount - 1 : 0,
      isWindowsTemplate(form.template_id) ? 3389 : 22
    ),
    [routingInfo, customNATMappings, managementPort, natEnabled, natPortCount, form.template_id]
  )
  const autoPortMappings = natAllocationPreview.autoMappings
  const natPreviewMappings = customNATMappings.length > 0 ? customNATMappings : autoPortMappings
  const sshPortPreview = managementPort || natAllocationPreview.managementPort
  const selectedStoragePool = storagePools.find((pool) => pool.id === form.storage_pool_id)
  const selectedAllowedImages = templates.filter((template) => (form.allowed_image_ids || []).includes(template.id))
  const networkSummary = form.assign_ipv4
    ? (manualIPv4s.length > 0
      ? `${networkText.publicIPv4}: ${manualIPv4s.join(', ')}`
      : `${networkText.publicIPv4}: ${t('自动分配')} × ${form.ipv4_count || 1}`)
    : lanIPv4Enabled
      ? `${t('局域网')}: ${lanStaticEnabled ? `${form.lan_ipv4_address}/${form.lan_ipv4_prefix_len}` : 'DHCP'}`
      : natEnabled
        ? `${networkText.publicNAT}: ${natPortCount} ${t('个端口')}`
        : form.assign_ipv6
          ? `${networkText.publicIPv6}: ${form.ipv6_count || 1}`
          : t('未配置网络')

  // Find next available batch index to avoid name conflicts
  const batchStartIndex = useMemo(() => {
    if (batchCount <= 1 || !form.name) return 1
    const prefix = `${form.name}-`
    let maxIdx = 0
    for (const existing of existingNames) {
      if (existing.startsWith(prefix)) {
        const suffix = existing.slice(prefix.length)
        const idx = parseInt(suffix, 10)
        if (!isNaN(idx) && idx > maxIdx) {
          maxIdx = idx
        }
      }
    }
    return maxIdx + 1
  }, [form.name, batchCount, existingNames])

  const handleNameChange = (value: string) => {
    setForm({ ...form, name: value })
    if (/\s/.test(value)) {
      setNameError('容器名称不能包含空格')
    } else if (value && existingNames.includes(value) && batchCount === 1) {
      setNameError('该容器名称已存在')
    } else {
      setNameError('')
    }
  }

  const validateStep = (step: number) => {
    if (step === 0) {
      if (!form.name.trim() || nameError) {
        dialog.alert(t('基础信息有误'), t('请填写有效且未被占用的容器名称'))
        return false
      }
      if (Object.keys(resourceErrors).length > 0) {
        dialog.alert(t('资源配置有误'), t('请按红色提示修改 vCPU、内存或磁盘配置'))
        return false
      }
      if (!storageReady) {
        dialog.alert(t('未配置存储'), `${t('请先在存储管理中为')} ${form.virtualization === 'kvm' ? t('KVM 磁盘') : t('LXC 容器')} ${t('开启至少一块存储磁盘')}`)
        return false
      }
      const authError = validateSSHAuthInputs(form, selectedTemplate)
      if (authError) {
        dialog.alert(t('登录方式有误'), authError)
        return false
      }
    }

    if (step === 1 && !form.template_id) {
      dialog.alert(t('请选择镜像'), t('请选择用于创建容器的系统镜像'))
      return false
    }

    if (step === 2) {
      if (!form.assign_ipv4 && !form.assign_ipv6 && !ipv6NATAvailable && form.assign_nat === false && form.lan_ipv4_mode !== 'dhcp' && form.lan_ipv4_mode !== 'static') {
        dialog.alert(t('网络配置有误'), t('请至少启用一种网络连接方式'))
        return false
      }
      if (form.lan_ipv4_mode === 'static' && (!isIPv4Address(form.lan_ipv4_address || '') || !isIPv4Address(form.lan_ipv4_gateway || '') || !form.lan_ipv4_prefix_len)) {
        dialog.alert(t('局域网 IPv4 配置有误'), t('请填写有效的 IPv4 地址、子网掩码和网关'))
        return false
      }
      const natMappingError = natEnabled
        ? validateBatchNATPortMappings(customNATMappings, managementPort, batchCount, natPortCount)
        : ''
      if (natMappingError) {
        dialog.alert(t('NAT 端口配置有误'), natMappingError)
        return false
      }
    }
    return true
  }

  const handleNextStep = () => {
    if (!validateStep(currentStep)) return
    setCurrentStep((step) => Math.min(wizardSteps.length - 1, step + 1))
  }

  const handleSubmit = async () => {
    if (!form.name || !form.template_id) {
      dialog.alert('提示', '请填写容器名称并选择系统模板')
      return
    }

    if (Object.keys(resourceErrors).length > 0) {
      dialog.alert('资源配置有误', '请按红色提示修改 vCPU、内存或磁盘配置')
      return
    }

    if (!form.assign_ipv4 && !form.assign_ipv6 && !ipv6NATAvailable && form.assign_nat === false && form.lan_ipv4_mode !== 'dhcp' && form.lan_ipv4_mode !== 'static') {
      dialog.alert('提示', '请勾选任意一个可用网络')
      return
    }

    if (form.lan_ipv4_mode === 'static') {
      if (!isIPv4Address(form.lan_ipv4_address || '') || !isIPv4Address(form.lan_ipv4_gateway || '') || !form.lan_ipv4_prefix_len) {
        dialog.alert('局域网 IPv4 配置有误', '请填写有效的 IPv4 地址、子网掩码和网关')
        return
      }
    }

    if (!storageReady) {
      dialog.alert('未配置存储', `请先在存储管理中为 ${form.virtualization === 'kvm' ? 'KVM 磁盘' : 'LXC 容器'}开启至少一块存储磁盘`)
      return
    }

    const natMappingError = natEnabled
      ? validateBatchNATPortMappings(customNATMappings, managementPort, batchCount, natPortCount)
      : ''
    if (natMappingError) {
      dialog.alert('NAT 端口配置有误', natMappingError)
      return
    }

    const authError = validateSSHAuthInputs(form, selectedTemplate)
    if (authError) {
      dialog.alert('登录方式有误', authError)
      return
    }

    const boundedForm = normalizeCreateForm(form, selectedTemplate)
    const wantsNAT = boundedForm.assign_nat !== false

    // Build batch of containers
    const containers: CreateContainerRequest[] = []
    const startIndex = batchStartIndex
    for (let i = 0; i < batchCount; i++) {
      const name = batchCount > 1 ? `${boundedForm.name}-${startIndex + i}` : boundedForm.name
      const expandedNAT = wantsNAT
        ? expandBatchNATConfig(boundedForm.nat_port_mappings || [], boundedForm.management_port || 0, i, boundedForm.port_mapping_count)
        : { mappings: [], managementPort: 0 }
      const natPortMappings = expandedNAT.mappings
      containers.push({
        ...boundedForm,
        name,
        assign_nat: wantsNAT,
        port_mapping_count: wantsNAT
          ? (natPortMappings.length > 0 ? natPortMappings.length + 1 : Math.max(2, boundedForm.port_mapping_count || 2))
          : 0,
        snapshot_limit: Math.max(1, boundedForm.snapshot_limit || 3),
        ipv4_count: boundedForm.assign_ipv4 ? Math.max(1, boundedForm.ipv4_count || 1) : 0,
        ipv6_count: boundedForm.assign_ipv6 ? Math.max(1, boundedForm.ipv6_count || 1) : 0,
        extra_ports: [],
        nat_port_mappings: natPortMappings,
        management_port: expandedNAT.managementPort,
      })
    }

    setLoading(true)
    try {
      await batchCreate(containers)
      await onSuccess(containers)
      onClose()
      setBatchCount(1)
      setForm({ ...defaultForm, template_id: templates[0]?.id || '', allowed_image_ids: templates[0]?.id ? [templates[0].id] : [], image_limit_configured: true })
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('创建失败', error.response?.data?.message || '请稍后重试')
    } finally {
      setLoading(false)
    }
  }

  if (!isOpen) return null

  return (
    <Modal dismissible={!loading} open={isOpen} onClose={onClose} title={t('创建新容器')} size="5xl" flush className="h-[760px] max-h-[92dvh]" bodyClassName="flex min-h-0 flex-col overflow-hidden">
      <nav aria-label={t('创建步骤')} className="shrink-0 border-b border-border bg-muted/20 px-4 py-2.5 sm:px-5">
        <ol className="grid grid-cols-4 gap-1 sm:gap-2">
          {wizardSteps.map((label, index) => {
            const completed = index < currentStep
            const active = index === currentStep
            return (
              <li key={label} className="min-w-0">
                <Button variant={active ? 'secondary' : 'ghost'}
                  type="button"
                  disabled={loading || index > currentStep}
                  onClick={() => setCurrentStep(index)}
                  aria-label={label}
                  aria-current={active ? 'step' : undefined}
                  className="h-auto w-full min-w-0 flex-col gap-1 px-1 py-1.5 text-center sm:flex-row sm:justify-start sm:gap-2 sm:px-2 sm:text-left"
                >
                  <Badge variant={active || completed ? 'default' : 'outline'} className="h-5 w-5 shrink-0 justify-center p-0 text-xs sm:h-6 sm:w-6">
                    {completed ? <Check className="h-3.5 w-3.5" /> : index + 1}
                  </Badge>
                  <span className="hidden min-w-0 truncate text-sm font-medium sm:block">{label}</span>
                  <span className="min-w-0 truncate text-xs font-medium sm:hidden">{[t('基础'), t('镜像'), t('网络'), t('预览')][index]}</span>
                </Button>
              </li>
            )
          })}
        </ol>
      </nav>
      <div ref={formBodyRef} className="min-h-0 flex-1 overflow-y-auto px-4 py-5 sm:px-5" data-create-form-body>
        {currentStep === 0 && (
          <div className="grid items-start gap-6 lg:grid-cols-2" data-create-basic-columns>
            <div className="min-w-0 space-y-6">
              <FormSection title={t('实例信息')}>
                <Field htmlFor="create-name" layout="row" label="容器名称">
                  <Input id="create-name"
                    type="text"
                    value={form.name}
                    aria-invalid={!!nameError}
                    onChange={(event) => handleNameChange(event.target.value)}
                    className={`  ${inputClass}    h-9`}
                    placeholder="my-container"
                    required
                  />
                  {nameError && <p className="text-xs text-red-500 mt-1">{nameError}</p>}
                </Field>
                <Field htmlFor="create-batch-count" layout="row" label="批量创建数量">
                  <NumberInput id="create-batch-count" value={batchCount} min={1} max={50} onChange={(value) => setBatchCount(Math.max(1, value || 1))} />
                </Field>
                {batchCount > 1 && <p className="text-xs leading-5 text-muted-foreground sm:pl-[124px]">将创建 {batchCount} 个容器：{form.name}-{batchStartIndex} 至 {form.name}-{batchStartIndex + batchCount - 1}</p>}
                <Field layout="row" label="虚拟化架构">
                  <div className="grid grid-cols-2 gap-2">
                    <Button variant={form.virtualization === 'lxc' ? 'default' : 'outline'}
                      type="button"
                      onClick={() => setForm((prev) => applyTemplateDefaults({ ...prev, virtualization: 'lxc', template_id: '', storage_pool_id: '', allowed_image_ids: [], image_limit_configured: false }))}
                      className="h-9 px-3 py-2 text-sm font-medium"
                    >
                      LXC 容器
                    </Button>
                    <Button variant={form.virtualization === 'kvm' ? 'default' : 'outline'}
                      type="button"
                      disabled={!kvmAvailable}
                      title={kvmAvailable ? '' : '当前宿主机不支持 KVM'}
                      onClick={() => {
                        if (kvmAvailable) {
                          setForm((prev) => applyTemplateDefaults({ ...prev, virtualization: 'kvm', template_id: '', storage_pool_id: '', allowed_image_ids: [], image_limit_configured: false }))
                        }
                      }}
                      className="h-9 px-3 py-2 text-sm font-medium"
                    >
                      KVM 虚拟机
                    </Button>
                  </div>
                </Field>
                <Field htmlFor="create-storage" layout="row" label="存储磁盘">
                  {storageLoading ? (
                    <Card className="flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground">
                      <RefreshCw className="h-4 w-4 animate-spin" />
                      正在检查存储配置...
                    </Card>
                  ) : storagePools.length > 0 ? (
                    <NativeSelect id="create-storage" wrapperClassName="w-full"
                      value={form.storage_pool_id || ''}
                      onChange={(event) => setForm({ ...form, storage_pool_id: event.target.value })}
                      className={`${inputClass} h-9 pr-9`}
                    >
                      <option value="">自动选择（默认盘优先，空间不足自动切换）</option>
                      {storagePools.map((pool) => (
                        <option key={pool.id} value={pool.id}>
                          {pool.name} · {pool.mount_point || pool.path}
                        </option>
                      ))}
                    </NativeSelect>
                  ) : (
                    <Alert variant="default" className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                      <span>尚未开启{form.virtualization === 'kvm' ? ' KVM 磁盘' : ' LXC 容器'}存储，当前无法创建。</span>
                      <Button variant="outline"
                        type="button"
                        onClick={() => { onClose(); navigate('/storage') }}
                        className="h-9 shrink-0 px-2.5 py-1.5 text-xs font-medium"
                      >
                        去开启
                      </Button>
                    </Alert>
                  )}
                </Field>
              </FormSection>
              {manualISO && <Alert className="text-xs leading-5">{t(manualMessage)}</Alert>}
              {linuxTemplate && (
                <FormSection title={t('登录凭据')}>
                  <Field label={t('认证方式')} layout="row"><div className="grid grid-cols-3 gap-2">
                    {([
                      ['auto_password', '自动生成密码'],
                      ['password', '自定义密码'],
                      ['key', 'SSH Key'],
                    ] as Array<[SSHAuthMode, string]>).map(([mode, label]) => (
                      <Button variant={sshAuthMode === mode ? 'default' : 'outline'}
                        key={mode}
                        type="button"
                        onClick={() => setForm({ ...form, ssh_auth_mode: mode })}
                        className="h-auto min-h-9 whitespace-normal px-2 py-1.5 text-xs font-medium leading-4"
                      >
                        {label}
                      </Button>
                    ))}
                  </div></Field>
                  {sshAuthMode === 'password' && <Field label={t('Root 密码')} htmlFor="create-ssh-password" layout="row"><div className="flex min-w-0 gap-2">
                    <Input id="create-ssh-password"
                      type="text"
                      value={form.ssh_password || ''}
                      onChange={(event) => setForm({ ...form, ssh_password: event.target.value })}
                      className={`${inputClass} h-9`}
                      placeholder="RootPass123"
                    />
                    <Button variant="outline"
                      type="button"
                      onClick={() => setForm({ ...form, ssh_password: generateSSHPassword() })}
                      className="inline-flex h-9 w-9 shrink-0 items-center justify-center p-0"
                      title="生成密码"
                    >
                      <RefreshCw className="h-4 w-4" />
                    </Button>
                  </div></Field>}
                  {sshAuthMode === 'key' && <Field label={t('SSH 公钥')} htmlFor="create-ssh-key" layout="row"><Textarea id="create-ssh-key"
                    value={form.ssh_public_key || ''}
                    onChange={(event) => setForm({ ...form, ssh_public_key: event.target.value })}
                    className={`  ${inputClass}  min-h-24 resize-y font-mono text-xs`}
                    placeholder="ssh-ed25519 AAAA..."
                  /></Field>}
                </FormSection>
              )}
              <FormSection title={t('配额与期限')}>
                <Field htmlFor="create-snapshot-limit" layout="row" label="子用户快照上限">
                  <NumberInput id="create-snapshot-limit"
                    value={form.snapshot_limit}
                    min={1}
                    max={999}
                    onChange={(value) => setForm({ ...form, snapshot_limit: Math.max(1, Math.round(value || 1)) })}
                  />
                </Field>
                <Field htmlFor="create-expires" layout="row" label="到期时间">
                  <div className="relative">
                    <CalendarClock className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                    <Input id="create-expires"
                      type="date"
                      value={form.expires_at}
                      onChange={(event) => setForm({ ...form, expires_at: event.target.value })}
                      min={new Date().toISOString().slice(0, 10)}
                      className={`  ${inputClass}  pl-10 h-9`}
                    />
                  </div>
                  <p className="mt-1 text-[11px] leading-4 text-muted-foreground">不选则长期有效</p>
                </Field>
              </FormSection>
            </div>
            <div className="min-w-0 space-y-6">
              <FormSection title={t('计算与存储')}>
                <Field htmlFor="create-vcpu" layout="row" label="vCPU">
                  <NumberInput id="create-vcpu"
                    value={form.vcpu}
                    min={form.virtualization === 'kvm' ? 1 : 0.25}
                    max={maxVCPU}
                    step={form.virtualization === 'kvm' ? 1 : 0.25}
                    invalid={!!resourceErrors.vcpu}
                    onChange={(value) => setForm({ ...form, vcpu: value })}
                  />
                  {resourceErrors.vcpu && <p className="mt-1 text-xs text-red-500">{resourceErrors.vcpu}</p>}
                </Field>
                <Field htmlFor="create-ram" layout="row" label="内存 (MB)">
                  <NumberInput id="create-ram"
                    value={form.ram_mb}
                    min={manualISO ? 512 : 128}
                    max={maxRAMMB}
                    step={128}
                    invalid={!!resourceErrors.ram_mb}
                    onChange={(value) => setForm({ ...form, ram_mb: value })}
                  />
                  {resourceErrors.ram_mb && <p className="mt-1 text-xs text-red-500">{resourceErrors.ram_mb}</p>}
                </Field>
                <Field htmlFor="create-disk" layout="row" label="磁盘 (GB)">
                  <NumberInput id="create-disk"
                    value={form.disk_gb}
                    min={Math.max(getTemplateMinDiskGB(form.template_id, form.virtualization, manualISO), selectedTemplate?.min_disk_gb || 0)}
                    step={form.virtualization === 'lxc' ? 0.25 : 1}
                    max={maxDiskGB}
                    invalid={!!resourceErrors.disk_gb}
                    onChange={(value) => setForm({ ...form, disk_gb: value })}
                  />
                  {resourceErrors.disk_gb && <p className="mt-1 text-xs text-red-500">{resourceErrors.disk_gb}</p>}
                </Field>
                <Field htmlFor="create-io-read" layout="row" label="读取 IO (MB/s)">
                  <NumberInput id="create-io-read" value={form.io_read_mbps} min={0} onChange={(value) => setForm({ ...form, io_read_mbps: value, io_speed_mbps: symmetricLimit(value, form.io_write_mbps) })} />
                </Field>
                <Field htmlFor="create-io-write" layout="row" label="写入 IO (MB/s)">
                  <NumberInput id="create-io-write" value={form.io_write_mbps} min={0} onChange={(value) => setForm({ ...form, io_write_mbps: value, io_speed_mbps: symmetricLimit(form.io_read_mbps, value) })} />
                </Field>
              </FormSection>
              <FormSection title={t('带宽与流量')}>
                <Field htmlFor="create-download" layout="row" label="下行带宽 (Mbps)">
                  <NumberInput id="create-download" value={form.network_down_mbps} min={0} onChange={(value) => setForm({ ...form, network_down_mbps: value, network_bw_mbps: symmetricLimit(value, form.network_up_mbps) })} />
                </Field>
                <Field htmlFor="create-upload" layout="row" label="上行带宽 (Mbps)">
                  <NumberInput id="create-upload" value={form.network_up_mbps} min={0} onChange={(value) => setForm({ ...form, network_up_mbps: value, network_bw_mbps: symmetricLimit(form.network_down_mbps, value) })} />
                </Field>
                <Field label={t('流量统计')} htmlFor="create-traffic-mode" layout="row">
                  <NativeSelect id="create-traffic-mode" wrapperClassName="w-full" value={form.traffic_mode} onChange={(e) => setForm({ ...form, traffic_mode: e.target.value })} className="h-9 text-sm pr-9">
                    <option value="total">双向统计</option><option value="in_out">入/出分离</option>
                  </NativeSelect>
                </Field>
                {form.traffic_mode === 'total' ? (
                  <Field label={t('月流量 (GB)')} htmlFor="create-traffic-total" layout="row"><NumberInput id="create-traffic-total" value={form.monthly_traffic_gb} min={0} onChange={(value) => setForm({ ...form, monthly_traffic_gb: value })} /></Field>
                ) : (
                  <>
                    <Field label={t('入站 (GB)')} htmlFor="create-traffic-in" layout="row"><NumberInput id="create-traffic-in" value={form.traffic_in_gb} min={0} onChange={(value) => setForm({ ...form, traffic_in_gb: value || 0 })} /></Field>
                    <Field label={t('出站 (GB)')} htmlFor="create-traffic-out" layout="row"><NumberInput id="create-traffic-out" value={form.traffic_out_gb} min={0} onChange={(value) => setForm({ ...form, traffic_out_gb: value || 0 })} /></Field>
                  </>
                )}
                <p className="text-[11px] leading-5 text-muted-foreground sm:pl-[124px]">{t('带宽、磁盘 IO 和月流量填 0 表示不限量。')}</p>
              </FormSection>
            </div>
          </div>
        )}
        {currentStep === 1 && (
          <div className="space-y-6">
            <FormSection title={t('系统镜像')}><Field htmlFor="create-template" layout="row" label="系统模板">
              {templates.length === 0 ? (
                <Alert variant="default" className="text-sm px-3 py-2">
                  暂无可用的{form.virtualization === 'kvm' ? ' KVM' : ' LXC'}系统镜像，请先在「镜像管理」中下载镜像模板。
                </Alert>
              ) : (
                <NativeSelect id="create-template" wrapperClassName="w-full"
                  value={form.template_id}
                  onChange={(event) => {
                    const templateID = event.target.value
                    const allowed = new Set(form.allowed_image_ids || [])
                    if (templateID) allowed.add(templateID)
                    setForm(applyTemplateDefaults({ ...form, template_id: templateID, allowed_image_ids: Array.from(allowed), image_limit_configured: true }, templates.find((item) => item.id === templateID)))
                  }}
                  className={`${inputClass} h-9 pr-9`}
                >
                  <option value="">{t('请选择镜像')}</option>
                  {templates.map((template) => (
                    <option key={template.id} value={template.id}>
                      {template.name}
                    </option>
                  ))}
                </NativeSelect>
              )}

            </Field>{manualISO && <Alert className="text-xs leading-5">{t(manualMessage)}</Alert>}</FormSection>
            {templates.length > 0 && <FormSection title={t('重装权限')}><Field layout="row" label="子用户可用镜像">
              <Card className="p-3">
                <div className="mb-2 text-xs text-muted-foreground">默认勾选当前系统；取消后，子用户也不能重装该系统。</div>
                <div className="grid gap-2 sm:grid-cols-2">
                  {templates.map((template) => {
                    const checked = (form.allowed_image_ids || []).includes(template.id)
                    const current = template.id === form.template_id
                    return (
                      <Card key={template.id} className="min-w-0 px-2.5 py-2"><Label className="flex min-w-0 cursor-pointer items-start gap-2 text-xs">
                        <Checkbox

                          checked={checked}
                          onCheckedChange={() => {
                            const currentIDs = form.allowed_image_ids || []
                            const next = checked ? currentIDs.filter((id) => id !== template.id) : [...currentIDs, template.id]
                            setForm({ ...form, allowed_image_ids: next, image_limit_configured: true })
                          }}
                          className="mt-0.5 h-4 w-4"
                        />
                        <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">
                          <span className="block font-medium text-foreground">{template.name}{current ? '（当前系统）' : ''}</span>
                          <span className="block text-muted-foreground">{template.arch} · {template.distro} {template.release}</span>
                        </span>
                      </Label></Card>
                    )
                  })}
                </div>
              </Card>
            </Field></FormSection>}
          </div>
        )}
        {currentStep === 2 && (
          <div className="grid items-start gap-6 lg:grid-cols-2">
            <FormSection title={t('IPv4 连接')}><Card className="px-3 py-2 text-sm">
              <Label className="flex items-start gap-3">
                <Checkbox

                  checked={!!form.assign_ipv4}
                  disabled={!ipv4Available}
                  onCheckedChange={(nextChecked) => setForm({
                    ...form,
                    assign_ipv4: (nextChecked === true),
                    public_ipv4s: (nextChecked === true) ? form.public_ipv4s : [],
                    ...((nextChecked === true) ? { assign_nat: false, port_mapping_count: 0, extra_ports: [], nat_port_mappings: [], management_port: 0, lan_ipv4_mode: '', lan_interface: '' } : {}),
                  })}
                  className="mt-1"
                />
                <span className="min-w-0">
                  <span className="block font-medium text-foreground">{networkText.publicIPv4}</span>
                  <span className="block text-xs text-muted-foreground">
                    {ipv4Available ? formatAllocatableIPv4Count(publicIPv4s.length, language) : networkText.noAllocatableIPv4}
                  </span>
                </span>
              </Label>
              {form.assign_ipv4 && (
                <RadioGroup value={manualIPv4s.length === 0 ? 'auto' : 'manual'} onValueChange={(value) => setForm({ ...form, public_ipv4s: value === 'auto' ? [] : (publicIPv4s[0]?.address ? [publicIPv4s[0].address] : []), ...(value === 'manual' ? { ipv4_count: 1 } : {}) })} className="mt-3 gap-3 pl-6">
                  <div className="grid grid-cols-2 gap-3">
                    <Label className="flex items-center gap-2 text-xs text-muted-foreground">
                      <RadioGroupItem value="auto" />
                      Auto assign
                    </Label>
                    <Field label="IPv4 count">
                      <NumberInput
                        value={form.ipv4_count || 1}
                        min={1}
                        max={Math.max(1, publicIPv4s.length)}
                        onChange={(value) => setForm({ ...form, ipv4_count: Math.max(1, Math.round(value || 1)) })}
                      />
                    </Field>
                  </div>
                  <div className="space-y-1.5">
                    <Label className="flex items-center gap-2 text-xs text-muted-foreground">
                      <RadioGroupItem value="manual" />
                      Manual select
                    </Label>
                    {manualIPv4s.length > 0 && (
                      <div className="grid gap-1.5 sm:grid-cols-2">
                        {publicIPv4s.map((ip) => (
                          <Card key={`${ip.interface}-${ip.address}`}><Label className="flex min-w-0 flex-wrap items-center gap-2 px-2 py-1.5 text-xs text-foreground">
                            <Checkbox

                              checked={manualIPv4s.includes(ip.address)}
                              onCheckedChange={(nextChecked) => {
                                const next = (nextChecked === true)
                                  ? [...manualIPv4s, ip.address]
                                  : manualIPv4s.filter((value) => value !== ip.address)
                                setForm({ ...form, public_ipv4s: next, ipv4_count: Math.max(1, next.length || 1) })
                              }}
                            />
                            <span className="truncate font-mono">{ip.address}</span>
                            <span className="shrink-0 text-muted-foreground">{ip.interface}</span>
                            {ip.gateway && <span className="shrink-0 text-muted-foreground">gw {ip.gateway}</span>}
                          </Label></Card>
                        ))}
                      </div>
                    )}
                  </div>
                </RadioGroup>
              )}
            </Card><Card className="px-3 py-2 text-sm">
                <div className="flex items-start justify-between gap-3">
                  <Label className="flex min-w-0 flex-1 items-start gap-3">
                    <Checkbox

                      checked={lanIPv4Enabled}
                      disabled={form.virtualization !== 'lxc' || lanInterfaces.length === 0}
                      onCheckedChange={(nextChecked) => {
                        const checked = (nextChecked === true)
                        setForm({
                          ...form,
                          lan_ipv4_mode: checked ? 'dhcp' : '',
                          lan_interface: checked ? (form.lan_interface || defaultLANInterface) : '',
                          assign_nat: checked ? false : form.assign_nat,
                          port_mapping_count: checked ? 0 : form.port_mapping_count,
                          extra_ports: checked ? [] : form.extra_ports,
                          nat_port_mappings: checked ? [] : form.nat_port_mappings,
                          management_port: checked ? 0 : form.management_port,
                          assign_ipv4: checked ? false : form.assign_ipv4,
                          public_ipv4s: checked ? [] : form.public_ipv4s,
                          ipv4_count: checked ? 0 : form.ipv4_count,
                        })
                      }}
                      className="mt-1"
                    />
                    <span className="min-w-0">
                      <span className="block font-medium text-foreground">局域网 DHCP</span>
                      <span className="block text-xs text-muted-foreground">
                        {lanInterfaces.length > 0 ? 'macvlan 独立局域网 IP' : '未检测到可用上联网卡'}
                      </span>
                    </span>
                  </Label>
                  {lanIPv4Enabled && (
                    <NativeSelect wrapperClassName="w-fit"
                      value={form.lan_interface || defaultLANInterface}
                      onChange={(event) => setForm({ ...form, lan_interface: event.target.value })}
                      className="h-9 w-32 shrink-0 px-2 text-xs pr-9"
                    >
                      {lanInterfaces.map((item) => (
                        <option key={item.name} value={item.name}>{item.name}</option>
                      ))}
                    </NativeSelect>
                  )}
                </div>
                {lanIPv4Enabled && (
                  <div className="mt-3 space-y-3 pl-6">
                    <div className="grid grid-cols-2 gap-2">
                      <Button variant={form.lan_ipv4_mode === 'dhcp' ? 'default' : 'outline'}
                        type="button"
                        onClick={() => setForm({ ...form, lan_ipv4_mode: 'dhcp' })}
                        className="h-auto px-3 py-2 text-xs font-medium"
                      >
                        DHCP 自动获取
                      </Button>
                      <Button variant={lanStaticEnabled ? 'default' : 'outline'}
                        type="button"
                        onClick={() => setForm({ ...form, lan_ipv4_mode: 'static' })}
                        className="h-auto px-3 py-2 text-xs font-medium"
                      >
                        手动配置
                      </Button>
                    </div>
                    {lanStaticEnabled && (
                      <div className="grid gap-3 sm:grid-cols-3">
                        <Field label="IPv4 地址">
                          <Input
                            value={form.lan_ipv4_address || ''}
                            onChange={(event) => setForm({ ...form, lan_ipv4_address: event.target.value })}
                            className={`${inputClass} h-auto`}
                            placeholder="192.168.2.250"
                          />
                        </Field>
                        <Field label="子网掩码">
                          <Input
                            value={subnetMaskFromPrefixLen(form.lan_ipv4_prefix_len || 24)}
                            onChange={(event) => setForm({ ...form, lan_ipv4_prefix_len: prefixLenFromSubnetMask(event.target.value) || 24 })}
                            className={`${inputClass} h-auto`}
                            placeholder="255.255.255.0"
                          />
                        </Field>
                        <Field label="网关">
                          <Input
                            value={form.lan_ipv4_gateway || ''}
                            onChange={(event) => setForm({ ...form, lan_ipv4_gateway: event.target.value })}
                            className={`${inputClass} h-auto`}
                            placeholder="192.168.2.202"
                          />
                        </Field>
                      </div>
                    )}
                  </div>
                )}
              </Card></FormSection>
            <FormSection title={t('IPv6 配置')}><Card className="px-3 py-2 text-sm">
              <div className="flex items-start justify-between gap-3">
                <Label className="flex min-w-0 flex-1 items-start gap-3">
                  <Checkbox

                    checked={!!form.assign_ipv6}
                    disabled={!ipv6Available}
                    onCheckedChange={(nextChecked) => setForm({ ...form, assign_ipv6: (nextChecked === true) })}
                    className="mt-1"
                  />
                  <span className="min-w-0">
                    <span className="block font-medium text-foreground">{networkText.publicIPv6}</span>
                    <span className="block text-xs text-muted-foreground truncate">
                      {ipv6Available ? `${networkText.use} ${ipv6Prefix}` : formatIPv6StatusReason(ipv6Status?.reason, language, networkText.checkingIPv6Prefix)}
                    </span>
                  </span>
                </Label>
                {form.assign_ipv6 && (
                  <span className="block w-24 shrink-0">
                    <NumberInput
                      value={form.ipv6_count || 1}
                      min={1}
                      max={64}
                      onChange={(value) => setForm({ ...form, ipv6_count: Math.max(1, Math.round(value || 1)) })}
                    />
                  </span>
                )}
              </div>
              {form.assign_ipv6 && (
                <div className="mt-3 space-y-3 pl-6">
                  <RadioGroup value={(form.ipv6_addresses || []).length === 0 ? 'auto' : 'manual'} onValueChange={(value) => setForm({ ...form, ipv6_addresses: value === 'auto' ? [] : [''], ...(value === 'manual' ? { ipv6_count: 1 } : {}) })} className="grid grid-cols-2 gap-3">
                    <Label className="flex items-center gap-2 text-xs text-muted-foreground">
                      <RadioGroupItem value="auto" />
                      Random assign
                    </Label>
                    <Label className="flex items-center gap-2 text-xs text-muted-foreground">
                      <RadioGroupItem value="manual" />
                      Custom assign
                    </Label>
                  </RadioGroup>
                  {(form.ipv6_addresses || []).length > 0 && (
                    <Textarea
                      value={(form.ipv6_addresses || []).join('\n')}
                      onChange={(event) => {
                        const next = splitAddressLines(event.target.value)
                        setForm({ ...form, ipv6_addresses: next.length ? next : [''], ipv6_count: Math.max(1, next.length || 1) })
                      }}
                      className={`  ${inputClass}  min-h-20 font-mono text-xs`}
                      placeholder="2001:db8:100::100"
                    />
                  )}
                </div>
              )}
            </Card>{ipv6NATAvailable && <Card className="space-y-3 px-3 py-3 text-sm">
              <div><span className="font-medium text-foreground">IPv6 NAT</span><p className="mt-1 text-xs text-muted-foreground">{language === 'en' ? `A private IPv6 address will be assigned automatically; outbound traffic shares ${ipv6NATStatus.host_address}.` : `将自动分配内网 IPv6，出网共享宿主机 ${ipv6NATStatus.host_address}。`}</p></div>
              <Label className="flex items-center gap-2 text-xs text-foreground"><Checkbox checked={form.ipv6_nat_sync_ipv4 !== false} onCheckedChange={nextChecked => setForm({ ...form, ipv6_nat_sync_ipv4: (nextChecked === true) })} />{language === 'en' ? 'Sync IPv4 port mappings to IPv6' : '将 IPv4 端口映射同步到 IPv6'}</Label>
              <Field layout="row" htmlFor="create-ipv6-quota" label={language === 'en' ? 'IPv6 port quota (0 = inherit IPv4 quota)' : 'IPv6 端口配额（0 = 继承 IPv4 配额）'}><NumberInput id="create-ipv6-quota" value={form.ipv6_port_mapping_count || 0} min={0} max={64} onChange={value => setForm({ ...form, ipv6_port_mapping_count: Math.min(64, Math.max(0, Math.round(value || 0))) })} /></Field>
              {form.ipv6_nat_sync_ipv4 === false && <p className="text-xs text-muted-foreground">{language === 'en' ? 'Add independent IPv6 mappings from the container details after creation.' : '创建完成后，可在容器详情中添加独立 IPv6 端口映射。'}</p>}
            </Card>}</FormSection>
            <div className="min-w-0 lg:col-span-2"><FormSection title={t('NAT 与端口映射')}><Card className="px-3 py-2 text-sm">
              <div className="flex items-start justify-between gap-3">
                <Label className="flex min-w-0 flex-1 items-start gap-3">
                  <Checkbox

                    checked={natEnabled}
                    onCheckedChange={(nextChecked) => {
                      const checked = (nextChecked === true)
                      setForm({
                        ...form,
                        assign_nat: checked,
                        port_mapping_count: checked ? Math.max(2, form.port_mapping_count || 2) : 0,
                        extra_ports: [],
                        nat_port_mappings: [],
                        management_port: checked ? form.management_port : 0,
                        ...(checked ? { assign_ipv4: false, public_ipv4s: [], ipv4_count: 0, lan_ipv4_mode: '', lan_interface: '' } : {}),
                      })
                    }}
                    className="mt-1"
                  />
                  <span className="min-w-0">
                    <span className="block font-medium text-foreground">{networkText.publicNAT}</span>
                    <span className="block text-xs text-muted-foreground">
                      {natEnabled ? formatNATPortCount(natPortCount, language) : networkText.noNATPorts}
                      {natEnabled && routingInfo && (
                        <span className="mt-0.5 block font-mono">
                          {language === 'en' ? 'Range' : '范围'} {routingInfo.nat4_port_range.start}-{routingInfo.nat4_port_range.end}
                          {' · '}
                          {managementPort > 0
                            ? (language === 'en' ? 'management' : '管理端口')
                            : (language === 'en' ? 'next' : '下一个')}
                          {' '}{sshPortPreview || '-'}
                        </span>
                      )}
                    </span>
                  </span>
                </Label>
                {natEnabled && customNATMappings.length === 0 && (
                  <span className="block w-24 shrink-0">
                    <NumberInput
                      value={natPortCount}
                      min={2}
                      max={64}
                      onChange={(value) => setForm({ ...form, port_mapping_count: Math.max(2, value || 2), assign_nat: true, extra_ports: [], nat_port_mappings: [] })}
                    />
                  </span>
                )}
              </div>
              {natEnabled && (
                <div className="mt-2 min-w-0 space-y-2 sm:pl-6" style={{ containerType: 'inline-size' }}>
                  <div className="space-y-1">
                    <div className="grid min-w-0 grid-cols-2 items-end gap-2 [@container(min-width:480px)]:grid-cols-[minmax(0,1fr)_20px_minmax(0,1fr)_56px]">
                      <Label className="min-w-0 text-[11px] text-muted-foreground">
                        <span className="mb-1 block">
                          {language === 'en'
                            ? `${isWindowsTemplate(form.template_id) ? 'RDP' : 'SSH'} public source port`
                            : `${isWindowsTemplate(form.template_id) ? 'RDP' : 'SSH'} 公网源端口`}
                        </span>
                        <Input
                          type="number"
                          min={1}
                          max={65535}
                          value={managementPort || ''}
                          onChange={(event) => setForm({
                            ...form,
                            management_port: Number(event.target.value) || 0,
                            assign_nat: true,
                          })}
                          className={`  ${inputClass}  min-w-0 font-mono text-xs h-auto`}
                          placeholder={language === 'en' ? 'Auto' : '自动'}
                        />
                      </Label>
                      <ArrowRight className="mb-3 hidden h-4 w-4 text-muted-foreground [@container(min-width:480px)]:block" />
                      <Label className="min-w-0 text-[11px] text-muted-foreground">
                        <span className="mb-1 block">{language === 'en' ? 'Container target port' : '容器目标端口'}</span>
                        <Input
                          type="number"
                          readOnly
                          value={isWindowsTemplate(form.template_id) ? 3389 : 22}
                          className={`  ${inputClass}  min-w-0 font-mono text-xs h-auto`}
                        />
                      </Label>
                      <Badge variant="secondary" className="mb-1 inline-flex h-9 w-fit items-center justify-center text-xs">
                        TCP
                      </Badge>
                    </div>
                  </div>
                  <RadioGroup value={customNATMappings.length === 0 ? 'auto' : 'manual'} onValueChange={(value) => {
                    if (value === 'auto') {
                      setForm({ ...form, extra_ports: [], nat_port_mappings: [], port_mapping_count: Math.max(2, form.port_mapping_count || 2) })
                      return
                    }
                    const suggestedPort = autoPortMappings[0]?.host_port || natAllocationPreview.managementPort
                    if (!suggestedPort) return
                    const next = customNATMappings.length > 0 ? customNATMappings : [{ host_port: suggestedPort, container_port: suggestedPort, protocol: 'tcp', description: `Port-${suggestedPort}` }]
                    setForm({ ...form, extra_ports: [], nat_port_mappings: next, port_mapping_count: next.length + 1, assign_nat: true })
                  }} className="grid gap-2 [@container(min-width:300px)]:grid-cols-2">
                    <Label className="flex items-center gap-2 text-xs text-muted-foreground">
                      <RadioGroupItem value="auto" />
                      {language === 'en' ? 'Auto ports' : '自动端口'}
                    </Label>
                    <Label className="flex items-center gap-2 text-xs text-muted-foreground">
                      <RadioGroupItem value="manual" disabled={!routingInfo} />
                      {language === 'en' ? 'Custom mappings' : '自定义映射'}
                    </Label>
                  </RadioGroup>
                  {customNATMappings.length > 0 && (
                    <div className="space-y-2">
                      <div className="hidden grid-cols-[minmax(0,1fr)_20px_minmax(0,1fr)_80px_32px] items-center gap-2 px-1 text-[11px] text-muted-foreground [@container(min-width:480px)]:grid">
                        <span>{language === 'en' ? 'Public source port' : '源端口（公网）'}</span>
                        <span />
                        <span>{language === 'en' ? 'Container target port' : '目标端口（容器）'}</span>
                        <span>{language === 'en' ? 'Protocol' : '协议'}</span>
                        <span />
                      </div>
                      {customNATMappings.map((mapping, index) => (
                        <div key={index} className="grid min-w-0 grid-cols-2 items-end gap-2 rounded-md border border-border p-2 [@container(min-width:480px)]:grid-cols-[minmax(0,1fr)_20px_minmax(0,1fr)_80px_32px] [@container(min-width:480px)]:items-center [@container(min-width:480px)]:border-0 [@container(min-width:480px)]:p-0">
                          <Label className="block min-w-0 text-[11px] font-normal text-muted-foreground">
                            <span className="mb-1 block [@container(min-width:480px)]:hidden">{language === 'en' ? 'Public source port' : '源端口（公网）'}</span>
                          <Input
                            type="number"
                            min={1}
                            max={65535}
                            value={mapping.host_port || ''}
                            onChange={(event) => {
                              const next = customNATMappings.map((item, itemIndex) => itemIndex === index
                                ? { ...item, host_port: Number(event.target.value) }
                                : item)
                              setForm({ ...form, extra_ports: [], nat_port_mappings: next, port_mapping_count: next.length + 1 })
                            }}
                            className={`  ${inputClass}  min-w-0 font-mono text-xs h-auto`}
                            placeholder="30080"
                          />
                          </Label>
                          <ArrowRight className="hidden h-4 w-4 text-muted-foreground [@container(min-width:480px)]:block" />
                          <Label className="block min-w-0 text-[11px] font-normal text-muted-foreground">
                            <span className="mb-1 block [@container(min-width:480px)]:hidden">{language === 'en' ? 'Container target port' : '目标端口（容器）'}</span>
                          <Input
                            type="number"
                            min={1}
                            max={65535}
                            value={mapping.container_port || ''}
                            onChange={(event) => {
                              const targetPort = Number(event.target.value)
                              const next = customNATMappings.map((item, itemIndex) => itemIndex === index
                                ? { ...item, container_port: targetPort, description: `Port-${targetPort}` }
                                : item)
                              setForm({ ...form, extra_ports: [], nat_port_mappings: next, port_mapping_count: next.length + 1 })
                            }}
                            className={`  ${inputClass}  min-w-0 font-mono text-xs h-auto`}
                            placeholder="80"
                          />
                          </Label>
                          <Label className="block min-w-0 text-[11px] font-normal text-muted-foreground">
                            <span className="mb-1 block [@container(min-width:480px)]:hidden">{language === 'en' ? 'Protocol' : '协议'}</span>
                          <NativeSelect wrapperClassName="min-w-0 w-full"
                            value={mapping.protocol || 'tcp'}
                            onChange={(event) => {
                              const next = customNATMappings.map((item, itemIndex) => itemIndex === index
                                ? { ...item, protocol: event.target.value }
                                : item)
                              setForm({ ...form, nat_port_mappings: next })
                            }}
                            className="h-10 px-2 text-xs pr-9"
                          >
                            <option value="tcp">TCP</option>
                            <option value="udp">UDP</option>
                          </NativeSelect>
                          </Label>
                          <Button variant="destructive"
                            type="button"
                            disabled={customNATMappings.length <= 1}
                            onClick={() => {
                              const next = customNATMappings.filter((_, itemIndex) => itemIndex !== index)
                              setForm({ ...form, nat_port_mappings: next, port_mapping_count: next.length + 1 })
                            }}
                            className="mb-1 inline-flex h-8 w-8 items-center justify-center justify-self-end p-0 [@container(min-width:480px)]:mb-0"
                            title={language === 'en' ? 'Remove mapping' : '删除映射'}
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      ))}
                      <Button variant="outline"
                        type="button"
                        disabled={customNATMappings.length >= 63}
                        onClick={() => {
                          const previous = customNATMappings[customNATMappings.length - 1]
                          const hostPort = Math.min(65535, (previous?.host_port || 22001) + 1)
                          const containerPort = Math.min(65535, (previous?.container_port || 22001) + 1)
                          const next = [...customNATMappings, {
                            host_port: hostPort,
                            container_port: containerPort,
                            protocol: 'tcp',
                            description: `Port-${containerPort}`,
                          }]
                          setForm({ ...form, nat_port_mappings: next, port_mapping_count: next.length + 1 })
                        }}
                        className="h-auto inline-flex items-center gap-1 px-2.5 py-1.5 text-xs"
                      >
                        <Plus className="h-3.5 w-3.5" />
                        {language === 'en' ? 'Add mapping' : '添加映射'}
                      </Button>
                      {batchCount > 1 && (
                        <p className="text-[11px] text-muted-foreground">
                          {language === 'en'
                            ? 'Each later container starts after the previous highest public port; target ports stay unchanged.'
                            : '后续容器从上一台的最高公网端口之后开始，容器内部端口保持不变。'}
                        </p>
                      )}
                    </div>
                  )}
                  <div className="flex flex-wrap gap-1.5">
                    <Badge variant="secondary" className="inline-flex px-2 py-1 text-xs font-mono">
                      {isWindowsTemplate(form.template_id) ? 'RDP' : 'SSH'}: {sshPortPreview || '--'} -&gt; {isWindowsTemplate(form.template_id) ? 3389 : 22}
                      {managementPort === 0 ? (language === 'en' ? ' (auto)' : '（自动）') : ''}
                    </Badge>
                    {natPreviewMappings.map((mapping, index) => (
                      <Badge variant="secondary" key={`${mapping.host_port}-${mapping.container_port}-${mapping.protocol}-${index}`} className="inline-flex px-2 py-1 text-xs font-mono">
                        {mapping.host_port} -&gt; {mapping.container_port}/{mapping.protocol.toUpperCase()}
                      </Badge>
                    ))}
                  </div>
                </div>
              )}
            </Card></FormSection></div>
          </div>
        )}
        {currentStep === 3 && <div className="space-y-5">
          <section>
            <h3 className="mb-2 text-sm font-semibold text-foreground">{t('基础信息')}</h3>
            <dl className="grid grid-cols-1 gap-x-6 sm:grid-cols-2">
              <ReviewItem label={t('容器名称')} value={batchCount > 1 ? `${form.name}-${batchStartIndex} … ${form.name}-${batchStartIndex + batchCount - 1}` : form.name} />
              <ReviewItem label={t('创建数量')} value={String(batchCount)} />
              <ReviewItem label={t('虚拟化架构')} value={form.virtualization === 'kvm' ? 'KVM' : 'LXC'} />
              <ReviewItem label={t('存储磁盘')} value={selectedStoragePool ? `${selectedStoragePool.name} · ${selectedStoragePool.mount_point || selectedStoragePool.path}` : t('自动选择')} />
              <ReviewItem label="vCPU" value={String(form.vcpu)} />
              <ReviewItem label={t('内存')} value={`${form.ram_mb} MB`} />
              <ReviewItem label={t('磁盘')} value={`${form.disk_gb} GB`} />
              <ReviewItem label={t('到期时间')} value={form.expires_at || t('长期有效')} />
            </dl>
          </section>

          <section>
            <h3 className="mb-2 text-sm font-semibold text-foreground">{t('镜像与登录')}</h3>
            <dl className="grid grid-cols-1 gap-x-6 sm:grid-cols-2">
              <ReviewItem label={t('系统镜像')} value={selectedTemplate?.name || '-'} />
              {manualISO && <ReviewItem label={t('安装方式')} value={t(manualDisk ? '预装磁盘，保留原账号与配置' : 'ISO / VNC 手动安装，不注入凭据')} />}
              <ReviewItem label={t('登录方式')} value={
                manualISO
                  ? t('通过 VNC 手动安装')
                  : !linuxTemplate
                  ? t('镜像默认')
                  : sshAuthMode === 'key'
                    ? 'SSH Key'
                    : sshAuthMode === 'password'
                      ? t('自定义密码')
                      : t('自动生成密码')
              } />
              <ReviewItem label={t('子用户可用镜像')} value={`${selectedAllowedImages.length} ${t('个')}`} />
            </dl>
            {selectedAllowedImages.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1.5">
                {selectedAllowedImages.map((template) => (
                  <Badge variant="secondary" key={template.id} className="px-2 py-1 text-xs">
                    {template.name}
                  </Badge>
                ))}
              </div>
            )}
          </section>

          <section>
            <h3 className="mb-2 text-sm font-semibold text-foreground">{t('网络配置')}</h3>
            <dl className="grid grid-cols-1 gap-x-6 sm:grid-cols-2">
              <ReviewItem label={t('主要网络')} value={networkSummary} />
              <ReviewItem
                label={networkText.publicIPv6}
                value={form.assign_ipv6 ? `${form.ipv6_count || 1} ${t('个地址')}` : t('未启用')}
              />
              {ipv6NATAvailable && <ReviewItem label="IPv6 NAT" value={form.ipv6_nat_sync_ipv4 !== false ? (language === 'en' ? 'Private IPv6 · sync IPv4 ports' : '内网 IPv6 · 同步 IPv4 端口') : (language === 'en' ? 'Private IPv6 · independent ports' : '内网 IPv6 · 独立端口')} />}
            </dl>
            {natEnabled && (
              <div className="mt-3">
                <div className="mb-1.5 text-xs font-medium text-muted-foreground">{t('端口映射')}</div>
                <div className="flex flex-wrap gap-1.5">
                  <Badge variant="secondary" className="px-2 py-1 font-mono text-xs">
                    {isWindowsTemplate(form.template_id) ? 'RDP' : 'SSH'}: {sshPortPreview || t('自动')} -&gt; {isWindowsTemplate(form.template_id) ? 3389 : 22}/TCP
                  </Badge>
                  {natPreviewMappings.map((mapping, index) => (
                    <Badge variant="secondary" key={`${mapping.host_port}-${mapping.container_port}-${mapping.protocol}-${index}`} className="px-2 py-1 font-mono text-xs">
                      {mapping.host_port || t('自动')} -&gt; {mapping.container_port}/{mapping.protocol.toUpperCase()}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </section>
        </div>}
      </div>
      <div className="flex shrink-0 flex-col-reverse items-stretch justify-between gap-2 border-t border-border bg-background px-4 py-3 sm:flex-row sm:items-center sm:px-5">
        <Button variant="ghost" onClick={onClose} disabled={loading} className="h-auto px-4 py-2 text-sm">
          {t('取消')}
        </Button>
        <div className="grid min-w-0 auto-cols-fr grid-flow-col items-center gap-2 sm:flex">
          {currentStep > 0 && (
            <Button variant="outline"
              type="button"
              onClick={() => setCurrentStep((step) => Math.max(0, step - 1))}
              disabled={loading}
              className="h-auto inline-flex items-center gap-2 px-4 py-2 text-sm"
            >
              <ArrowLeft className="h-4 w-4" />
              {t('上一步')}
            </Button>
          )}
          {currentStep < wizardSteps.length - 1 ? (
            <Button variant="default"
              type="button"
              onClick={handleNextStep}
              disabled={currentStep === 0 && storageLoading}
              className="h-auto inline-flex items-center gap-2 px-4 py-2 text-sm"
            >
              {t('下一步')}
              <ArrowRight className="h-4 w-4" />
            </Button>
          ) : (
            <Button variant="default"
              type="button"
              onClick={handleSubmit}
              disabled={loading}
              className="h-auto inline-flex items-center gap-2 px-4 py-2 text-sm"
            >
              {loading ? t('创建中...') : t('确认创建')}
            </Button>
          )}
        </div>
      </div>
    </Modal>
  )
}

function ReviewItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid min-w-0 grid-cols-[112px_minmax(0,1fr)] items-start gap-3 border-b border-border/70 py-2.5">
      <dt className="pt-0.5 text-xs text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-sm font-medium text-foreground [overflow-wrap:anywhere]">{value || '-'}</dd>
    </div>
  )
}

function FormSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="min-w-0 space-y-3" aria-label={title}>
      <h3 className="border-b border-border pb-2 text-sm font-semibold tracking-tight text-foreground">{title}</h3>
      <div className="min-w-0 space-y-3">{children}</div>
    </section>
  )
}

function Field({ label, children, layout = 'stack', htmlFor }: { label: string; children: ReactNode; layout?: 'row' | 'stack'; htmlFor?: string }) {
  const labelId = useId()
  return (
    <div className={layout === 'row' ? 'grid min-w-0 gap-1.5 sm:grid-cols-[112px_minmax(0,1fr)] sm:gap-3' : 'min-w-0'}>
      <Label id={labelId} htmlFor={htmlFor} className={layout === 'row' ? 'text-xs font-medium leading-5 text-muted-foreground sm:pt-2 sm:text-right' : 'mb-1.5 block text-xs font-medium text-foreground'}>{label}</Label>
      <div className="min-w-0" role={htmlFor ? undefined : 'group'} aria-labelledby={htmlFor ? undefined : labelId}>{children}</div>
    </div>
  )
}

function NumberInput({
  id,
  value,
  min,
  max,
  step,
  invalid,
  onChange,
}: {
  id?: string
  value: number
  min?: number
  max?: number
  step?: number
  invalid?: boolean
  onChange: (value: number) => void
}) {
  const [draft, setDraft] = useState(Number.isFinite(value) ? String(value) : '')
  const [focused, setFocused] = useState(false)

  useEffect(() => {
    if (!focused) {
      setDraft(Number.isFinite(value) ? String(value) : '')
    }
  }, [focused, value])

  return (
    <Input
      id={id}
      type="text"
      inputMode={step && !Number.isInteger(step) ? 'decimal' : 'numeric'}
      value={draft}
      onFocus={() => setFocused(true)}
      onBlur={() => {
        setFocused(false)
        setDraft(Number.isFinite(value) ? String(value) : '')
      }}
      onChange={(event) => {
        const raw = event.target.value
        setDraft(raw)
        const next = step && !Number.isInteger(step) ? parseFloat(raw) : parseInt(raw, 10)
        onChange(next)
      }}
      aria-invalid={invalid || undefined}
      data-min={min}
      data-max={max}
      data-step={step}
      className={`${inputClass} tabular-nums`}
    />
  )
}

function getTemplateMinDiskGB(templateID: string, virtualization: string, manualISO = false): number {
  if (manualISO) return 1
  const id = (templateID || '').toLowerCase()
  if (virtualization === 'kvm') {
    if (id.includes('windows-11') || id.includes('win11')) return 64
    if (id.includes('windows') || id.includes('win')) return 30
    if (id.includes('desktop') || id.includes('xfce') || id.includes('gnome')) return 20
    return 5
  }
  // LXC
  if (id.includes('alpine')) return 0.5
  if (id.includes('debian')) return 1.0
  if (id.includes('ubuntu')) return 1.5
  if (id.includes('centos') || id.includes('rocky') || id.includes('fedora') || id.includes('almalinux') || id.includes('arch')) return 2.0
  return 0.5
}

function normalizeLXCDisk(value: number) {
  const val = Number.isFinite(value) ? value : 1
  const rounded = Math.round(val * 100) / 100
  return Math.max(0.25, Number(rounded.toFixed(2)))
}

function validateResourceInputs(form: CreateContainerRequest, maxVCPU: number, maxRAMMB?: number, maxDiskGB?: number, manualISO = false, templateMin = 0) {
  const errors: Partial<Record<'vcpu' | 'ram_mb' | 'disk_gb', string>> = {}
  const windows = !manualISO && isWindowsTemplate(form.template_id)
  const windows11 = windows && form.template_id.toLowerCase().includes('windows-11')
  const minVCPU = windows ? 2 : (form.virtualization === 'kvm' ? 1 : 0.25)
  const minRAMMB = manualISO ? 512 : windows11 ? 4096 : windows ? 2048 : 128
  const minDiskGB = Math.max(getTemplateMinDiskGB(form.template_id, form.virtualization, manualISO), templateMin)

  if (!Number.isFinite(form.vcpu)) {
    errors.vcpu = '请输入 vCPU'
  } else if (form.vcpu < minVCPU) {
    errors.vcpu = `不能小于 ${minVCPU} 核`
  } else if (form.vcpu > maxVCPU) {
    errors.vcpu = `不能大于 ${maxVCPU} 核`
  } else if (form.virtualization === 'kvm' && form.vcpu !== Math.round(form.vcpu)) {
    errors.vcpu = 'KVM vCPU 必须是整数'
  }

  if (!Number.isFinite(form.ram_mb)) {
    errors.ram_mb = '请输入内存'
  } else if (form.ram_mb < minRAMMB) {
    errors.ram_mb = `不能小于 ${minRAMMB} MB`
  } else if (maxRAMMB && form.ram_mb > maxRAMMB) {
    errors.ram_mb = `不能大于 ${maxRAMMB} MB`
  }

  if (!Number.isFinite(form.disk_gb)) {
    errors.disk_gb = '请输入磁盘'
  } else if (form.disk_gb < minDiskGB) {
    errors.disk_gb = `不能小于 ${minDiskGB} GB`
  } else if (maxDiskGB && form.disk_gb > maxDiskGB) {
    errors.disk_gb = `不能大于 ${maxDiskGB} GB`
  }

  return errors
}

function normalizeCreateForm(form: CreateContainerRequest, template?: Template): CreateContainerRequest {
  const manualISO = form.virtualization === 'kvm' && isManualISOTemplate(template)
  const normalized = applyTemplateDefaults(form, template)
  const wantsLANDHCP = normalized.virtualization === 'lxc' && normalized.lan_ipv4_mode === 'dhcp'
  const wantsLANStatic = normalized.virtualization === 'lxc' && normalized.lan_ipv4_mode === 'static'
  const wantsLANIPv4 = wantsLANDHCP || wantsLANStatic
  const wantsIPv4 = !!normalized.assign_ipv4
  const wantsIPv6 = !!normalized.assign_ipv6
  // IPv4 and NAT are mutually exclusive
  const wantsNAT = wantsLANIPv4 || wantsIPv4 ? false : normalized.assign_nat !== false
  const legacyMappings = normalizePortList(normalized.extra_ports || []).map((port) => ({
    host_port: port,
    container_port: port,
    protocol: 'tcp',
    description: `Port-${port}`,
  }))
  const natPortMappings = wantsNAT
    ? normalizeNATPortMappings((normalized.nat_port_mappings?.length ? normalized.nat_port_mappings : legacyMappings))
    : []
  const managementPort = wantsNAT ? Math.round(Number(normalized.management_port) || 0) : 0
  const portMappingCount = wantsNAT
    ? (natPortMappings.length > 0 ? natPortMappings.length + 1 : clampInt(normalized.port_mapping_count || 2, 2, 64, 2))
    : 0
  const linuxTemplate = !manualISO && !isWindowsTemplate(normalized.template_id)
  const sshAuthMode = linuxTemplate ? (normalized.ssh_auth_mode || 'auto_password') : 'auto_password'
  return {
    ...normalized,
    vcpu: normalized.virtualization === 'kvm' ? Math.round(normalized.vcpu) : normalizeLXCvCPU(normalized.vcpu),
    ram_mb: Math.round(normalized.ram_mb),
    disk_gb: normalized.virtualization === 'kvm' ? Math.round(normalized.disk_gb) : normalizeLXCDisk(normalized.disk_gb),
    assign_nat: wantsNAT,
    port_mapping_count: portMappingCount,
    ipv6_port_mapping_count: normalized.virtualization === 'lxc' && !wantsLANIPv4 ? clampInt(normalized.ipv6_port_mapping_count || 0, 0, 64, 0) : 0,
    ipv6_nat_sync_ipv4: normalized.virtualization === 'lxc' ? normalized.ipv6_nat_sync_ipv4 !== false : undefined,
    extra_ports: [],
    nat_port_mappings: natPortMappings,
    management_port: managementPort,
    lan_ipv4_mode: wantsLANDHCP ? 'dhcp' : (wantsLANStatic ? 'static' : ''),
    lan_interface: wantsLANIPv4 ? (normalized.lan_interface || '').trim() : '',
    lan_ipv4_address: wantsLANStatic ? (normalized.lan_ipv4_address || '').trim() : '',
    lan_ipv4_prefix_len: wantsLANStatic ? clampInt(normalized.lan_ipv4_prefix_len || 24, 1, 32, 24) : 0,
    lan_ipv4_gateway: wantsLANStatic ? (normalized.lan_ipv4_gateway || '').trim() : '',
    assign_ipv4: wantsIPv4,
    ipv4_count: wantsIPv4 ? clampInt(normalized.ipv4_count || 1, 1, 64, 1) : 0,
    public_ipv4s: wantsIPv4 ? (normalized.public_ipv4s || []) : [],
    assign_ipv6: wantsIPv6,
    ipv6_count: wantsIPv6 ? clampInt(normalized.ipv6_count || 1, 1, 64, 1) : 0,
    ipv6_addresses: wantsIPv6 ? (normalized.ipv6_addresses || []).map((item) => item.trim()).filter(Boolean) : [],
    ssh_auth_mode: manualISO ? undefined : sshAuthMode,
    ssh_password: linuxTemplate && sshAuthMode === 'password' ? (normalized.ssh_password || '').trim() : '',
    ssh_public_key: linuxTemplate && sshAuthMode === 'key' ? (normalized.ssh_public_key || '').trim() : '',
    snapshot_limit: clampInt(normalized.snapshot_limit, 1, undefined, 3),
  }
}

function validateSSHAuthInputs(form: CreateContainerRequest, template?: Template) {
  if (isManualISOTemplate(template) || isWindowsTemplate(form.template_id)) return ''
  const mode = form.ssh_auth_mode || 'auto_password'
  if (mode === 'password') return sshPasswordError((form.ssh_password || '').trim())
  if (mode === 'key') return sshPublicKeyError(form.ssh_public_key || '')
  if (mode !== 'auto_password') return '请选择登录方式'
  return ''
}

function getLANDHCPInterfaces(report: HostProbeReport | null) {
  const interfaces = report?.network_interfaces || []
  return interfaces.filter((item) => {
    const name = item.name || ''
    if (!name || name === 'lo') return false
    if (name.startsWith('lxc') || name.startsWith('docker') || name.startsWith('br-') || name.startsWith('veth') || name.startsWith('virbr') || name.startsWith('clmv-')) return false
    return (item.state || '').toLowerCase() === 'up'
  })
}

function isManualISOTemplate(template?: Template) {
  return template?.manual_install === true || template?.provisioner === 'manual-iso' || template?.provisioner === 'manual-disk'
}

function applyTemplateDefaults(form: CreateContainerRequest, template?: Template): CreateContainerRequest {
  if (template?.min_disk_gb && form.disk_gb < template.min_disk_gb) form = { ...form, disk_gb: template.min_disk_gb }
  if (isManualISOTemplate(template) || !isWindowsTemplate(form.template_id)) return form
  return {
    ...form,
    virtualization: 'kvm',
    vcpu: Math.max(2, Math.round(Number.isFinite(form.vcpu) ? form.vcpu : 2)),
    ram_mb: Math.max(2048, Math.round(Number.isFinite(form.ram_mb) ? form.ram_mb : 2048)),
    disk_gb: Math.max(30, Math.round(Number.isFinite(form.disk_gb) ? form.disk_gb : 30)),
  }
}

function isWindowsTemplate(templateID: string) {
  return templateID.toLowerCase().includes('windows')
}

function normalizeLXCvCPU(value: number) {
  const rounded = Math.round((Number.isFinite(value) ? value : 1) * 4) / 4
  return Number(rounded.toFixed(2))
}

function clampInt(value: number, min: number, max?: number, fallback = min) {
  const next = Math.round(Number.isFinite(value) ? value : fallback)
  return Math.min(Math.max(next, min), max ?? next)
}

function normalizePortList(ports: number[]) {
  const seen = new Set<number>()
  const result: number[] = []
  for (const port of ports) {
    if (!Number.isFinite(port)) continue
    const next = Math.round(port)
    if (next < 1 || next > 65535 || seen.has(next)) continue
    seen.add(next)
    result.push(next)
    if (result.length >= 63) break
  }
  return result
}

function normalizeNATPortMappings(mappings: PortMapping[]) {
  return mappings.slice(0, 63).map((mapping) => {
    const hostPort = Math.round(Number(mapping.host_port) || 0)
    const containerPort = Math.round(Number(mapping.container_port) || 0)
    const protocol = (mapping.protocol || 'tcp').toLowerCase() === 'udp' ? 'udp' : 'tcp'
    return {
      host_port: hostPort,
      container_port: containerPort,
      protocol,
      description: mapping.description?.trim() || `Port-${containerPort}`,
    }
  })
}

function previewNATAllocation(
  routing: RoutingInfo | null,
  customMappings: PortMapping[],
  explicitManagementPort: number,
  autoMappingCount: number,
  managementTargetPort: number
) {
  if (!routing) {
    return { managementPort: explicitManagementPort, autoMappings: [] as PortMapping[] }
  }

  const { start, end } = routing.nat4_port_range
  const used = new Set(
    (routing.nat4_mappings || [])
      .map((mapping) => Math.round(Number(mapping.host_port) || 0))
      .filter((port) => port >= start && port <= end)
  )
  const excluded = new Set(
    customMappings
      .map((mapping) => Math.round(Number(mapping.host_port) || 0))
      .filter((port) => port >= start && port <= end)
  )

  let managementPort = explicitManagementPort
  if (managementPort === 0) {
    const cursor = routing.nat4_next_port >= start && routing.nat4_next_port <= end
      ? routing.nat4_next_port
      : start
    managementPort = findAvailableNATPort(start, end, cursor, new Set([...used, ...excluded]))
  }

  const autoMappings: PortMapping[] = []
  if (customMappings.length === 0 && autoMappingCount > 0) {
    const unavailable = new Set(used)
    if (managementPort > 0) unavailable.add(managementPort)
    if (managementTargetPort >= start && managementTargetPort <= end) unavailable.add(managementTargetPort)
    for (let port = start; port <= end && autoMappings.length < autoMappingCount; port++) {
      if (unavailable.has(port)) continue
      unavailable.add(port)
      autoMappings.push({
        host_port: port,
        container_port: port,
        protocol: 'tcp',
        description: `Port-${port}`,
      })
    }
  }

  return { managementPort, autoMappings }
}

function findAvailableNATPort(start: number, end: number, cursor: number, unavailable: Set<number>) {
  const capacity = end - start + 1
  for (let offset = 0; offset < capacity; offset++) {
    const candidate = start + ((cursor - start + offset) % capacity)
    if (!unavailable.has(candidate)) return candidate
  }
  return 0
}

function batchNATEffectiveStride(mappings: PortMapping[], managementPort: number, portMappingCount?: number) {
  const stride = batchNATPortStride(batchNATSourceMappings(mappings, managementPort))
  if (mappings.length > 0) return stride
  const autoCount = Math.min(Math.max(Math.round(Number(portMappingCount) || 2), 2), 64)
  return Math.max(stride, autoCount)
}

function expandBatchNATConfig(mappings: PortMapping[], managementPort: number, batchIndex: number, portMappingCount?: number) {
  const stride = batchNATEffectiveStride(mappings, managementPort, portMappingCount)
  const offset = batchIndex * stride
  return {
    mappings: mappings.map((mapping) => ({
      ...mapping,
      host_port: mapping.host_port + offset,
    })),
    managementPort: managementPort > 0 ? managementPort + offset : 0,
  }
}

function batchNATSourceMappings(mappings: PortMapping[], managementPort: number) {
  if (managementPort <= 0) return mappings
  return [
    {
      host_port: managementPort,
      container_port: 0,
      protocol: 'tcp',
      description: 'Management',
    },
    ...mappings,
  ]
}

function batchNATPortStride(mappings: PortMapping[]) {
  const sourcePorts = mappings
    .map((mapping) => Math.round(Number(mapping.host_port) || 0))
    .filter((port) => port > 0)
  if (sourcePorts.length === 0) return 1
  return Math.max(...sourcePorts) - Math.min(...sourcePorts) + 1
}

function validateBatchNATPortMappings(mappings: PortMapping[], managementPort: number, batchCount: number, portMappingCount?: number) {
  if (mappings.length === 0 && managementPort === 0) return ''
  if (managementPort < 0 || managementPort > 65535) {
    return 'SSH/RDP 公网源端口必须在 1-65535 之间，留空则自动分配'
  }
  if (mappings.length > 63) return '每个容器最多可配置 63 条自定义 NAT 映射'

  const used = new Map<string, string>()
  const stride = batchNATEffectiveStride(mappings, managementPort, portMappingCount)
  const autoCount = Math.min(Math.max(Math.round(Number(portMappingCount) || 2), 2), 64)
  for (let batchIndex = 0; batchIndex < batchCount; batchIndex++) {
    if (managementPort > 0) {
      const expandedManagementPort = managementPort + batchIndex * stride
      const maxPort = mappings.length === 0 ? expandedManagementPort + autoCount - 1 : expandedManagementPort
      if (maxPort > 65535) {
        return '批量展开后的 SSH/RDP 公网源端口超出 1-65535'
      }
      const managementKey = `${expandedManagementPort}/tcp`
      const managementOwner = used.get(managementKey)
      if (managementOwner) {
        return `批量端口冲突：${managementKey} 同时被 ${managementOwner} 和第 ${batchIndex + 1} 台容器的管理端口使用`
      }
      used.set(managementKey, `第 ${batchIndex + 1} 台容器的管理端口`)
    }
    for (let mappingIndex = 0; mappingIndex < mappings.length; mappingIndex++) {
      const mapping = mappings[mappingIndex]
      const hostPort = Math.round(Number(mapping.host_port) || 0) + batchIndex * stride
      const containerPort = Math.round(Number(mapping.container_port) || 0)
      const protocol = (mapping.protocol || 'tcp').toLowerCase() === 'udp' ? 'udp' : 'tcp'
      if (hostPort < 1 || hostPort > 65535) {
        return `第 ${mappingIndex + 1} 条映射展开后的公网端口超出 1-65535`
      }
      if (containerPort < 1 || containerPort > 65535) {
        return `第 ${mappingIndex + 1} 条映射的容器端口必须在 1-65535 之间`
      }
      const key = `${hostPort}/${protocol}`
      const owner = used.get(key)
      if (owner) {
        return `批量端口冲突：${key} 同时被 ${owner} 和第 ${batchIndex + 1} 台容器使用`
      }
      used.set(key, `第 ${batchIndex + 1} 台容器`)
    }
  }
  return ''
}

function isIPv4Address(value: string) {
  const parts = value.trim().split('.')
  return parts.length === 4 && parts.every((part) => {
    if (!/^\d+$/.test(part)) return false
    const n = Number(part)
    return n >= 0 && n <= 255
  })
}

function splitAddressLines(value: string) {
  return value
    .split(/[\n,，\s]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function subnetMaskFromPrefixLen(prefixLen: number) {
  if (!Number.isFinite(prefixLen) || prefixLen < 0 || prefixLen > 32) return '255.255.255.0'
  const mask = prefixLen === 0 ? 0 : (0xffffffff << (32 - prefixLen)) >>> 0
  return [24, 16, 8, 0].map((shift) => (mask >>> shift) & 255).join('.')
}

function prefixLenFromSubnetMask(mask: string) {
  if (!isIPv4Address(mask)) return 0
  const bits = mask.split('.').map((part) => Number(part).toString(2).padStart(8, '0')).join('')
  if (!/^1*0*$/.test(bits)) return 0
  return bits.indexOf('0') === -1 ? 32 : bits.indexOf('0')
}

const createNetworkText = {
  zh: {
    publicIPv4: '公网 IPv4',
    noAllocatableIPv4: '未检测到可分配公网 IPv4',
    publicIPv6: '可分配 IPv6 前缀',
    use: '使用',
    checkingIPv6Prefix: '正在检测 IPv6 前缀...',
    publicNAT: '公网 NAT',
    noNATPorts: '不分配 NAT 端口',
  },
  en: {
    publicIPv4: 'Public IPv4',
    noAllocatableIPv4: 'No allocatable public IPv4 detected',
    publicIPv6: 'Allocatable IPv6 Prefix',
    use: 'Use',
    checkingIPv6Prefix: 'Checking IPv6 prefix...',
    publicNAT: 'Public NAT',
    noNATPorts: 'No NAT ports will be assigned',
  },
} as const

function formatIPv6StatusReason(reason: string | undefined, language: Language, fallback: string) {
  if (!reason) return fallback
  if (reason.includes('/128 single-address IPv6 is not assignable')) {
    return language === 'en'
      ? 'No allocatable IPv6 prefix. The host only has a /128 single IPv6 address.'
      : '未检测到可分配 IPv6 前缀；宿主机只有 /128 单个 IPv6 地址，不能分配给容器。'
  }
  if (reason.includes('outbound IPv6 connectivity test failed')) {
    return language === 'en'
      ? reason
      : '宿主机检测到 IPv6 前缀，但 IPv6 出站连通性测试失败。'
  }
  return reason
}

function formatAllocatableIPv4Count(count: number, language: Language) {
  return language === 'en'
    ? `${count} allocatable address${count === 1 ? '' : 'es'} detected`
    : `检测到 ${count} 个可分配地址`
}

function formatNATPortCount(count: number, language: Language) {
  return language === 'en'
    ? `${count} NAT ports will be assigned`
    : `将分配 ${count} 个 NAT 端口`
}

function symmetricLimit(a: number, b: number) {
  const left = Math.max(0, Number(a) || 0)
  const right = Math.max(0, Number(b) || 0)
  if (left === right) return left
  if (left === 0) return right
  if (right === 0) return left
  return Math.min(left, right)
}

const inputClass = 'h-9 min-w-0 w-full px-3 py-2 text-sm'
