import axios from 'axios'

const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Request interceptor to add auth token
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('clicd_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor to handle auth errors
api.interceptors.response.use(
  (response) => response,
  (error) => {
    const requestURL = String(error.config?.url || '')
    const isLoginRequest = ['/login', '/sub-user/login', '/sub-user/access']
      .some((path) => requestURL === path || requestURL.endsWith(path))
    if (error.response?.status === 401 && !isLoginRequest) {
      localStorage.removeItem('clicd_token')
      localStorage.removeItem('clicd_username')
      if (window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
    }
    return Promise.reject(error)
  }
)

export interface LoginResponse {
  token: string
  username: string
}

export type ContainerIdentifier = number | string

export interface PortMapping {
  container_port: number
  host_port: number
  host_ip?: string
  protocol: string
  description: string
}

export interface FirewallRule {
  id: string
  network?: 'ipv4' | 'ipv6' | 'all'
  direction: 'in' | 'out'
  protocol: 'tcp' | 'udp' | 'icmp' | 'all'
  port: string
  source_ip: string
  action: 'ACCEPT' | 'DROP'
  description: string
  enabled: boolean
}

export interface PublicIPv4Assignment {
  address: string
  interface?: string
  prefix_len?: number
  gateway?: string
}

export interface IPv6Assignment {
  address: string
  prefix_len: number
  interface?: string
}

export interface Container {
  id: number
  uuid: string
  name: string
  virtualization?: string
  storage_pool_id?: string
  storage_path?: string
  template: string
  vcpu: number
  ram_mb: number
  disk_gb: number
  network_bw_mbps: number
  network_down_mbps: number
  network_up_mbps: number
  monthly_traffic_gb: number
  traffic_mode: string
  traffic_in_gb: number
  traffic_out_gb: number
  traffic_used_rx: number
  traffic_used_tx: number
  traffic_reset_date: string
  io_speed_mbps: number
  io_read_mbps: number
  io_write_mbps: number
  status: string
  ip: string
  lan_ipv4_mode?: string
  lan_interface?: string
  lan_ipv4_address?: string
  lan_ipv4_prefix_len?: number
  lan_ipv4_gateway?: string
  mac_address?: string
  public_ipv4s?: PublicIPv4Assignment[]
  ipv6: string
  ipv6_prefix_len: number
  ipv6_interface: string
  ipv6_addresses?: IPv6Assignment[]
  vnc_port: number
  ssh_port: number
  ssh_password: string
  port_mappings: PortMapping[]
  port_mapping_limit: number
  firewall_enabled: boolean
  firewall_default_action: 'ACCEPT' | 'DROP'
  firewall_rules: FirewallRule[]
  snapshot_limit: number
  created_at: string
  expires_at: string
  snapshot_schedule_enabled: boolean
  snapshot_schedule_interval_hours: number
  snapshot_schedule_time: string
  snapshot_schedule_last_run: string
  snapshot_schedule_next_run: string
  snapshot_schedule_created_by: string
  policy_blocked?: boolean
  policy_blocked_reason?: string
  policy_blocked_at?: string
}

export interface Template {
  id: string
  name: string
  type?: string
  distro: string
  release: string
  arch: string
  variant?: string
  desktop?: string
  description: string
}

export interface CreateContainerRequest {
  name: string
  virtualization: string
  template_id: string
  storage_pool_id?: string
  vcpu: number
  cpu_percent: number
  ram_mb: number
  disk_gb: number
  network_bw_mbps: number
  network_down_mbps: number
  network_up_mbps: number
  monthly_traffic_gb: number
  traffic_mode: string
  traffic_in_gb: number
  traffic_out_gb: number
  io_speed_mbps: number
  io_read_mbps: number
  io_write_mbps: number
  extra_ports: number[]
  nat_port_mappings?: PortMapping[]
  management_port?: number
  port_mapping_count: number
  assign_nat?: boolean
  lan_ipv4_mode?: string
  lan_interface?: string
  lan_ipv4_address?: string
  lan_ipv4_prefix_len?: number
  lan_ipv4_gateway?: string
  snapshot_limit: number
  assign_ipv4?: boolean
  ipv4_count?: number
  public_ipv4s?: string[]
  assign_ipv6: boolean
  ipv6_count?: number
  ipv6_addresses?: string[]
  ssh_auth_mode?: string
  ssh_password?: string
  ssh_public_key?: string
  allowed_image_ids?: string[]
  image_limit_configured?: boolean
  expires_at: string
}

export interface StoragePool {
  id: string
  name: string
  path: string
  content_types: string[]
  default_contents?: string[]
  enabled: boolean
  available?: boolean
  exists?: boolean
  size_bytes?: number
  used_bytes?: number
  free_bytes?: number
  mount_point?: string
  clicd_used_bytes?: number
  content_usage?: StorageContentUsage[]
  error?: string
}

export interface StorageContentUsage {
  content_type: string
  size_bytes: number
}

export interface StorageDisk {
  name: string
  path: string
  type: string
  fstype: string
  mount_point: string
  model: string
  size_bytes: number
  used_bytes: number
  free_bytes: number
  storage_pool_id?: string
  storage_path?: string
  clicd_used_bytes?: number
  content_usage?: StorageContentUsage[]
}

export interface StorageInfo {
  pools: StoragePool[]
  disks: StorageDisk[]
  content_types: string[]
}

export interface ReinstallContainerOptions {
  ssh_auth_mode?: string
  ssh_password?: string
  ssh_public_key?: string
}

export interface IPv6PrefixInfo {
  interface: string
  address: string
  prefix: string
  prefix_len: number
  gateway: string
  is_tunnel?: boolean
  source?: string
}

export interface IPv6Status {
  available: boolean
  reachable: boolean
  reason: string
  prefixes: IPv6PrefixInfo[]
}

export interface PublicIPv4Info {
  interface: string
  address: string
  prefix: string
  prefix_len?: number
  subnet_mask?: string
  gateway?: string
  is_tunnel?: boolean
  source?: string
}

export interface IPv4PrefixInfo {
  interface: string
  address: string
  prefix: string
  prefix_len: number
  subnet_mask: string
  gateway: string
  source: string
}

export interface DashboardStats {
  total_containers: number
  running: number
  stopped: number
}

export interface HostInfo {
  cpu: { cores: number; usage_pct: number }
  ram: { total_mb: number; used_mb: number; free_mb: number }
  disk: { total_gb: number; used_gb: number; free_gb: number }
  network: {
    rx_bytes: number
    tx_bytes: number
    rx_bps: number
    tx_bps: number
    public_ipv4?: string
    public_ipv4_interface?: string
    public_ipv4_addresses?: PublicIPv4Info[]
    public_ipv6?: string
    public_ipv6_interface?: string
    ipv6_prefixes?: IPv6PrefixInfo[]
  }
  disk_io: { read_bytes: number; write_bytes: number; read_bps: number; write_bps: number }
  load: { load1: number; load5: number; load15: number }
  runtime?: {
    lxc_available: boolean
    kvm_available: boolean
    dev_kvm: boolean
    nested_virtualization: boolean
    nested_detail: string
    support_mode: string
  }
}

export interface CreateSnapshotOptions {
  storage_pool_id?: string
}

export interface HostMetricPoint {
  ts: number
  cpu: number
  memory: number
  network: number
  network_rx: number
  network_tx: number
  disk_io: number
  disk_read: number
  disk_write: number
  disk_usage_pct: number
}

export interface HostProbeReport {
  generated_at: string
  hostname: string
  kernel: string
  os: string
  cpu: {
    model: string
    cores: number
    threads: number
    architecture: string
    flags: string[]
    has_integrated_gpu: boolean
    virtualization: boolean
    virtualization_key: string
  }
  memory: {
    total_mb: number
    used_mb: number
    free_mb: number
    modules: Array<{
      locator: string
      size: string
      type: string
      speed: string
      manufacturer: string
      part_number: string
      serial_number: string
    }>
  }
  disks: Array<{
    name: string
    path: string
    model: string
    serial: string
    size_bytes: number
    type: string
    virtual?: boolean
    rotational: boolean
    mountpoints: string[]
    health: string
    health_detail: string
    smart?: {
      available: boolean
      life_used_percent?: number
      power_on_hours?: number
      power_cycle_count?: number
      read_data_bytes?: number
      written_data_bytes?: number
      read_commands?: number
      write_commands?: number
      wear_leveling_count?: string
      erase_count?: string
      media_errors?: number
    }
  }>
  network_interfaces: Array<{
    name: string
    mac: string
    state: string
    speed_mbps: number
    driver: string
    model: string
    ipv4: Array<{ interface: string; address: string; prefix_len: number; scope: string; gateway?: string }>
    ipv6: Array<{ interface: string; address: string; prefix_len: number; scope: string; gateway?: string }>
  }>
  public_ipv4: string[]
  ipv4_addresses: Array<{ interface: string; address: string; prefix_len: number; scope: string; gateway?: string }>
  ipv4_prefixes: IPv4PrefixInfo[]
  ipv6_addresses: Array<{ interface: string; address: string; prefix_len: number; scope: string; gateway?: string }>
  ipv6_prefixes: IPv6PrefixInfo[]
  gateways: Array<{ family: string; interface: string; gateway: string }>
  gpus: Array<{ name: string; vendor: string; driver: string; type: string }>
  runtime: {
    lxc_available: boolean
    kvm_available: boolean
    dev_kvm: boolean
    nested_virtualization: boolean
    nested_detail: string
    support_mode: string
  }
  system: {
    uptime_seconds: number
    uptime_text: string
    process_count: number
  }
  environment: Array<{ key: string; label: string; ok: boolean; required: boolean; detail: string }>
}

export interface ContainerUsage {
  memory_usage_bytes: number
  memory_total_bytes?: number
  cpu_usage_usec: number
  cpu_usage_pct: number
  disk_usage_bytes: number
  network_rx_bytes: number
  network_tx_bytes: number
  network_rx_bps: number
  network_tx_bps: number
  disk_read_bytes: number
  disk_write_bytes: number
  disk_read_bps: number
  disk_write_bps: number
  load1: number
  load5: number
  load15: number
  guest_metrics?: boolean
}

export interface ContainerMetricPoint {
  ts: number
  cpu: number
  memory: number
  network: number
  network_rx: number
  network_tx: number
  disk_io: number
  disk_read: number
  disk_write: number
}

export interface APIResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

// Auth
export const login = (username: string, password: string) =>
  api.post<APIResponse<LoginResponse>>('/login', { username, password })

export const checkAuth = () =>
  api.get<APIResponse>('/check-auth')

export const changePassword = (oldPassword: string, newPassword: string) =>
  api.post<APIResponse>('/change-password', { old_password: oldPassword, new_password: newPassword })

export const changeUsername = (newUsername: string, password: string) =>
  api.post<APIResponse>('/change-username', { new_username: newUsername, password })

// Login Logs
export interface LoginLog {
  time: string
  username: string
  ip: string
  user_agent: string
  success: boolean
}

export interface AuditLog {
  time: string
  action: string
  target: string
  detail: string
  user: string
}

export const getLoginLogs = () =>
  api.get<APIResponse<LoginLog[]>>('/login-logs')

export interface TaskQueueSettings {
  concurrency: number
  active: number
  pending: number
}

export const getTaskQueueSettings = () =>
  api.get<APIResponse<TaskQueueSettings>>('/task-queue/settings')

export const updateTaskQueueSettings = (concurrency: number) =>
  api.put<APIResponse<TaskQueueSettings>>('/task-queue/settings', { concurrency })

export interface SSLCertificateInfo {
  subject: string
  issuer: string
  dns_names: string[]
  ip_names: string[]
  not_before: string
  not_after: string
  valid: boolean
}

export interface SSLSettings {
  enabled: boolean
  mode: 'disabled' | 'letsencrypt' | 'self_signed' | 'uploaded'
  target: string
  email?: string
  cert_path?: string
  key_path?: string
  last_issued_at?: string
  last_error?: string
  detected_host?: string
  certificate?: SSLCertificateInfo
  mode_certificates?: Record<string, SSLSettings>
  needs_restart?: boolean
}

export interface UpdateSSLSettingsRequest {
  enabled: boolean
  mode: 'disabled' | 'letsencrypt' | 'self_signed' | 'uploaded'
  target?: string
  email?: string
  cert_pem?: string
  key_pem?: string
  apply_now?: boolean
}

export const getSSLSettings = () =>
  api.get<APIResponse<SSLSettings>>('/ssl')

export const updateSSLSettings = (data: UpdateSSLSettingsRequest) =>
  api.put<APIResponse<SSLSettings>>('/ssl', data)

export interface WebSSHOriginSettings {
  origins: string[]
  current_origin?: string
}

export const getWebSSHOriginSettings = () =>
  api.get<APIResponse<WebSSHOriginSettings>>('/webssh-origins')

export const updateWebSSHOriginSettings = (origins: string[]) =>
  api.put<APIResponse<WebSSHOriginSettings>>('/webssh-origins', { origins })

export interface PanelAccessPolicy {
  enabled: boolean
  allowed_sources: string[]
  trusted_proxies: string[]
  current_source: string
  direct_source: string
  using_forwarded: boolean
}

export const getPanelAccessPolicy = () =>
  api.get<APIResponse<PanelAccessPolicy>>('/access-policy')

export const updatePanelAccessPolicy = (data: Pick<PanelAccessPolicy, 'enabled' | 'allowed_sources' | 'trusted_proxies'>) =>
  api.put<APIResponse<PanelAccessPolicy>>('/access-policy', data)

// Containers
export const getContainers = () =>
  api.get<APIResponse<Container[]>>('/containers')

export const getContainer = (id: ContainerIdentifier) =>
  api.get<APIResponse<Container>>(`/containers/${id}`)

export const createContainer = (data: CreateContainerRequest) =>
  api.post<APIResponse>('/containers', data)

export const deleteContainer = (id: ContainerIdentifier) =>
  api.delete<APIResponse>(`/containers/${id}/delete`)

export const startContainer = (id: ContainerIdentifier) =>
  api.post<APIResponse>(`/containers/${id}/start`)

export const stopContainer = (id: ContainerIdentifier) =>
  api.post<APIResponse>(`/containers/${id}/stop`)

export const restartContainer = (id: ContainerIdentifier) =>
  api.post<APIResponse>(`/containers/${id}/restart`)

export const reinstallContainer = (id: ContainerIdentifier, templateId: string, options?: ReinstallContainerOptions) =>
  api.post<APIResponse>(`/containers/${id}/reinstall`, { template_id: templateId, ...(options || {}) })

export const resetSSHPassword = (id: ContainerIdentifier, password?: string) =>
  api.post<APIResponse<{ password: string }>>(`/containers/${id}/reset-password`, password ? { password } : {})

export const getContainerUsage = (id: ContainerIdentifier) =>
  api.get<APIResponse<ContainerUsage>>(`/containers/${id}/usage`)

export const getContainerHistory = (id: ContainerIdentifier) =>
  api.get<APIResponse<ContainerMetricPoint[]>>(`/containers/${id}/history`)

export interface TrafficInfo {
  total_used_bytes: number
  rx_used_bytes: number
  tx_used_bytes: number
  mode: string
  limit_gb: number
  in_limit_gb: number
  out_limit_gb: number
  used_pct: number
  reset_date: string
}

export const getTrafficInfo = (id: ContainerIdentifier) =>
  api.get<APIResponse<TrafficInfo>>(`/containers/${id}/traffic`)

export const resetTraffic = (id: ContainerIdentifier) =>
  api.post<APIResponse>(`/containers/${id}/traffic-reset`)

export const updateTrafficLimit = (id: ContainerIdentifier, data: {
  traffic_mode: string
  monthly_traffic_gb: number
  traffic_in_gb: number
  traffic_out_gb: number
}) =>
  api.put<APIResponse>(`/containers/${id}/traffic-limit`, data)

export const updateResourceLimit = (id: ContainerIdentifier, data: {
  vcpu: number
  ram_mb: number
  io_speed_mbps?: number
  io_read_mbps?: number
  io_write_mbps?: number
  network_bw_mbps?: number
  network_down_mbps?: number
  network_up_mbps?: number
}) =>
  api.put<APIResponse>(`/containers/${id}/resource-limit`, data)

export const addPortMapping = (id: ContainerIdentifier, data: PortMapping) =>
  api.post<APIResponse<PortMapping[]>>(`/containers/${id}/port-mappings`, data)

export const updatePortMapping = (id: ContainerIdentifier, index: number, data: PortMapping) =>
  api.put<APIResponse<PortMapping[]>>(`/containers/${id}/port-mappings/${index}`, data)

export const deletePortMapping = (id: ContainerIdentifier, index: number) =>
  api.delete<APIResponse<PortMapping[]>>(`/containers/${id}/port-mappings/${index}`)

export const getFirewall = (id: ContainerIdentifier) =>
  api.get<APIResponse<{ enabled: boolean; default_action: 'ACCEPT' | 'DROP'; rules: FirewallRule[] }>>(`/containers/${id}/firewall`)

export const updateFirewall = (id: ContainerIdentifier, data: { enabled?: boolean; default_action?: 'ACCEPT' | 'DROP'; rules?: FirewallRule[] }) =>
  api.put<APIResponse<{ enabled: boolean; default_action: 'ACCEPT' | 'DROP'; rules: FirewallRule[] }>>(`/containers/${id}/firewall`, data)

export const updateContainerExpiry = (id: ContainerIdentifier, expiresAt: string) =>
  api.put<APIResponse>(`/containers/${id}/expiry`, { expires_at: expiresAt })

export const getIPv6Status = () =>
  api.get<APIResponse<IPv6Status>>('/ipv6/status')

export const assignIPv6 = (id: ContainerIdentifier) =>
  api.post<APIResponse<Container>>(`/containers/${id}/ipv6`)

export interface IPAssignmentUpdateRequest {
  mode: 'clear' | 'random' | 'custom'
  count?: number
  addresses?: string[]
}

export const updatePublicIPv4Assignments = (id: ContainerIdentifier, data: IPAssignmentUpdateRequest) =>
  api.put<APIResponse<Container>>(`/containers/${id}/public-ipv4`, data)

export const updateIPv6Assignments = (id: ContainerIdentifier, data: IPAssignmentUpdateRequest) =>
  api.put<APIResponse<Container>>(`/containers/${id}/ipv6-addresses`, data)

export interface RouteCapacity {
  used: number
  remaining: string
  total: string
}

export interface NAT4PortRange {
  start: number
  end: number
}

export interface NAT4Route {
  container_id: number
  container_name: string
  lxc_name: string
  status: string
  ip: string
  host_ip: string
  host_port: number
  container_port: number
  protocol: string
  description: string
}

export interface IPv4Route {
  container_id: number
  container_name: string
  lxc_name: string
  status: string
  address: string
  interface: string
  prefix_len?: number
  gateway?: string
}

export interface LANDHCPRoute {
  container_id: number
  container_name: string
  lxc_name: string
  status: string
  address: string
  interface: string
  prefix_len?: number
  gateway?: string
  mac_address?: string
  mode: string
}

export interface IPv6Route {
  container_id: number
  container_name: string
  lxc_name: string
  status: string
  address: string
  prefix_len: number
  interface: string
}

export interface RoutingInfo {
  nat4: RouteCapacity
  nat4_port_range: NAT4PortRange
  nat4_next_port: number
  nat4_networks: {
    lxc: NATNetworkInfo
    kvm: NATNetworkInfo
  }
  ipv4: RouteCapacity
  lan_dhcp: RouteCapacity
  ipv6: RouteCapacity
  host_public_ipv4?: PublicIPv4Info
  public_ipv4_addresses: PublicIPv4Info[]
  ipv4_assignments: IPv4Route[]
  lan_dhcp_assignments: LANDHCPRoute[]
  nat4_mappings: NAT4Route[]
  ipv6_assignments: IPv6Route[]
  ipv6_prefixes: IPv6PrefixInfo[]
}

export interface PublicIPv4ScanResult extends PublicIPv4Info {
  status: string
  usable: boolean
  reason: string
}

export const getRoutingInfo = () =>
  api.get<APIResponse<RoutingInfo>>('/routing')

export const updateRoutingPools = (payload: { items?: PublicIPv4Info[]; ipv6_prefixes?: IPv6PrefixInfo[]; nat4_port_range?: NAT4PortRange }) =>
  api.put<APIResponse<RoutingInfo>>('/routing', payload)

export const updateRoutingIPv4Pool = (items: PublicIPv4Info[]) =>
  updateRoutingPools({ items })

export const updateRoutingIPv6Prefixes = (ipv6_prefixes: IPv6PrefixInfo[]) =>
  updateRoutingPools({ ipv6_prefixes })

export const scanRoutingIPv4Segment = (payload: { cidr: string; interface: string; gateway: string; verify: boolean; limit?: number }) =>
  api.post<APIResponse<PublicIPv4ScanResult[]>>('/routing/ipv4-scan', payload)

// Templates
export const getTemplates = () =>
  api.get<APIResponse<Template[]>>('/templates')

// Images (template download/enable management)
export interface ImageInfo {
  id: string
  name: string
  type: string
  distro: string
  release: string
  arch: string
  description: string
  downloaded: boolean
  enabled: boolean
  downloading: boolean
  progress: number
  downloaded_bytes: number
  total_bytes: number
  stage?: string
  error?: string
  size_bytes: number
  manual_path?: string
  desktop?: string
  provisioner?: string
  custom?: boolean
  sha256?: string
}

export interface CustomKVMImageInput {
  type: 'lxc' | 'kvm'
  name: string
  description: string
  distro: string
  release: string
  arch: string
  url: string
  provisioner?: 'linux-cloud-init' | 'windows-10' | 'windows-11' | 'lxc-rootfs'
  sha256?: string
}

export interface CustomKVMImage extends CustomKVMImageInput {
  id: string
  created_at: string
}

export const getImages = () =>
  api.get<APIResponse<ImageInfo[]>>('/images')

export const createCustomKVMImage = (payload: CustomKVMImageInput) =>
  api.post<APIResponse<CustomKVMImage>>('/images/custom', payload)

export const removeCustomKVMImage = (id: string) =>
  api.delete<APIResponse>('/images/custom', { data: { id } })

export const downloadImage = (templateId: string) =>
  api.post<APIResponse>('/images/download', { template_id: templateId })

export const cancelImageDownload = (templateId: string) =>
  api.post<APIResponse>('/images/cancel', { template_id: templateId })

export const deleteImage = (templateId: string) =>
  api.delete<APIResponse>('/images/delete', { data: { template_id: templateId } })

export const toggleImage = (templateId: string, enabled: boolean) =>
  api.put<APIResponse>('/images/toggle', { template_id: templateId, enabled })

export const getEnabledImages = (virtualization = 'lxc', container?: ContainerIdentifier) =>
  api.get<APIResponse<Template[]>>('/images/enabled', { params: { type: virtualization, ...(container ? { container: String(container) } : {}) } })

// Dashboard
export const getDashboard = () =>
  api.get<APIResponse<DashboardStats>>('/dashboard')

export const getHostInfo = () =>
  api.get<APIResponse<HostInfo>>('/host-info')

export const getHostHistory = () =>
  api.get<APIResponse<HostMetricPoint[]>>('/host-history')

export const getHostReport = () =>
  api.get<APIResponse<HostProbeReport>>('/host-report')

export const getStorageInfo = () =>
  api.get<APIResponse<StorageInfo>>('/storage')

export const updateStoragePools = (pools: StoragePool[]) =>
  api.put<APIResponse<StorageInfo>>('/storage', { pools })

// Snapshots
export interface Snapshot {
  id: string
  container_id: number
  container_name: string
  lxc_name: string
  created_at: string
  created_by: string
  scheduled: boolean
  path: string
  size_bytes: number
}

export interface SnapshotSchedule {
  enabled: boolean
  interval_hours: number
  time: string
  last_run: string
  next_run: string
  created_by: string
}

export interface ContainerSnapshotsResponse {
  snapshots: Snapshot[]
  quota: number
  schedule: SnapshotSchedule
}

export const getSnapshots = () =>
  api.get<APIResponse<Snapshot[]>>('/snapshots')

export const getContainerSnapshots = (id: ContainerIdentifier) =>
  api.get<APIResponse<ContainerSnapshotsResponse>>(`/containers/${id}/snapshots`)

export const createContainerSnapshot = (id: ContainerIdentifier, options?: CreateSnapshotOptions) =>
  api.post<APIResponse<Snapshot>>(`/containers/${id}/snapshots`, options || {}, { timeout: 600000 })

export const deleteContainerSnapshot = (id: ContainerIdentifier, snapshotId: string) =>
  api.delete<APIResponse>(`/containers/${id}/snapshots/${snapshotId}`, { timeout: 600000 })

export const restoreContainerSnapshot = (id: ContainerIdentifier, snapshotId: string) =>
  api.post<APIResponse>(`/containers/${id}/snapshots/${snapshotId}/restore`, {}, { timeout: 600000 })

export const updateSnapshotSchedule = (id: ContainerIdentifier, enabled: boolean, intervalHours: number, time: string) =>
  api.post<APIResponse<{ container: Container; snapshot?: Snapshot }>>(
    `/containers/${id}/snapshots/schedule`,
    { enabled, interval_hours: intervalHours, time },
    { timeout: 600000 }
  )

export const updateSnapshotQuota = (id: ContainerIdentifier, snapshotLimit: number) =>
  api.put<APIResponse<{ container: Container; quota: number }>>(
    `/containers/${id}/snapshots/quota`,
    { snapshot_limit: snapshotLimit }
  )

// WebSSH URL generator
export const getWebSSHUrl = (containerName: string) => {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({ container: containerName })
  return `${protocol}//${window.location.host}/api/ssh?${params.toString()}`
}

export const getWebVNCUrl = (containerName: string) => {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({ container: containerName })
  return `${protocol}//${window.location.host}/api/vnc?${params.toString()}`
}

// Task Queue
export interface Task {
  id: string
  type: string
  container_id?: number
  container_name: string
  status: string
  error?: string
  stage?: string
  stage_detail?: string
  created_at: string
  template_id?: string
  config?: CreateContainerRequest
}

export const getTasks = () =>
  api.get<APIResponse<Task[]>>('/tasks')

export const deleteTask = (taskId: string) =>
  api.delete<APIResponse>(`/tasks/${taskId}`)

export const batchCreate = (containers: CreateContainerRequest[]) =>
  api.post<APIResponse<string[]>>('/batch-create', { containers })

export const batchAction = (action: string, containers: number[], templateId?: string) =>
  api.post<APIResponse>('/batch-action', { action, containers, template_id: templateId })

// Sub Users
export interface SubUser {
  id: string
  username: string
  password?: string
  container_names: string[]
  container_uuids?: string[]
  allowed_image_ids?: string[]
  image_limit_configured?: boolean
  current_image_ids?: string[]
  access_code: string
  created_at: string
}

export interface NATNetworkInfo {
  subnet: string
  gateway: string
  netmask: string
  dhcp_start: string
  dhcp_end: string
  dhcp_max: number
  prefix_bits: number
}

export const createSubUser = (containerId: ContainerIdentifier) =>
  api.post<APIResponse<SubUser>>('/sub-user/create', { container_name: String(containerId) })

export const updateSubUserImages = (id: string, allowedImageIds: string[]) =>
  api.put<APIResponse<SubUser>>(`/sub-users/${id}/images`, { allowed_image_ids: allowedImageIds })

// Audit Logs
export interface AuditLog {
  time: string
  action: string
  target: string
  detail: string
  user: string
}

export const getAuditLogs = () =>
  api.get<APIResponse<AuditLog[]>>('/audit-logs')

// Security
export interface SecurityAlert {
  id: string
  container_name: string
  type: string
  severity: string
  source_ip: string
  target_ip: string
  target_port: number
  detail: string
  log_line: string
  timestamp: string
  count: number
}

export interface SecuritySummary {
  total_alerts: number
  critical: number
  high: number
  medium: number
  low: number
}

export interface SecuritySettings {
  auto_shutdown: boolean
}

export interface SecurityLog {
  src_ip: string
  dst_ip: string
  src_port: number
  dst_port: number
  protocol: string
  state: string
}

export const getSecurityAlerts = () =>
  api.get<APIResponse<SecurityAlert[]>>('/security/alerts')

export const checkContainerSecurity = (containerName: string) =>
  api.post<APIResponse>('/security/check', { container_name: containerName })

export const getSecurityLogs = (containerName: string) =>
  api.get<APIResponse<SecurityLog[]>>('/security/logs', { params: { container: containerName } })

export const getSecuritySummary = () =>
  api.get<APIResponse<SecuritySummary>>('/security/summary')

export const getSecuritySettings = () =>
  api.get<APIResponse<SecuritySettings>>('/security/settings')

export const updateSecuritySettings = (data: SecuritySettings) =>
  api.put<APIResponse<SecuritySettings>>('/security/settings', data)

export const createWebSSHTicket = (containerName: string) =>
  api.post<APIResponse<{ ticket: string }>>('/ssh-ticket', { container_name: containerName })

export const createVNCTicket = (containerName: string) =>
  api.post<APIResponse<{ ticket: string }>>('/vnc-ticket', { container_name: containerName })

// Language
export type PanelLanguage = 'zh' | 'en'

export const getLanguage = () =>
  api.get<APIResponse<{ language: PanelLanguage }>>('/language')

export const updateLanguage = (language: PanelLanguage) =>
  api.post<APIResponse<{ language: PanelLanguage }>>('/language', { language })

// Version
export const getVersion = () =>
  api.get<APIResponse<{ version: string }>>('/version')

export default api
