import { Modal } from '../components/ui/modal'
import { Button } from '../components/ui/button'
import { Card } from '../components/ui/card'
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '../components/ui/table'
import { Badge } from '../components/ui/badge'
import { Label } from '../components/ui/label'
import { Input } from '../components/ui/input'
import { Textarea } from '../components/ui/textarea'
import { Checkbox } from '../components/ui/checkbox'
import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Check,
  ChevronDown,
  ChevronUp,
  Copy,
  Edit3,
  Eye,
  Key,
  Plus,
  RefreshCw,
  ShieldCheck,
  Trash2,
  X,
} from 'lucide-react'
import api, { APIResponse, Container } from '../services/api'
import { useLanguage } from '../contexts/LanguageContext'
import { copyToClipboard } from '../utils/clipboard'
import { useDialog } from '../components/Dialog'

interface ApiKeyItem {
  id: string
  name: string
  key?: string
  prefix: string
  ip_whitelist: string
  created_at: string
  last_used: string
  scopes?: string[]
  expires_at?: string
  disabled?: boolean
  container_uuids?: string[]
  last_used_ip?: string
}

interface ApiKeyForm {
  name: string
  ipWhitelist: string
  scopes: string[]
  expiresAt: string
  disabled: boolean
  containerUUIDs: string[]
}

const BASE_URL = window.location.origin
const SAMPLE_BASE_URL = 'https://panel.example.com'
const DOCS_ORIGIN = 'https://cli.cd'

type HttpMethod = 'GET' | 'HEAD' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
type EndpointTuple = [method: HttpMethod, path: string, description: string, access: string]

interface EndpointDoc {
  method: HttpMethod
  path: string
  desc: string
  access: string
  examplePath: string
  body?: Record<string, unknown>
  upload?: { file: string; fields?: Record<string, string> }
  download?: string
  response: unknown
  note?: string
}

const scopeGroups = [
  {
    title: '总览与任务',
    scopes: [
      ['dashboard:read', '控制面板'],
      ['host:read', '主机资源'],
      ['routing:read', '路由信息'],
      ['routing:write', '路由配置'],
      ['ipv6:read', 'IPv6 状态'],
      ['task:read', '任务列表与详情'],
      ['task:delete', '删除任务'],
      ['image:read', '镜像列表'],
    ],
  },
  {
    title: '容器',
    scopes: [
      ['container:read', '查看容器'],
      ['container:create', '创建容器'],
      ['container:power', '开关机/重启'],
      ['container:reinstall', '重装系统与安装介质'],
      ['container:delete', '删除容器'],
      ['container:resize', '资源、到期与磁盘'],
      ['container:traffic', '流量管理'],
      ['container:network', '网络与端口映射'],
      ['container:password', '重置密码'],
      ['ipv6:assign', '分配 IPv6'],
    ],
  },
  {
    title: '快照与终端',
    scopes: [
      ['snapshot:read', '查看快照'],
      ['snapshot:create', '创建快照'],
      ['snapshot:delete', '删除快照'],
      ['snapshot:restore', '恢复快照'],
      ['snapshot:schedule', '计划/配额'],
      ['terminal:ssh', 'WebSSH 票据'],
      ['terminal:vnc', 'WebVNC 票据'],
    ],
  },
  {
    title: '平台管理',
    scopes: [
      ['image:download', '下载或上传镜像'],
      ['image:delete', '删除镜像'],
      ['image:toggle', '启停镜像'],
      ['swap:read', 'Swap 信息'],
      ['swap:manage', 'Swap 管理'],
      ['subuser:read', '子用户列表'],
      ['subuser:create', '创建子用户'],
      ['subuser:update', '更新子用户'],
      ['audit:read', '操作日志'],
      ['loginlog:read', '登录日志'],
      ['apikey:read', 'Key 列表'],
      ['apikey:create', '创建 Key'],
      ['apikey:update', '更新 Key'],
      ['apikey:delete', '删除 Key'],
      ['admin:access', '主机设置、备份与模板'],
    ],
  },
]

const defaultReadScopes = [
  'dashboard:read',
  'container:read',
  'task:read',
  'image:read',
  'snapshot:read',
  'routing:read',
  'ipv6:read',
  'host:read',
]

const endpointGroups: Array<{ title: string; endpoints: EndpointTuple[] }> = [
  {
    title: '总览',
    endpoints: [
      ['GET', '/api/v1/dashboard', '控制面板统计', 'dashboard:read'],
      ['GET', '/api/v1/version', '服务版本', '公开'],
      ['GET', '/api/v1/host-info', '主机资源', 'host:read'],
      ['GET', '/api/v1/host-history', '宿主机历史指标（后台每 30 秒采集）', 'host:read'],
      ['GET', '/api/v1/host-report', '宿主机硬件、网络与运行环境探测报告', 'host:read'],
      ['GET', '/api/v1/routing', 'NAT/IPv4/IPv6 路由', 'routing:read / routing:write'],
      ['PUT', '/api/v1/routing', '更新公网 IPv4/IPv6 池', 'routing:write'],
      ['POST', '/api/v1/routing/ipv4-scan', '扫描公网 IPv4 段', 'routing:write'],
      ['GET', '/api/v1/ipv6/status', 'IPv6 状态', 'ipv6:read'],
      ['GET', '/api/v1/tasks', '任务队列', 'task:read'],
      ['GET', '/api/v1/tasks/{task_id}', '任务详情', 'task:read'],
      ['DELETE', '/api/v1/tasks/{task_id}', '删除任务', 'task:delete'],
    ],
  },
  {
    title: '容器',
    endpoints: [
      ['GET', '/api/v1/containers', '容器列表', 'container:read'],
      ['POST', '/api/v1/containers/list', '容器列表（兼容 POST 写法）', 'container:read'],
      ['POST', '/api/v1/containers', '创建容器', 'container:create'],
      ['GET', '/api/v1/containers/{id|uuid|name}', '容器详情', 'container:read'],
      ['PATCH', '/api/v1/containers/{id}/name', '修改面板展示名称', 'container:resize'],
      ['POST', '/api/v1/containers/{id}/start', '开机', 'container:power'],
      ['POST', '/api/v1/containers/{id}/stop', '关机', 'container:power'],
      ['POST', '/api/v1/containers/{id}/restart', '重启', 'container:power'],
      ['POST', '/api/v1/containers/{id}/reinstall', '重装', 'container:reinstall'],
      ['DELETE', '/api/v1/containers/{id}/delete', '删除', 'container:delete'],
      ['GET', '/api/v1/containers/{id}/usage', '资源用量', 'container:read'],
      ['GET', '/api/v1/containers/{id}/history', '容器历史指标（后台每 30 秒采集）', 'container:read'],
      ['GET', '/api/v1/containers/{id}/traffic', '流量统计', 'container:read'],
      ['POST', '/api/v1/containers/{id}/traffic-reset', '重置流量', 'container:traffic'],
      ['PUT', '/api/v1/containers/{id}/traffic-limit', '调整流量限制', 'container:traffic'],
      ['PUT', '/api/v1/containers/{id}/resource-limit', '调整资源限制', 'container:resize'],
      ['PUT', '/api/v1/containers/{id}/expiry', '调整到期时间', 'container:resize'],
      ['POST', '/api/v1/containers/{id}/reset-password', '重置 SSH 密码', 'container:password'],
      ['POST', '/api/v1/containers/{id}/ipv6', '分配 IPv6', 'ipv6:assign'],
      ['PUT', '/api/v1/containers/{id}/public-ipv4', '更新独立公网 IPv4 地址', 'container:network'],
      ['PUT', '/api/v1/containers/{id}/ipv6-addresses', '更新独立 IPv6 地址', 'ipv6:assign'],
      ['GET', '/api/v1/containers/{id}/installation-media', '查询 KVM 安装介质', 'container:read'],
      ['PUT', '/api/v1/containers/{id}/installation-media', '挂载或卸载 KVM 安装介质', 'container:reinstall'],
    ],
  },
  {
    title: '磁盘与备份',
    endpoints: [
      ['GET', '/api/v1/containers/{id}/disks', '磁盘清单', 'container:read'],
      ['POST', '/api/v1/containers/{id}/disks', '添加数据盘', 'container:resize'],
      ['PUT', '/api/v1/containers/{id}/disks/{disk_id}', '扩容系统盘或数据盘', 'container:resize'],
      ['PUT', '/api/v1/containers/{id}/disks/{disk_id}/attachment', '挂载或卸载数据盘', 'container:resize'],
      ['DELETE', '/api/v1/containers/{id}/disks/{disk_id}', '删除数据盘', 'container:resize'],
      ['POST', '/api/v1/containers/{id}/backup', '完整备份', 'admin:access'],
      ['POST', '/api/v1/containers/{id}/clone', '完整克隆', 'admin:access'],
      ['POST', '/api/v1/containers/{id}/template', '保存实例模板', 'admin:access'],
      ['GET', '/api/v1/backups', '备份列表', 'admin:access'],
      ['POST', '/api/v1/backups', '导入备份', 'admin:access'],
      ['GET', '/api/v1/backups/{backup_id}', '下载备份', 'admin:access'],
      ['HEAD', '/api/v1/backups/{backup_id}', '检查备份下载元数据', 'admin:access'],
      ['DELETE', '/api/v1/backups/{backup_id}', '删除备份', 'admin:access'],
      ['POST', '/api/v1/backups/{backup_id}/restore', '恢复为新实例', 'admin:access'],
      ['POST', '/api/v1/backups/{backup_id}/download-ticket', '创建备份下载票据', 'admin:access'],
      ['GET', '/api/v1/backup-download/{ticket}', '使用票据下载备份', '下载票据'],
      ['HEAD', '/api/v1/backup-download/{ticket}', '使用票据检查备份下载元数据', '下载票据'],
      ['GET', '/api/v1/instance-templates', '实例模板列表', 'admin:access'],
      ['POST', '/api/v1/instance-templates/{template_id}/instantiate', '从模板创建实例', 'admin:access'],
      ['DELETE', '/api/v1/instance-templates/{template_id}', '删除实例模板', 'admin:access'],
    ],
  },
  {
    title: '端口与快照',
    endpoints: [
      ['GET', '/api/v1/containers/{id}/random-port', '随机可用端口', 'container:network'],
      ['GET', '/api/v1/containers/{id}/port-mappings', '查询 IPv4 端口映射', 'container:read'],
      ['POST', '/api/v1/containers/{id}/port-mappings', '添加端口映射', 'container:network'],
      ['PUT', '/api/v1/containers/{id}/port-mappings/{index}', '更新端口映射', 'container:network'],
      ['DELETE', '/api/v1/containers/{id}/port-mappings/{index}', '删除端口映射', 'container:network'],
      ['GET', '/api/v1/containers/{id}/ipv6-port-mappings', '查询 IPv6 NAT 端口映射', 'container:network'],
      ['POST', '/api/v1/containers/{id}/ipv6-port-mappings', '添加 IPv6 NAT 端口映射', 'container:network'],
      ['PUT', '/api/v1/containers/{id}/ipv6-port-mappings/{index}', '更新 IPv6 NAT 端口映射', 'container:network'],
      ['DELETE', '/api/v1/containers/{id}/ipv6-port-mappings/{index}', '删除 IPv6 NAT 端口映射', 'container:network'],
      ['PUT', '/api/v1/containers/{id}/ipv6-nat-sync', '设置 IPv6 NAT 同步 IPv4 映射', 'container:network'],
      ['GET', '/api/v1/containers/{id}/firewall', '获取防火墙设置', 'container:network'],
      ['PUT', '/api/v1/containers/{id}/firewall', '更新防火墙设置', 'container:network'],
      ['GET', '/api/v1/snapshots', '快照总览', 'snapshot:read'],
      ['GET', '/api/v1/containers/{id}/snapshots', '容器快照', 'snapshot:read'],
      ['POST', '/api/v1/containers/{id}/snapshots', '创建快照', 'snapshot:create'],
      ['DELETE', '/api/v1/containers/{id}/snapshots/{snapshot_id}', '删除快照', 'snapshot:delete'],
      ['POST', '/api/v1/containers/{id}/snapshots/{snapshot_id}/restore', '恢复快照', 'snapshot:restore'],
      ['POST', '/api/v1/containers/{id}/snapshots/schedule', '计划快照', 'snapshot:schedule'],
      ['PUT', '/api/v1/containers/{id}/snapshots/quota', '快照配额', 'snapshot:schedule'],
    ],
  },
  {
    title: '平台管理',
    endpoints: [
      ['GET', '/api/v1/templates', '模板列表', 'image:read'],
      ['GET', '/api/v1/images', '镜像管理列表', 'image:read'],
      ['GET', '/api/v1/images/enabled?type=lxc&container={id}', '可用于创建或重装的已启用镜像', 'image:read'],
      ['POST', '/api/v1/images/custom', '添加第三方 LXC/KVM 镜像源', 'admin:access + image:download'],
      ['DELETE', '/api/v1/images/custom', '移除第三方 LXC/KVM 镜像源', 'admin:access + image:delete'],
      ['POST', '/api/v1/images/download', '下载镜像', 'admin:access + image:download'],
      ['POST', '/api/v1/images/cancel', '取消镜像下载', 'admin:access + image:download'],
      ['DELETE', '/api/v1/images/delete', '删除镜像缓存', 'admin:access + image:delete'],
      ['PUT', '/api/v1/images/toggle', '启用/禁用镜像', 'admin:access + image:toggle'],
      ['POST', '/api/v1/images/upload', '上传本地 KVM 镜像', 'admin:access + image:download + image:toggle'],
      ['GET', '/api/v1/swap', 'Swap 信息', 'swap:read'],
      ['POST', '/api/v1/swap', '调整 Swap', 'swap:manage'],
      ['POST', '/api/v1/batch-create', '批量创建容器', 'container:create'],
      ['POST', '/api/v1/batch-action', '批量开关机/删除/重装', 'container:power / container:delete / container:reinstall'],
      ['POST', '/api/v1/ssh-ticket', '创建 WebSSH 票据', 'terminal:ssh'],
      ['POST', '/api/v1/vnc-ticket', '创建 WebVNC 票据', 'terminal:vnc'],
    ],
  },
  {
    title: '主机与设置',
    endpoints: [
      ['GET', '/api/v1/storage', '已挂载磁盘、存储池和空间占用', 'admin:access'],
      ['PUT', '/api/v1/storage', '更新各磁盘的存储用途和默认盘', 'admin:access'],
      ['GET', '/api/v1/ipv6/nat', '查询 LXC IPv6 NAT 设置', 'admin:access + ipv6:read'],
      ['PUT', '/api/v1/ipv6/nat', '更新 LXC IPv6 NAT 设置', 'admin:access + routing:write'],
      ['GET', '/api/v1/task-queue/settings', '任务队列并发状态', 'admin:access'],
      ['PUT', '/api/v1/task-queue/settings', '调整任务并发数量', 'admin:access'],
      ['GET', '/api/v1/ssl', 'SSL 配置和证书状态', 'admin:access'],
      ['PUT', '/api/v1/ssl', '更新 SSL 配置', 'admin:access'],
      ['GET', '/api/v1/webssh-origins', 'WebSSH/VNC Origin 白名单', 'admin:access'],
      ['PUT', '/api/v1/webssh-origins', '更新 WebSSH/VNC Origin 白名单', 'admin:access'],
      ['GET', '/api/v1/access-policy', '面板访问来源策略', 'admin:access'],
      ['PUT', '/api/v1/access-policy', '更新面板访问来源策略', 'admin:access'],
      ['GET', '/api/v1/language', '面板语言', '公开'],
      ['PUT', '/api/v1/language', '更新面板语言', 'admin:access'],
    ],
  },
  {
    title: '账号与日志',
    endpoints: [
      ['POST', '/api/v1/sub-user/create', '创建子用户链接', 'subuser:create'],
      ['GET', '/api/v1/sub-users', '子用户列表', 'subuser:read'],
      ['POST', '/api/v1/sub-users/{id}/rotate-password', '轮换子用户密码', 'subuser:update'],
      ['PUT', '/api/v1/sub-users/{id}/images', '更新子用户镜像权限', 'subuser:update'],
      ['GET', '/api/v1/sub-users/{id}/audit-logs', '子用户操作日志', 'audit:read'],
      ['GET', '/api/v1/sub-users/{id}/login-logs', '子用户登录日志', 'loginlog:read'],
      ['GET', '/api/v1/audit-logs', '操作日志', 'audit:read'],
      ['GET', '/api/v1/login-logs', '登录日志', 'loginlog:read'],
      ['GET', '/api/v1/api-keys', 'API Key 列表', 'apikey:read'],
      ['POST', '/api/v1/api-keys', '创建 API Key', 'apikey:create'],
      ['PATCH', '/api/v1/api-keys/{id}', '更新 API Key', 'apikey:update'],
      ['DELETE', '/api/v1/api-keys/{id}', '删除 API Key', 'apikey:delete'],
    ],
  },
]

const emptyForm = (): ApiKeyForm => ({
  name: '',
  ipWhitelist: '',
  scopes: [...defaultReadScopes],
  expiresAt: '',
  disabled: false,
  containerUUIDs: [],
})

export default function ApiIntegration() {
  const dialog = useDialog()
  const { language, t } = useLanguage()
  const [keys, setKeys] = useState<ApiKeyItem[]>([])
  const [containers, setContainers] = useState<Container[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editingKey, setEditingKey] = useState<ApiKeyItem | null>(null)
  const [form, setForm] = useState<ApiKeyForm>(emptyForm)
  const [saving, setSaving] = useState(false)
  const [newKey, setNewKey] = useState('')
  const [copiedKey, setCopiedKey] = useState(false)
  const [showDocs, setShowDocs] = useState(true)
  const [selectedEndpoint, setSelectedEndpoint] = useState<EndpointDoc | null>(null)
  const [copiedDoc, setCopiedDoc] = useState(false)

  const containerNameByUUID = useMemo(() => {
    const map = new Map<string, string>()
    containers.forEach(c => map.set(c.uuid, c.name))
    return map
  }, [containers])

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const [keyRes, containerRes] = await Promise.all([
        api.get<APIResponse<ApiKeyItem[]>>('/api-keys'),
        api.get<APIResponse<Container[]>>('/containers'),
      ])
      setKeys(keyRes.data.data || [])
      setContainers(containerRes.data.data || [])
    } catch {
      // keep the page usable if one request fails
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const openCreate = () => {
    setEditingKey(null)
    setForm(emptyForm())
    setShowForm(true)
  }

  const openEdit = (item: ApiKeyItem) => {
    setEditingKey(item)
    setForm({
      name: item.name,
      ipWhitelist: item.ip_whitelist || '',
      scopes: item.scopes?.length ? item.scopes : ['*'],
      expiresAt: toDateTimeLocal(item.expires_at || ''),
      disabled: Boolean(item.disabled),
      containerUUIDs: item.container_uuids || [],
    })
    setShowForm(true)
  }

  const saveKey = async () => {
    if (!form.name.trim()) return
    setSaving(true)
    const payload = {
      name: form.name.trim(),
      ip_whitelist: form.ipWhitelist.trim(),
      scopes: form.scopes,
      expires_at: fromDateTimeLocal(form.expiresAt),
      disabled: form.disabled,
      container_uuids: form.containerUUIDs,
    }
    try {
      if (editingKey) {
        const res = await api.patch<APIResponse<ApiKeyItem>>(`/api-keys/${editingKey.id}`, payload)
        if (res.data.data) {
          setKeys(prev => prev.map(k => (k.id === editingKey.id ? res.data.data! : k)))
        }
      } else {
        const res = await api.post<APIResponse<ApiKeyItem>>('/api-keys', payload)
        if (res.data.data) {
          setKeys(prev => [res.data.data!, ...prev])
          if (res.data.data.key) setNewKey(res.data.data.key)
        }
      }
      setShowForm(false)
    } catch {
      // axios interceptor handles auth; form stays open
    } finally {
      setSaving(false)
    }
  }

  const deleteKey = async (id: string) => {
    if (!(await dialog.confirm(t('删除 API Key'), t('确定删除这个 API Key 吗？')))) return
    try {
      await api.delete(`/api-keys/${id}`)
      setKeys(prev => prev.filter(k => k.id !== id))
    } catch {
      // ignore
    }
  }

  const copyKey = async () => {
    const copied = await copyToClipboard(newKey)
    if (copied) {
      setCopiedKey(true)
      setTimeout(() => setCopiedKey(false), 1600)
    }
  }

  const copyDocCode = async () => {
    if (!selectedEndpoint) return
    const copied = await copyToClipboard(buildPythonExample(selectedEndpoint))
    if (copied) {
      setCopiedDoc(true)
      setTimeout(() => setCopiedDoc(false), 1600)
    }
  }

  const toggleScope = (scope: string) => {
    setForm(prev => {
      if (scope === '*') {
        return { ...prev, scopes: prev.scopes.includes('*') ? [...defaultReadScopes] : ['*'] }
      }
      const withoutAll = prev.scopes.filter(s => s !== '*')
      const scopes = withoutAll.includes(scope)
        ? withoutAll.filter(s => s !== scope)
        : [...withoutAll, scope]
      return { ...prev, scopes: scopes.length ? scopes : [...defaultReadScopes] }
    })
  }

  const toggleContainer = (uuid: string) => {
    setForm(prev => ({
      ...prev,
      containerUUIDs: prev.containerUUIDs.includes(uuid)
        ? prev.containerUUIDs.filter(item => item !== uuid)
        : [...prev.containerUUIDs, uuid],
    }))
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">API 集成</h1>
          <p className="mt-1 text-sm text-muted-foreground">管理外部调用凭据、权限范围与平台 API 文档</p>
        </div>
        <Button variant="default"
          onClick={openCreate}
          className="h-auto inline-flex items-center gap-1.5 px-3 py-2 text-sm"
        >
          <Plus className="h-4 w-4" />
          创建 Key
        </Button>
      </div>

      {newKey && (
        <Card className="p-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <div className="text-sm font-semibold text-foreground">新的 API Key 已生成</div>
            <Button variant="ghost" onClick={() => setNewKey('')} className="h-auto p-1" title="关闭">
              <X className="h-4 w-4" />
            </Button>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            <code className="min-w-0 flex-1 break-all rounded border border-border bg-background px-3 py-2 font-mono text-xs text-foreground">
              {newKey}
            </code>
            <Button variant="default"
              onClick={copyKey}
              className="h-auto inline-flex items-center justify-center gap-1.5 px-3 py-2 text-xs"
            >
              {copiedKey ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {copiedKey ? '已复制' : '复制'}
            </Button>
          </div>
        </Card>
      )}

      <Card className="">
        <div className="flex items-center justify-between gap-3 border-b border-border px-5 py-4">
          <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
            <Key className="h-4 w-4" />
            API Keys
          </h2>
          <Button variant="ghost" onClick={fetchData} className="h-auto p-1.5" title="刷新">
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>

        {loading ? (
          <div className="py-10 text-center text-sm text-muted-foreground">加载中...</div>
        ) : keys.length === 0 ? (
          <div className="py-10 text-center text-sm text-muted-foreground">暂无 API Key</div>
        ) : (
          <div className="overflow-x-auto">
            <Table className="w-full text-sm">
              <TableHeader>
                <TableRow className="border-b border-border text-left text-xs font-medium text-muted-foreground">
                  <TableHead className="h-auto px-4 py-3">名称</TableHead>
                  <TableHead className="h-auto px-4 py-3">权限</TableHead>
                  <TableHead className="h-auto px-4 py-3">绑定容器</TableHead>
                  <TableHead className="h-auto px-4 py-3">限制</TableHead>
                  <TableHead className="h-auto px-4 py-3">最后使用</TableHead>
                  <TableHead className="h-auto px-4 py-3 text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody className="divide-y divide-border">
                {keys.map(item => (
                  <TableRow key={item.id} className="hover:bg-muted/50">
                    <TableCell className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-foreground">{item.name}</span>
                        {item.disabled && (
                          <Badge variant="destructive" className="px-1.5 py-0.5 text-[10px] font-medium">已禁用</Badge>
                        )}
                      </div>
                      <div className="mt-1 font-mono text-xs text-muted-foreground">{item.prefix}</div>
                    </TableCell>
                    <TableCell className="px-4 py-3">
                      <ScopeSummary scopes={item.scopes || ['*']} />
                    </TableCell>
                    <TableCell className="px-4 py-3 text-xs text-muted-foreground">
                      {item.container_uuids?.length
                        ? item.container_uuids.map(uuid => containerNameByUUID.get(uuid) || uuid).join('、')
                        : '全部容器'}
                    </TableCell>
                    <TableCell className="px-4 py-3 text-xs text-muted-foreground">
                      <div>{item.ip_whitelist ? 'IP 白名单' : '不限 IP'}</div>
                      <div>{item.expires_at ? `到期 ${item.expires_at}` : '长期有效'}</div>
                    </TableCell>
                    <TableCell className="px-4 py-3 text-xs text-muted-foreground">
                      <div>{item.last_used || '从未使用'}</div>
                      {item.last_used_ip && <div className="font-mono text-[11px] text-muted-foreground">{item.last_used_ip}</div>}
                    </TableCell>
                    <TableCell className="px-4 py-3">
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" onClick={() => openEdit(item)} className="h-auto p-1.5" title="编辑">
                          <Edit3 className="h-3.5 w-3.5" />
                        </Button>
                        <Button variant="ghost" onClick={() => deleteKey(item.id)} className="h-auto p-1.5" title="删除">
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Card>

      <Card className="">
        <Button variant="outline"
          onClick={() => setShowDocs(value => !value)}
          className="h-auto flex w-full items-center justify-between gap-3 px-5 py-4 text-left"
        >
          <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
            <ShieldCheck className="h-4 w-4" />
            API 文档
          </h2>
          {showDocs ? <ChevronUp className="h-4 w-4 text-muted-foreground" /> : <ChevronDown className="h-4 w-4 text-muted-foreground" />}
        </Button>

        {showDocs && (
          <div className="space-y-6 p-5">
            <div className="rounded-lg bg-background p-4 font-mono text-xs text-muted-foreground">
              <div>curl -X GET {BASE_URL}/api/v1/containers -H "X-API-Key: clicd_sk_xxxx"</div>
              <div className="mt-2 text-muted-foreground">curl -X GET {BASE_URL}/api/v1/dashboard -H "Authorization: Bearer clicd_sk_xxxx"</div>
            </div>
            <a
              className="inline-flex text-sm font-medium underline underline-offset-4"
              href={`${DOCS_ORIGIN}${language === 'en' ? '/en' : ''}/features/api`}
              target="_blank"
              rel="noreferrer"
            >
              {t('在线 API 文档')}
            </a>

            {endpointGroups.map(group => (
              <section key={group.title}>
                <h3 className="mb-2 text-sm font-semibold text-foreground">{group.title}</h3>
                <div className="overflow-hidden rounded-lg border border-border">
                  {group.endpoints.map(([method, path, desc, access]) => (
                    <div key={`${method}-${path}`} className="grid gap-2 border-b border-border px-3 py-2 text-xs last:border-b-0 md:grid-cols-[72px_minmax(220px,1fr)_180px_72px]">
                      <Badge variant="outline" className="w-fit px-1.5 py-0.5 font-mono font-bold">{method}</Badge>
                      <code className="min-w-0 break-all font-mono text-foreground">{path}</code>
                      <div className="min-w-0 space-y-1 text-muted-foreground">
                        <div>{t(desc)}</div>
                        <code className="block break-words text-[11px]">{t(access)}</code>
                      </div>
                      <Button variant="outline"
                        onClick={() => setSelectedEndpoint(buildEndpointDoc(method, path, desc, access))}
                        className="h-auto inline-flex w-fit items-center justify-center gap-1 px-2 py-1"
                        title="查看使用范例"
                      >
                        <Eye className="h-3.5 w-3.5" />
                        查看
                      </Button>
                    </div>
                  ))}
                </div>
              </section>
            ))}
          </div>
        )}
      </Card>

      {selectedEndpoint && (
        <Modal size="5xl" onClose={() => setSelectedEndpoint(null)} title={<span className="flex flex-wrap items-center gap-2"><Badge variant="outline" className="px-1.5 py-0.5 font-mono text-xs">{selectedEndpoint.method}</Badge><code className="min-w-0 break-all font-mono text-sm">{selectedEndpoint.path}</code></span>} description={<>{t(selectedEndpoint.desc)} · <code>{t(selectedEndpoint.access)}</code></>} flush>
<div className="min-h-0 flex-1 overflow-y-auto p-5">
              <div className="grid gap-4 lg:grid-cols-2">
                <section className="min-w-0">
                  <div className="mb-2 flex items-center justify-between gap-2">
                    <h3 className="text-sm font-semibold text-foreground">Python 使用范例</h3>
                    <Button variant="outline"
                      onClick={copyDocCode}
                      className="h-auto inline-flex items-center gap-1 px-2 py-1 text-xs"
                    >
                      {copiedDoc ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                      {copiedDoc ? '已复制' : '复制'}
                    </Button>
                  </div>
                  <pre className="max-h-[52vh] overflow-auto rounded-lg bg-background p-4 text-xs text-muted-foreground">
                    <code>{buildPythonExample(selectedEndpoint)}</code>
                  </pre>
                </section>

                <section className="min-w-0">
                  <h3 className="mb-2 text-sm font-semibold text-foreground">返回响应样例</h3>
                  {selectedEndpoint.note && (
                    <div className="mb-2 rounded border border-border bg-muted px-3 py-2 text-xs text-muted-foreground">{selectedEndpoint.note}</div>
                  )}
                  <pre className="max-h-[52vh] overflow-auto rounded-lg bg-background p-4 text-xs text-muted-foreground">
                    <code>{typeof selectedEndpoint.response === 'string' ? selectedEndpoint.response : formatJSON(selectedEndpoint.response)}</code>
                  </pre>
                </section>
              </div>
            </div>
</Modal>
      )}

      {showForm && (
        <Modal size="4xl" onClose={() => setShowForm(false)} title={editingKey ? '编辑 API Key' : '创建 API Key'} flush footer={<>
              <Button variant="outline" onClick={() => setShowForm(false)} className="h-auto px-4 py-2 text-sm">
                取消
              </Button>
              <Button variant="default"
                onClick={saveKey}
                disabled={saving || !form.name.trim()}
                className="h-auto px-4 py-2 text-sm"
              >
                {saving ? '保存中...' : '保存'}
              </Button>
            </>}>
<div className="min-h-0 flex-1 overflow-y-auto p-5">
              <div className="grid gap-5 lg:grid-cols-[1fr_1.2fr]">
                <div className="space-y-4">
                  <Label className="block leading-normal">
                    <span className="mb-1 block text-xs text-muted-foreground">名称</span>
                    <Input
                      value={form.name}
                      onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))}
                      className="h-auto w-full px-3 py-2 text-sm"
                      placeholder="CI/CD、计费系统、自动化脚本"
                    />
                  </Label>

                  <Label className="block leading-normal">
                    <span className="mb-1 block text-xs text-muted-foreground">IP 白名单</span>
                    <Textarea
                      value={form.ipWhitelist}
                      onChange={e => setForm(prev => ({ ...prev, ipWhitelist: e.target.value }))}
                      rows={4}
                      className="w-full resize-none px-3 py-2 font-mono text-sm"
                      placeholder={`1.2.3.4\n10.0.0.0/24`}
                    />
                  </Label>

                  <Label className="block leading-normal">
                    <span className="mb-1 block text-xs text-muted-foreground">过期时间</span>
                    <Input
                      type="datetime-local"
                      value={form.expiresAt}
                      onChange={e => setForm(prev => ({ ...prev, expiresAt: e.target.value }))}
                      className="h-auto w-full px-3 py-2 text-sm"
                    />
                  </Label>

                  <Label className="flex items-center gap-2 text-sm text-foreground leading-normal">
                    <Checkbox
                      checked={form.disabled}
                      onCheckedChange={checked => setForm(prev => ({ ...prev, disabled: checked === true }))}
                      className="h-4 w-4"
                    />
                    禁用这个 Key
                  </Label>

                  <div>
                    <div className="mb-2 text-xs text-muted-foreground">绑定容器</div>
                    <div className="max-h-48 space-y-1 overflow-y-auto rounded-md border border-border p-2">
                      <Label className="flex items-center gap-2 rounded px-2 py-1.5 text-sm text-foreground hover:bg-muted/50 leading-normal">
                        <Checkbox
                          checked={form.containerUUIDs.length === 0}
                          onCheckedChange={() => setForm(prev => ({ ...prev, containerUUIDs: [] }))}
                          className="h-4 w-4"
                        />
                        全部容器
                      </Label>
                      {containers.map(container => (
                        <Label key={container.uuid} className="flex items-center gap-2 rounded px-2 py-1.5 text-sm text-foreground hover:bg-muted/50 leading-normal">
                          <Checkbox
                            checked={form.containerUUIDs.includes(container.uuid)}
                            onCheckedChange={() => toggleContainer(container.uuid)}
                            className="h-4 w-4"
                          />
                          <span className="min-w-0 truncate">{container.name}</span>
                          <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{container.uuid.slice(0, 8)}</span>
                        </Label>
                      ))}
                    </div>
                  </div>
                </div>

                <div>
                  <div className="mb-2 flex items-center justify-between gap-3">
                    <div className="text-xs text-muted-foreground">权限范围</div>
                    <Button variant="outline" onClick={() => toggleScope('*')} className="h-auto px-2 py-1 text-xs">
                      {form.scopes.includes('*') ? '取消全权限' : '全权限'}
                    </Button>
                  </div>
                  <p className="mb-3 text-xs leading-relaxed text-muted-foreground">{t('以接口标注的权限为准。上传镜像还需要 image:toggle 和 admin:access；绑定容器的 Key 无法执行磁盘变更和备份操作。')}</p>
                  <div className="space-y-4">
                    {scopeGroups.map(group => (
                      <div key={group.title}>
                        <div className="mb-2 text-xs font-medium text-foreground">{group.title}</div>
                        <div className="grid gap-2 sm:grid-cols-2">
                          {group.scopes.map(([scope, label]) => (
                            <Label key={scope} className="flex items-center gap-2 rounded border border-border px-2 py-2 text-xs text-foreground hover:bg-muted/50 leading-normal">
                              <Checkbox
                                checked={form.scopes.includes('*') || form.scopes.includes(scope)}
                                disabled={form.scopes.includes('*')}
                                onCheckedChange={() => toggleScope(scope)}
                                className="h-4 w-4"
                              />
                              <span className="min-w-0 flex-1 truncate">{label}</span>
                              <code className="hidden shrink-0 font-mono text-[10px] text-muted-foreground sm:block">{scope}</code>
                            </Label>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
</Modal>
      )}
    </div>
  )
}

const requestBodySamples: Record<string, Record<string, unknown>> = {
  'PUT /api/v1/containers/{id}/installation-media': { image_id: 'custom-kvm-a1b2c3d4e5', boot: 'cdrom' },
  'POST /api/v1/containers/{id}/disks': { name: 'Data', size_gb: 20, storage_pool_id: 'disk-root' },
  'PUT /api/v1/containers/{id}/disks/{disk_id}': { size_gb: 40 },
  'PUT /api/v1/containers/{id}/disks/{disk_id}/attachment': { attached: true },
  'DELETE /api/v1/containers/{id}/disks/{disk_id}': { delete_data: true },
  'POST /api/v1/containers/{id}/backup': { name: 'Before maintenance' },
  'POST /api/v1/containers/{id}/clone': { name: 'vm-copy', assign_nat: true, port_mapping_count: 2 },
  'POST /api/v1/containers/{id}/template': { name: 'Base image' },
  'POST /api/v1/backups/{backup_id}/restore': { name: 'vm-restored', assign_nat: true, port_mapping_count: 2 },
  'POST /api/v1/instance-templates/{template_id}/instantiate': { name: 'vm-from-template', assign_nat: true, port_mapping_count: 2 },
  'POST /api/v1/containers/{id}/ipv6-port-mappings': { host_port: 30053, container_port: 53, protocol: 'udp', description: 'DNS' },
  'PUT /api/v1/containers/{id}/ipv6-port-mappings/{index}': { host_port: 30053, container_port: 5353, protocol: 'udp', description: 'DNS' },
  'PUT /api/v1/containers/{id}/ipv6-nat-sync': { enabled: false },
  'PUT /api/v1/ipv6/nat': { enabled: true, subnet: 'fd42:636c:6963::/64', interface: 'eth0' },
  'PUT /api/v1/sub-users/{id}/images': { allowed_image_ids: ['debian-bookworm'] },
  'POST /api/v1/containers/list': {},
  'POST /api/v1/containers': {
    name: 'demo-lxc-01',
    virtualization: 'lxc',
    privileged: false,
    nesting: false,
    template_id: 'debian-bookworm',
    storage_pool_id: 'disk-root',
    vcpu: 1,
    ram_mb: 512,
    disk_gb: 10,
    network_bw_mbps: 0,
    network_down_mbps: 100,
    network_up_mbps: 20,
    monthly_traffic_gb: 0,
    traffic_mode: 'total',
    traffic_in_gb: 0,
    traffic_out_gb: 0,
    io_speed_mbps: 0,
    io_read_mbps: 80,
    io_write_mbps: 30,
    extra_ports: [],
    nat_port_mappings: [
      {
        host_port: 30080,
        container_port: 80,
        protocol: 'tcp',
        description: 'HTTP',
      },
    ],
    management_port: 30022,
    port_mapping_count: 2,
    assign_nat: true,
    lan_ipv4_mode: '',
    lan_interface: '',
    lan_ipv4_address: '',
    lan_ipv4_prefix_len: 24,
    lan_ipv4_gateway: '',
    snapshot_limit: 1,
    allowed_image_ids: ['debian-bookworm'],
    image_limit_configured: true,
    assign_ipv4: false,
    ipv4_count: 1,
    public_ipv4s: [],
    assign_ipv6: true,
    ipv6_count: 1,
    ipv6_addresses: [],
    ssh_auth_mode: 'auto_password',
    ssh_password: '',
    ssh_public_key: '',
    expires_at: '',
  },
  'PATCH /api/v1/containers/{id}/name': { name: 'renamed-vm' },
  'POST /api/v1/containers/{id}/reinstall': {
    template_id: 'debian-bookworm',
    ssh_auth_mode: 'keep',
    ssh_password: '',
    ssh_public_key: '',
  },
  'PUT /api/v1/containers/{id}/traffic-limit': {
    traffic_mode: 'total',
    monthly_traffic_gb: 100,
    traffic_in_gb: 0,
    traffic_out_gb: 0,
  },
  'PUT /api/v1/containers/{id}/resource-limit': {
    vcpu: 1,
    ram_mb: 512,
    network_down_mbps: 100,
    network_up_mbps: 20,
    io_read_mbps: 80,
    io_write_mbps: 30,
    network_bw_mbps: 20,
    io_speed_mbps: 30,
  },
  'PUT /api/v1/containers/{id}/expiry': { expires_at: '2026-12-31 23:59:59' },
  'POST /api/v1/containers/{id}/reset-password': { password: 'NewPass123456' },
  'PUT /api/v1/containers/{id}/public-ipv4': {
    mode: 'random',
    count: 1,
  },
  'PUT /api/v1/containers/{id}/ipv6-addresses': {
    mode: 'custom',
    addresses: ['2001:db8:100::1005'],
  },
  'POST /api/v1/containers/{id}/port-mappings': {
    container_port: 8080,
    host_port: 61320,
    protocol: 'tcp',
    description: 'HTTP',
  },
  'PUT /api/v1/containers/{id}/port-mappings/{index}': {
    container_port: 8081,
    host_port: 61320,
    protocol: 'tcp',
    description: 'HTTP',
  },
  'POST /api/v1/containers/{id}/snapshots': { storage_pool_id: 'disk-root' },
  'POST /api/v1/containers/{id}/snapshots/schedule': {
    enabled: true,
    interval_hours: 24,
    time: '03:00',
  },
  'PUT /api/v1/containers/{id}/snapshots/quota': { snapshot_limit: 2 },
  'POST /api/v1/images/custom': {
    type: 'kvm',
    name: 'Custom Ubuntu Cloud',
    description: 'Private mirror image',
    distro: 'ubuntu',
    release: 'noble',
    arch: 'amd64',
    url: 'https://images.example.com/ubuntu-noble.qcow2',
    provisioner: 'linux-cloud-init',
    sha256: '',
  },
  'DELETE /api/v1/images/custom': { id: 'custom-kvm-a1b2c3d4e5' },
  'POST /api/v1/images/download': { template_id: 'debian-bookworm' },
  'POST /api/v1/images/cancel': { template_id: 'debian-bookworm' },
  'DELETE /api/v1/images/delete': { template_id: 'debian-bookworm' },
  'PUT /api/v1/images/toggle': { template_id: 'debian-bookworm', enabled: true },
  'PUT /api/v1/storage': {
    pools: [
      {
        id: 'disk-root',
        name: 'system (/)',
        path: '/var/lib/clicd',
        mount_point: '/',
        content_types: ['lxc', 'kvm', 'images', 'snapshots', 'backups'],
        default_contents: ['lxc', 'kvm', 'images', 'snapshots', 'backups'],
        enabled: true,
      },
    ],
  },
  'PUT /api/v1/task-queue/settings': { concurrency: 4 },
  'PUT /api/v1/ssl': {
    enabled: true,
    mode: 'letsencrypt',
    target: 'panel.example.com',
    email: 'admin@example.com',
    apply_now: false,
  },
  'PUT /api/v1/webssh-origins': {
    origins: ['https://panel.example.com'],
  },
  'PUT /api/v1/access-policy': {
    enabled: true,
    allowed_sources: ['203.0.113.10', '192.168.1.0/24', '2001:db8::/32'],
    trusted_proxies: ['127.0.0.1'],
  },
  'PUT /api/v1/language': { language: 'zh' },
  'PUT /api/v1/routing': {
    items: [
      {
        address: '203.0.113.10',
        interface: 'eth0',
        prefix_len: 32,
        gateway: '203.0.113.1',
      },
    ],
    ipv6_prefixes: [
      {
        address: '2001:db8:100::2',
        prefix: '2001:db8:100::/64',
        prefix_len: 64,
        interface: 'eth0',
        gateway: '2001:db8:100::1',
      },
    ],
  },
  'POST /api/v1/routing/ipv4-scan': {
    cidr: '203.0.113.0/29',
    interface: 'eth0',
    gateway: '203.0.113.1',
    verify: true,
    limit: 64,
  },
  'PUT /api/v1/containers/{id}/firewall': {
    enabled: true,
    default_action: 'DROP',
    rules: [
      { id: '', network: 'ipv4', direction: 'in', protocol: 'tcp', port: '22', source_ip: '', action: 'ACCEPT', description: 'Allow SSH over IPv4/NAT4', enabled: true },
      { id: '', network: 'ipv6', direction: 'in', protocol: 'tcp', port: '22', source_ip: '', action: 'ACCEPT', description: 'Allow SSH over IPv6', enabled: true },
      { id: '', network: 'all', direction: 'out', protocol: 'tcp', port: '', source_ip: '', action: 'ACCEPT', description: 'Allow outbound TCP', enabled: true },
    ],
  },
  'POST /api/v1/swap': { action: 'resize', size_mb: 16384 },
  'POST /api/v1/batch-create': {
    containers: [
      {
        name: 'batch-lxc-01',
        virtualization: 'lxc',
        privileged: false,
        nesting: false,
        template_id: 'debian-bookworm',
        vcpu: 1,
        ram_mb: 512,
        disk_gb: 10,
        assign_nat: true,
        management_port: 30022,
        port_mapping_count: 2,
        nat_port_mappings: [
          {
            host_port: 30080,
            container_port: 80,
            protocol: 'tcp',
            description: 'HTTP',
          },
        ],
        snapshot_limit: 1,
        assign_ipv4: false,
        ipv4_count: 1,
        public_ipv4s: [],
        assign_ipv6: true,
        ipv6_count: 1,
        ipv6_addresses: [],
        ssh_auth_mode: 'key',
        ssh_public_key: 'ssh-ed25519 AAAA... user@example',
      },
    ],
  },
  'POST /api/v1/batch-action': {
    action: 'reinstall',
    containers: [5],
    template_id: 'debian-bookworm',
    ssh_auth_mode: 'keep',
    ssh_password: '',
    ssh_public_key: '',
  },
  'POST /api/v1/ssh-ticket': { container_name: 'example-vm' },
  'POST /api/v1/vnc-ticket': { container_name: 'kvm-demo' },
  'POST /api/v1/sub-user/create': { container_name: 'example-vm' },
  'POST /api/v1/sub-users/{id}/rotate-password': {},
  'POST /api/v1/api-keys': {
    name: 'Automation',
    ip_whitelist: '',
    scopes: ['dashboard:read', 'container:read'],
    expires_at: '',
    disabled: false,
    container_uuids: [],
  },
  'PATCH /api/v1/api-keys/{id}': {
    name: 'Automation',
    ip_whitelist: '',
    scopes: ['dashboard:read', 'container:read'],
    expires_at: '',
    disabled: false,
    container_uuids: [],
  },
}

const backupID = 'bk-0123456789abcdef0123456789abcdef'
const backupSample = {
  id: backupID, name: 'Before maintenance', kind: 'backup', runtime: 'kvm', arch: 'amd64',
  created_at: '2026-11-07T06:30:00Z', source_name: 'example-vm', size_bytes: 134217728,
  disk_count: 2, format_version: 1, sha256: '<SHA-256>', credentials_preserved: true, cold_copy: true,
}
const templateID = 'tpl-0123456789abcdef0123456789abcdef'
const ipv6MappingSample = { host_port: 30053, container_port: 53, protocol: 'udp', description: 'DNS' }
const ipv6MappingStateSample = { enabled: true, sync_ipv4: false, host_ipv6: '2001:db8::2', guest_ipv6: 'fd42:636c:6963::10', limit: 2, mappings: [ipv6MappingSample] }
const ipv6NATSample = { settings: { enabled: true, subnet: 'fd42:636c:6963::/64', interface: 'eth0' }, available: true, reason: '', bridge: 'lxcbr0', gateway: 'fd42:636c:6963::1', host_address: '2001:db8::2', interface: 'eth0' }
const installationMediaSample = {
  supported: true, image_id: 'custom-kvm-a1b2c3d4e5', image_name: 'Debian Installer', boot: 'cdrom',
  manual_install: true, running: false,
  available: [{ id: 'custom-kvm-a1b2c3d4e5', name: 'Debian Installer', distro: 'debian', release: '12', arch: 'amd64', provisioner: 'manual-iso', format: 'iso' }],
}
const diskQueuedSample = { success: true, message: 'Disk operation queued', data: { task_id: 'task-10', status: 'pending' } }
const transferQueuedSample = { success: true, data: { task_id: 'task-10' } }

const responseSamples: Record<string, unknown> = {
  'GET /api/v1/version': { success: true, data: { version: '1.1.38' } },
  'GET /api/v1/tasks/{task_id}': { success: true, data: {
    id: 'task-10', type: 'backup', container_id: 5, container_name: 'example-vm', status: 'done',
    created_at: '2026-11-07 06:29:00', stage: 'completed', finished_at: '2026-11-07T06:30:00Z',
    logs: [{ time: '2026-11-07T06:29:00Z', stage: 'queued', detail: 'Queued' }],
    result: { artifact_id: backupID, artifact: backupSample },
  } },
  'GET /api/v1/containers/{id}/installation-media': { success: true, data: installationMediaSample },
  'PUT /api/v1/containers/{id}/installation-media': { success: true, message: 'Installation media updated', data: installationMediaSample },
  'GET /api/v1/containers/{id}/disks': { success: true, data: {
    system: { id: 'system', name: 'System', size_gb: 20, allocated_bytes: 1073741824, format: 'qcow2', target: 'vda', storage_pool_id: 'disk-root', attached: true, can_resize: true },
    data_disks: [], can_edit: true, limits: { max_data_disks: 8 },
  } },
  'POST /api/v1/containers/{id}/disks': diskQueuedSample,
  'PUT /api/v1/containers/{id}/disks/{disk_id}': diskQueuedSample,
  'PUT /api/v1/containers/{id}/disks/{disk_id}/attachment': diskQueuedSample,
  'DELETE /api/v1/containers/{id}/disks/{disk_id}': diskQueuedSample,
  'POST /api/v1/containers/{id}/backup': transferQueuedSample,
  'POST /api/v1/containers/{id}/clone': transferQueuedSample,
  'POST /api/v1/containers/{id}/template': transferQueuedSample,
  'GET /api/v1/backups': { success: true, data: [backupSample] },
  'POST /api/v1/backups': { success: true, data: backupSample },
  'DELETE /api/v1/backups/{backup_id}': { success: true },
  'POST /api/v1/backups/{backup_id}/restore': transferQueuedSample,
  'POST /api/v1/backups/{backup_id}/download-ticket': { success: true, data: { url: '/api/v1/backup-download/<ticket>', expires_at: '2026-11-07T06:32:00Z' } },
  'GET /api/v1/instance-templates': { success: true, data: [{ ...backupSample, id: templateID, kind: 'template', name: 'Base image' }] },
  'POST /api/v1/instance-templates/{template_id}/instantiate': transferQueuedSample,
  'DELETE /api/v1/instance-templates/{template_id}': { success: true },
  'GET /api/v1/containers/{id}/port-mappings': { success: true, data: [{ container_port: 22, host_port: 22004, protocol: 'tcp', description: 'SSH' }] },
  'GET /api/v1/containers/{id}/ipv6-port-mappings': { success: true, data: ipv6MappingStateSample },
  'POST /api/v1/containers/{id}/ipv6-port-mappings': { success: true, data: [ipv6MappingSample] },
  'PUT /api/v1/containers/{id}/ipv6-port-mappings/{index}': { success: true, data: [{ ...ipv6MappingSample, container_port: 5353 }] },
  'DELETE /api/v1/containers/{id}/ipv6-port-mappings/{index}': { success: true, data: [] },
  'PUT /api/v1/containers/{id}/ipv6-nat-sync': { success: true, data: ipv6MappingStateSample },
  'GET /api/v1/ipv6/nat': { success: true, data: ipv6NATSample },
  'PUT /api/v1/ipv6/nat': { success: true, data: ipv6NATSample },
  'POST /api/v1/images/upload': { success: true, data: { id: 'custom-kvm-a1b2c3d4e5', name: 'Debian Installer', format: 'iso', size_bytes: 734003200, sha256: '<SHA-256>' } },
  'PUT /api/v1/sub-users/{id}/images': { success: true, data: { id: 'a1b2c3d4', username: 'user-xxxxxxxx', allowed_image_ids: ['debian-bookworm'], image_limit_configured: true } },
  'GET /api/v1/dashboard': { success: true, data: { running: 31, stopped: 0, total_containers: 31 } },
  'GET /api/v1/host-info': {
    success: true,
    data: {
      cpu: { cores: 8, usage_pct: 1.16 },
      ram: { total_mb: 31825, used_mb: 1275, free_mb: 30550 },
      disk: { total_gb: 1750.49, used_gb: 123.98, free_gb: 1626.51 },
      network: {
        public_ipv4: '203.0.113.10',
        public_ipv4_interface: 'eth0',
        public_ipv6: '2001:db8:100::2',
        public_ipv6_interface: 'eth0',
      },
      load: { load1: 0.01, load5: 0.03, load15: 0.01 },
    },
  },
  'GET /api/v1/host-history': {
    success: true,
    data: [
      {
        ts: 1784642400000,
        cpu: 8.4,
        memory: 21.3,
        network: 12288,
        network_rx: 10240,
        network_tx: 2048,
        disk_io: 1052672,
        disk_read: 4096,
        disk_write: 1048576,
        disk_usage_pct: 18.8,
      },
    ],
  },
  'GET /api/v1/host-report': {
    success: true,
    data: {
      generated_at: '2026-07-21 14:00:00',
      hostname: 'ubuntu',
      os: 'Ubuntu 22.04.5 LTS',
      kernel: 'Linux 6.8.0-1054-oracle aarch64 GNU/Linux',
      cpu: { model: 'Neoverse-N1', cores: 4, threads: 4, architecture: 'arm64', virtualization: true },
      memory: { total_mb: 11980, used_mb: 2100, free_mb: 9880, modules: [] },
      runtime: { lxc_available: true, kvm_available: false, support_mode: 'lxc_only' },
      public_ipv4: [{ address: '203.0.113.10', interface: 'eth0' }],
      ipv6_prefixes: [],
    },
  },
  'GET /api/v1/routing': {
    success: true,
    data: {
      nat4: { used: 62, remaining: '45474', total: '45536' },
      nat4_port_range: { start: 20000, end: 65535 },
      nat4_next_port: 22005,
      nat4_networks: {
        lxc: { subnet: '10.0.3.0/24', gateway: '10.0.3.1', netmask: '255.255.255.0', dhcp_start: '10.0.3.2', dhcp_end: '10.0.3.254', dhcp_max: 253, prefix_bits: 24 },
        kvm: { subnet: '192.168.122.0/24', gateway: '192.168.122.1', netmask: '255.255.255.0', dhcp_start: '192.168.122.2', dhcp_end: '192.168.122.254', dhcp_max: 253, prefix_bits: 24 },
      },
      ipv4: { used: 1, remaining: '3', total: '4' },
      ipv6: { used: 31, remaining: 'large', total: 'large' },
      public_ipv4_addresses: [{ address: '203.0.113.10', interface: 'eth0', prefix_len: 32, gateway: '203.0.113.1' }],
      ipv4_assignments: [{ container_id: 5, container_name: 'example-vm', address: '203.0.113.10', interface: 'eth0', prefix_len: 32, gateway: '203.0.113.1' }],
      nat4_mappings: [
        { container_id: 5, container_name: 'example-vm', status: 'running', ip: '10.0.0.10', host_port: 22004, container_port: 22, protocol: 'tcp' },
      ],
      ipv6_assignments: [{ container_id: 5, container_name: 'example-vm', address: '2001:db8:100::1005', prefix_len: 64, interface: 'eth0' }],
    },
  },
  'PUT /api/v1/routing': {
    success: true,
    data: {
      nat4: { used: 62, remaining: '45474', total: '45536' },
      nat4_port_range: { start: 20000, end: 65535 },
      nat4_next_port: 22005,
      nat4_networks: {
        lxc: { subnet: '10.0.3.0/24', gateway: '10.0.3.1' },
        kvm: { subnet: '192.168.122.0/24', gateway: '192.168.122.1' },
      },
      ipv4: { used: 1, remaining: '3', total: '4' },
      public_ipv4_addresses: [{ address: '203.0.113.10', interface: 'eth0', prefix_len: 32, gateway: '203.0.113.1' }],
      ipv6_prefixes: [{ interface: 'eth0', address: '2001:db8:100::2', prefix: '2001:db8:100::/64', prefix_len: 64, gateway: '2001:db8:100::1' }],
    },
  },
  'POST /api/v1/routing/ipv4-scan': {
    success: true,
    data: [
      { address: '203.0.113.10', interface: 'eth0', prefix_len: 32, gateway: '203.0.113.1', status: 'available', usable: true, reason: '' },
    ],
  },
  'GET /api/v1/ipv6/status': {
    success: true,
    data: {
      available: true,
      reachable: true,
      reason: 'usable public IPv6 prefix detected',
      prefixes: [{ interface: 'eth0', address: '2001:db8:100::2', prefix: '2001:db8:100::/64', prefix_len: 64, gateway: '2001:db8:100::1' }],
    },
  },
  'GET /api/v1/tasks': { success: true, data: [] },
  'DELETE /api/v1/tasks/{task_id}': { success: true, message: 'Task deleted' },
  'GET /api/v1/containers': {
    success: true,
    data: [
      {
        id: 5,
        uuid: '00000000-0000-4000-8000-000000000005',
        name: 'example-vm',
        virtualization: 'lxc',
        lxc_privileged: true,
        lxc_nesting: true,
        template: 'debian-bullseye',
        vcpu: 1,
        ram_mb: 512,
        disk_gb: 10,
        status: 'running',
        ip: '10.0.0.10',
        ipv6: '2001:db8:100::1005',
        ssh_port: 22004,
        ssh_password: '***',
        port_mappings: [
          { container_port: 22, host_port: 22004, protocol: 'tcp', description: 'SSH' },
          { container_port: 20000, host_port: 20000, protocol: 'tcp', description: 'Port-20000' },
        ],
      },
    ],
  },
  'POST /api/v1/containers/list': {
    success: true,
    data: [
      { id: 5, uuid: '00000000-0000-4000-8000-000000000005', name: 'example-vm', status: 'running', ip: '10.0.0.10' },
    ],
  },
  'POST /api/v1/containers': { success: true, message: 'Container created successfully' },
  'GET /api/v1/containers/{id|uuid|name}': {
    success: true,
    data: {
      id: 5,
      uuid: '00000000-0000-4000-8000-000000000005',
      name: 'example-vm',
      virtualization: 'lxc',
      lxc_privileged: true,
      lxc_nesting: true,
      status: 'running',
      ip: '10.0.0.10',
      ipv6: '2001:db8:100::1005',
      ssh_port: 22004,
      ssh_password: '***',
    },
  },
  'PATCH /api/v1/containers/{id}/name': {
    success: true,
    message: 'Container name updated',
    data: { id: 5, uuid: '00000000-0000-4000-8000-000000000005', name: 'renamed-vm', kvm_name: 'vm-5' },
  },
  'POST /api/v1/containers/{id}/start': queuedTaskSample('start'),
  'POST /api/v1/containers/{id}/stop': queuedTaskSample('stop'),
  'POST /api/v1/containers/{id}/restart': queuedTaskSample('restart'),
  'POST /api/v1/containers/{id}/reinstall': queuedTaskSample('reinstall'),
  'DELETE /api/v1/containers/{id}/delete': queuedTaskSample('delete'),
  'GET /api/v1/containers/{id}/usage': {
    success: true,
    data: {
      cpu_usage_pct: 0,
      cpu_usage_usec: 3908852,
      memory_usage_bytes: 29331456,
      disk_usage_bytes: 515100672,
      network_rx_bytes: 131232,
      network_tx_bytes: 16828,
      load1: 0.1,
      load5: 0.06,
      load15: 0.01,
    },
  },
  'GET /api/v1/containers/{id}/history': {
    success: true,
    data: [
      { ts: 1784642400000, cpu: 1.2, memory: 5.6, network: 4096, network_rx: 3072, network_tx: 1024, disk_io: 8192, disk_read: 2048, disk_write: 6144 },
    ],
  },
  'GET /api/v1/containers/{id}/traffic': {
    success: true,
    data: {
      mode: 'total',
      limit_gb: 0,
      in_limit_gb: 0,
      out_limit_gb: 0,
      total_used_bytes: 142082,
      rx_used_bytes: 127212,
      tx_used_bytes: 14870,
      used_pct: 0,
      reset_date: '2026-06',
    },
  },
  'POST /api/v1/containers/{id}/traffic-reset': { success: true, message: 'Traffic reset' },
  'PUT /api/v1/containers/{id}/traffic-limit': { success: true, message: 'Traffic limit updated' },
  'PUT /api/v1/containers/{id}/resource-limit': { success: true, message: 'Resource limits updated' },
  'PUT /api/v1/containers/{id}/expiry': { success: true, message: 'Expiry updated' },
  'POST /api/v1/containers/{id}/reset-password': { success: true, message: 'SSH password reset successfully', data: { password: '***' } },
  'POST /api/v1/containers/{id}/ipv6': { success: true, message: 'IPv6 assigned', data: { id: 5, name: 'example-vm', ipv6: '2001:db8:100::1005' } },
  'PUT /api/v1/containers/{id}/public-ipv4': {
    success: true,
    message: 'Public IPv4 assignments updated',
    data: { id: 5, name: 'example-vm', public_ipv4s: ['203.0.113.10'] },
  },
  'PUT /api/v1/containers/{id}/ipv6-addresses': {
    success: true,
    message: 'IPv6 assignments updated',
    data: { id: 5, name: 'example-vm', ipv6_addresses: ['2001:db8:100::1005'] },
  },
  'GET /api/v1/containers/{id}/random-port': { success: true, data: { port: 61320 } },
  'POST /api/v1/containers/{id}/port-mappings': {
    success: true,
    data: [
      { container_port: 22, host_port: 22004, protocol: 'tcp', description: 'SSH' },
      { container_port: 8080, host_port: 61320, protocol: 'tcp', description: 'HTTP' },
    ],
  },
  'PUT /api/v1/containers/{id}/port-mappings/{index}': {
    success: true,
    data: [{ container_port: 8081, host_port: 61320, protocol: 'tcp', description: 'HTTP' }],
  },
  'DELETE /api/v1/containers/{id}/port-mappings/{index}': { success: true, data: [] },
  'GET /api/v1/containers/{id}/firewall': {
    success: true,
    data: {
      enabled: true,
      default_action: 'DROP',
      rules: [
        { id: 'a1b2c3d4', network: 'ipv4', direction: 'in', protocol: 'tcp', port: '22', source_ip: '', action: 'ACCEPT', description: 'Allow SSH over IPv4/NAT4', enabled: true },
        { id: 'e5f6g7h8', network: 'ipv6', direction: 'in', protocol: 'tcp', port: '22', source_ip: '', action: 'ACCEPT', description: 'Allow SSH over IPv6', enabled: true },
        { id: 'i9j0k1l2', network: 'all', direction: 'out', protocol: 'tcp', port: '', source_ip: '', action: 'ACCEPT', description: 'Allow outbound TCP', enabled: true },
      ],
    },
  },
  'PUT /api/v1/containers/{id}/firewall': {
    success: true,
    message: 'Firewall updated',
    data: {
      enabled: true,
      default_action: 'DROP',
      rules: [
        { id: 'a1b2c3d4', network: 'ipv4', direction: 'in', protocol: 'tcp', port: '22', source_ip: '', action: 'ACCEPT', description: 'Allow SSH over IPv4/NAT4', enabled: true },
        { id: 'e5f6g7h8', network: 'ipv6', direction: 'in', protocol: 'tcp', port: '22', source_ip: '', action: 'ACCEPT', description: 'Allow SSH over IPv6', enabled: true },
        { id: 'i9j0k1l2', network: 'all', direction: 'out', protocol: 'tcp', port: '', source_ip: '', action: 'ACCEPT', description: 'Allow outbound TCP', enabled: true },
      ],
    },
  },
  'GET /api/v1/snapshots': { success: true, data: null },
  'GET /api/v1/containers/{id}/snapshots': {
    success: true,
    data: { quota: 1, schedule: { enabled: false, interval_hours: 0, last_run: '', next_run: '', time: '', created_by: '' }, snapshots: [] },
  },
  'POST /api/v1/containers/{id}/snapshots': {
    success: true,
    data: { id: 'snap-20260608-001', container_id: 5, container_name: 'example-vm', created_at: '2026-06-08 16:00:00', created_by: 'api:Automation', scheduled: false, size_bytes: 10485760 },
  },
  'DELETE /api/v1/containers/{id}/snapshots/{snapshot_id}': { success: true, message: 'Snapshot deleted' },
  'POST /api/v1/containers/{id}/snapshots/{snapshot_id}/restore': { success: true, message: 'Snapshot restored' },
  'POST /api/v1/containers/{id}/snapshots/schedule': { success: true, data: { container: { id: 5, name: 'example-vm', snapshot_schedule_enabled: true, snapshot_schedule_interval_hours: 24, snapshot_schedule_time: '03:00' } } },
  'PUT /api/v1/containers/{id}/snapshots/quota': { success: true, data: { quota: 2, container: { id: 5, name: 'example-vm', snapshot_limit: 2 } } },
  'GET /api/v1/templates': {
    success: true,
    data: [
      { id: 'ubuntu-noble', name: 'Ubuntu 24.04', distro: 'ubuntu', release: 'noble', arch: 'amd64', description: 'Ubuntu 24.04 LTS' },
      { id: 'debian-bookworm', name: 'Debian 12', distro: 'debian', release: 'bookworm', arch: 'amd64', description: 'Debian 12 (Bookworm)' },
    ],
  },
  'GET /api/v1/images': {
    success: true,
    data: [
      { id: 'ubuntu-noble', name: 'Ubuntu 24.04', type: 'lxc', downloaded: true, enabled: true, downloading: false, progress: 0, size_bytes: 135005452 },
    ],
  },
  'GET /api/v1/images/enabled?type=lxc&container={id}': {
    success: true,
    data: [
      { id: 'debian-bookworm', name: 'Debian 12', distro: 'debian', release: 'bookworm', arch: 'amd64', type: 'lxc', downloaded: true, enabled: true },
    ],
  },
  'POST /api/v1/images/custom': {
    success: true,
    message: 'Custom image added',
    data: { id: 'custom-kvm-a1b2c3d4e5', name: 'Custom Ubuntu Cloud' },
  },
  'DELETE /api/v1/images/custom': { success: true, message: 'Custom image removed' },
  'POST /api/v1/images/download': { success: true, message: 'Already downloaded' },
  'POST /api/v1/images/cancel': { success: true, message: 'Cancel requested' },
  'DELETE /api/v1/images/delete': { success: true, message: 'Deleted' },
  'PUT /api/v1/images/toggle': { success: true, message: 'OK' },
  'GET /api/v1/storage': {
    success: true,
    data: {
      pools: [
        {
          id: 'disk-root',
          name: 'system (/)',
          path: '/var/lib/clicd',
          mount_point: '/',
          content_types: ['lxc', 'kvm', 'images', 'snapshots', 'backups'],
          default_contents: ['lxc', 'kvm', 'images', 'snapshots', 'backups'],
          enabled: true,
          available: true,
          free_bytes: 54653493248,
        },
      ],
      disks: [
        { name: 'sda2', path: '/dev/sda2', fstype: 'ext4', mount_point: '/', size_bytes: 67331063808, used_bytes: 12677570560, free_bytes: 54653493248 },
      ],
      content_types: ['lxc', 'kvm', 'images', 'snapshots', 'backups'],
    },
  },
  'PUT /api/v1/storage': {
    success: true,
    data: {
      pools: [{ id: 'disk-root', path: '/var/lib/clicd', mount_point: '/', content_types: ['lxc', 'kvm', 'images', 'snapshots', 'backups'], enabled: true, available: true }],
      disks: [],
      content_types: ['lxc', 'kvm', 'images', 'snapshots', 'backups'],
    },
  },
  'GET /api/v1/task-queue/settings': { success: true, data: { concurrency: 4, active: 1, pending: 2 } },
  'PUT /api/v1/task-queue/settings': { success: true, message: '任务队列设置已保存', data: { concurrency: 4, active: 1, pending: 2 } },
  'GET /api/v1/ssl': {
    success: true,
    data: { enabled: true, mode: 'letsencrypt', target: 'panel.example.com', email: 'admin@example.com', detected_host: 'panel.example.com', certificate: { subject: 'panel.example.com', issuer: "Let's Encrypt", dns_names: ['panel.example.com'], ip_names: [], valid: true } },
  },
  'PUT /api/v1/ssl': { success: true, message: 'SSL settings saved', data: { enabled: true, mode: 'letsencrypt', target: 'panel.example.com', needs_restart: true } },
  'GET /api/v1/webssh-origins': { success: true, data: { origins: ['https://panel.example.com'], current_origin: 'https://panel.example.com' } },
  'PUT /api/v1/webssh-origins': { success: true, message: 'Origin allowlist saved', data: { origins: ['https://panel.example.com'], current_origin: 'https://panel.example.com' } },
  'GET /api/v1/access-policy': { success: true, data: { enabled: true, allowed_sources: ['203.0.113.10', '192.168.1.0/24'], trusted_proxies: ['127.0.0.1'], current_source: '203.0.113.10', direct_source: '127.0.0.1', using_forwarded: true } },
  'PUT /api/v1/access-policy': { success: true, message: 'Panel access policy saved', data: { enabled: true, allowed_sources: ['203.0.113.10', '192.168.1.0/24'], trusted_proxies: ['127.0.0.1'], current_source: '203.0.113.10', direct_source: '127.0.0.1', using_forwarded: true } },
  'GET /api/v1/language': { success: true, data: { language: 'zh' } },
  'PUT /api/v1/language': { success: true, data: { language: 'zh' } },
  'GET /api/v1/swap': { success: true, data: { total_mb: 16383, used_mb: 0, free_mb: 16383, enabled: true, swap_file: '/swapfile' } },
  'POST /api/v1/swap': { success: true, message: 'SWAP adjusted to 16384 MB', data: { total_mb: 16383, used_mb: 0, free_mb: 16383, enabled: true, swap_file: '/swapfile' } },
  'POST /api/v1/batch-create': { success: true, data: ['task-12'] },
  'POST /api/v1/batch-action': { success: true, data: ['task-13'] },
  'POST /api/v1/ssh-ticket': { success: true, data: { ticket: '***60-second valid ticket***' } },
  'POST /api/v1/vnc-ticket': { success: true, data: { ticket: '***60-second valid ticket***' } },
  'POST /api/v1/sub-user/create': {
    success: true,
    message: 'Sub-user created',
    data: { id: 'sub-xxxxxxxx', username: 'user-xxxxxxxx', password: '***', container_names: ['example-vm'], access_code: '********', created_at: '2026-06-08 16:00:00' },
  },
  'GET /api/v1/sub-users': { success: true, data: [] },
  'POST /api/v1/sub-users/{id}/rotate-password': { success: true, data: { username: 'user-xxxxxxxx', password: '***', access_code: '********' } },
  'GET /api/v1/sub-users/{id}/audit-logs': { success: true, data: [] },
  'GET /api/v1/sub-users/{id}/login-logs': { success: true, data: [] },
  'GET /api/v1/audit-logs': {
    success: true,
    data: [{ time: '2026-06-08 15:44:40', action: 'apikey.create', target: 'Test', detail: 'scopes=*', user: 'admin', success: true }],
  },
  'GET /api/v1/login-logs': {
    success: true,
    data: [{ time: '2026-06-08 08:24:00 UTC', username: 'admin', ip: '198.51.100.23', user_agent: 'Mozilla/5.0 ...', success: true }],
  },
  'GET /api/v1/api-keys': {
    success: true,
    data: [{ id: 'c271023f', name: 'Test', prefix: 'clicd_sk_dd9d...', ip_whitelist: '', created_at: '2026-06-08 15:44:40', last_used: '2026-06-08 15:46:10', scopes: ['*'], last_used_ip: '198.51.100.23' }],
  },
  'POST /api/v1/api-keys': {
    success: true,
    message: "API key created. Save this key now - it won't be shown again.",
    data: { id: 'a1b2c3d4', name: 'Automation', key: 'clicd_sk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx', prefix: 'clicd_sk_xxxx...', scopes: ['dashboard:read', 'container:read'] },
  },
  'PATCH /api/v1/api-keys/{id}': { success: true, data: { id: 'a1b2c3d4', name: 'Automation', prefix: 'clicd_sk_xxxx...', scopes: ['dashboard:read', 'container:read'], disabled: false } },
  'DELETE /api/v1/api-keys/{id}': { success: true, message: 'API key deleted' },
}

function queuedTaskSample(action: string) {
  return {
    success: true,
    message: 'Task queued',
    data: { task_id: 'task-10', container_name: 'example-vm', status: 'pending', action },
  }
}

function buildEndpointDoc(method: HttpMethod, path: string, desc: string, access: string): EndpointDoc {
  const key = `${method} ${path}`
  const isBackupFile = path.endsWith('/backups/{backup_id}') || path.endsWith('/backup-download/{ticket}')
  const download = isBackupFile && method === 'GET' ? 'backup.clicd.tar.gz' : undefined
  const upload = key === 'POST /api/v1/backups'
    ? { file: 'backup.clicd.tar.gz' }
    : key === 'POST /api/v1/images/upload'
      ? { file: 'debian.iso', fields: { name: 'Debian Installer', provisioner: 'manual-iso' } }
      : undefined
  return {
    method,
    path,
    desc,
    access,
    examplePath: examplePathFor(method, path),
    body: requestBodySamples[key],
    upload,
    download,
    response: isBackupFile && (method === 'GET' || method === 'HEAD')
      ? 'Content-Type: application/gzip\nContent-Disposition: attachment\nAccept-Ranges: bytes'
      : responseSamples[key] || defaultResponseFor(method),
    note: endpointNoteFor(key),
  }
}

function examplePathFor(method: HttpMethod, path: string) {
  return path
    .replace('{id|uuid|name}', '5')
    .replace('{id}', /\/(api-keys|sub-users)\//.test(path) ? 'a1b2c3d4' : '5')
    .replace('{task_id}', 'task-10')
    .replace('{index}', '1')
    .replace('{snapshot_id}', 'snap-20260608-001')
    .replace('{name}', 'example-vm')
    .replace('{disk_id}', method === 'PUT' && path.endsWith('/{disk_id}') ? 'system' : 'disk-00000000-0000-4000-8000-000000000001')
    .replace('{backup_id}', backupID)
    .replace('{template_id}', templateID)
    .replace('{ticket}', '<ticket>')
}

function endpointNoteFor(key: string) {
  const notes: string[] = []
  if (key === 'POST /api/v1/containers') {
    notes.push('Linux container creation supports ssh_auth_mode=auto_password|password|key. Public IPv4, IPv6, and NAT can be configured with assign_nat, assign_ipv4, and assign_ipv6.')
    notes.push('Set management_port to choose the public/source port for SSH (target 22) or Windows RDP (target 3389). Omit it or pass 0 for automatic allocation.')
    notes.push('For other custom NAT rules, use nat_port_mappings with host_port (public/source port), container_port (target port), and protocol=tcp|udp. extra_ports remains accepted for compatibility and maps each port to the same port inside the container.')
    notes.push('Supports independent upload/download bandwidth limits and read/write I/O limits. network_bw_mbps and io_speed_mbps are deprecated symmetric compatibility aliases. New integrations should use network_down_mbps, network_up_mbps, io_read_mbps, and io_write_mbps.')
    notes.push('storage_pool_id selects an enabled disk for the runtime. For an LXC with an independent LAN address, set lan_ipv4_mode=dhcp or static and set assign_nat=false; static mode also requires lan_ipv4_address, lan_ipv4_prefix_len, and lan_ipv4_gateway.')
    notes.push('allowed_image_ids and image_limit_configured define which downloaded images the container owner may use for reinstall. Include the initial template ID when it should remain reinstallable.')
    notes.push('LXC-only: nesting=true enables Docker/LXC-in-LXC and some systemd workloads. privileged=true creates a trusted privileged container, requires administrator permission, and should not be used for untrusted tenants; existing containers cannot switch modes in place. On KVM, setting either option to true returns 400; generic KVM minimums are 1 vCPU, 1024 MB RAM, and 10 GB disk, subject to higher image requirements.')
  }
  if (key === 'PATCH /api/v1/containers/{id}/name') {
    notes.push('Only the panel display name changes. The runtime name, UUID, storage paths, MAC address, and guest hostname remain unchanged. Requires container:resize scope.')
  }
  if (key === 'POST /api/v1/containers/{id}/reinstall') {
    notes.push('Reinstall supports ssh_auth_mode=keep|auto_password|password|key. keep is only for reinstall requests; if SSH fields are omitted, the existing behavior is kept.')
  }
  if (key === 'POST /api/v1/batch-create') {
    notes.push('Each containers[] item in batch creation supports the same storage, network, image allowlist, and SSH authentication fields as POST /api/v1/containers.')
    notes.push('Each item also accepts privileged and nesting for LXC only. privileged=true requires administrator permission; setting either option to true for KVM returns 400.')
    notes.push('Custom management_port and NAT host_port values must be unique across the batch. The panel places each later source-port group after the previous container\'s highest public port while keeping every target container_port unchanged; direct API clients should submit the expanded values explicitly.')
  }
  if (key === 'PUT /api/v1/containers/{id}/resource-limit') {
    notes.push('Supports independent download/upload bandwidth limits and read/write I/O limits. Omitted fields keep their current values, and explicitly passing 0 makes that direction unlimited. network_bw_mbps and io_speed_mbps are deprecated symmetric compatibility aliases.')
  }
  if (key === 'PUT /api/v1/containers/{id}/firewall') {
    notes.push('Backward compatible: default_action is optional; if omitted, the existing policy is kept. rule.network is optional; if omitted, it is treated as ipv4. default_action: DROP=deny unmatched traffic, ACCEPT=allow unmatched traffic. network: ipv4=IPv4 NAT/public IPv4, ipv6=IPv6, all=apply to both IPv4 and IPv6. For NAT inbound rules, port is the container internal port, not the host public port.')
  }
  if (key.includes('/port-mappings') || key.includes('/ipv6-port-mappings')) {
    notes.push('Port mappings accept tcp and udp. The mapping index starts at 0.')
  }
  if (key.includes('/ipv6-port-mappings') || key.includes('/ipv6-nat-sync')) {
    notes.push('IPv6 NAT requires an eligible LXC container. Disable IPv4 synchronization before changing its separate IPv6 mappings.')
  }
  if (key.includes('/installation-media')) {
    notes.push('KVM only. Stop the VM before changing installation media. To eject the ISO, use image_id="" and boot="disk".')
  }
  if (key.includes('/disks') && !key.startsWith('GET ')) {
    notes.push('Returns HTTP 202. Poll GET /api/v1/tasks/{task_id} to confirm completion. Disk changes require an API key without container binding. Only growth is supported; use disk ID "system" for the system disk.')
  }
  if (key.startsWith('POST ') && ['/backup', '/clone', '/template', '/restore', '/instantiate'].some(suffix => key.endsWith(suffix)) && !key.includes('/snapshots/')) {
    notes.push('Returns HTTP 202 with data.task_id. Poll GET /api/v1/tasks/{task_id}. A full copy may stop and then restart the source instance.')
  }
  if (key.includes('/backups') || key.includes('/instance-templates') || key.endsWith('/backup') || key.endsWith('/clone') || key.endsWith('/template')) {
    notes.push('Requires a key without container binding. Backup archives may contain guest credentials and data.')
  }
  if (key === 'POST /api/v1/backups') notes.push('Send a CLICD backup archive as a binary body or multipart/form-data with the file field. Use the optional storage_pool_id query parameter to choose a pool. Returns HTTP 201 after verification.')
  if (key.includes('/backup-download/') || key.endsWith('/download-ticket')) notes.push('The ticket expires after two minutes. Download using the returned data.url and keep the ticket private.')
  if (key === 'POST /api/v1/images/upload') notes.push('Upload a complete ISO or standalone QCOW2 file (up to 32 GiB). Use multipart/form-data, optionally pass storage_pool_id in the query, and leave the API key unbound. The server validates and enables the image before returning HTTP 201.')
  if (key === 'POST /api/v1/batch-action') {
    notes.push('When action=reinstall, you can include template_id, ssh_auth_mode, ssh_password, and ssh_public_key. Other actions ignore these reinstall fields.')
  }
  if (key === 'PUT /api/v1/routing') {
    notes.push('Updating NAT4 port range and public address pools requires routing:write. Addresses already assigned to containers cannot be removed from the pool.')
  }
  if (key === 'POST /api/v1/routing/ipv4-scan') {
    notes.push('Scanning public IPv4 prefixes requires routing:write. When verify=true, the API also attempts to check address availability.')
  }
  if (key === 'GET /api/v1/host-history' || key === 'GET /api/v1/containers/{id}/history') {
    notes.push('Metrics are collected in the background every 30 seconds, even when the statistics page is closed.')
  }
  if (key === 'PUT /api/v1/containers/{id}/public-ipv4' || key === 'PUT /api/v1/containers/{id}/ipv6-addresses') {
    notes.push('mode accepts random, custom, or clear. random uses count, custom uses addresses, and clear removes all assignments of that address family.')
  }
  if (key === 'GET /api/v1/images/enabled?type=lxc&container={id}') {
    notes.push('type accepts lxc or kvm. Supplying container applies that container image allowlist; omit container when listing images for a new container.')
  }
  if (key === 'POST /api/v1/containers/{id}/snapshots') {
    notes.push('storage_pool_id is optional. The selected pool must be enabled for snapshots; otherwise the server chooses an available snapshot pool by free space and default priority.')
  }
  if (key === 'PUT /api/v1/storage') {
    notes.push('Start from GET /api/v1/storage and submit mounted disks returned by the server. Paths and mount points are server-managed and custom paths are rejected. content_types enables a disk for each workload; only one pool may be the default for each type.')
  }
  if (key.includes('/api/v1/storage') || key.includes('/task-queue/settings') || key.includes('/api/v1/ssl') || key.includes('/webssh-origins') || key.includes('/access-policy')) {
    notes.push('This endpoint requires an API key with admin:access.')
  }
  if (key === 'PUT /api/v1/access-policy') {
    notes.push('allowed_sources and trusted_proxies accept IPv4, IPv6, or CIDR values. Forwarded client headers are ignored unless the direct peer matches trusted_proxies. The server rejects an enabled policy that excludes the current source.')
  }
  if (key === 'PUT /api/v1/task-queue/settings') {
    notes.push('concurrency must be between 1 and 16. Tasks targeting the same container are still serialized.')
  }
  if (key === 'PUT /api/v1/ssl') {
    notes.push('mode accepts disabled, letsencrypt, self_signed, or uploaded. uploaded mode uses cert_pem and key_pem. apply_now requests a service restart after saving.')
  }
  if (key.includes('/vnc-ticket')) notes.push('WebVNC only applies to KVM VMs; LXC containers return "VNC console is only available for KVM VMs".')
  if (key.includes('/containers/{id}/delete') || key.includes('/batch-action')) notes.push('This API enters the task queue. Call GET /api/v1/tasks afterward to check execution status.')
  if (key.includes('/reset-password') || key.includes('/api-keys') || key.includes('/sub-user')) notes.push('Keys, passwords, and tickets in examples are masked. Full secrets from create APIs appear only once in the creation response.')
  return notes.join(' ')
}

function defaultResponseFor(method: HttpMethod) {
  if (method === 'GET') return { success: true, data: [] }
  if (method === 'DELETE') return { success: true, message: 'Deleted' }
  return { success: true, message: 'OK' }
}

function buildPythonExample(doc: EndpointDoc) {
  const hasBody = doc.body !== undefined && doc.method !== 'GET'
  const needsKey = doc.access !== '公开' && doc.access !== '下载票据'
  const header = needsKey ? '{"X-API-Key": API_KEY}' : '{}'
  const url = `f"{BASE_URL}${doc.examplePath}"`
  const setup = [
    'import requests',
    '',
    `BASE_URL = "${SAMPLE_BASE_URL}"`,
    ...(needsKey ? ['API_KEY = "clicd_sk_xxxx"'] : []),
    '',
  ]
  if (doc.upload) {
    const { file, fields } = doc.upload
    const multipart = fields !== undefined
    return [
      ...setup,
      ...(multipart ? ['# pip install requests-toolbelt', 'from requests_toolbelt.multipart.encoder import MultipartEncoder', ''] : []),
      `with open("${file}", "rb") as file:`,
      ...(multipart
        ? [
            `    fields = ${JSON.stringify(fields)}`,
            `    fields["file"] = ("${file}", file, "application/octet-stream")`,
            '    upload = MultipartEncoder(fields=fields)',
            '    content_type = upload.content_type',
          ]
        : ['    upload = file', '    content_type = "application/gzip"']),
      `    response = requests.post(${url}, data=upload,`,
      '                             headers={"X-API-Key": API_KEY, "Content-Type": content_type},',
      '                             timeout=(10, 600))',
      '    response.raise_for_status()',
      '    print(response.json())',
    ].join('\n')
  }
  if (doc.download) {
    return [
      ...setup,
      `with requests.get(${url}, headers=${header}, stream=True, timeout=(10, 120)) as response:`,
      '    response.raise_for_status()',
      `    with open("${doc.download}", "wb") as output:`,
      '        for chunk in response.iter_content(chunk_size=1024 * 1024):',
      '            output.write(chunk)',
    ].join('\n')
  }
  return [
    ...(hasBody ? ['import json'] : []),
    ...setup,
    ...(hasBody ? [`payload = json.loads(r'''${formatJSON(doc.body)}''')`, ''] : []),
    `response = requests.${doc.method.toLowerCase()}(`,
    `    ${url},`,
    ...(needsKey ? [`    headers=${header},`] : []),
    ...(hasBody ? ['    json=payload,'] : []),
    '    timeout=30,',
    ')',
    'response.raise_for_status()',
    doc.method === 'HEAD' ? 'print(response.headers)' : 'print(response.json())',
  ].join('\n')
}

function formatJSON(value: unknown) {
  return JSON.stringify(value, null, 2)
}

function ScopeSummary({ scopes }: { scopes: string[] }) {
  if (scopes.includes('*')) {
    return <Badge variant="destructive" className="px-2 py-1 text-xs font-medium">全权限</Badge>
  }
  const visible = scopes.slice(0, 3)
  return (
    <div className="flex max-w-xs flex-wrap gap-1">
      {visible.map(scope => (
        <Badge variant="secondary" key={scope} className="px-1.5 py-0.5 font-mono text-[10px]">
          {scope}
        </Badge>
      ))}
      {scopes.length > visible.length && (
        <Badge variant="secondary" className="px-1.5 py-0.5 text-[10px]">+{scopes.length - visible.length}</Badge>
      )}
    </div>
  )
}

function toDateTimeLocal(value: string) {
  if (!value) return ''
  return value.replace(' ', 'T').slice(0, 16)
}

function fromDateTimeLocal(value: string) {
  if (!value) return ''
  return `${value.replace('T', ' ')}:00`
}
