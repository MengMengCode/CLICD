import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { ArrowLeft, ArrowRight, CalendarClock, Check, Plus, RefreshCw, Trash2, X } from 'lucide-react'
import { useNavigate } from 'react-router'
import { batchCreate, getIPv6Status, getEnabledImages, getHostInfo, getHostReport, getRoutingInfo, getStorageInfo, CreateContainerRequest, HostInfo, HostProbeReport, IPv6Status, PortMapping, RoutingInfo, StorageInfo, Template } from '../services/api'
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
  const [nameError, setNameError] = useState('')

  useEffect(() => {
    if (isOpen) setCurrentStep(0)
  }, [isOpen])

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
          })
        })
      })
      .catch(console.error)

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
  const ipv6Prefixes = ipv6Status?.prefixes || []
  const ipv6Prefix = ipv6Prefixes.length > 1 ? `${ipv6Prefixes.length} prefixes configured` : (ipv6Prefixes[0]?.prefix || '')
  const publicIPv4s = hostInfo?.network.public_ipv4_addresses || []
  const ipv4Available = publicIPv4s.length > 0
  const manualIPv4s = form.public_ipv4s || []
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
  const resourceErrors = validateResourceInputs(form, maxVCPU, maxRAMMB, maxDiskGB)
  const lanIPv4Enabled = form.lan_ipv4_mode === 'dhcp' || form.lan_ipv4_mode === 'static'
  const lanStaticEnabled = form.lan_ipv4_mode === 'static'
  const natEnabled = form.assign_nat !== false && !lanIPv4Enabled
  const lanInterfaces = useMemo(() => getLANDHCPInterfaces(hostReport), [hostReport])
  const defaultLANInterface = lanInterfaces[0]?.name || ''
  const customNATMappings = form.nat_port_mappings || []
  const natPortCount = natEnabled
    ? (customNATMappings.length > 0 ? customNATMappings.length + 1 : Math.max(2, form.port_mapping_count || 2))
    : 0
  const linuxTemplate = !isWindowsTemplate(form.template_id)
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
  const selectedTemplate = templates.find((template) => template.id === form.template_id)
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
      const authError = validateSSHAuthInputs(form)
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
      if (!form.assign_ipv4 && !form.assign_ipv6 && form.assign_nat === false && form.lan_ipv4_mode !== 'dhcp' && form.lan_ipv4_mode !== 'static') {
        dialog.alert(t('网络配置有误'), t('请至少启用一种网络连接方式'))
        return false
      }
      if (form.lan_ipv4_mode === 'static' && (!isIPv4Address(form.lan_ipv4_address || '') || !isIPv4Address(form.lan_ipv4_gateway || '') || !form.lan_ipv4_prefix_len)) {
        dialog.alert(t('局域网 IPv4 配置有误'), t('请填写有效的 IPv4 地址、子网掩码和网关'))
        return false
      }
      const natMappingError = natEnabled
        ? validateBatchNATPortMappings(customNATMappings, managementPort, batchCount)
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

    if (!form.assign_ipv4 && !form.assign_ipv6 && form.assign_nat === false && form.lan_ipv4_mode !== 'dhcp' && form.lan_ipv4_mode !== 'static') {
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
      ? validateBatchNATPortMappings(customNATMappings, managementPort, batchCount)
      : ''
    if (natMappingError) {
      dialog.alert('NAT 端口配置有误', natMappingError)
      return
    }

    const authError = validateSSHAuthInputs(form)
    if (authError) {
      dialog.alert('登录方式有误', authError)
      return
    }

    const boundedForm = normalizeCreateForm(form)
    const wantsNAT = boundedForm.assign_nat !== false

    // Build batch of containers
    const containers: CreateContainerRequest[] = []
    const startIndex = batchStartIndex
    for (let i = 0; i < batchCount; i++) {
      const name = batchCount > 1 ? `${boundedForm.name}-${startIndex + i}` : boundedForm.name
      const expandedNAT = wantsNAT
        ? expandBatchNATConfig(boundedForm.nat_port_mappings || [], boundedForm.management_port || 0, i)
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
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="flex max-h-[92vh] w-full max-w-5xl flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl">
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200">
          <h2 className="text-lg font-semibold text-black">创建新容器</h2>
          <button onClick={onClose} className="p-1 hover:bg-gray-100 rounded text-gray-500" title="关闭">
            <X className="w-5 h-5" />
          </button>
        </div>

        <nav aria-label={t('创建步骤')} className="border-b border-gray-200 px-5 py-3">
          <ol className="grid grid-cols-4 gap-2">
            {wizardSteps.map((label, index) => {
              const completed = index < currentStep
              const active = index === currentStep
              return (
                <li key={label} className="min-w-0">
                  <button
                    type="button"
                    disabled={index > currentStep}
                    onClick={() => setCurrentStep(index)}
                    aria-current={active ? 'step' : undefined}
                    className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors disabled:cursor-default ${
                      active ? 'bg-gray-100 text-black' : completed ? 'text-gray-700 hover:bg-gray-50' : 'text-gray-400'
                    }`}
                  >
                    <span className={`inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-xs font-semibold ${
                      active || completed ? 'border-black bg-black text-white' : 'border-gray-300 bg-white'
                    }`}>
                      {completed ? <Check className="h-3.5 w-3.5" /> : index + 1}
                    </span>
                    <span className="min-w-0 truncate text-xs font-medium sm:text-sm">{label}</span>
                  </button>
                </li>
              )
            })}
          </ol>
        </nav>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          <div className="space-y-3">
          {currentStep === 0 && (
            <>
          <div className="grid grid-cols-2 gap-3">
            <Field label="容器名称">
              <input
                type="text"
                value={form.name}
                onChange={(event) => handleNameChange(event.target.value)}
                className={`${inputClass} ${nameError ? 'border-red-400 focus:ring-red-400 focus:border-red-400' : ''}`}
                placeholder="my-container"
                required
              />
              {nameError && <p className="text-xs text-red-500 mt-1">{nameError}</p>}
            </Field>
            <Field label="批量创建数量">
              <NumberInput value={batchCount} min={1} max={50} onChange={(value) => setBatchCount(Math.max(1, value || 1))} />
            </Field>
          </div>
          {batchCount > 1 && <p className="text-xs text-gray-400">将创建 {batchCount} 个容器：{form.name}-{batchStartIndex} 至 {form.name}-{batchStartIndex + batchCount - 1}</p>}

          <Field label="虚拟化架构">
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() => setForm((prev) => applyTemplateDefaults({ ...prev, virtualization: 'lxc', template_id: '', storage_pool_id: '', allowed_image_ids: [], image_limit_configured: false }))}
                className={`rounded-md border px-3 py-2 text-sm font-medium transition-colors ${form.virtualization === 'lxc' ? 'border-black bg-black text-white' : 'border-gray-300 text-gray-700 hover:bg-gray-50'}`}
              >
                LXC 容器
              </button>
              <button
                type="button"
                disabled={!kvmAvailable}
                title={kvmAvailable ? '' : '当前宿主机不支持 KVM'}
                onClick={() => {
                  if (kvmAvailable) {
                    setForm((prev) => applyTemplateDefaults({ ...prev, virtualization: 'kvm', template_id: '', storage_pool_id: '', allowed_image_ids: [], image_limit_configured: false }))
                  }
                }}
                className={`rounded-md border px-3 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:border-gray-200 disabled:bg-gray-50 disabled:text-gray-400 ${form.virtualization === 'kvm' ? 'border-black bg-black text-white' : 'border-gray-300 text-gray-700 hover:bg-gray-50'}`}
              >
                KVM 虚拟机
              </button>
            </div>
          </Field>
            </>
          )}

          {currentStep === 1 && (
            <>
          <Field label="系统模板">
            {templates.length === 0 ? (
              <div className="text-sm text-amber-600 bg-amber-50 border border-amber-200 rounded-md px-3 py-2">
                暂无可用的{form.virtualization === 'kvm' ? ' KVM' : ' LXC'}系统镜像，请先在「镜像管理」中下载镜像模板。
              </div>
            ) : (
            <select
              value={form.template_id}
              onChange={(event) => {
                const templateID = event.target.value
                const allowed = new Set(form.allowed_image_ids || [])
                if (templateID) allowed.add(templateID)
                setForm(applyTemplateDefaults({ ...form, template_id: templateID, allowed_image_ids: Array.from(allowed), image_limit_configured: true }))
              }}
              className={inputClass}
            >
              {templates.map((template) => (
                <option key={template.id} value={template.id}>
                  {template.name}
                </option>
              ))}
            </select>
            )}

          </Field>
            </>
          )}

          {currentStep === 0 && (
          <Field label="存储磁盘">
            {storageLoading ? (
              <div className="flex items-center gap-2 rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-600">
                <RefreshCw className="h-4 w-4 animate-spin" />
                正在检查存储配置...
              </div>
            ) : storagePools.length > 0 ? (
              <select
                value={form.storage_pool_id || ''}
                onChange={(event) => setForm({ ...form, storage_pool_id: event.target.value })}
                className={inputClass}
              >
                <option value="">自动选择（默认盘优先，空间不足自动切换）</option>
                {storagePools.map((pool) => (
                  <option key={pool.id} value={pool.id}>
                    {pool.name} · {pool.mount_point || pool.path}
                  </option>
                ))}
              </select>
            ) : (
              <div className="flex items-center justify-between gap-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">
                <span>尚未开启{form.virtualization === 'kvm' ? ' KVM 磁盘' : ' LXC 容器'}存储，当前无法创建。</span>
                <button
                  type="button"
                  onClick={() => { onClose(); navigate('/storage') }}
                  className="shrink-0 rounded-md border border-amber-300 bg-white px-2.5 py-1.5 text-xs font-medium text-amber-800 hover:bg-amber-100"
                >
                  去开启
                </button>
              </div>
            )}
          </Field>
          )}

          {currentStep === 1 && templates.length > 0 && (
            <Field label="子用户可用镜像">
              <div className="rounded-md border border-gray-200 bg-gray-50 p-3">
                <div className="mb-2 text-xs text-gray-500">默认勾选当前系统；取消后，子用户也不能重装该系统。</div>
                <div className="grid gap-2 sm:grid-cols-2">
                  {templates.map((template) => {
                    const checked = (form.allowed_image_ids || []).includes(template.id)
                    const current = template.id === form.template_id
                    return (
                      <label key={template.id} className={`flex cursor-pointer items-start gap-2 rounded border px-2.5 py-2 text-xs ${checked ? 'border-black bg-white' : 'border-gray-200 bg-white hover:bg-gray-50'}`}>
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() => {
                            const currentIDs = form.allowed_image_ids || []
                            const next = checked ? currentIDs.filter((id) => id !== template.id) : [...currentIDs, template.id]
                            setForm({ ...form, allowed_image_ids: next, image_limit_configured: true })
                          }}
                          className="mt-0.5 h-4 w-4 rounded border-gray-300 text-black focus:ring-black"
                        />
                        <span className="min-w-0">
                          <span className="block truncate font-medium text-gray-800">{template.name}{current ? '（当前系统）' : ''}</span>
                          <span className="block text-gray-500">{template.arch} · {template.distro} {template.release}</span>
                        </span>
                      </label>
                    )
                  })}
                </div>
              </div>
            </Field>
          )}

          {currentStep === 0 && linuxTemplate && (
            <div className="rounded-md border border-gray-200 bg-white px-3 py-3 text-sm">
              <div className="mb-2 font-medium text-gray-800">登录方式</div>
              <div className="grid grid-cols-3 gap-2">
                {([
                  ['auto_password', '自动生成密码'],
                  ['password', '自定义密码'],
                  ['key', 'SSH Key'],
                ] as Array<[SSHAuthMode, string]>).map(([mode, label]) => (
                  <button
                    key={mode}
                    type="button"
                    onClick={() => setForm({ ...form, ssh_auth_mode: mode })}
                    className={`rounded-md border px-3 py-2 text-xs font-medium transition-colors ${sshAuthMode === mode ? 'border-black bg-black text-white' : 'border-gray-300 text-gray-700 hover:bg-gray-50'}`}
                  >
                    {label}
                  </button>
                ))}
              </div>
              {sshAuthMode === 'password' && (
                <div className="mt-3 flex gap-2">
                  <input
                    type="text"
                    value={form.ssh_password || ''}
                    onChange={(event) => setForm({ ...form, ssh_password: event.target.value })}
                    className={inputClass}
                    placeholder="RootPass123"
                  />
                  <button
                    type="button"
                    onClick={() => setForm({ ...form, ssh_password: generateSSHPassword() })}
                    className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-gray-300 text-gray-600 hover:bg-gray-50"
                    title="生成密码"
                  >
                    <RefreshCw className="h-4 w-4" />
                  </button>
                </div>
              )}
              {sshAuthMode === 'key' && (
                <textarea
                  value={form.ssh_public_key || ''}
                  onChange={(event) => setForm({ ...form, ssh_public_key: event.target.value })}
                  className={`${inputClass} mt-3 min-h-20 resize-y font-mono text-xs`}
                  placeholder="ssh-ed25519 AAAA..."
                />
              )}
            </div>
          )}

          {currentStep === 2 && (
          <div className="grid gap-3 lg:grid-cols-2">
          <div className={`rounded-md border px-3 py-2 text-sm ${ipv4Available ? 'border-gray-200 bg-white' : 'border-gray-200 bg-gray-50 text-gray-400'}`}>
            <label className="flex items-start gap-3">
              <input
                type="checkbox"
                checked={!!form.assign_ipv4}
                disabled={!ipv4Available}
                onChange={(event) => setForm({
                  ...form,
                  assign_ipv4: event.target.checked,
                  public_ipv4s: event.target.checked ? form.public_ipv4s : [],
                  ...(event.target.checked ? { assign_nat: false, port_mapping_count: 0, extra_ports: [], nat_port_mappings: [], management_port: 0, lan_ipv4_mode: '', lan_interface: '' } : {}),
                })}
                className="mt-1"
              />
              <span className="min-w-0">
                <span className="block font-medium text-gray-800">{networkText.publicIPv4}</span>
                <span className="block text-xs text-gray-500">
                  {ipv4Available ? formatAllocatableIPv4Count(publicIPv4s.length, language) : networkText.noAllocatableIPv4}
                </span>
              </span>
            </label>
            {form.assign_ipv4 && (
              <div className="mt-3 space-y-3 pl-6">
                <div className="grid grid-cols-2 gap-3">
                  <label className="flex items-center gap-2 text-xs text-gray-600">
                    <input
                      type="radio"
                      checked={manualIPv4s.length === 0}
                      onChange={() => setForm({ ...form, public_ipv4s: [] })}
                    />
                    Auto assign
                  </label>
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
                  <label className="flex items-center gap-2 text-xs text-gray-600">
                    <input
                      type="radio"
                      checked={manualIPv4s.length > 0}
                      onChange={() => setForm({ ...form, public_ipv4s: publicIPv4s[0]?.address ? [publicIPv4s[0].address] : [], ipv4_count: 1 })}
                    />
                    Manual select
                  </label>
                  {manualIPv4s.length > 0 && (
                    <div className="grid gap-1.5 sm:grid-cols-2">
                      {publicIPv4s.map((ip) => (
                        <label key={`${ip.interface}-${ip.address}`} className="flex min-w-0 items-center gap-2 rounded border border-gray-200 px-2 py-1.5 text-xs text-gray-700">
                          <input
                            type="checkbox"
                            checked={manualIPv4s.includes(ip.address)}
                            onChange={(event) => {
                              const next = event.target.checked
                                ? [...manualIPv4s, ip.address]
                                : manualIPv4s.filter((value) => value !== ip.address)
                              setForm({ ...form, public_ipv4s: next, ipv4_count: Math.max(1, next.length || 1) })
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
              </div>
            )}
          </div>

          <div className={`rounded-md border px-3 py-2 text-sm ${form.virtualization === 'lxc' && lanInterfaces.length > 0 ? 'border-gray-200 bg-white' : 'border-gray-200 bg-gray-50 text-gray-400'}`}>
            <div className="flex items-start justify-between gap-3">
              <label className="flex min-w-0 flex-1 items-start gap-3">
                <input
                  type="checkbox"
                  checked={lanIPv4Enabled}
                  disabled={form.virtualization !== 'lxc' || lanInterfaces.length === 0}
                  onChange={(event) => {
                    const checked = event.target.checked
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
                  <span className="block font-medium text-gray-800">局域网 DHCP</span>
                  <span className="block text-xs text-gray-500">
                    {lanInterfaces.length > 0 ? 'macvlan 独立局域网 IP' : '未检测到可用上联网卡'}
                  </span>
                </span>
              </label>
              {lanIPv4Enabled && (
                <select
                  value={form.lan_interface || defaultLANInterface}
                  onChange={(event) => setForm({ ...form, lan_interface: event.target.value })}
                  className="h-9 w-32 shrink-0 rounded-md border border-gray-300 bg-white px-2 text-xs text-gray-700 focus:outline-none focus:ring-1 focus:ring-black"
                >
                  {lanInterfaces.map((item) => (
                    <option key={item.name} value={item.name}>{item.name}</option>
                  ))}
                </select>
              )}
            </div>
            {lanIPv4Enabled && (
              <div className="mt-3 space-y-3 pl-6">
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    onClick={() => setForm({ ...form, lan_ipv4_mode: 'dhcp' })}
                    className={`rounded-md border px-3 py-2 text-xs font-medium ${form.lan_ipv4_mode === 'dhcp' ? 'border-black bg-black text-white' : 'border-gray-300 text-gray-700 hover:bg-gray-50'}`}
                  >
                    DHCP 自动获取
                  </button>
                  <button
                    type="button"
                    onClick={() => setForm({ ...form, lan_ipv4_mode: 'static' })}
                    className={`rounded-md border px-3 py-2 text-xs font-medium ${lanStaticEnabled ? 'border-black bg-black text-white' : 'border-gray-300 text-gray-700 hover:bg-gray-50'}`}
                  >
                    手动配置
                  </button>
                </div>
                {lanStaticEnabled && (
                  <div className="grid gap-3 sm:grid-cols-3">
                    <Field label="IPv4 地址">
                      <input
                        value={form.lan_ipv4_address || ''}
                        onChange={(event) => setForm({ ...form, lan_ipv4_address: event.target.value })}
                        className={inputClass}
                        placeholder="192.168.2.250"
                      />
                    </Field>
                    <Field label="子网掩码">
                      <input
                        value={subnetMaskFromPrefixLen(form.lan_ipv4_prefix_len || 24)}
                        onChange={(event) => setForm({ ...form, lan_ipv4_prefix_len: prefixLenFromSubnetMask(event.target.value) || 24 })}
                        className={inputClass}
                        placeholder="255.255.255.0"
                      />
                    </Field>
                    <Field label="网关">
                      <input
                        value={form.lan_ipv4_gateway || ''}
                        onChange={(event) => setForm({ ...form, lan_ipv4_gateway: event.target.value })}
                        className={inputClass}
                        placeholder="192.168.2.202"
                      />
                    </Field>
                  </div>
                )}
              </div>
            )}
          </div>

          <div className={`rounded-md border px-3 py-2 text-sm ${ipv6Available ? 'border-gray-200 bg-white' : 'border-gray-200 bg-gray-50 text-gray-400'}`}>
            <div className="flex items-start justify-between gap-3">
              <label className="flex min-w-0 flex-1 items-start gap-3">
                <input
                  type="checkbox"
                  checked={!!form.assign_ipv6}
                  disabled={!ipv6Available}
                  onChange={(event) => setForm({ ...form, assign_ipv6: event.target.checked })}
                  className="mt-1"
                />
                <span className="min-w-0">
              <span className="block font-medium text-gray-800">{networkText.publicIPv6}</span>
              <span className="block text-xs text-gray-500 truncate">
                    {ipv6Available ? `${networkText.use} ${ipv6Prefix}` : formatIPv6StatusReason(ipv6Status?.reason, language, networkText.checkingIPv6Prefix)}
              </span>
                </span>
              </label>
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
                <div className="grid grid-cols-2 gap-3">
                  <label className="flex items-center gap-2 text-xs text-gray-600">
                    <input
                      type="radio"
                      checked={(form.ipv6_addresses || []).length === 0}
                      onChange={() => setForm({ ...form, ipv6_addresses: [] })}
                    />
                    Random assign
                  </label>
                  <label className="flex items-center gap-2 text-xs text-gray-600">
                    <input
                      type="radio"
                      checked={(form.ipv6_addresses || []).length > 0}
                      onChange={() => setForm({ ...form, ipv6_addresses: [''], ipv6_count: 1 })}
                    />
                    Custom assign
                  </label>
                </div>
                {(form.ipv6_addresses || []).length > 0 && (
                  <textarea
                    value={(form.ipv6_addresses || []).join('\n')}
                    onChange={(event) => {
                      const next = splitAddressLines(event.target.value)
                      setForm({ ...form, ipv6_addresses: next.length ? next : [''], ipv6_count: Math.max(1, next.length || 1) })
                    }}
                    className={`${inputClass} min-h-20 font-mono text-xs`}
                    placeholder="2001:db8:100::100"
                  />
                )}
              </div>
            )}
          </div>

          <div className="rounded-md border border-gray-200 bg-white px-3 py-2 text-sm">
            <div className="flex items-start justify-between gap-3">
              <label className="flex min-w-0 flex-1 items-start gap-3">
                <input
                  type="checkbox"
                  checked={natEnabled}
                  onChange={(event) => {
                    const checked = event.target.checked
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
                  <span className="block font-medium text-gray-800">{networkText.publicNAT}</span>
                  <span className="block text-xs text-gray-500">
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
              </label>
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
              <div className="mt-2 space-y-2 pl-6">
                <div className="space-y-1">
                  <div className="grid grid-cols-[minmax(0,1fr)_20px_minmax(0,1fr)_56px] items-end gap-2">
                    <label className="min-w-0 text-[11px] text-gray-500">
                      <span className="mb-1 block">
                        {language === 'en'
                          ? `${isWindowsTemplate(form.template_id) ? 'RDP' : 'SSH'} public source port`
                          : `${isWindowsTemplate(form.template_id) ? 'RDP' : 'SSH'} 公网源端口`}
                      </span>
                      <input
                        type="number"
                        min={1}
                        max={65535}
                        value={managementPort || ''}
                        onChange={(event) => setForm({
                          ...form,
                          management_port: Number(event.target.value) || 0,
                          assign_nat: true,
                        })}
                        className={`${inputClass} min-w-0 font-mono text-xs`}
                        placeholder={language === 'en' ? 'Auto' : '自动'}
                      />
                    </label>
                    <ArrowRight className="mb-3 h-4 w-4 text-gray-400" />
                    <label className="min-w-0 text-[11px] text-gray-500">
                      <span className="mb-1 block">{language === 'en' ? 'Container target port' : '容器目标端口'}</span>
                      <input
                        type="number"
                        readOnly
                        value={isWindowsTemplate(form.template_id) ? 3389 : 22}
                        className={`${inputClass} min-w-0 bg-gray-50 font-mono text-xs text-gray-500`}
                      />
                    </label>
                    <span className="mb-1 inline-flex h-9 items-center justify-center rounded-md border border-gray-200 bg-gray-50 text-xs text-gray-600">
                      TCP
                    </span>
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <label className="flex items-center gap-2 text-xs text-gray-600">
                    <input
                      type="radio"
                      checked={customNATMappings.length === 0}
                      onChange={() => setForm({ ...form, extra_ports: [], nat_port_mappings: [], port_mapping_count: Math.max(2, form.port_mapping_count || 2) })}
                    />
                    {language === 'en' ? 'Auto ports' : '自动端口'}
                  </label>
                  <label className="flex items-center gap-2 text-xs text-gray-600">
                    <input
                      type="radio"
                      checked={customNATMappings.length > 0}
                      disabled={!routingInfo}
                      onChange={() => {
                        const suggestedPort = autoPortMappings[0]?.host_port || natAllocationPreview.managementPort
                        if (!suggestedPort) return
                        const next = customNATMappings.length > 0
                          ? customNATMappings
                          : [{ host_port: suggestedPort, container_port: suggestedPort, protocol: 'tcp', description: `Port-${suggestedPort}` }]
                        setForm({ ...form, extra_ports: [], nat_port_mappings: next, port_mapping_count: next.length + 1, assign_nat: true })
                      }}
                    />
                    {language === 'en' ? 'Custom mappings' : '自定义映射'}
                  </label>
                </div>
                {customNATMappings.length > 0 && (
                  <div className="space-y-2">
                    <div className="grid grid-cols-[minmax(0,1fr)_20px_minmax(0,1fr)_72px_32px] items-center gap-2 px-1 text-[11px] text-gray-500">
                      <span>{language === 'en' ? 'Public source port' : '源端口（公网）'}</span>
                      <span />
                      <span>{language === 'en' ? 'Container target port' : '目标端口（容器）'}</span>
                      <span>{language === 'en' ? 'Protocol' : '协议'}</span>
                      <span />
                    </div>
                    {customNATMappings.map((mapping, index) => (
                      <div key={index} className="grid grid-cols-[minmax(0,1fr)_20px_minmax(0,1fr)_72px_32px] items-center gap-2">
                        <input
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
                          className={`${inputClass} min-w-0 font-mono text-xs`}
                          placeholder="30080"
                        />
                        <ArrowRight className="h-4 w-4 text-gray-400" />
                        <input
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
                          className={`${inputClass} min-w-0 font-mono text-xs`}
                          placeholder="80"
                        />
                        <select
                          value={mapping.protocol || 'tcp'}
                          onChange={(event) => {
                            const next = customNATMappings.map((item, itemIndex) => itemIndex === index
                              ? { ...item, protocol: event.target.value }
                              : item)
                            setForm({ ...form, nat_port_mappings: next })
                          }}
                          className="h-10 rounded-md border border-gray-300 bg-white px-2 text-xs text-gray-700"
                        >
                          <option value="tcp">TCP</option>
                          <option value="udp">UDP</option>
                        </select>
                        <button
                          type="button"
                          disabled={customNATMappings.length <= 1}
                          onClick={() => {
                            const next = customNATMappings.filter((_, itemIndex) => itemIndex !== index)
                            setForm({ ...form, nat_port_mappings: next, port_mapping_count: next.length + 1 })
                          }}
                          className="inline-flex h-8 w-8 items-center justify-center rounded text-gray-400 hover:bg-red-50 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-30"
                          title={language === 'en' ? 'Remove mapping' : '删除映射'}
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    ))}
                    <button
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
                      className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1.5 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40"
                    >
                      <Plus className="h-3.5 w-3.5" />
                      {language === 'en' ? 'Add mapping' : '添加映射'}
                    </button>
                    {batchCount > 1 && (
                      <p className="text-[11px] text-gray-500">
                        {language === 'en'
                          ? 'Each later container starts after the previous highest public port; target ports stay unchanged.'
                          : '后续容器从上一台的最高公网端口之后开始，容器内部端口保持不变。'}
                      </p>
                    )}
                  </div>
                )}
                <div className="flex flex-wrap gap-1.5">
                  <span className="inline-flex px-2 py-1 bg-emerald-50 text-emerald-700 rounded text-xs font-mono">
                    {isWindowsTemplate(form.template_id) ? 'RDP' : 'SSH'}: {sshPortPreview || '--'} -&gt; {isWindowsTemplate(form.template_id) ? 3389 : 22}
                    {managementPort === 0 ? (language === 'en' ? ' (auto)' : '（自动）') : ''}
                  </span>
                  {natPreviewMappings.map((mapping, index) => (
                    <span key={`${mapping.host_port}-${mapping.container_port}-${mapping.protocol}-${index}`} className="inline-flex px-2 py-1 bg-gray-100 text-gray-700 rounded text-xs font-mono">
                      {mapping.host_port} -&gt; {mapping.container_port}/{mapping.protocol.toUpperCase()}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
          </div>
          )}

          {currentStep === 0 && (
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <Field label="vCPU">
              <NumberInput
                value={form.vcpu}
                min={form.virtualization === 'kvm' ? 1 : 0.25}
                max={maxVCPU}
                step={form.virtualization === 'kvm' ? 1 : 0.25}
                invalid={!!resourceErrors.vcpu}
                onChange={(value) => setForm({ ...form, vcpu: value })}
              />
              {resourceErrors.vcpu && <p className="mt-1 text-xs text-red-500">{resourceErrors.vcpu}</p>}
            </Field>
            <Field label="内存 (MB)">
              <NumberInput
                value={form.ram_mb}
                min={128}
                max={maxRAMMB}
                step={128}
                invalid={!!resourceErrors.ram_mb}
                onChange={(value) => setForm({ ...form, ram_mb: value })}
              />
              {resourceErrors.ram_mb && <p className="mt-1 text-xs text-red-500">{resourceErrors.ram_mb}</p>}
            </Field>
            <Field label="磁盘 (GB)">
              <NumberInput
                value={form.disk_gb}
                min={1}
                max={maxDiskGB}
                invalid={!!resourceErrors.disk_gb}
                onChange={(value) => setForm({ ...form, disk_gb: value })}
              />
              {resourceErrors.disk_gb && <p className="mt-1 text-xs text-red-500">{resourceErrors.disk_gb}</p>}
            </Field>
            <Field label="下行带宽 (Mbps)">
              <NumberInput value={form.network_down_mbps} min={0} onChange={(value) => setForm({ ...form, network_down_mbps: value, network_bw_mbps: symmetricLimit(value, form.network_up_mbps) })} />
            </Field>
            <Field label="上行带宽 (Mbps)">
              <NumberInput value={form.network_up_mbps} min={0} onChange={(value) => setForm({ ...form, network_up_mbps: value, network_bw_mbps: symmetricLimit(form.network_down_mbps, value) })} />
            </Field>
            <Field label="读取 IO (MB/s)">
              <NumberInput value={form.io_read_mbps} min={0} onChange={(value) => setForm({ ...form, io_read_mbps: value, io_speed_mbps: symmetricLimit(value, form.io_write_mbps) })} />
            </Field>
            <Field label="写入 IO (MB/s)">
              <NumberInput value={form.io_write_mbps} min={0} onChange={(value) => setForm({ ...form, io_write_mbps: value, io_speed_mbps: symmetricLimit(form.io_read_mbps, value) })} />
            </Field>
            <div>
              <div className="mb-1.5 flex items-center justify-between gap-2">
                <label className="text-sm font-medium text-gray-700">月流量</label>
                <select
                  value={form.traffic_mode}
                  onChange={(e) => setForm({ ...form, traffic_mode: e.target.value })}
                  className="h-8 px-2 border border-gray-300 rounded text-xs text-gray-600 bg-white"
                >
                  <option value="total">双向统计</option>
                  <option value="in_out">入/出分离</option>
                </select>
              </div>
              {form.traffic_mode === 'total' ? (
                <div className="flex items-center gap-2">
                  <NumberInput value={form.monthly_traffic_gb} min={0} onChange={(value) => setForm({ ...form, monthly_traffic_gb: value })} />
                  <span className="shrink-0 text-xs text-gray-400">GB</span>
                </div>
              ) : (
                <div className="grid grid-cols-2 gap-2">
                  <Field label="入站 (GB)">
                    <NumberInput value={form.traffic_in_gb} min={0} onChange={(value) => setForm({ ...form, traffic_in_gb: value || 0 })} />
                  </Field>
                  <Field label="出站 (GB)">
                    <NumberInput value={form.traffic_out_gb} min={0} onChange={(value) => setForm({ ...form, traffic_out_gb: value || 0 })} />
                  </Field>
                </div>
              )}
            </div>
            <Field label="子用户快照上限">
              <NumberInput
                value={form.snapshot_limit}
                min={1}
                max={999}
                onChange={(value) => setForm({ ...form, snapshot_limit: Math.max(1, Math.round(value || 1)) })}
              />
            </Field>
            <Field label="到期时间">
              <div className="relative">
                <CalendarClock className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
                <input
                  type="date"
                  value={form.expires_at}
                  onChange={(event) => setForm({ ...form, expires_at: event.target.value })}
                  min={new Date().toISOString().slice(0, 10)}
                  className={`${inputClass} pl-10`}
                />
              </div>
              <p className="mt-1 text-[11px] leading-4 text-gray-400">不选则长期有效</p>
            </Field>
          </div>
          )}

          {currentStep === 3 && (
            <div className="space-y-5">
              <section>
                <h3 className="mb-2 text-sm font-semibold text-gray-900">{t('基础信息')}</h3>
                <dl className="grid grid-cols-1 border-y border-gray-200 sm:grid-cols-2 lg:grid-cols-4">
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
                <h3 className="mb-2 text-sm font-semibold text-gray-900">{t('镜像与登录')}</h3>
                <dl className="grid grid-cols-1 border-y border-gray-200 sm:grid-cols-3">
                  <ReviewItem label={t('系统镜像')} value={selectedTemplate?.name || '-'} />
                  <ReviewItem label={t('登录方式')} value={
                    !linuxTemplate
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
                      <span key={template.id} className="rounded bg-gray-100 px-2 py-1 text-xs text-gray-700">
                        {template.name}
                      </span>
                    ))}
                  </div>
                )}
              </section>

              <section>
                <h3 className="mb-2 text-sm font-semibold text-gray-900">{t('网络配置')}</h3>
                <dl className="grid grid-cols-1 border-y border-gray-200 sm:grid-cols-2">
                  <ReviewItem label={t('主要网络')} value={networkSummary} />
                  <ReviewItem
                    label={networkText.publicIPv6}
                    value={form.assign_ipv6 ? `${form.ipv6_count || 1} ${t('个地址')}` : t('未启用')}
                  />
                </dl>
                {natEnabled && (
                  <div className="mt-3">
                    <div className="mb-1.5 text-xs font-medium text-gray-500">{t('端口映射')}</div>
                    <div className="flex flex-wrap gap-1.5">
                      <span className="rounded bg-emerald-50 px-2 py-1 font-mono text-xs text-emerald-700">
                        {isWindowsTemplate(form.template_id) ? 'RDP' : 'SSH'}: {sshPortPreview || t('自动')} -&gt; {isWindowsTemplate(form.template_id) ? 3389 : 22}/TCP
                      </span>
                      {natPreviewMappings.map((mapping, index) => (
                        <span key={`${mapping.host_port}-${mapping.container_port}-${mapping.protocol}-${index}`} className="rounded bg-gray-100 px-2 py-1 font-mono text-xs text-gray-700">
                          {mapping.host_port || t('自动')} -&gt; {mapping.container_port}/{mapping.protocol.toUpperCase()}
                        </span>
                      ))}
                    </div>
                  </div>
                )}
              </section>
            </div>
          )}
          </div>
        </div>

        <div className="flex items-center justify-between gap-3 border-t border-gray-200 px-6 py-4">
          <button onClick={onClose} className="px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 rounded-md transition-colors">
            {t('取消')}
          </button>
          <div className="flex items-center gap-2">
            {currentStep > 0 && (
              <button
                type="button"
                onClick={() => setCurrentStep((step) => Math.max(0, step - 1))}
                className="inline-flex items-center gap-2 rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-50"
              >
                <ArrowLeft className="h-4 w-4" />
                {t('上一步')}
              </button>
            )}
            {currentStep < wizardSteps.length - 1 ? (
              <button
                type="button"
                onClick={handleNextStep}
                disabled={currentStep === 0 && storageLoading}
                className="inline-flex items-center gap-2 rounded-md bg-black px-4 py-2 text-sm text-white transition-colors hover:bg-gray-800 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {t('下一步')}
                <ArrowRight className="h-4 w-4" />
              </button>
            ) : (
              <button
                type="button"
                onClick={handleSubmit}
                disabled={loading}
                className="inline-flex items-center gap-2 rounded-md bg-black px-4 py-2 text-sm text-white transition-colors hover:bg-gray-800 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {loading ? t('创建中...') : t('确认创建')}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function ReviewItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 border-b border-gray-100 px-3 py-2.5 last:border-b-0 sm:border-b-0 sm:border-r sm:last:border-r-0">
      <dt className="text-xs text-gray-500">{label}</dt>
      <dd className="mt-1 break-words text-sm font-medium text-gray-800">{value || '-'}</dd>
    </div>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 mb-1.5">{label}</label>
      {children}
    </div>
  )
}

function NumberInput({
  value,
  min,
  max,
  step,
  invalid,
  onChange,
}: {
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
    <input
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
      className={`${inputClass} ${invalid ? 'border-red-400 focus:border-red-400 focus:ring-red-400' : ''}`}
    />
  )
}

function validateResourceInputs(form: CreateContainerRequest, maxVCPU: number, maxRAMMB?: number, maxDiskGB?: number) {
  const errors: Partial<Record<'vcpu' | 'ram_mb' | 'disk_gb', string>> = {}
  const windows = isWindowsTemplate(form.template_id)
  const windows11 = form.template_id.toLowerCase().includes('windows-11')
  const minVCPU = windows ? 2 : (form.virtualization === 'kvm' ? 1 : 0.25)
  const minRAMMB = windows11 ? 4096 : windows ? 2048 : 128
  const minDiskGB = windows11 ? 64 : windows ? 30 : 1

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

function normalizeCreateForm(form: CreateContainerRequest): CreateContainerRequest {
  const normalized = applyTemplateDefaults(form)
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
  const linuxTemplate = !isWindowsTemplate(normalized.template_id)
  const sshAuthMode = linuxTemplate ? (normalized.ssh_auth_mode || 'auto_password') : 'auto_password'
  return {
    ...normalized,
    vcpu: normalized.virtualization === 'kvm' ? Math.round(normalized.vcpu) : normalizeLXCvCPU(normalized.vcpu),
    ram_mb: Math.round(normalized.ram_mb),
    disk_gb: Math.round(normalized.disk_gb),
    assign_nat: wantsNAT,
    port_mapping_count: portMappingCount,
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
    ssh_auth_mode: sshAuthMode,
    ssh_password: linuxTemplate && sshAuthMode === 'password' ? (normalized.ssh_password || '').trim() : '',
    ssh_public_key: linuxTemplate && sshAuthMode === 'key' ? (normalized.ssh_public_key || '').trim() : '',
    snapshot_limit: clampInt(normalized.snapshot_limit, 1, undefined, 3),
  }
}

function validateSSHAuthInputs(form: CreateContainerRequest) {
  if (isWindowsTemplate(form.template_id)) return ''
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

function applyTemplateDefaults(form: CreateContainerRequest): CreateContainerRequest {
  if (!isWindowsTemplate(form.template_id)) return form
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

function expandBatchNATConfig(mappings: PortMapping[], managementPort: number, batchIndex: number) {
  const stride = batchNATPortStride(batchNATSourceMappings(mappings, managementPort))
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

function validateBatchNATPortMappings(mappings: PortMapping[], managementPort: number, batchCount: number) {
  if (mappings.length === 0 && managementPort === 0) return ''
  if (managementPort < 0 || managementPort > 65535) {
    return 'SSH/RDP 公网源端口必须在 1-65535 之间，留空则自动分配'
  }
  if (mappings.length > 63) return '每个容器最多可配置 63 条自定义 NAT 映射'

  const used = new Map<string, string>()
  const stride = batchNATPortStride(batchNATSourceMappings(mappings, managementPort))
  for (let batchIndex = 0; batchIndex < batchCount; batchIndex++) {
    if (managementPort > 0) {
      const expandedManagementPort = managementPort + batchIndex * stride
      if (expandedManagementPort > 65535) {
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

const inputClass =
  'w-full px-3 py-2 border border-gray-300 rounded-md text-sm text-black bg-white focus:outline-none focus:ring-2 focus:ring-black focus:border-black'
