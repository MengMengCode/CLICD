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

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
type EndpointTuple = [HttpMethod, string, string]

interface EndpointDoc {
  method: HttpMethod
  path: string
  desc: string
  examplePath: string
  body?: Record<string, unknown>
  response: unknown
  note?: string
}

const scopeGroups = [
  {
    title: '总览与只读',
    scopes: [
      ['dashboard:read', '控制面板'],
      ['host:read', '主机资源'],
      ['routing:read', '路由信息'],
      ['routing:write', '路由配置'],
      ['ipv6:read', 'IPv6 状态'],
      ['task:read', '任务列表'],
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
      ['container:reinstall', '重装系统'],
      ['container:delete', '删除容器'],
      ['container:resize', '资源/到期'],
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
      ['image:download', '下载镜像'],
      ['image:delete', '删除镜像'],
      ['image:toggle', '启停镜像'],
      ['security:read', '安全数据'],
      ['security:check', '安全扫描'],
      ['security:settings', '安全设置'],
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
      ['admin:access', '管理员接口'],
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
      ['GET', '/api/v1/dashboard', '控制面板统计'],
      ['GET', '/api/v1/host-info', '主机资源'],
      ['GET', '/api/v1/host-history', '宿主机历史指标（后台每 30 秒采集）'],
      ['GET', '/api/v1/host-report', '宿主机硬件、网络与运行环境探测报告'],
      ['GET', '/api/v1/routing', 'NAT/IPv4/IPv6 路由'],
      ['PUT', '/api/v1/routing', '更新公网 IPv4/IPv6 池'],
      ['POST', '/api/v1/routing/ipv4-scan', '扫描公网 IPv4 段'],
      ['GET', '/api/v1/ipv6/status', 'IPv6 状态'],
      ['GET', '/api/v1/tasks', '任务队列'],
      ['DELETE', '/api/v1/tasks/{task_id}', '删除任务'],
    ],
  },
  {
    title: '容器',
    endpoints: [
      ['GET', '/api/v1/containers', '容器列表'],
      ['POST', '/api/v1/containers/list', '容器列表（兼容 POST 写法）'],
      ['POST', '/api/v1/containers', '创建容器'],
      ['GET', '/api/v1/containers/{id|uuid|name}', '容器详情'],
      ['POST', '/api/v1/containers/{id}/start', '开机'],
      ['POST', '/api/v1/containers/{id}/stop', '关机'],
      ['POST', '/api/v1/containers/{id}/restart', '重启'],
      ['POST', '/api/v1/containers/{id}/reinstall', '重装'],
      ['DELETE', '/api/v1/containers/{id}/delete', '删除'],
      ['GET', '/api/v1/containers/{id}/usage', '资源用量'],
      ['GET', '/api/v1/containers/{id}/history', '容器历史指标（后台每 30 秒采集）'],
      ['GET', '/api/v1/containers/{id}/traffic', '流量统计'],
      ['POST', '/api/v1/containers/{id}/traffic-reset', '重置流量'],
      ['PUT', '/api/v1/containers/{id}/traffic-limit', '调整流量限制'],
      ['PUT', '/api/v1/containers/{id}/resource-limit', '调整资源限制'],
      ['PUT', '/api/v1/containers/{id}/expiry', '调整到期时间'],
      ['POST', '/api/v1/containers/{id}/reset-password', '重置 SSH 密码'],
      ['POST', '/api/v1/containers/{id}/ipv6', '分配 IPv6'],
      ['PUT', '/api/v1/containers/{id}/public-ipv4', '更新独立公网 IPv4 地址'],
      ['PUT', '/api/v1/containers/{id}/ipv6-addresses', '更新独立 IPv6 地址'],
    ],
  },
  {
    title: '端口与快照',
    endpoints: [
      ['GET', '/api/v1/containers/{id}/random-port', '随机可用端口'],
      ['POST', '/api/v1/containers/{id}/port-mappings', '添加端口映射'],
      ['PUT', '/api/v1/containers/{id}/port-mappings/{index}', '更新端口映射'],
      ['DELETE', '/api/v1/containers/{id}/port-mappings/{index}', '删除端口映射'],
      ['GET', '/api/v1/containers/{id}/firewall', '获取防火墙设置'],
      ['PUT', '/api/v1/containers/{id}/firewall', '更新防火墙设置'],
      ['GET', '/api/v1/snapshots', '快照总览'],
      ['GET', '/api/v1/containers/{id}/snapshots', '容器快照'],
      ['POST', '/api/v1/containers/{id}/snapshots', '创建快照'],
      ['DELETE', '/api/v1/containers/{id}/snapshots/{snapshot_id}', '删除快照'],
      ['POST', '/api/v1/containers/{id}/snapshots/{snapshot_id}/restore', '恢复快照'],
      ['POST', '/api/v1/containers/{id}/snapshots/schedule', '计划快照'],
      ['PUT', '/api/v1/containers/{id}/snapshots/quota', '快照配额'],
    ],
  },
  {
    title: '平台管理',
    endpoints: [
      ['GET', '/api/v1/templates', '模板列表'],
      ['GET', '/api/v1/images', '镜像管理列表'],
      ['GET', '/api/v1/images/enabled?type=lxc&container={id}', '可用于创建或重装的已启用镜像'],
      ['POST', '/api/v1/images/download', '下载镜像'],
      ['POST', '/api/v1/images/cancel', '取消镜像下载'],
      ['DELETE', '/api/v1/images/delete', '删除镜像缓存'],
      ['PUT', '/api/v1/images/toggle', '启用/禁用镜像'],
      ['GET', '/api/v1/security/alerts', '安全告警'],
      ['POST', '/api/v1/security/check', '立即安全检查'],
      ['GET', '/api/v1/security/logs?container={name}', '安全连接日志'],
      ['GET', '/api/v1/security/summary', '安全汇总'],
      ['GET', '/api/v1/security/settings', '安全设置'],
      ['PUT', '/api/v1/security/settings', '更新安全设置'],
      ['GET', '/api/v1/swap', 'Swap 信息'],
      ['POST', '/api/v1/swap', '调整 Swap'],
      ['POST', '/api/v1/batch-create', '批量创建容器'],
      ['POST', '/api/v1/batch-action', '批量开关机/删除/重装'],
      ['POST', '/api/v1/ssh-ticket', '创建 WebSSH 票据'],
      ['POST', '/api/v1/vnc-ticket', '创建 WebVNC 票据'],
    ],
  },
  {
    title: '主机与设置',
    endpoints: [
      ['GET', '/api/v1/storage', '已挂载磁盘、存储池和空间占用'],
      ['PUT', '/api/v1/storage', '更新各磁盘的存储用途和默认盘'],
      ['GET', '/api/v1/task-queue/settings', '任务队列并发状态'],
      ['PUT', '/api/v1/task-queue/settings', '调整任务并发数量'],
      ['GET', '/api/v1/ssl', 'SSL 配置和证书状态'],
      ['PUT', '/api/v1/ssl', '更新 SSL 配置'],
      ['GET', '/api/v1/webssh-origins', 'WebSSH/VNC Origin 白名单'],
      ['PUT', '/api/v1/webssh-origins', '更新 WebSSH/VNC Origin 白名单'],
      ['GET', '/api/v1/language', '面板语言'],
      ['PUT', '/api/v1/language', '更新面板语言'],
    ],
  },
  {
    title: '账号与日志',
    endpoints: [
      ['POST', '/api/v1/sub-user/create', '创建子用户链接'],
      ['GET', '/api/v1/sub-users', '子用户列表'],
      ['POST', '/api/v1/sub-users/{id}/rotate-password', '轮换子用户密码'],
      ['GET', '/api/v1/sub-users/{id}/audit-logs', '子用户操作日志'],
      ['GET', '/api/v1/sub-users/{id}/login-logs', '子用户登录日志'],
      ['GET', '/api/v1/audit-logs', '操作日志'],
      ['GET', '/api/v1/login-logs', '登录日志'],
      ['GET', '/api/v1/api-keys', 'API Key 列表'],
      ['POST', '/api/v1/api-keys', '创建 API Key'],
      ['PATCH', '/api/v1/api-keys/{id}', '更新 API Key'],
      ['DELETE', '/api/v1/api-keys/{id}', '删除 API Key'],
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
  const { t } = useLanguage()
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
    if (!window.confirm(t('确定删除这个 API Key 吗？'))) return
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
          <h1 className="text-2xl font-bold text-black">API 集成</h1>
          <p className="mt-1 text-sm text-gray-500">管理外部调用凭据、权限范围与平台 API 文档</p>
        </div>
        <button
          onClick={openCreate}
          className="inline-flex items-center gap-1.5 rounded-md bg-black px-3 py-2 text-sm text-white hover:bg-gray-800"
        >
          <Plus className="h-4 w-4" />
          创建 Key
        </button>
      </div>

      {newKey && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <div className="text-sm font-semibold text-amber-800">新的 API Key 已生成</div>
            <button onClick={() => setNewKey('')} className="rounded p-1 text-amber-700 hover:bg-amber-100" title="关闭">
              <X className="h-4 w-4" />
            </button>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            <code className="min-w-0 flex-1 break-all rounded border border-amber-300 bg-white px-3 py-2 font-mono text-xs text-gray-800">
              {newKey}
            </code>
            <button
              onClick={copyKey}
              className="inline-flex items-center justify-center gap-1.5 rounded-md bg-amber-600 px-3 py-2 text-xs text-white hover:bg-amber-700"
            >
              {copiedKey ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {copiedKey ? '已复制' : '复制'}
            </button>
          </div>
        </div>
      )}

      <div className="rounded-lg border border-gray-200 bg-white">
        <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-5 py-4">
          <h2 className="flex items-center gap-2 text-sm font-semibold text-black">
            <Key className="h-4 w-4" />
            API Keys
          </h2>
          <button onClick={fetchData} className="rounded p-1.5 text-gray-400 hover:text-black" title="刷新">
            <RefreshCw className="h-4 w-4" />
          </button>
        </div>

        {loading ? (
          <div className="py-10 text-center text-sm text-gray-400">加载中...</div>
        ) : keys.length === 0 ? (
          <div className="py-10 text-center text-sm text-gray-400">暂无 API Key</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-100 text-left text-xs font-medium text-gray-500">
                  <th className="px-4 py-3">名称</th>
                  <th className="px-4 py-3">权限</th>
                  <th className="px-4 py-3">绑定容器</th>
                  <th className="px-4 py-3">限制</th>
                  <th className="px-4 py-3">最后使用</th>
                  <th className="px-4 py-3 text-right">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {keys.map(item => (
                  <tr key={item.id} className="hover:bg-gray-50">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-gray-900">{item.name}</span>
                        {item.disabled && (
                          <span className="rounded bg-red-50 px-1.5 py-0.5 text-[10px] font-medium text-red-600">已禁用</span>
                        )}
                      </div>
                      <div className="mt-1 font-mono text-xs text-gray-400">{item.prefix}</div>
                    </td>
                    <td className="px-4 py-3">
                      <ScopeSummary scopes={item.scopes || ['*']} />
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-500">
                      {item.container_uuids?.length
                        ? item.container_uuids.map(uuid => containerNameByUUID.get(uuid) || uuid).join('、')
                        : '全部容器'}
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-500">
                      <div>{item.ip_whitelist ? 'IP 白名单' : '不限 IP'}</div>
                      <div>{item.expires_at ? `到期 ${item.expires_at}` : '长期有效'}</div>
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-500">
                      <div>{item.last_used || '从未使用'}</div>
                      {item.last_used_ip && <div className="font-mono text-[11px] text-gray-400">{item.last_used_ip}</div>}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex justify-end gap-1">
                        <button onClick={() => openEdit(item)} className="rounded p-1.5 text-gray-400 hover:text-black" title="编辑">
                          <Edit3 className="h-3.5 w-3.5" />
                        </button>
                        <button onClick={() => deleteKey(item.id)} className="rounded p-1.5 text-gray-400 hover:text-red-600" title="删除">
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="rounded-lg border border-gray-200 bg-white">
        <button
          onClick={() => setShowDocs(value => !value)}
          className="flex w-full items-center justify-between gap-3 border-b border-gray-200 px-5 py-4 text-left"
        >
          <h2 className="flex items-center gap-2 text-sm font-semibold text-black">
            <ShieldCheck className="h-4 w-4" />
            API 文档
          </h2>
          {showDocs ? <ChevronUp className="h-4 w-4 text-gray-400" /> : <ChevronDown className="h-4 w-4 text-gray-400" />}
        </button>

        {showDocs && (
          <div className="space-y-6 p-5">
            <div className="rounded-lg bg-gray-900 p-4 font-mono text-xs text-gray-100">
              <div>curl -X GET {BASE_URL}/api/v1/containers -H "X-API-Key: clicd_sk_xxxx"</div>
              <div className="mt-2 text-gray-400">curl -X GET {BASE_URL}/api/v1/dashboard -H "Authorization: Bearer clicd_sk_xxxx"</div>
            </div>

            {endpointGroups.map(group => (
              <section key={group.title}>
                <h3 className="mb-2 text-sm font-semibold text-black">{group.title}</h3>
                <div className="overflow-hidden rounded-lg border border-gray-200">
                  {group.endpoints.map(([method, path, desc]) => (
                    <div key={`${method}-${path}`} className="grid gap-2 border-b border-gray-100 px-3 py-2 text-xs last:border-b-0 md:grid-cols-[72px_minmax(220px,1fr)_180px_72px]">
                      <span className="w-fit rounded border border-blue-200 bg-blue-50 px-1.5 py-0.5 font-mono font-bold text-blue-700">{method}</span>
                      <code className="min-w-0 break-all font-mono text-gray-800">{path}</code>
                      <span className="text-gray-500">{desc}</span>
                      <button
                        onClick={() => setSelectedEndpoint(buildEndpointDoc(method, path, desc))}
                        className="inline-flex w-fit items-center justify-center gap-1 rounded border border-gray-200 px-2 py-1 text-gray-600 hover:border-gray-300 hover:bg-gray-50 hover:text-black"
                        title="查看使用范例"
                      >
                        <Eye className="h-3.5 w-3.5" />
                        查看
                      </button>
                    </div>
                  ))}
                </div>
              </section>
            ))}
          </div>
        )}
      </div>

      {selectedEndpoint && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div className="absolute inset-0 bg-black/50" onClick={() => setSelectedEndpoint(null)} />
          <div className="relative flex max-h-[90vh] w-full max-w-5xl flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl">
            <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-5 py-4">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="rounded border border-blue-200 bg-blue-50 px-1.5 py-0.5 font-mono text-xs font-bold text-blue-700">
                    {selectedEndpoint.method}
                  </span>
                  <code className="min-w-0 break-all font-mono text-sm text-gray-900">{selectedEndpoint.path}</code>
                </div>
                <p className="mt-1 text-xs text-gray-500">{selectedEndpoint.desc}</p>
              </div>
              <button onClick={() => setSelectedEndpoint(null)} className="shrink-0 rounded p-1 text-gray-400 hover:text-black" title="关闭">
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto p-5">
              <div className="grid gap-4 lg:grid-cols-2">
                <section className="min-w-0">
                  <div className="mb-2 flex items-center justify-between gap-2">
                    <h3 className="text-sm font-semibold text-black">Python 使用范例</h3>
                    <button
                      onClick={copyDocCode}
                      className="inline-flex items-center gap-1 rounded border border-gray-200 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50"
                    >
                      {copiedDoc ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                      {copiedDoc ? '已复制' : '复制'}
                    </button>
                  </div>
                  <pre className="max-h-[52vh] overflow-auto rounded-lg bg-gray-900 p-4 text-xs text-gray-100">
                    <code>{buildPythonExample(selectedEndpoint)}</code>
                  </pre>
                </section>

                <section className="min-w-0">
                  <h3 className="mb-2 text-sm font-semibold text-black">返回响应样例</h3>
                  {selectedEndpoint.note && (
                    <div className="mb-2 rounded border border-gray-200 bg-gray-50 px-3 py-2 text-xs text-gray-500">{selectedEndpoint.note}</div>
                  )}
                  <pre className="max-h-[52vh] overflow-auto rounded-lg bg-gray-900 p-4 text-xs text-gray-100">
                    <code>{formatJSON(selectedEndpoint.response)}</code>
                  </pre>
                </section>
              </div>
            </div>
          </div>
        </div>
      )}

      {showForm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div className="absolute inset-0 bg-black/50" onClick={() => setShowForm(false)} />
          <div className="relative flex max-h-[90vh] w-full max-w-4xl flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl">
            <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-5 py-4">
              <h3 className="text-base font-semibold text-black">{editingKey ? '编辑 API Key' : '创建 API Key'}</h3>
              <button onClick={() => setShowForm(false)} className="rounded p-1 text-gray-400 hover:text-black" title="关闭">
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-5">
              <div className="grid gap-5 lg:grid-cols-[1fr_1.2fr]">
                <div className="space-y-4">
                  <label className="block">
                    <span className="mb-1 block text-xs text-gray-500">名称</span>
                    <input
                      value={form.name}
                      onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))}
                      className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                      placeholder="CI/CD、计费系统、自动化脚本"
                    />
                  </label>

                  <label className="block">
                    <span className="mb-1 block text-xs text-gray-500">IP 白名单</span>
                    <textarea
                      value={form.ipWhitelist}
                      onChange={e => setForm(prev => ({ ...prev, ipWhitelist: e.target.value }))}
                      rows={4}
                      className="w-full resize-none rounded-md border border-gray-300 px-3 py-2 font-mono text-sm"
                      placeholder={`1.2.3.4\n10.0.0.0/24`}
                    />
                  </label>

                  <label className="block">
                    <span className="mb-1 block text-xs text-gray-500">过期时间</span>
                    <input
                      type="datetime-local"
                      value={form.expiresAt}
                      onChange={e => setForm(prev => ({ ...prev, expiresAt: e.target.value }))}
                      className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                    />
                  </label>

                  <label className="flex items-center gap-2 text-sm text-gray-700">
                    <input
                      type="checkbox"
                      checked={form.disabled}
                      onChange={e => setForm(prev => ({ ...prev, disabled: e.target.checked }))}
                      className="h-4 w-4 accent-black"
                    />
                    禁用这个 Key
                  </label>

                  <div>
                    <div className="mb-2 text-xs text-gray-500">绑定容器</div>
                    <div className="max-h-48 space-y-1 overflow-y-auto rounded-md border border-gray-200 p-2">
                      <label className="flex items-center gap-2 rounded px-2 py-1.5 text-sm text-gray-700 hover:bg-gray-50">
                        <input
                          type="checkbox"
                          checked={form.containerUUIDs.length === 0}
                          onChange={() => setForm(prev => ({ ...prev, containerUUIDs: [] }))}
                          className="h-4 w-4 accent-black"
                        />
                        全部容器
                      </label>
                      {containers.map(container => (
                        <label key={container.uuid} className="flex items-center gap-2 rounded px-2 py-1.5 text-sm text-gray-700 hover:bg-gray-50">
                          <input
                            type="checkbox"
                            checked={form.containerUUIDs.includes(container.uuid)}
                            onChange={() => toggleContainer(container.uuid)}
                            className="h-4 w-4 accent-black"
                          />
                          <span className="min-w-0 truncate">{container.name}</span>
                          <span className="shrink-0 font-mono text-[10px] text-gray-400">{container.uuid.slice(0, 8)}</span>
                        </label>
                      ))}
                    </div>
                  </div>
                </div>

                <div>
                  <div className="mb-2 flex items-center justify-between gap-3">
                    <div className="text-xs text-gray-500">权限范围</div>
                    <button onClick={() => toggleScope('*')} className="rounded border border-gray-200 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50">
                      {form.scopes.includes('*') ? '取消全权限' : '全权限'}
                    </button>
                  </div>
                  <div className="space-y-4">
                    {scopeGroups.map(group => (
                      <div key={group.title}>
                        <div className="mb-2 text-xs font-medium text-gray-700">{group.title}</div>
                        <div className="grid gap-2 sm:grid-cols-2">
                          {group.scopes.map(([scope, label]) => (
                            <label key={scope} className="flex items-center gap-2 rounded border border-gray-200 px-2 py-2 text-xs text-gray-700 hover:bg-gray-50">
                              <input
                                type="checkbox"
                                checked={form.scopes.includes('*') || form.scopes.includes(scope)}
                                disabled={form.scopes.includes('*')}
                                onChange={() => toggleScope(scope)}
                                className="h-4 w-4 accent-black"
                              />
                              <span className="min-w-0 flex-1 truncate">{label}</span>
                              <code className="hidden shrink-0 font-mono text-[10px] text-gray-400 sm:block">{scope}</code>
                            </label>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
            <div className="flex justify-end gap-2 border-t border-gray-200 px-5 py-4">
              <button onClick={() => setShowForm(false)} className="rounded-md border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50">
                取消
              </button>
              <button
                onClick={saveKey}
                disabled={saving || !form.name.trim()}
                className="rounded-md bg-black px-4 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50"
              >
                {saving ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

const requestBodySamples: Record<string, Record<string, unknown>> = {
  'POST /api/v1/containers/list': {},
  'POST /api/v1/containers': {
    name: 'demo-lxc-01',
    virtualization: 'lxc',
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
    extra_ports: [8080],
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
  'POST /api/v1/security/check': { container_name: 'example-vm' },
  'PUT /api/v1/containers/{id}/firewall': {
    enabled: true,
    default_action: 'DROP',
    rules: [
      { id: '', network: 'ipv4', direction: 'in', protocol: 'tcp', port: '22', source_ip: '', action: 'ACCEPT', description: 'Allow SSH over IPv4/NAT4', enabled: true },
      { id: '', network: 'ipv6', direction: 'in', protocol: 'tcp', port: '22', source_ip: '', action: 'ACCEPT', description: 'Allow SSH over IPv6', enabled: true },
      { id: '', network: 'all', direction: 'out', protocol: 'tcp', port: '', source_ip: '', action: 'ACCEPT', description: 'Allow outbound TCP', enabled: true },
    ],
  },
  'PUT /api/v1/security/settings': { auto_shutdown: false },
  'POST /api/v1/swap': { action: 'resize', size_mb: 16384 },
  'POST /api/v1/batch-create': {
    containers: [
      {
        name: 'batch-lxc-01',
        virtualization: 'lxc',
        template_id: 'debian-bookworm',
        vcpu: 1,
        ram_mb: 512,
        disk_gb: 10,
        assign_nat: true,
        port_mapping_count: 2,
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

const responseSamples: Record<string, unknown> = {
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
      status: 'running',
      ip: '10.0.0.10',
      ipv6: '2001:db8:100::1005',
      ssh_port: 22004,
      ssh_password: '***',
      policy_blocked: false,
    },
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
  'GET /api/v1/language': { success: true, data: { language: 'zh' } },
  'PUT /api/v1/language': { success: true, data: { language: 'zh' } },
  'GET /api/v1/security/alerts': { success: true, data: [] },
  'POST /api/v1/security/check': { success: true, message: 'Security check completed' },
  'GET /api/v1/security/logs?container={name}': { success: true, data: [] },
  'GET /api/v1/security/summary': { success: true, data: { critical: 0, high: 0, low: 0, medium: 0, total_alerts: 0 } },
  'GET /api/v1/security/settings': { success: true, data: { auto_shutdown: false } },
  'PUT /api/v1/security/settings': { success: true, data: { auto_shutdown: false } },
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

function buildEndpointDoc(method: HttpMethod, path: string, desc: string): EndpointDoc {
  const key = `${method} ${path}`
  return {
    method,
    path,
    desc,
    examplePath: examplePathFor(path),
    body: requestBodySamples[key],
    response: responseSamples[key] || defaultResponseFor(method),
    note: endpointNoteFor(key),
  }
}

function examplePathFor(path: string) {
  return path
    .replace('{id|uuid|name}', '5')
    .replace('{id}', '5')
    .replace('{task_id}', 'task-10')
    .replace('{index}', '1')
    .replace('{snapshot_id}', 'snap-20260608-001')
    .replace('{name}', 'example-vm')
}

function endpointNoteFor(key: string) {
  const notes: string[] = []
  if (key === 'POST /api/v1/containers') {
    notes.push('Linux container creation supports ssh_auth_mode=auto_password|password|key. Public IPv4, IPv6, and NAT can be configured with assign_nat, assign_ipv4, and assign_ipv6.')
    notes.push('Supports independent upload/download bandwidth limits and read/write I/O limits. network_bw_mbps and io_speed_mbps are deprecated symmetric compatibility aliases. New integrations should use network_down_mbps, network_up_mbps, io_read_mbps, and io_write_mbps.')
    notes.push('storage_pool_id selects an enabled disk for the runtime. For an LXC with an independent LAN address, set lan_ipv4_mode=dhcp or static and set assign_nat=false; static mode also requires lan_ipv4_address, lan_ipv4_prefix_len, and lan_ipv4_gateway.')
    notes.push('allowed_image_ids and image_limit_configured define which downloaded images the container owner may use for reinstall. Include the initial template ID when it should remain reinstallable.')
  }
  if (key === 'POST /api/v1/containers/{id}/reinstall') {
    notes.push('Reinstall supports ssh_auth_mode=keep|auto_password|password|key. keep is only for reinstall requests; if SSH fields are omitted, the existing behavior is kept.')
  }
  if (key === 'POST /api/v1/batch-create') {
    notes.push('Each containers[] item in batch creation supports the same storage, network, image allowlist, and SSH authentication fields as POST /api/v1/containers.')
  }
  if (key === 'PUT /api/v1/containers/{id}/resource-limit') {
    notes.push('Supports independent download/upload bandwidth limits and read/write I/O limits. Omitted fields keep their current values, and explicitly passing 0 makes that direction unlimited. network_bw_mbps and io_speed_mbps are deprecated symmetric compatibility aliases.')
  }
  if (key === 'PUT /api/v1/containers/{id}/firewall') {
    notes.push('Backward compatible: default_action is optional; if omitted, the existing policy is kept. rule.network is optional; if omitted, it is treated as ipv4. default_action: DROP=deny unmatched traffic, ACCEPT=allow unmatched traffic. network: ipv4=IPv4 NAT/public IPv4, ipv6=IPv6, all=apply to both IPv4 and IPv6. For NAT inbound rules, port is the container internal port, not the host public port.')
  }
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
  if (key.includes('/api/v1/storage') || key.includes('/task-queue/settings') || key.includes('/api/v1/ssl') || key.includes('/webssh-origins')) {
    notes.push('This endpoint requires an API key with admin:access.')
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
  const bodyJSON = hasBody ? formatJSON(doc.body) : ''
  return [
    'import json',
    'import requests',
    '',
    `BASE_URL = "${SAMPLE_BASE_URL}"`,
    'API_KEY = "clicd_sk_xxxx"',
    '',
    ...(hasBody ? [`payload = json.loads(r'''${bodyJSON}''')`, ''] : []),
    `response = requests.${doc.method.toLowerCase()}(`,
    `    f"{BASE_URL}${doc.examplePath}",`,
    '    headers={"X-API-Key": API_KEY},',
    ...(hasBody ? ['    json=payload,'] : []),
    '    timeout=30,',
    ')',
    'response.raise_for_status()',
    'print(response.json())',
  ].join('\n')
}

function formatJSON(value: unknown) {
  return JSON.stringify(value, null, 2)
}

function ScopeSummary({ scopes }: { scopes: string[] }) {
  if (scopes.includes('*')) {
    return <span className="rounded bg-red-50 px-2 py-1 text-xs font-medium text-red-600">全权限</span>
  }
  const visible = scopes.slice(0, 3)
  return (
    <div className="flex max-w-xs flex-wrap gap-1">
      {visible.map(scope => (
        <span key={scope} className="rounded bg-gray-100 px-1.5 py-0.5 font-mono text-[10px] text-gray-600">
          {scope}
        </span>
      ))}
      {scopes.length > visible.length && (
        <span className="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] text-gray-500">+{scopes.length - visible.length}</span>
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
