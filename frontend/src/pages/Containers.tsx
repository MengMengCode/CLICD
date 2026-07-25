import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router'
import {
  ArrowDown,
  ArrowUp,
  Cpu,
  Eye,
  HardDrive,
  MemoryStick,
  Network,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  Server,
  Square,
  Trash2,
  ListTodo,
  X,
} from 'lucide-react'
import CreateContainerModal from '../components/CreateContainerModal'
import { useAuth } from '../contexts/AuthContext'
import { useLanguage } from '../contexts/LanguageContext'
import {
  Container,
  CreateContainerRequest,
  ContainerUsage,
  getContainerUsage,
  getContainers,
  batchAction,
  Task,
  getTasks,
  deleteTask,
} from '../services/api'
import { actionLabel, taskStatusClass, taskStatusLabel } from '../utils/labels'

export default function Containers() {
  const navigate = useNavigate()
  const { isSubUser } = useAuth()
  const [containers, setContainers] = useState<Container[]>([])
  const [usageByName, setUsageByName] = useState<Record<string, ContainerUsage>>({})
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [batchLoading, setBatchLoading] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [showTasks, setShowTasks] = useState(false)
  const [tasks, setTasks] = useState<Task[]>([])
  const [queuedCreates, setQueuedCreates] = useState<Record<string, CreateContainerRequest>>({})
  const [searchText, setSearchText] = useState('')
  const [typeFilter, setTypeFilter] = useState('all')
  const [systemFilter, setSystemFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const refreshUsage = useCallback(async (items: Container[]) => {
    const targets = items.filter((container) => container.status === 'running')
    if (targets.length === 0) {
      setUsageByName({})
      return
    }
    const results = await Promise.allSettled(
      targets.map(async (container) => {
        const res = await getContainerUsage(container.uuid || container.id)
        return [container.name, res.data.data] as const
      })
    )
    setUsageByName((current) => {
      const next: Record<string, ContainerUsage> = {}
      const activeNames = new Set(items.map((container) => container.name))
      for (const [name, usage] of Object.entries(current)) {
        if (activeNames.has(name)) next[name] = usage
      }
      for (const result of results) {
        if (result.status === 'fulfilled' && result.value[1]) {
          next[result.value[0]] = result.value[1]
        }
      }
      return next
    })
  }, [])

  const fetchData = useCallback(async () => {
    try {
      const res = await getContainers()
      const nextContainers = res.data.data || []
      setContainers(nextContainers)
      await refreshUsage(nextContainers)
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }, [refreshUsage])

  useEffect(() => {
    fetchData()
    const interval = window.setInterval(fetchData, 5000)
    return () => window.clearInterval(interval)
  }, [fetchData])

  const toggleSelect = (id: number) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  // Map of container_id -> current task status.
  // For create tasks, container_id may be 0 initially but gets set after creation,
  // so we also index by container_name as fallback for placeholder items.
  const taskStatusMap: Record<number, Task> = {}
  const taskNameMap: Record<string, Task> = {}
  for (const t of tasks) {
    if (t.status === 'pending' || t.status === 'running') {
      if (t.container_id != null && t.container_id > 0) {
        taskStatusMap[t.container_id] = t
      }
      if (t.container_name) {
        taskNameMap[t.container_name] = t
      }
    }
  }

  const handleBatchAction = async (action: string) => {
    if (selected.size === 0) return
    setBatchLoading(true)
    try {
      await batchAction(action, [...selected])
      setSelected(new Set())
      await fetchTasks()
    } catch (err) {
      console.error(err)
    } finally {
      setBatchLoading(false)
    }
  }

  const fetchTasks = useCallback(async () => {
    try {
      const res = await getTasks()
      const nextTasks = res.data.data || []
      setTasks(nextTasks)
      setQueuedCreates((current) => syncQueuedCreates(current, nextTasks, containers))
    } catch { /* ignore */ }
  }, [containers])

  useEffect(() => { fetchTasks(); const t = setInterval(fetchTasks, 2000); return () => clearInterval(t) }, [fetchTasks])

  const handleRefreshList = useCallback(async () => {
    setRefreshing(true)
    try {
      await Promise.all([fetchData(), fetchTasks()])
    } finally {
      setRefreshing(false)
    }
  }, [fetchData, fetchTasks])

  const actionLabels: Record<string, string> = {
    create: '正在初始化', start: '开机中', stop: '关机中', restart: '重启中', delete: '删除中', reinstall: '重装中',
  }

  const displayContainers = buildDisplayContainers(containers, queuedCreates, tasks)
  const activeTaskCount = tasks.filter((task) => task.status === 'pending' || task.status === 'running').length
  const systemOptions = useMemo(() => buildSystemOptions(displayContainers), [displayContainers])
  const filteredContainers = useMemo(() => {
    return filterContainers(displayContainers, {
      search: searchText,
      type: typeFilter,
      system: systemFilter,
      status: statusFilter,
      taskStatusMap,
      taskNameMap,
    })
  }, [displayContainers, searchText, typeFilter, systemFilter, statusFilter, tasks])
  const totalPages = Math.max(1, Math.ceil(filteredContainers.length / pageSize))
  const currentPage = Math.min(page, totalPages)
  const pageStart = (currentPage - 1) * pageSize
  const pageContainers = filteredContainers.slice(pageStart, pageStart + pageSize)
  const selectableIDs = filteredContainers
    .filter((container) => !container.isPlaceholder && !taskStatusMap[container.id] && !taskNameMap[container.name])
    .map((container) => container.id)
  const allFilteredSelected = selectableIDs.length > 0 && selectableIDs.every((id) => selected.has(id))

  useEffect(() => {
    setPage(1)
  }, [searchText, typeFilter, systemFilter, statusFilter, pageSize])

  const toggleAll = () => {
    if (allFilteredSelected) {
      setSelected((prev) => {
        const next = new Set(prev)
        selectableIDs.forEach((id) => next.delete(id))
        return next
      })
    } else {
      setSelected((prev) => {
        const next = new Set(prev)
        selectableIDs.forEach((id) => next.add(id))
        return next
      })
    }
  }

  const handleCreateQueued = async (items: CreateContainerRequest[]) => {
    setQueuedCreates((current) => {
      const next = { ...current }
      for (const item of items) {
        next[item.name] = item
      }
      return next
    })
    fetchTasks()
    fetchData()
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-black"></div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-[180px]">
          <h1 className="text-2xl font-bold text-black">容器管理</h1>
          <p className="text-sm text-gray-500 mt-1">
            共 {displayContainers.length} 个容器
            {filteredContainers.length !== displayContainers.length && `，筛选后 ${filteredContainers.length} 个`}
            {selected.size > 0 && `，已选 ${selected.size} 个`}
          </p>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <button
            onClick={handleRefreshList}
            disabled={refreshing}
            className="flex items-center gap-1.5 px-3 py-1.5 border border-gray-300 text-gray-700 rounded-md hover:bg-gray-50 transition-colors text-xs font-medium whitespace-nowrap disabled:opacity-50 disabled:cursor-not-allowed"
            title="刷新列表"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${refreshing ? 'animate-spin' : ''}`} />
            刷新
          </button>
          <button
            onClick={() => setShowTasks(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 border border-gray-300 text-gray-700 rounded-md hover:bg-gray-50 transition-colors text-xs font-medium whitespace-nowrap"
          >
            <ListTodo className="w-3.5 h-3.5" />
            任务队列
            {activeTaskCount > 0 && (
              <span className="ml-0.5 rounded bg-amber-100 px-1.5 py-0.5 text-[11px] font-medium text-amber-700">
                {activeTaskCount}
              </span>
            )}
          </button>
          {!isSubUser && (
            <button
              onClick={() => setShowCreate(true)}
              className="flex items-center gap-1.5 px-3 py-1.5 bg-black text-white rounded-md hover:bg-gray-800 transition-colors text-xs font-medium whitespace-nowrap"
            >
              <Plus className="w-3.5 h-3.5" />
              创建容器
            </button>
          )}
        </div>
      </div>

      {displayContainers.length > 0 && (
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative w-[260px]">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-400" />
              <input
                value={searchText}
                onChange={(event) => setSearchText(event.target.value)}
                className="h-8 w-full rounded-md border border-gray-300 bg-white pl-8 pr-2 text-xs text-black outline-none focus:border-black focus:ring-2 focus:ring-black"
                placeholder="搜索名称、ID、UUID、IP"
              />
            </div>
            <select
              value={typeFilter}
              onChange={(event) => setTypeFilter(event.target.value)}
              className="h-8 rounded-md border border-gray-300 bg-white px-2 text-xs text-gray-700 outline-none focus:border-black focus:ring-2 focus:ring-black"
              title="类型筛选"
            >
              <option value="all">全部类型</option>
              <option value="lxc">LXC</option>
              <option value="kvm">KVM</option>
            </select>
            <select
              value={systemFilter}
              onChange={(event) => setSystemFilter(event.target.value)}
              className="h-8 rounded-md border border-gray-300 bg-white px-2 text-xs text-gray-700 outline-none focus:border-black focus:ring-2 focus:ring-black"
              title="系统筛选"
            >
              <option value="all">全部系统</option>
              {systemOptions.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
            <select
              value={statusFilter}
              onChange={(event) => setStatusFilter(event.target.value)}
              className="h-8 rounded-md border border-gray-300 bg-white px-2 text-xs text-gray-700 outline-none focus:border-black focus:ring-2 focus:ring-black"
              title="状态筛选"
            >
              <option value="all">全部状态</option>
              <option value="running">在线</option>
              <option value="stopped">离线</option>
              <option value="task">任务中</option>
              <option value="creating">创建中</option>
              <option value="failed">失败</option>
            </select>
            <select
              value={pageSize}
              onChange={(event) => setPageSize(Number(event.target.value))}
              className="h-8 rounded-md border border-gray-300 bg-white px-2 text-xs text-gray-700 outline-none focus:border-black focus:ring-2 focus:ring-black"
              title="每页数量"
            >
              <option value={10}>10 / 页</option>
              <option value={20}>20 / 页</option>
              <option value={50}>50 / 页</option>
            </select>
          </div>

          {selected.size > 0 && (
            <div className="flex flex-wrap items-center justify-end gap-1.5">
              <span className="text-xs text-gray-500">{selected.size} 个</span>
              <button onClick={() => handleBatchAction('start')} disabled={batchLoading || hasActiveTasks(tasks)} className="inline-flex h-8 items-center gap-1 px-2.5 text-xs text-gray-700 hover:bg-gray-100 rounded border border-gray-300 disabled:opacity-50 disabled:cursor-not-allowed">
                <Play className="w-3 h-3" />{batchLoading ? '执行中...' : '开机'}
              </button>
              <button onClick={() => handleBatchAction('stop')} disabled={batchLoading || hasActiveTasks(tasks)} className="inline-flex h-8 items-center gap-1 px-2.5 text-xs text-gray-700 hover:bg-gray-100 rounded border border-gray-300 disabled:opacity-50 disabled:cursor-not-allowed">
                <Square className="w-3 h-3" />{batchLoading ? '执行中...' : '关机'}
              </button>
              <button onClick={() => handleBatchAction('restart')} disabled={batchLoading || hasActiveTasks(tasks)} className="inline-flex h-8 items-center gap-1 px-2.5 text-xs text-gray-700 hover:bg-gray-100 rounded border border-gray-300 disabled:opacity-50 disabled:cursor-not-allowed">
                <RotateCcw className="w-3 h-3" />{batchLoading ? '执行中...' : '重启'}
              </button>
              <button onClick={() => handleBatchAction('delete')} disabled={batchLoading || hasActiveTasks(tasks)} className="inline-flex h-8 items-center gap-1 px-2.5 text-xs text-red-600 hover:bg-red-50 rounded border border-red-200 disabled:opacity-50 disabled:cursor-not-allowed">
                <Trash2 className="w-3 h-3" />{batchLoading ? '执行中...' : '删除'}
              </button>
            </div>
          )}
        </div>
      )}

      {displayContainers.length === 0 ? (
        <div className="bg-white border border-gray-200 rounded-lg p-12 text-center">
          <div className="w-16 h-16 bg-gray-100 rounded-lg flex items-center justify-center mx-auto mb-4">
            <Server className="w-8 h-8 text-gray-400" />
          </div>
          <h3 className="text-lg font-medium text-gray-700 mb-2">暂无容器</h3>
          <p className="text-sm text-gray-500 mb-4">点击"创建容器"开始</p>
        </div>
      ) : (
        <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[1260px]">
              <thead>
                <tr className="border-b border-gray-200 bg-gray-50">
                  <th className="w-10 px-3 py-3">
                    {!isSubUser && (
                      <input
                        type="checkbox"
                        checked={allFilteredSelected}
                        disabled={selectableIDs.length === 0}
                        onChange={toggleAll}
                        className="w-4 h-4 rounded border-gray-300 text-black focus:ring-black accent-black"
                      />
                    )}
                  </th>
                  <TableHead>ID</TableHead>
                  <TableHead>名称</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>系统</TableHead>
                  <TableHead>类型</TableHead>
                  <TableHead icon><Cpu className="w-3.5 h-3.5" />CPU</TableHead>
                  <TableHead icon><MemoryStick className="w-3.5 h-3.5" />MEMORY</TableHead>
                  <TableHead icon><HardDrive className="w-3.5 h-3.5" />DISK</TableHead>
                  <TableHead icon><Network className="w-3.5 h-3.5" />NET</TableHead>
                  <TableHead>配置</TableHead>
                  <TableHead>剩余时间</TableHead>
                  <TableHead right>操作</TableHead>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {pageContainers.map((container) => {
                  const isRunning = container.status === 'running'
                  const isInitializing = container.status === 'initializing'
                  const task = (container.id > 0 ? taskStatusMap[container.id] : undefined) || taskNameMap[container.name] || container.createTask
                  const isPlaceholder = !!container.isPlaceholder
                  const isPolicyBlocked = !!container.policy_blocked
                  const usage = usageByName[container.name]
                  const cpuPct = isRunning
                    ? clamp((usage?.cpu_usage_pct || 0) / (container.vcpu || 1))
                    : 0
                  const ramTotalBytes = usage?.memory_total_bytes && usage.memory_total_bytes > 0
                    ? usage.memory_total_bytes
                    : container.ram_mb * 1024 * 1024
                  const ramPct = isRunning && ramTotalBytes > 0
                    ? clamp(((usage?.memory_usage_bytes || 0) / ramTotalBytes) * 100)
                    : 0
                  const diskPct = container.disk_gb > 0
                    ? clamp(((usage?.disk_usage_bytes || 0) / (container.disk_gb * 1024 * 1024 * 1024)) * 100)
                    : 0
                  const rx = isRunning ? usage?.network_rx_bps || 0 : 0
                  const tx = isRunning ? usage?.network_tx_bps || 0 : 0

                  return (
                    <tr key={container.isPlaceholder ? `placeholder-${container.name}` : container.id} className="hover:bg-gray-50 transition-colors">
                      <td className="px-2 py-2 align-top">
                        {!isSubUser && (
                          <input
                            type="checkbox"
                            checked={selected.has(container.id)}
                            onChange={() => toggleSelect(container.id)}
                            disabled={isPlaceholder || !!taskStatusMap[container.id] || !!taskNameMap[container.name]}
                            className="w-3.5 h-3.5 rounded border-gray-300 text-black focus:ring-black accent-black disabled:opacity-30"
                          />
                        )}
                      </td>
                      <td className="px-2.5 py-2 align-top text-xs text-gray-400 font-mono whitespace-nowrap">
                        #{container.id}
                      </td>
                      <td className="px-2.5 py-2 align-top">
                        <button
                          onClick={() => navigate(`/container/${encodeURIComponent(container.uuid || String(container.id))}`)}
                          disabled={isPlaceholder}
                          className="font-medium text-black hover:underline text-xs disabled:no-underline disabled:text-gray-500 disabled:cursor-not-allowed whitespace-nowrap"
                        >
                          {container.name}
                        </button>
                      </td>
                      <td className="px-2.5 py-2 align-top">
                        <StatusBadge running={isRunning} initializing={isInitializing} task={task} placeholder={isPlaceholder} policyBlocked={isPolicyBlocked} />
                      </td>
                      <td className="px-2.5 py-2 align-top text-xs text-gray-600 whitespace-nowrap">
                        <span className="inline-flex items-center gap-1">
                          {getTemplateIcon(container.template)}
                          {getTemplateName(container.template)}
                        </span>
                      </td>
                      <td className="px-2.5 py-2 align-top">
                        <RuntimeBadge runtime={container.virtualization || 'lxc'} />
                      </td>
                      <td className="px-2.5 py-2 align-top">
                        <ProgressCell pct={cpuPct} />
                      </td>
                      <td className="px-2.5 py-2 align-top">
                        <ProgressCell pct={ramPct} />
                      </td>
                      <td className="px-2.5 py-2 align-top">
                        <ProgressCell pct={diskPct} />
                      </td>
                      <td className="px-2.5 py-2 align-top">
                        <div className="space-y-0.5 text-[11px] font-medium tabular-nums min-w-[70px] whitespace-nowrap">
                          <div className="flex items-center gap-0.5">
                            <ArrowUp className="w-3 h-3 text-gray-400" />
                            <span className="text-gray-700">{formatRate(tx)}</span>
                          </div>
                          <div className="flex items-center gap-0.5">
                            <ArrowDown className="w-3 h-3 text-gray-400" />
                            <span className="text-gray-700">{formatRate(rx)}</span>
                          </div>
                        </div>
                      </td>
                      <td className="px-2.5 py-2 align-top text-xs text-gray-600 whitespace-nowrap">
                        {container.vcpu}核/{formatRAM(container.ram_mb)}/{container.disk_gb}GB
                      </td>
                      <td className="px-2.5 py-2 align-top text-xs whitespace-nowrap">
                        {container.expires_at ? getRemaining(container.expires_at) : <span className="text-gray-400">永久</span>}
                      </td>
                      <td className="px-2.5 py-2 align-top">
                        <div className="flex justify-end">
                          {task?.status === 'failed' ? (
                            <button
                              onClick={async () => {
                                try {
                                  const { default: api } = await import('../services/api')
                                  await api.delete(`/tasks/${task.id}`)
                                  await Promise.all([fetchData(), fetchTasks()])
                                } catch { /* ignore */ }
                              }}
                              className="inline-flex items-center gap-1 px-2 py-1 rounded-md border border-red-200 text-[11px] text-red-600 hover:bg-red-50 transition-colors whitespace-nowrap"
                            >
                              <Trash2 className="w-3 h-3" />
                              删除
                            </button>
                          ) : (
                            <button
                              onClick={() => navigate(`/container/${encodeURIComponent(container.uuid || String(container.id))}`)}
                              disabled={isPlaceholder}
                              className="inline-flex items-center gap-1 px-2 py-1 rounded-md border border-gray-300 text-[11px] text-gray-700 hover:bg-gray-100 transition-colors disabled:opacity-50 disabled:cursor-not-allowed whitespace-nowrap"
                            >
                              <Eye className="w-3 h-3" />
                              查看
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          {filteredContainers.length === 0 ? (
            <div className="border-t border-gray-100 px-4 py-10 text-center text-sm text-gray-500">
              没有匹配的容器
            </div>
          ) : (
            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 px-4 py-3">
              <div className="text-xs text-gray-500">
                显示 {pageStart + 1}-{Math.min(pageStart + pageSize, filteredContainers.length)} / {filteredContainers.length}
              </div>
              <div className="flex items-center gap-1">
                <button
                  onClick={() => setPage(1)}
                  disabled={currentPage === 1}
                  className="rounded border border-gray-200 px-2.5 py-1.5 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40"
                >
                  首页
                </button>
                <button
                  onClick={() => setPage((value) => Math.max(1, value - 1))}
                  disabled={currentPage === 1}
                  className="rounded border border-gray-200 px-2.5 py-1.5 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40"
                >
                  上一页
                </button>
                <span className="px-2 text-xs text-gray-500">
                  {currentPage} / {totalPages}
                </span>
                <button
                  onClick={() => setPage((value) => Math.min(totalPages, value + 1))}
                  disabled={currentPage === totalPages}
                  className="rounded border border-gray-200 px-2.5 py-1.5 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40"
                >
                  下一页
                </button>
                <button
                  onClick={() => setPage(totalPages)}
                  disabled={currentPage === totalPages}
                  className="rounded border border-gray-200 px-2.5 py-1.5 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40"
                >
                  末页
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      <CreateContainerModal isOpen={showCreate} onClose={() => setShowCreate(false)} onSuccess={handleCreateQueued} existingNames={containers.map(c => c.name)} />
      {showTasks && (
        <TaskQueueModal
          tasks={tasks}
          onRefresh={fetchTasks}
          onClose={() => setShowTasks(false)}
        />
      )}
    </div>
  )
}

function TableHead({ children, right, icon }: { children: ReactNode; right?: boolean; icon?: boolean }) {
  return (
    <th className={`${right ? 'text-right' : 'text-left'} px-2.5 py-2 text-[11px] font-medium text-gray-500 uppercase whitespace-nowrap`}>
      <span className={icon ? 'inline-flex items-center gap-1' : ''}>{children}</span>
    </th>
  )
}

type DisplayContainer = Container & {
  isPlaceholder?: boolean
  createTask?: Task
}

function StatusBadge({ running, initializing, task, placeholder, policyBlocked }: { running: boolean; initializing?: boolean; task?: Task; placeholder?: boolean; policyBlocked?: boolean }) {
  const { t } = useLanguage()
  const baseClass = "inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium whitespace-nowrap"
  if (policyBlocked) {
    return (
      <span className={`${baseClass} bg-red-50 text-red-700`}>
        <span className="w-1.5 h-1.5 rounded-full bg-red-500"></span>
        {t('策略封禁')}
      </span>
    )
  }

  if (task?.status === 'failed') {
    return (
      <span className={`${baseClass} bg-red-50 text-red-700`}>
        <span className="w-1.5 h-1.5 rounded-full bg-red-500"></span>
        {t('初始化失败')}
      </span>
    )
  }

  if (task?.type === 'create' && task.status === 'done') {
    return (
      <span className={`${baseClass} bg-emerald-50 text-emerald-700`}>
        <span className="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
        {t('初始化完成')}
      </span>
    )
  }

  if (task?.type === 'create' && task.status === 'running') {
    const detail = t(task.stage_detail || '正在初始化')
    return (
      <span className={`${baseClass} max-w-[210px] bg-amber-50 text-amber-700`} title={`${t('正在初始化')}: ${detail}`}>
        <span className="h-1.5 w-1.5 flex-shrink-0 rounded-full bg-amber-500 animate-pulse"></span>
        <span className="truncate">{detail}</span>
      </span>
    )
  }

  if (placeholder || task?.type === 'create') {
    return (
      <span className={`${baseClass} bg-gray-100 text-gray-500`}>
        <span className="w-1.5 h-1.5 rounded-full bg-gray-400"></span>
        {t('排队等待')}
      </span>
    )
  }

  if (task && task.status !== 'done' && task.status !== 'failed') {
    const taskLabels: Record<string, string> = {
      start: '开机中', stop: '关机中', restart: '重启中', delete: '删除中', reinstall: '重装中',
    }
    return (
      <span className={`${baseClass} bg-amber-50 text-amber-700`}>
        <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse"></span>
        {t(taskLabels[task.type] || '处理中')}
      </span>
    )
  }

  if (initializing) {
    return (
      <span className={`${baseClass} bg-amber-50 text-amber-700`}>
        <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse"></span>
        {t('正在初始化')}
      </span>
    )
  }

  return (
    <span className={`${baseClass} ${running ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-600'}`}>
      <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${running ? 'bg-green-500' : 'bg-red-500'}`}></span>
      {t(running ? '在线' : '离线')}
    </span>
  )
}

function RuntimeBadge({ runtime }: { runtime: string }) {
  const normalized = runtime === 'kvm' ? 'kvm' : 'lxc'
  return (
    <span className={`inline-flex rounded px-2 py-0.5 text-[11px] font-medium ${normalized === 'kvm' ? 'bg-indigo-50 text-indigo-700' : 'bg-gray-100 text-gray-700'}`}>
      {normalized.toUpperCase()}
    </span>
  )
}

function buildDisplayContainers(
  containers: Container[],
  queuedCreates: Record<string, CreateContainerRequest>,
  tasks: Task[]
): DisplayContainer[] {
  const realNames = new Set(containers.map((container) => container.name))
  const placeholders = new Map<string, DisplayContainer>()

  for (const [name, cfg] of Object.entries(queuedCreates)) {
    if (!realNames.has(name)) {
      placeholders.set(name, toPlaceholder(cfg))
    }
  }

  for (const task of tasks) {
    if (task.type !== 'create' || !task.config?.name || realNames.has(task.config.name)) {
      continue
    }
    if (task.status === 'pending' || task.status === 'running' || task.status === 'failed' || placeholders.has(task.config.name)) {
      placeholders.set(task.config.name, { ...toPlaceholder(task.config), createTask: task })
    }
  }

  return [...containers, ...placeholders.values()]
}

function toPlaceholder(cfg: CreateContainerRequest): DisplayContainer {
  return {
    id: 0,
    uuid: '',
    name: cfg.name,
    virtualization: cfg.virtualization || 'lxc',
    template: cfg.template_id,
    vcpu: cfg.vcpu,
    ram_mb: cfg.ram_mb,
    disk_gb: cfg.disk_gb,
    network_bw_mbps: cfg.network_bw_mbps,
    network_down_mbps: cfg.network_down_mbps,
    network_up_mbps: cfg.network_up_mbps,
    monthly_traffic_gb: cfg.monthly_traffic_gb,
    traffic_mode: cfg.traffic_mode || 'total',
    traffic_in_gb: cfg.traffic_in_gb || 0,
    traffic_out_gb: cfg.traffic_out_gb || 0,
    traffic_used_rx: 0,
    traffic_used_tx: 0,
    traffic_reset_date: '',
    io_speed_mbps: cfg.io_speed_mbps,
    io_read_mbps: cfg.io_read_mbps,
    io_write_mbps: cfg.io_write_mbps,
    status: 'creating',
    ip: '',
    public_ipv4s: [],
    ipv6: '',
    ipv6_prefix_len: 0,
    ipv6_interface: '',
    ipv6_addresses: [],
    vnc_port: 0,
    ssh_port: 0,
    ssh_password: '',
    port_mappings: [],
    port_mapping_limit: cfg.assign_nat === false ? 0 : (cfg.port_mapping_count || 0),
    firewall_enabled: false,
    firewall_default_action: 'DROP',
    firewall_rules: [],
    snapshot_limit: cfg.snapshot_limit || 3,
    created_at: '',
    expires_at: cfg.expires_at,
    snapshot_schedule_enabled: false,
    snapshot_schedule_interval_hours: 24,
    snapshot_schedule_time: '03:00',
    snapshot_schedule_last_run: '',
    snapshot_schedule_next_run: '',
    snapshot_schedule_created_by: '',
    isPlaceholder: true,
  }
}

function syncQueuedCreates(
  current: Record<string, CreateContainerRequest>,
  tasks: Task[],
  containers: Container[]
): Record<string, CreateContainerRequest> {
  const realNames = new Set(containers.map((container) => container.name))
  const activeOrFailedCreateNames = new Set(
    tasks
      .filter((task) => task.type === 'create' && (task.status === 'pending' || task.status === 'running' || task.status === 'failed' || task.status === 'done'))
      .map((task) => task.config?.name || task.container_name)
  )

  const next: Record<string, CreateContainerRequest> = {}
  for (const [name, cfg] of Object.entries(current)) {
    if (!realNames.has(name)) {
      next[name] = cfg
    }
  }

  for (const task of tasks) {
    if (task.type === 'create' && task.config?.name && !realNames.has(task.config.name)) {
      if (task.status === 'pending' || task.status === 'running' || task.status === 'failed') {
        next[task.config.name] = task.config
      }
    }
  }

  return next
}

function hasActiveTasks(tasks: Task[]) {
  return tasks.some((task) => task.status === 'pending' || task.status === 'running')
}

type ContainerFilters = {
  search: string
  type: string
  system: string
  status: string
  taskStatusMap: Record<number, Task>
  taskNameMap: Record<string, Task>
}

function filterContainers(containers: DisplayContainer[], filters: ContainerFilters): DisplayContainer[] {
  const keyword = filters.search.trim().toLowerCase()
  return containers.filter((container) => {
    const task = (container.id > 0 ? filters.taskStatusMap[container.id] : undefined) || filters.taskNameMap[container.name] || container.createTask
    if (filters.system !== 'all' && getSystemFilterValue(container.template) !== filters.system) {
      return false
    }
    if (filters.type !== 'all' && (container.virtualization || 'lxc') !== filters.type) {
      return false
    }
    if (filters.status !== 'all' && getContainerStatusFilterValue(container, task) !== filters.status) {
      return false
    }
    if (!keyword) return true

    const fields = [
      String(container.id),
      container.name,
      container.uuid,
      container.ip,
      container.ipv6,
      container.template,
      container.virtualization || 'lxc',
      getTemplateName(container.template),
      getSystemFilterLabel(getSystemFilterValue(container.template)),
      String(container.ssh_port || ''),
    ]
    return fields.some((field) => field.toLowerCase().includes(keyword))
  })
}

function buildSystemOptions(containers: DisplayContainer[]) {
  const systems = new Map<string, string>()
  for (const container of containers) {
    const value = getSystemFilterValue(container.template)
    systems.set(value, getSystemFilterLabel(value))
  }
  return Array.from(systems.entries())
    .map(([value, label]) => ({ value, label }))
    .sort((a, b) => a.label.localeCompare(b.label))
}

function getSystemFilterValue(template: string) {
  const normalized = template.startsWith('kvm-') ? template.slice(4) : template
  if (normalized.startsWith('ubuntu')) return 'ubuntu'
  if (normalized.startsWith('debian')) return 'debian'
  if (normalized.startsWith('alpine')) return 'alpine'
  if (normalized.startsWith('centos')) return 'centos'
  if (normalized.startsWith('archlinux')) return 'archlinux'
  if (normalized.startsWith('fedora')) return 'fedora'
  if (normalized.startsWith('rockylinux')) return 'rockylinux'
  if (normalized.startsWith('windows')) return 'windows'
  return normalized || 'unknown'
}

function getSystemFilterLabel(system: string) {
  const labels: Record<string, string> = {
    ubuntu: 'Ubuntu',
    debian: 'Debian',
    alpine: 'Alpine',
    centos: 'CentOS',
    archlinux: 'Arch Linux',
    fedora: 'Fedora',
    rockylinux: 'Rocky Linux',
    windows: 'Windows',
    unknown: '未知系统',
  }
  return labels[system] || system
}

function getContainerStatusFilterValue(container: DisplayContainer, task?: Task) {
  if (task?.status === 'failed') return 'failed'
  if (container.isPlaceholder || task?.type === 'create') return 'creating'
  if (task && task.status !== 'done' && task.status !== 'failed') return 'task'
  return container.status === 'running' ? 'running' : 'stopped'
}

function taskLineLabel(task: Task, actionLabels: Record<string, string>) {
  if (task.status === 'failed') return task.type === 'create' ? '初始化失败' : '处理失败'
  if (task.type === 'create' && task.status === 'done') return '初始化完成'
  if (task.type === 'create' && task.status === 'running') return task.stage_detail || '正在初始化'
  return actionLabels[task.type] || '处理中...'
}

function TaskQueueModal({ tasks, onRefresh, onClose }: {
  tasks: Task[]
  onRefresh: () => void | Promise<void>
  onClose: () => void
}) {
  const { t } = useLanguage()
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="flex max-h-[86vh] w-full max-w-6xl flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl">
        <div className="flex items-center justify-between gap-4 border-b border-gray-200 px-5 py-4">
          <div>
            <h2 className="text-base font-semibold text-black">{t('任务队列')}</h2>
            <p className="mt-0.5 text-xs text-gray-500">{t(`共 ${tasks.length} 个任务`)}</p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={onRefresh}
              className="inline-flex items-center gap-2 rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50"
            >
              <RefreshCw className="h-4 w-4" />
              {t('刷新')}
            </button>
            <button onClick={onClose} className="rounded p-2 text-gray-500 hover:bg-gray-100" title={t('关闭')}>
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        {tasks.length === 0 ? (
          <div className="p-8 text-center text-sm text-gray-500">{t('暂无任务')}</div>
        ) : (
          <div className="overflow-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-100 bg-gray-50 text-left text-xs font-medium text-gray-500">
                  <th className="whitespace-nowrap px-4 py-2.5">{t('状态')}</th>
                  <th className="whitespace-nowrap px-4 py-2.5">{t('操作')}</th>
                  <th className="whitespace-nowrap px-4 py-2.5">{t('容器')}</th>
                  <th className="whitespace-nowrap px-4 py-2.5">{t('当前阶段')}</th>
                  <th className="whitespace-nowrap px-4 py-2.5">{t('创建时间')}</th>
                  <th className="px-4 py-2.5">{t('错误')}</th>
                  <th className="whitespace-nowrap px-4 py-2.5 w-10"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {tasks.map((task) => (
                  <tr key={task.id} className="hover:bg-gray-50">
                    <td className="whitespace-nowrap px-4 py-2.5">
                      <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${taskStatusClass(task.status)}`}>
                        {t(taskStatusLabel(task.status))}
                      </span>
                    </td>
                    <td className="whitespace-nowrap px-4 py-2.5 text-gray-800">{t(actionLabel(task.type))}</td>
                    <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-gray-700">{task.container_name}</td>
                    <td className="min-w-[210px] px-4 py-2.5 text-xs text-gray-700">
                      {task.type === 'create' ? t(task.stage_detail || (task.status === 'pending' ? '排队等待' : '-')) : '-'}
                    </td>
                    <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-gray-500">{task.created_at}</td>
                    <td className="min-w-[260px] px-4 py-2.5 text-gray-600">{task.error || '-'}</td>
                    <td className="whitespace-nowrap px-2 py-2.5">
                      {task.status === 'pending' && (
                        <button
                          onClick={async () => {
                            try {
                              await deleteTask(task.id)
                              onRefresh()
                            } catch { /* ignore */ }
                          }}
                          className="p-1 rounded hover:bg-red-50 text-gray-400 hover:text-red-600 transition-colors"
                          title={t('取消任务')}
                        >
                          <X className="w-3.5 h-3.5" />
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}

function ProgressCell({ pct }: { pct: number }) {
  return (
    <div className="flex items-center gap-2 min-w-[100px]">
      <span className="w-10 text-xs font-medium tabular-nums text-gray-700">{pct.toFixed(1)}%</span>
      <div className="h-1.5 flex-1 rounded-full bg-gray-100 overflow-hidden">
        <div
          className="h-full bg-gray-500 transition-all duration-500"
          style={{ width: `${Math.max(pct, pct > 0 ? 2 : 0)}%` }}
        />
      </div>
    </div>
  )
}

function getTemplateName(id: string) {
  const map: Record<string, string> = {
    'ubuntu-noble': 'Ubuntu 24.04',
    'ubuntu-jammy': 'Ubuntu 22.04',
    'debian-trixie': 'Debian 13',
    'debian-bookworm': 'Debian 12',
    'debian-bullseye': 'Debian 11',
    'alpine-3.21': 'Alpine 3.21',
    'centos-9-stream': 'CentOS 9',
    'archlinux-current': 'Arch Linux',
    'fedora-44': 'Fedora 44',
    'rockylinux-10': 'Rocky 10',
    'kvm-ubuntu-noble': 'Ubuntu 24.04',
    'kvm-ubuntu-jammy': 'Ubuntu 22.04',
    'kvm-debian-trixie': 'Debian 13',
    'kvm-debian-trixie-xfce': 'Debian 13 XFCE',
    'kvm-debian-bookworm': 'Debian 12',
    'kvm-debian-bullseye': 'Debian 11',
    'kvm-rockylinux-9': 'Rocky 9',
    'kvm-windows-11': 'Windows 11',
    'kvm-windows-10': 'Windows 10',
  }
  return map[id] || id
}

function getTemplateIcon(id: string): ReactNode {
  const size = 'w-4 h-4'
  id = id.startsWith('kvm-') ? id.slice(4) : id
  if (id.startsWith('debian')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M935.473 375.359a558.602 558.602 0 0 0-22.351-114.655l13.308 4.436c-35.66-81.385-90.086-163.623-153.556-199.282-8.701-5.118-35.147 4.948-26.616-12.113s-37.536-8.19-56.816-4.778c-26.275 4.266-30.028-29.175-75.071-35.83-25.593-3.582-32.247 18.427-44.702 13.309-23.545-9.384-20.816-27.64-57.669-9.384-18.427 9.042 11.602-26.105-49.138-4.607L457.744 0C349.23 41.63 318.69 76.266 288.15 79.337c-6.996 0-34.124 32.759-53.574 53.062-17.062 17.062-26.275 36.512-49.138 39.583l-17.062 70.636A136.494 136.494 0 0 0 119.41 339.7a66.711 66.711 0 0 1 4.436-52.892c-17.062 6.825-45.896 17.062-29.687 96.91 12.796 63.13-5.29 135.13 10.066 204.742 4.777 20.986 0 40.095 6.142 51.185 107.66 235.794 208.836 392.08 472.44 384.06l4.436-8.872c-28.152-6.825-55.11-17.062-111.584-30.711-18.597-4.436-23.033-34.124-40.265-44.19-9.384-5.46-28.323-4.095-37.195-9.896s4.266-21.668-19.962-14.332c-8.531 2.56-13.82-10.92-20.133-17.061s0-23.716-23.375-24.74-18.426-29.687-19.791-44.702c-12.114 1.536-1.195-1.535-13.308 4.436a63.64 63.64 0 0 1-23.887-31.735c-10.237-48.967-10.578-21.497-15.014-32.417a322.297 322.297 0 0 0-19.28-42.142l26.787 8.872h4.436l4.436-13.309-26.616-8.701h31.223c-7.678 13.99 2.047 5.29-13.479 8.872v13.308l22.35-8.872v-13.308c-20.644-10.237-28.663-13.308-49.137-22.01l9.043 8.872v4.436h-49.138c-22.01-14.843-13.99-31.734-17.915-53.062 17.062 0 9.213 6.655 17.062-13.137l-17.062 8.872 13.308-33.953-13.308 13.138c-29.176-38.73-16.209-97.764-11.943-152.02A180.684 180.684 0 0 1 211.2 372.97c8.872-10.067 5.119-25.251 5.46-37.195l31.223-26.445H265.8c7.678 17.061 4.777 5.46 0 22.01l8.872 4.435c7.166-8.701 6.142-5.971 8.872-22.01-10.066-10.578-6.995-9.895-26.616-13.308A119.432 119.432 0 0 1 368.51 243.13l4.436-13.308-17.915 9.043-4.436-13.138a109.536 109.536 0 0 1 76.095-27.128c6.313 0 6.996-17.062 12.797-19.45 161.574-60.57 309.33 9.383 371.093 147.413a324.173 324.173 0 0 1 8.19 34.123c17.061 56.987-7.167 121.48 9.725 155.604-7.849 36-36.683 13.82-40.266 30.881-8.531 41.29-14.844 59.717-40.778 78.826a196.38 196.38 0 0 1-30.711 22.35 84.285 84.285 0 0 0 22.35-39.753c-106.294 111.584-262.58 63.981-290.049-105.954a101.176 101.176 0 0 1 35.147-93.157c92.987-87.527 150.144-52.38 205.765-20.474l-8.872-30.711c-32.93-24.398-17.062-19.792-9.043-57.328v-4.436l-17.915-13.137c2.56 10.066 1.024 5.289 9.043 17.061-4.436 16.039 0 9.043-8.872 17.062-15.014 9.725-23.716 7.337-44.702 4.436l4.436-13.308-13.308-13.308c0 11.773-4.095 2.73 0 17.062-126.086 9.896-218.05 80.02-178.636 260.191a220.608 220.608 0 0 0 8.872 44.19l-8.872 8.702-4.436-26.446h-13.48l-4.435 13.308c-12.626-25.763-0.853 10.75 40.265 52.892a149.29 149.29 0 0 0 12.797 12.625c47.773 34.124 113.29 81.385 201.328 49.138h9.043v-4.436l-102.37-13.308-4.436-8.701c106.806 24.74 176.93-8.531 236.646-48.456 13.138-17.062 11.431-24.057 22.18-9.043 19.28-17.061 3.925-26.786 13.48-44.019 6.483-11.772 32.587-17.062 44.7-35.318l40.096-136.494h-17.062c3.071-14.332 22.522-34.123-4.436-48.455-2.559-1.536 9.043-1.365 8.872-4.266a145.537 145.537 0 0 0-22.18-66.37c33.1 21.669 36.342 68.247 53.574 105.783v8.872h4.436V375.36zM453.308 595.455l-9.555-26.446 62.446 57.328zM146.196 211.736l-23.204-4.436v39.754c16.72-10.578 18.939-10.407 22.35-35.318z m574.981 176.419a57.498 57.498 0 0 0-17.062 44.19l13.48 8.701a37.877 37.877 0 0 0 4.435-52.891zM868.42 555.872c26.275-11.602 54.598-58.01 35.83-97.081l-35.83 96.91z m-174.03-79.508c-15.697 11.773-19.791 13.308-22.35 39.754l13.307 8.872 17.915-8.872a60.228 60.228 0 0 0 4.436-48.455c-8.36 13.478-2.559 20.644-13.308 8.701z m-67.053 79.508c15.868-10.92 11.944-14.844 17.915-22.18v-4.778a292.097 292.097 0 0 1-62.446 0c-13.137-13.99-13.308-29.346-31.223-39.583 17.062 35.147 3.242 38.218 31.223 61.764a158.162 158.162 0 0 0 40.095 4.436c1.536 0-6.824-1.024 4.436 0zM207.79 520.554H194.31l-8.872 8.702c9.555 10.237 5.46 7.166 13.308-4.436L212.225 547l4.436-17.062-8.872-8.701z m17.062 57.328l4.436-8.873c-10.067-8.701 0-3.583-13.308 0l-13.309-17.061 4.436 17.061v8.873h17.062z" fill="#CE0C48"/></svg>
  if (id.startsWith('ubuntu')) return <svg className={size} viewBox="0 0 1024 1024"><circle cx="512" cy="512" r="511" fill="#DD4814"/><path d="M164.532 442.532c-37.676 0-68.2 30.524-68.2 68.2 0 37.656 30.524 68.184 68.2 68.184 37.66 0 68.184-30.528 68.184-68.184 0-37.676-30.524-68.2-68.184-68.2z m486.86 309.912c-32.612 18.84-43.8 60.52-24.96 93.116 18.82 32.616 60.5 43.796 93.116 24.96 32.612-18.82 43.796-60.5 24.96-93.12-18.82-32.592-60.524-43.772-93.116-24.956z m-338.744-241.712c0-67.384 33.472-126.92 84.684-162.968L347.48 264.268c-59.656 39.88-104.048 100.816-122.496 172.188 21.528 17.56 35.304 44.3 35.304 74.272 0 29.956-13.776 56.696-35.304 74.26C243.408 656.376 287.8 717.32 347.48 757.2l49.852-83.52c-51.212-36.028-84.684-95.56-84.684-162.948z m199.168-199.188c104.052 0 189.42 79.776 198.38 181.52l97.16-1.432c-4.776-75.112-37.592-142.544-88.008-192.128-25.928 9.796-55.88 8.296-81.76-6.624-25.932-14.964-42.192-40.208-46.636-67.608a297.04 297.04 0 0 0-79.14-10.76 295.148 295.148 0 0 0-131.276 30.652l47.38 84.908a198.384 198.384 0 0 1 83.9-18.528z m0 398.36a198.404 198.404 0 0 1-83.896-18.528l-47.38 84.9a294.848 294.848 0 0 0 131.28 30.684 296.16 296.16 0 0 0 79.136-10.788c4.444-27.4 20.708-52.62 46.632-67.608 25.904-14.948 55.836-16.42 81.76-6.624 50.42-49.584 83.232-117.016 88.016-192.128l-97.188-1.432c-8.94 101.772-94.304 181.52-198.36 181.52z m139.552-440.924c32.616 18.832 74.3 7.68 93.116-24.936 18.84-32.616 7.68-74.3-24.936-93.14-32.616-18.816-74.296-7.64-93.14 24.976-18.812 32.6-7.632 74.28 24.96 93.1z" fill="#FFF"/></svg>
  if (id.startsWith('alpine')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M255.914667 68.565333L0 512l255.914667 443.434667h512.170666L1024 512 768.085333 68.565333H255.914667zM425.173333 303.786667L540.16 422.4l68.181333 68.053333 0.085334-0.085333 102.826666 100.821333c-8.533333 5.973333-16.469333 10.752-24.021333 14.677334a160.256 160.256 0 0 1-21.162667 9.258666 115.285333 115.285333 0 0 1-18.133333 4.736c-5.589333 0.981333-10.666667 1.450667-15.274667 1.450667-5.546667 0-10.325333-0.597333-14.421333-1.450667a56.192 56.192 0 0 1-10.24-3.072 40.533333 40.533333 0 0 1-8.533333-4.821333l-45.312-46.592-129.664-129.749333-46.933334 44.928-130.986666 131.072a41.557333 41.557333 0 0 1-8.533334 4.736 54.357333 54.357333 0 0 1-10.112 3.114666 70.826667 70.826667 0 0 1-14.421333 1.408c-4.608 0-9.685333-0.384-15.274667-1.322666a115.2 115.2 0 0 1-18.133333-4.864 159.914667 159.914667 0 0 1-21.162667-9.258667 223.061333 223.061333 0 0 1-24.021333-14.634667L425.173333 303.786667z m201.386667 33.493333l195.370667 196.181333 58.965333 57.728a223.573333 223.573333 0 0 1-24.064 14.677334 159.146667 159.146667 0 0 1-21.077333 9.258666 115.072 115.072 0 0 1-18.176 4.736c-5.546667 0.981333-10.709333 1.450667-15.36 1.450667-5.504 0-10.282667-0.597333-14.378667-1.450667a54.826667 54.826667 0 0 1-16.426667-6.229333 10.197333 10.197333 0 0 1-2.261333-1.706667l-52.565333-51.968-90.069334-90.069333-14.250666 14.250667L545.706667 418.133333l80.896-80.938666z m-254.549333 175.786667v107.904a90.794667 90.794667 0 0 1-15.189334-1.493334 117.973333 117.973333 0 0 1-18.005333-4.949333 158.208 158.208 0 0 1-20.821333-9.130667 222.592 222.592 0 0 1-23.68-14.506666l77.653333-77.866667z" fill="#0D597F"/></svg>
  if (id.startsWith('centos')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M153.650377 358.349623v112.005247h-3.694326v-108.310921l3.694326-3.694326z" fill="#932279"/><path d="M453.058708 512l-29.554608 29.554608H137.86553v108.310922L0 512l137.86553-137.86553v108.310922h285.63857l29.554608 29.554608zM738.529354 226.529354L553.64513 411.413578V149.956051h108.310921l3.694326 3.694326 72.878977 72.878977z" fill="#932279"/><path d="M649.86553 137.86553h-108.310922v285.63857l-29.554608 29.554608-29.554608-29.554608V137.86553h-108.310922L512 0l137.86553 137.86553zM874.043949 553.64513v108.310921l-3.694326 3.694326-72.878977 72.878977-184.884224-184.884224h261.457527z" fill="#EFA724"/><path d="M886.13447 361.036405v13.098065l-6.04526-6.04526-6.045261-6.045261v108.310921h-3.694326v-125.103312l3.694326 3.694326 6.045261 6.045261 6.04526 6.04526z" fill="#262577"/><path d="M886.13447 649.86553v-108.310922H600.4959L570.941292 512l29.554608-29.554608h285.63857v-108.310922l137.86553 137.86553-137.86553 137.86553z" fill="#262577"/><path d="M411.413578 470.35487H149.956051v-108.310921l3.694326-3.694326L226.529354 285.470646 411.413578 470.35487zM470.35487 149.956051V411.413578L285.470646 226.529354l72.878977-72.878977 3.694326-3.694326h108.310921z" fill="#9CCD2A"/><path d="M738.529354 797.470646L553.64513 612.586422v261.457527h108.310921l3.694326-3.694326 72.878977-72.878977z" fill="#EFA724"/><path d="M649.86553 886.13447h-108.310922V600.4959L512 570.941292l-29.554608 29.554608v285.63857h-108.310922l137.86553 137.86553 137.86553-137.86553z" fill="#9CCD2A"/><path d="M470.35487 874.043949V612.586422L285.470646 797.470646l72.878977 72.878977 3.694326 3.694326h108.310921z" fill="#262577"/><path d="M470.35487 428.541817v41.813053h-41.813053L226.529354 268.342407l-76.573303 76.573303V149.956051h194.959659l-76.573303 76.573303 202.012463 202.012463z" fill="#9CCD2A"/><path d="M880.08921 143.91079v224.17842l-6.045261-6.045261v108.310921H612.586422L797.470646 285.470646l72.878977 72.878977v-13.098065l3.694326 3.694326v-4.030174l-76.573303-76.573303-202.012463 202.012463h-41.813053v-41.813053L755.657593 226.529354l-82.618564-82.618564h207.050181z" fill="#932279"/><path d="M666.993768 137.86553l12.090522 12.090521h194.959659v212.087898l6.045261 6.045261 6.04526 6.04526V137.86553z" fill="#FFF"/><path d="M874.043949 679.08429v194.959659H679.08429L755.657593 797.470646 553.64513 595.458183v-41.813053h41.813053L797.470646 755.657593l76.573303-76.573303z" fill="#EFA724"/><path d="M411.413578 553.64513L226.529354 738.529354l-72.878977-72.878977-3.694326-3.694326v-108.310921H411.413578z" fill="#262577"/><path d="M470.35487 595.458183L268.342407 797.470646l76.573303 76.573303H149.956051V679.08429L226.529354 755.657593l202.012463-202.012463h41.813053v41.813053z" fill="#262577"/><path d="M874.043949 344.91571v4.030174l-3.694326-3.694326v13.098065l3.694326 3.694326 6.045261 6.045261 6.04526 6.04526v-16.792391z" fill="#FFF"/></svg>
  if (id.startsWith('archlinux')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M504.149333 7.850667c-44.373333 108.544-70.997333 179.2-120.149333 284.330666 30.037333 32.085333 67.242667 69.290667 127.317333 111.274667-64.512-26.624-108.544-53.248-141.653333-80.896-63.146667 131.413333-161.792 318.464-361.813333 678.229333 157.696-90.794667 279.552-146.773333 393.216-168.277333-4.778667-21.162667-7.509333-43.690667-7.509334-67.584l0.341334-5.12c2.389333-100.693333 54.954667-178.517333 117.077333-173.056s110.592 91.477333 107.861333 192.170667c-0.341333 18.090667-2.389333 36.522667-6.485333 54.272 112.64 21.845333 233.130667 77.824 388.437333 167.594666l-83.968-155.648c-40.96-31.744-83.968-73.386667-171.349333-118.101333 60.074667 15.701333 103.082667 33.792 136.533333 53.930667-265.557333-493.909333-287.061333-559.786667-377.856-773.12z" fill="#1793D1"/></svg>
  if (id.startsWith('fedora')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M512 0C229.344 0 0.224 229.024 0 511.648V907.84a116.384 116.384 0 0 0 116.384 116.128h395.808c282.656-0.128 511.776-229.28 511.776-512 0-282.752-229.248-512-512-512z m196.064 237.952c-16.16 0-22.016-3.104-45.728-3.104a126.848 126.848 0 0 0-126.848 126.624v110.208c0 9.888 8.032 17.92 17.92 17.92h83.328c31.072 0 56.16 24.736 56.16 55.904 0 31.328-25.344 55.968-56.736 55.968h-100.608v127.36a240.32 240.32 0 0 1-240.288 240.288h-1.248a190.944 190.944 0 0 1-53.216-7.52l1.344 0.32c-27.168-7.072-49.376-29.408-49.376-55.296 0-31.328 22.752-54.112 56.736-54.112 16.128 0 22.016 3.072 45.696 3.072a126.848 126.848 0 0 0 126.848-126.624v-110.208a17.92 17.92 0 0 0-17.92-17.888h-83.328a55.808 55.808 0 0 1-56.096-55.904c0-31.328 25.344-55.968 56.736-55.968h100.576v-127.36a240.32 240.32 0 0 1 240.288-240.288c20.128 0 34.432 2.272 53.088 7.136 27.168 7.136 49.408 29.44 49.408 55.296 0 31.36-22.752 54.144-56.736 54.144z" fill="#294172"/></svg>
  if (id.startsWith('rockylinux')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M995.498667 680.362667c18.474667-52.778667 28.501333-109.568 28.501333-168.704C1024 229.077333 794.752 0 512 0S0 229.077333 0 511.658667c0 139.818667 56.106667 266.496 147.114667 358.826666L666.453333 351.530667l128.213334 128.170666 200.832 200.704z m-93.525334 162.816l-235.52-235.349334-368.896 368.597334A510.506667 510.506667 0 0 0 512 1023.274667c156.16 0 296.106667-69.888 389.973333-180.053334h0.042667z" fill="#10B981"/></svg>
  if (id.startsWith('windows')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M56.888889 227.555556l398.222222-70.542223V512H56.888889V227.555556z m0 625.777777l398.222222 70.542223V568.888889H56.888889v284.444444zM512 147.342222L1024 56.888889v455.111111H512V147.342222z m0 786.204445L1024 1024v-455.111111H512v364.657778z" fill="#16C6FE"/></svg>
  return null
}

function clamp(value: number) {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(value, 100))
}

function formatRAM(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(0)} GB`
  return `${mb} MB`
}

function getRemaining(expires: string): ReactNode {
  const end = new Date(expires).getTime()
  const now = Date.now()
  const diff = end - now
  if (diff <= 0) return <span className="text-red-600 font-medium">已到期</span>
  const days = Math.floor(diff / 86400000)
  if (days > 30) return `${Math.floor(days / 30)}个月`
  if (days > 0) return `${days}天`
  const hours = Math.floor(diff / 3600000)
  if (hours > 0) return `${hours}小时`
  return `${Math.floor(diff / 60000)}分钟`
}

function formatRate(value: number) {
  if (value < 1024) return `${value.toFixed(0)} B/s`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(2)} KB/s`
  return `${(value / 1024 / 1024).toFixed(2)} MB/s`
}
