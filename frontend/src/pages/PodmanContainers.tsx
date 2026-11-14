import { useCallback, useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { isAxiosError } from 'axios'
import { Activity, AlertCircle, ArrowDownToLine, Box, CircleStop, Container, Copy, FileText, Loader2, Package, Play, Plus, RefreshCw, RotateCw, Search, Square, Trash2 } from 'lucide-react'
import { useDialog } from '../components/Dialog'
import { Alert, AlertDescription, AlertTitle } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { Modal } from '../components/ui/modal'
import { NativeSelect, NativeSelectOption } from '../components/ui/native-select'
import { Switch } from '../components/ui/switch'
import { Textarea } from '../components/ui/textarea'
import { useLanguage } from '../contexts/LanguageContext'
import {
  createPodmanContainer,
  getPodmanLogs,
  getPodmanStats,
  getPodmanStatus,
  listPodmanContainers,
  listPodmanImages,
  pullPodmanImage,
  runPodmanAction,
  type CreatePodmanContainerRequest,
  type PodmanAction,
  type PodmanContainer,
  type PodmanImage,
  type PodmanPort,
  type PodmanStats,
  type PodmanStatus,
} from '../services/api'
import { formatBytes } from '../services/operations'
import { copyToClipboard } from '../utils/clipboard'

const statuses: Record<string, string> = {
  created: '待启动',
  running: '运行中',
  paused: '已暂停',
  stopped: '已停止',
  stopping: '正在停止',
  restarting: '重启中',
}

const listColumns = 'md:grid-cols-[minmax(0,1.25fr)_6.25rem_minmax(0,0.8fr)_auto]'

function errorMessage(error: unknown, fallback: string) {
  if (isAxiosError<{ message?: string }>(error)) return error.response?.data?.message || fallback
  return error instanceof Error ? error.message : fallback
}

function formatDate(value: string | undefined, language: string) {
  if (!value || !Number.isFinite(Date.parse(value)) || value.startsWith('0001-')) return '—'
  return new Date(value).toLocaleString(language === 'zh' ? 'zh-CN' : 'en-US')
}

export default function PodmanContainers() {
  const { t } = useLanguage()
  const { confirm } = useDialog()
  const request = useRef<AbortController | null>(null)
  const imageRequest = useRef<AbortController | null>(null)
  const [status, setStatus] = useState<PodmanStatus | null>(null)
  const [containers, setContainers] = useState<PodmanContainer[]>([])
  const [images, setImages] = useState<PodmanImage[]>([])
  const [loading, setLoading] = useState(true)
  const [imagesLoading, setImagesLoading] = useState(true)
  const [error, setError] = useState('')
  const [imageError, setImageError] = useState('')
  const [actionError, setActionError] = useState('')
  const [pendingId, setPendingId] = useState('')
  const [create, setCreate] = useState<{ image: string } | null>(null)
  const [pullOpen, setPullOpen] = useState(false)
  const [detail, setDetail] = useState<{ id: string; tab: 'overview' | 'logs' } | null>(null)
  const [tab, setTab] = useState<'apps' | 'images'>('apps')
  const [query, setQuery] = useState('')
  const [imageQuery, setImageQuery] = useState('')
  const [filter, setFilter] = useState('all')

  const refresh = useCallback(async () => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    setLoading(true)
    setError('')
    try {
      const result = await getPodmanStatus(controller.signal)
      const currentStatus = result.data.data
      if (!currentStatus) throw new Error('无法读取 Podman 状态')
      if (controller.signal.aborted) return
      setStatus(currentStatus)
      if (!currentStatus.available) {
        setContainers([])
        return
      }
      const resultList = await listPodmanContainers(controller.signal)
      if (!controller.signal.aborted) setContainers(resultList.data.data || [])
    } catch (err) {
      if (!controller.signal.aborted) setError(errorMessage(err, '无法加载 Podman 容器'))
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }, [])

  const refreshImages = useCallback(async () => {
    imageRequest.current?.abort()
    const controller = new AbortController()
    imageRequest.current = controller
    setImagesLoading(true)
    setImageError('')
    try {
      const result = await listPodmanImages(controller.signal)
      if (!controller.signal.aborted) setImages(result.data.data || [])
    } catch (err) {
      if (!controller.signal.aborted) setImageError(errorMessage(err, '无法加载本地镜像'))
    } finally {
      if (!controller.signal.aborted) setImagesLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
    return () => request.current?.abort()
  }, [refresh])

  const engineAvailable = status?.available === true
  useEffect(() => {
    if (tab === 'images' && engineAvailable) void refreshImages()
    return () => imageRequest.current?.abort()
  }, [tab, engineAvailable, refreshImages])

  const refreshPage = () => {
    void refresh()
    if (tab === 'images' && engineAvailable) void refreshImages()
  }

  const performAction = async (item: PodmanContainer, action: PodmanAction) => {
    if (action === 'delete') {
      const accepted = await confirm('删除容器', item.name + '\n' + t('删除容器会同时移除它的可写层和持久卷。'))
      if (!accepted) return
    }
    setPendingId(item.id)
    setActionError('')
    try {
      await runPodmanAction(item.id, action)
      if (action === 'delete') setDetail(current => current?.id === item.id ? null : current)
    } catch (err) {
      setActionError(errorMessage(err, '操作失败'))
    } finally {
      await refresh()
      setPendingId('')
    }
  }

  const search = query.trim().toLowerCase()
  const visible = containers.filter(item =>
    (filter === 'all' || filter === item.status) &&
    [item.name, item.image, item.id, ...(item.ports || []).map(port => String(port.host_port))].some(value => value.toLowerCase().includes(search)),
  )
  const detailItem = detail && containers.find(item => item.id === detail.id)
  const isRefreshing = loading || (tab === 'images' && imagesLoading)

  return (
    <div className="mx-auto max-w-7xl space-y-5">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="flex items-center gap-2 text-2xl font-semibold tracking-tight">
              <Container className="size-5 shrink-0" aria-hidden="true" />
              {t('Podman 容器')}
            </h1>
            {engineAvailable && status?.version && <Badge variant="outline">Podman {status.version}</Badge>}
          </div>
          <p className="text-sm text-muted-foreground">{t('从 OCI 镜像运行和管理应用容器。')}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" disabled={isRefreshing} onClick={refreshPage}>
            <RefreshCw className={isRefreshing ? 'size-4 animate-spin' : 'size-4'} />{t('刷新')}
          </Button>
          <Button disabled={!engineAvailable || isRefreshing} onClick={() => tab === 'apps' ? setCreate({ image: '' }) : setPullOpen(true)}>
            {tab === 'apps' ? <Plus className="size-4" /> : <ArrowDownToLine className="size-4" />}
            {t(tab === 'apps' ? '创建容器' : '拉取镜像')}
          </Button>
        </div>
      </header>

      {error && <ErrorAlert message={t(error)} />}
      {actionError && !detail && <ErrorAlert message={t(actionError)} />}
      {status && !engineAvailable && (
        <Alert>
          <AlertCircle className="size-4" />
          <AlertTitle>{t('Podman 不可用')}</AlertTitle>
          <AlertDescription className="space-y-1 break-words">
            <p>{t('请在宿主机安装 Podman，确认面板服务可以运行 Podman 后点击刷新。')}</p>
            {status.message && <p data-no-translate>{status.message}</p>}
          </AlertDescription>
        </Alert>
      )}
      {loading && !status && <ListLoading />}

      {engineAvailable && <>
        <div className="grid grid-cols-3 gap-2 sm:gap-3">
          <CountCard icon={<Box />} label={t('全部容器')} count={containers.length} />
          <CountCard icon={<Activity />} label={t('运行中')} count={containers.filter(item => item.status === 'running').length} />
          <CountCard icon={<CircleStop />} label={t('已停止')} count={containers.filter(item => item.status === 'stopped').length} />
        </div>

        <SectionTabs label={t('Podman 工作区')} current={tab} options={[
          { value: 'apps', label: t('应用容器'), icon: <Container className="size-4" /> },
          { value: 'images', label: t('本地镜像'), icon: <Package className="size-4" /> },
        ]} onChange={setTab} />

        {tab === 'apps' ? (
          <Card className="min-w-0 overflow-hidden p-0" aria-busy={loading}>
            <div className="flex flex-wrap items-center gap-2 border-b p-3 sm:px-4">
              <SearchField label={t('搜索容器名称、镜像或端口')} value={query} onChange={setQuery} />
              <NativeSelect aria-label={t('筛选运行状态')} value={filter} onChange={event => setFilter(event.target.value)}>
                <NativeSelectOption value="all">{t('全部状态')}</NativeSelectOption>
                {Object.entries(statuses).map(([value, label]) => <NativeSelectOption value={value} key={value}>{t(label)}</NativeSelectOption>)}
              </NativeSelect>
            </div>
            {loading && containers.length === 0 ? <ListLoading /> : visible.length ? <>
              <div className={'hidden gap-3 border-b bg-muted/30 px-4 py-2.5 text-xs text-muted-foreground md:grid ' + listColumns}>
                <span>{t('容器 / 镜像')}</span><span>{t('状态')}</span><span>{t('端口映射')}</span><span className="w-36 text-right">{t('操作')}</span>
              </div>
              <ul className="divide-y">
                {visible.map(item => (
                  <li key={item.id} className={'grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-4 py-3 ' + listColumns}>
                    <div className="flex min-w-0 items-center gap-3">
                      <div className="hidden shrink-0 rounded-lg border bg-muted/30 p-2 sm:block"><Box className="size-4 text-muted-foreground" /></div>
                      <div className="min-w-0 space-y-0.5">
                        <button type="button" onClick={() => { setActionError(''); setDetail({ id: item.id, tab: 'overview' }) }} className="block max-w-full truncate text-left text-sm font-medium hover:underline focus-visible:outline-ring" title={item.name} data-no-translate>{item.name}</button>
                        <p className="truncate font-mono text-xs text-muted-foreground" title={item.image} data-no-translate>{item.image}</p>
                      </div>
                    </div>
                    <div className="justify-self-end md:justify-self-start">{pendingId === item.id ? (
                      <Badge variant="outline" role="status"><Loader2 className="size-3 animate-spin" />{t('处理中...')}</Badge>
                    ) : <StatusBadge status={item.status} />}</div>
                    <div className="min-w-0"><PortLabels ports={item.ports} compact /></div>
                    <div className="flex items-center justify-end gap-1">
                      <Button size="icon" className="size-8" variant="ghost" aria-label={t('查看详情')} title={t('查看详情')} onClick={() => { setActionError(''); setDetail({ id: item.id, tab: 'overview' }) }}><Activity /></Button>
                      <Button size="icon" className="size-8" variant="ghost" aria-label={t('日志')} title={t('日志')} onClick={() => { setActionError(''); setDetail({ id: item.id, tab: 'logs' }) }}><FileText /></Button>
                      <PowerButton item={item} disabled={!!pendingId} onAction={performAction} compact />
                    </div>
                  </li>
                ))}
              </ul>
            </> : !error && (
              <EmptyState icon={<Box />} title={t(containers.length ? '没有匹配的容器' : '还没有 Podman 容器。')}>
                {containers.length > 0 && (
                  <Button size="sm" variant="outline" onClick={() => { setQuery(''); setFilter('all') }}>{t('清除筛选')}</Button>
                )}
              </EmptyState>
            )}
          </Card>
        ) : (
          <ImageList images={images} query={imageQuery} onQueryChange={setImageQuery} loading={imagesLoading} error={imageError}
            onUse={image => setCreate({ image })} />
        )}
      </>}

      {create && <CreateModal initialImage={create.image} onClose={() => setCreate(null)} onCreated={() => {
        setCreate(null); setTab('apps'); setQuery(''); setFilter('all'); void refresh()
      }} />}
      {pullOpen && <PullImageModal onClose={() => setPullOpen(false)} onPulled={() => { setPullOpen(false); setImageQuery(''); void refreshImages() }} />}
      {detailItem && detail && <DetailsModal key={detailItem.id} container={detailItem} tab={detail.tab} onTab={next => setDetail({ id: detail.id, tab: next })}
        pending={!!pendingId} refreshing={loading} error={actionError || error} onRefresh={refresh} onClose={() => setDetail(null)} onAction={performAction} />}
    </div>
  )
}

function CountCard({ icon, label, count }: { icon: ReactNode; label: string; count: number }) {
  return (
    <Card className="flex min-w-0 flex-row items-start justify-between gap-1 p-3 sm:p-4">
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className="text-2xl font-semibold tabular-nums">{count}</p>
      </div>
      <span className="hidden shrink-0 rounded-md bg-muted p-2 text-muted-foreground [&>svg]:size-4 sm:block">{icon}</span>
    </Card>
  )
}

function SectionTabs<T extends string>({ label, current, options, onChange }: {
  label: string
  current: T
  options: { value: T; label: string; icon: ReactNode }[]
  onChange: (value: T) => void
}) {
  return (
    <div role="group" aria-label={label} className="inline-flex max-w-full flex-wrap items-center gap-1 rounded-lg bg-muted p-1">
      {options.map(option => (
        <Button key={option.value} type="button" variant="ghost" size="sm" aria-pressed={current === option.value} onClick={() => onChange(option.value)}
          className="gap-2 text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm">
          {option.icon}{option.label}
        </Button>
      ))}
    </div>
  )
}

function SearchField({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <div className="relative flex-1 sm:max-w-80">
      <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input aria-label={label} placeholder={label} type="search" className="pl-9" value={value} onChange={event => onChange(event.target.value)} />
    </div>
  )
}

function EmptyState({ icon, title, children }: { icon: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="flex min-h-44 flex-col items-center justify-center gap-3 px-4 py-8 text-center">
      <span className="rounded-full bg-muted p-3 text-muted-foreground [&>svg]:size-5">{icon}</span>
      <p className="text-sm font-medium">{title}</p>
      {children}
    </div>
  )
}

function ListLoading() {
  const { t } = useLanguage()
  return <div role="status" className="flex items-center justify-center gap-2 px-4 py-10 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t('加载中...')}</div>
}

function StatusBadge({ status }: { status: string }) {
  const { t } = useLanguage()
  const color = status === 'running' ? 'bg-emerald-500' : status === 'paused' ? 'bg-amber-500' : 'bg-muted-foreground'
  return <Badge variant="outline" className="gap-1.5"><span aria-hidden className={'size-1.5 rounded-full ' + color} />{t(statuses[status] || '未知')}</Badge>
}

function PortLabels({ ports, compact = false }: { ports?: PodmanPort[]; compact?: boolean }) {
  const { t } = useLanguage()
  if (!ports?.length) return <span className="text-xs text-muted-foreground">{compact ? '—' : t('未发布端口')}</span>
  return (
    <div className="flex flex-wrap gap-1">
      {(compact ? ports.slice(0, 2) : ports).map(port => (
        <Badge key={port.host_port + '/' + port.protocol} variant="secondary" className="font-mono font-normal" data-no-translate>
          {port.host_port} → {port.container_port}/{port.protocol}
        </Badge>
      ))}
      {compact && ports.length > 2 && <Badge variant="outline">+{ports.length - 2}</Badge>}
    </div>
  )
}

function PowerButton({ item, disabled, onAction, compact = false }: {
  item: PodmanContainer; disabled: boolean; onAction: (item: PodmanContainer, action: PodmanAction) => Promise<void>; compact?: boolean
}) {
  const { t } = useLanguage()
  const canStop = item.status === 'running' || item.status === 'paused'
  if (!canStop && item.status !== 'created' && item.status !== 'stopped') return null
  const label = t(canStop ? '停止' : '启动')
  return (
    <Button size={compact ? 'icon' : 'sm'} className={compact ? 'size-8' : undefined} variant={compact ? 'ghost' : 'outline'} disabled={disabled} onClick={() => void onAction(item, canStop ? 'stop' : 'start')} aria-label={label} title={label}>
      {canStop ? <Square /> : <Play />}{!compact && label}
    </Button>
  )
}

interface PortDraft {
  id: number
  host_port: string
  container_port: string
  protocol: PodmanPort['protocol']
}

function CreateModal({ initialImage, onClose, onCreated }: { initialImage: string; onClose: () => void; onCreated: () => void }) {
  const { t } = useLanguage()
  const formId = useId()
  const nextPortId = useRef(0)
  const [name, setName] = useState('')
  const [image, setImage] = useState(initialImage)
  const [cpus, setCpus] = useState('')
  const [memory, setMemory] = useState('')
  const [ports, setPorts] = useState<PortDraft[]>([])
  const [command, setCommand] = useState('')
  const [env, setEnv] = useState('')
  const [volumes, setVolumes] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const changePort = (id: number, patch: Partial<Omit<PortDraft, 'id'>>) => {
    setPorts(items => items.map(item => item.id === id ? { ...item, ...patch } : item))
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setError('')
    let args: string[] | undefined
    if (command.trim()) {
      try {
        const parsed: unknown = JSON.parse(command)
        if (!Array.isArray(parsed) || !parsed.every(item => typeof item === 'string')) throw new Error()
        args = parsed
      } catch {
        setError('启动参数须填写为 JSON 字符串数组。')
        return
      }
    }
    const envEntries: Array<[string, string]> = []
    const names = new Set<string>()
    for (const line of env.split(/\r?\n/)) {
      if (!line.trim()) continue
      const separator = line.indexOf('=')
      const key = line.slice(0, separator)
      if (separator < 1 || !/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(key) || names.has(key)) {
        setError('环境变量每行须为 KEY=value，且变量名不能重复。')
        return
      }
      names.add(key)
      envEntries.push([key, line.slice(separator + 1)])
    }
    const request: CreatePodmanContainerRequest = {
      name: name.trim(),
      image: image.trim(),
      cpus: cpus ? Number(cpus) : undefined,
      memory_mb: memory ? Number(memory) : undefined,
      ports: ports.map(port => ({ host_port: Number(port.host_port), container_port: Number(port.container_port), protocol: port.protocol })),
      command: args,
      env: envEntries.length ? Object.fromEntries(envEntries) : undefined,
      volumes: volumes.split(/\r?\n/).map(path => path.trim()).filter(Boolean),
    }
    setSubmitting(true)
    try {
      await createPodmanContainer(request)
      onCreated()
    } catch (err) {
      setError(errorMessage(err, '创建 Podman 容器失败'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal title={t('创建 Podman 容器')} size="3xl" onClose={onClose} dismissible={!submitting}
      footer={<>
        <Button variant="outline" disabled={submitting} onClick={onClose}>{t('取消')}</Button>
        <Button type="submit" form={formId} disabled={submitting}>
          {submitting && <Loader2 className="size-4 animate-spin" />}
          {t(submitting ? '正在拉取镜像并创建容器...' : '创建容器')}
        </Button>
      </>}>
      <form id={formId} onSubmit={submit} className="space-y-4" aria-busy={submitting}>
        {error && <ErrorAlert message={t(error)} />}
        <fieldset disabled={submitting} className="min-w-0 space-y-5">
          <section className="space-y-3 rounded-lg border p-3 sm:p-4">
            <h2 className="text-sm font-medium">{t('基础设置')}</h2>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor={formId + '-name'}>{t('容器名称')}</Label>
                <Input id={formId + '-name'} value={name} onChange={event => setName(event.target.value)} placeholder="web-demo" maxLength={64} required />
              </div>
              <div className="space-y-2">
                <Label htmlFor={formId + '-image'}>{t('OCI 镜像')}</Label>
                <Input id={formId + '-image'} value={image} onChange={event => setImage(event.target.value)} placeholder="docker.io/library/nginx:alpine" spellCheck={false} required />
                <p className="text-xs text-muted-foreground">{t('填写包含仓库域名的完整镜像名称。')}</p>
              </div>
            </div>
          </section>
          <section className="space-y-3 rounded-lg border p-3 sm:p-4">
            <h2 className="text-sm font-medium">{t('资源限制')}</h2>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor={formId + '-cpus'}>{t('CPU 上限（核）')}</Label>
                <Input id={formId + '-cpus'} type="number" min="0.01" max="256" step="0.01" value={cpus} onChange={event => setCpus(event.target.value)} placeholder={t('可选')} />
              </div>
              <div className="space-y-2">
                <Label htmlFor={formId + '-memory'}>{t('内存上限（MiB）')}</Label>
                <Input id={formId + '-memory'} type="number" min="16" max="1048576" step="1" value={memory} onChange={event => setMemory(event.target.value)} placeholder={t('可选')} />
              </div>
            </div>
          </section>
          <section className="space-y-2 rounded-lg border p-3 sm:p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-sm font-medium">{t('端口映射')}</h2>
              <Button size="sm" type="button" variant="outline" onClick={() => {
                const id = nextPortId.current++
                setPorts(items => [...items, { id, host_port: '', container_port: '', protocol: 'tcp' }])
              }}><Plus className="size-3.5" />{t('添加端口')}</Button>
            </div>
            <p className="text-xs text-muted-foreground">{t('服务可通过宿主机地址和已发布的端口访问。')}</p>
            <p className="text-xs text-muted-foreground">{t('请选择 CLICD IPv4 NAT 配置范围（默认 20000–65535）之外的宿主机端口，避免冲突。')}</p>
            {ports.map((port) => {
              const id = formId + '-port-' + port.id
              return (
                <div key={port.id} className="grid min-w-0 grid-cols-2 items-end gap-2 rounded-md border p-2 sm:grid-cols-[1fr_1fr_1fr_auto]">
                  <div className="min-w-0 space-y-1">
                    <Label className="text-xs leading-tight" htmlFor={id + '-host'}>{t('宿主机端口')}</Label>
                    <Input id={id + '-host'} type="number" min={1} max={65535} value={port.host_port} onChange={event => changePort(port.id, { host_port: event.target.value })} placeholder="8080" required />
                  </div>
                  <div className="min-w-0 space-y-1">
                    <Label className="text-xs leading-tight" htmlFor={id + '-container'}>{t('容器端口')}</Label>
                    <Input id={id + '-container'} type="number" min={1} max={65535} value={port.container_port} onChange={event => changePort(port.id, { container_port: event.target.value })} placeholder="80" required />
                  </div>
                  <div className="min-w-0 space-y-1">
                    <Label className="text-xs" htmlFor={id + '-protocol'}>{t('协议')}</Label>
                    <NativeSelect id={id + '-protocol'} wrapperClassName="w-full" value={port.protocol} onChange={event => changePort(port.id, { protocol: event.target.value as PodmanPort['protocol'] })}>
                      <NativeSelectOption value="tcp">TCP</NativeSelectOption>
                      <NativeSelectOption value="udp">UDP</NativeSelectOption>
                    </NativeSelect>
                  </div>
                  <Button size="icon" type="button" variant="ghost" className="justify-self-end" aria-label={t('移除此端口映射')} title={t('移除此端口映射')} onClick={() => setPorts(items => items.filter(item => item.id !== port.id))}><Trash2 className="size-4" /></Button>
                </div>
              )
            })}
          </section>
          <details className="rounded-lg border p-3">
            <summary className="cursor-pointer text-sm font-medium">{t('高级选项')}</summary>
            <div className="mt-4 space-y-4">
              <div className="space-y-2">
                <Label htmlFor={formId + '-command'}>{t('启动参数')}</Label>
                <Textarea id={formId + '-command'} className="font-mono text-xs" value={command} onChange={event => setCommand(event.target.value)} placeholder={'["sh", "-c", "sleep 3600"]'} rows={2} spellCheck={false} />
                <p className="text-xs text-muted-foreground">{t('填写 JSON 字符串数组。留空时使用镜像默认参数。')}</p>
              </div>
              <div className="space-y-2">
                <Label htmlFor={formId + '-env'}>{t('环境变量')}</Label>
                <Textarea id={formId + '-env'} className="font-mono text-xs" value={env} onChange={event => setEnv(event.target.value)} placeholder="TZ=Asia/Singapore" rows={2} spellCheck={false} />
                <p className="text-xs text-muted-foreground">{t('每行一条 KEY=value。')}</p>
              </div>
              <div className="space-y-2">
                <Label htmlFor={formId + '-volumes'}>{t('持久卷')}</Label>
                <Textarea id={formId + '-volumes'} className="font-mono text-xs" value={volumes} onChange={event => setVolumes(event.target.value)} placeholder="/app/data" rows={2} spellCheck={false} />
                <p className="text-xs text-muted-foreground">{t('每行填写一个容器内绝对路径。数据在容器停止后保留，删除容器时移除。')}</p>
              </div>
            </div>
          </details>
        </fieldset>
      </form>
    </Modal>
  )
}

function ImageList({ images, query, onQueryChange, loading, error, onUse }: {
  images: PodmanImage[]; query: string; onQueryChange: (query: string) => void
  loading: boolean; error: string; onUse: (image: string) => void
}) {
  const { t, language } = useLanguage()
  const search = query.trim().toLowerCase()
  const matches = images.filter(image => image.id.toLowerCase().includes(search) || image.tags?.some(tag => tag.toLowerCase().includes(search)))
  return (
    <Card className="min-w-0 overflow-hidden p-0" aria-busy={loading}>
      <div className="flex flex-wrap items-center gap-2 border-b p-3 sm:px-4">
        <SearchField label={t('搜索镜像名称或 ID')} value={query} onChange={onQueryChange} />
        {!loading && !error && <span className="ml-auto text-xs tabular-nums text-muted-foreground">{matches.length} / {images.length}</span>}
      </div>
      {error && <div className="p-4"><ErrorAlert message={t(error)} /></div>}
      {loading && images.length === 0 ? <ListLoading /> : matches.length ? (
        <ul className="divide-y">
          {matches.map(image => (
            <li key={image.id} className="p-4">
              <div className="flex min-w-0 items-start gap-3">
                <span className="shrink-0 rounded-lg border bg-muted/30 p-2"><Package className="size-4 text-muted-foreground" /></span>
                <div className="min-w-0 space-y-1.5">
                  <p className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                    <span className="font-mono" title={image.id} data-no-translate>{image.id.replace(/^sha256:/, '').slice(0, 12)}</span>
                    <span>{formatBytes(image.size_bytes)}</span>
                    <span>{formatDate(image.created_at, language)}</span>
                  </p>
                  {image.tags?.length ? (
                    <ul className="space-y-1.5">
                      {image.tags.map(tag => (
                        <li key={tag} className="flex min-w-0 flex-wrap items-center gap-1.5">
                          <span className="break-all font-mono text-xs font-medium" data-no-translate>{tag}</span>
                          <Button type="button" variant="outline" className="h-6 px-2 text-xs" onClick={() => onUse(tag)}>{t('使用镜像')}</Button>
                        </li>
                      ))}
                    </ul>
                  ) : <p className="text-sm text-muted-foreground">{t('无标签')}</p>}
                </div>
              </div>
            </li>
          ))}
        </ul>
      ) : !error && (
        <EmptyState icon={<Package />} title={t(images.length ? '没有匹配的镜像' : '还没有本地 OCI 镜像。')}>
          {images.length > 0 && <Button size="sm" variant="outline" onClick={() => onQueryChange('')}>{t('清除筛选')}</Button>}
        </EmptyState>
      )}
    </Card>
  )
}

function PullImageModal({ onClose, onPulled }: { onClose: () => void; onPulled: () => void }) {
  const { t } = useLanguage()
  const formId = useId()
  const [image, setImage] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!image.trim() || busy) return
    setBusy(true)
    setError('')
    try {
      await pullPodmanImage(image.trim())
      onPulled()
    } catch (err) {
      setError(errorMessage(err, '拉取镜像失败'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title={t('拉取镜像')} onClose={onClose} dismissible={!busy} footer={<>
      <Button variant="outline" onClick={onClose} disabled={busy}>{t('取消')}</Button>
      <Button type="submit" form={formId} disabled={busy}>{busy && <Loader2 className="size-4 animate-spin" />}{t(busy ? '正在拉取镜像…' : '拉取镜像')}</Button>
    </>}>
      <form className="space-y-3" id={formId} onSubmit={submit} aria-busy={busy}>
        {error && <ErrorAlert message={t(error)} />}
        <div className="space-y-2">
          <Label htmlFor={formId + '-image'}>{t('OCI 镜像')}</Label>
          <Input id={formId + '-image'} value={image} onChange={event => setImage(event.target.value)} placeholder="docker.io/library/nginx:alpine" spellCheck={false} disabled={busy} required />
          <p className="text-xs text-muted-foreground">{t('填写包含仓库域名的完整镜像名称。')}</p>
        </div>
      </form>
    </Modal>
  )
}

function DetailsModal({ container, tab, onTab, pending, refreshing, error, onRefresh, onClose, onAction }: {
  container: PodmanContainer
  tab: 'overview' | 'logs'
  onTab: (tab: 'overview' | 'logs') => void
  pending: boolean
  refreshing: boolean
  error: string
  onRefresh: () => Promise<void>
  onClose: () => void
  onAction: (item: PodmanContainer, action: PodmanAction) => Promise<void>
}) {
  const { t } = useLanguage()
  const active = container.status === 'running' || container.status === 'paused'
  return (
    <Modal title={<span data-no-translate>{container.name}</span>} description={t('容器详情')} extra={<StatusBadge status={container.status} />} size="4xl" onClose={onClose}
      footer={<>
        <Button variant="ghost" className="text-destructive sm:mr-auto" disabled={pending} onClick={() => void onAction(container, 'delete')}><Trash2 className="size-4" />{t('删除')}</Button>
        <PowerButton item={container} disabled={pending} onAction={onAction} />
        {active && <Button variant="outline" disabled={pending} onClick={() => void onAction(container, 'restart')}><RotateCw className="size-4" />{t('重启')}</Button>}
        <Button onClick={onClose}>{t('关闭')}</Button>
      </>}>
      <div className="space-y-4" aria-busy={pending}>
        {error && <ErrorAlert message={t(error)} />}
        {pending && <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t('处理中...')}</p>}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <SectionTabs label={t('容器详情')} current={tab} options={[
            { value: 'overview', label: t('概览'), icon: <Activity className="size-4" /> },
            { value: 'logs', label: t('日志'), icon: <FileText className="size-4" /> },
          ]} onChange={onTab} />
          {tab === 'overview' && <Button size="sm" variant="outline" disabled={pending || refreshing} onClick={() => void onRefresh()}>
            <RefreshCw className={refreshing ? 'size-4 animate-spin' : 'size-4'} />{t('刷新')}
          </Button>}
        </div>
        {tab === 'overview' ? <ContainerOverview container={container} /> : <ContainerLogs container={container} />}
      </div>
    </Modal>
  )
}

function ContainerOverview({ container }: { container: PodmanContainer }) {
  const { t, language } = useLanguage()
  const fields = [
    [t('OCI 镜像'), container.image, 'font-mono'],
    ['ID', container.id, 'font-mono'],
    [t('创建时间'), formatDate(container.created_at, language), ''],
    [t('最近启动'), formatDate(container.started_at, language), ''],
    ...(container.health ? [[t('健康检查'), t(({ healthy: '健康', unhealthy: '异常', starting: '正在启动' } as Record<string, string>)[container.health] || container.health), '']] : []),
    ...(container.status === 'stopped' && container.exit_code != null ? [[t('退出码'), String(container.exit_code), 'font-mono']] : []),
    ...(container.status === 'stopped' && container.finished_at ? [[t('最近退出'), formatDate(container.finished_at, language), '']] : []),
    [t('CPU 上限'), container.cpus ? String(container.cpus) : t('未限制'), ''],
    [t('内存上限'), container.memory_mb ? container.memory_mb + ' MiB' : t('未限制'), ''],
  ]
  return (
    <div className="space-y-4">
      <dl className="grid gap-3 rounded-lg border p-3 sm:grid-cols-2 sm:p-4">
        {fields.map(([label, value, className]) => (
          <div key={label} className="min-w-0 space-y-1">
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className={'text-sm [overflow-wrap:anywhere] ' + className} data-no-translate>{value}</dd>
          </div>
        ))}
      </dl>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="min-w-0 space-y-2 rounded-lg border p-3 sm:p-4">
          <h3 className="text-sm font-medium">{t('端口映射')}</h3>
          <PortLabels ports={container.ports} />
        </div>
        <div className="min-w-0 space-y-2 rounded-lg border p-3 sm:p-4">
          <h3 className="text-sm font-medium">{t('持久卷')}</h3>
          {container.volumes?.length ? <ul className="flex flex-wrap gap-1.5">{container.volumes.map(volume => (
            <li key={volume} className="max-w-full break-all rounded bg-muted px-2 py-1 font-mono text-xs" data-no-translate>{volume}</li>
          ))}</ul> : <p className="text-xs text-muted-foreground">{t('未配置持久卷')}</p>}
        </div>
      </div>
      {container.status === 'running' ? <LiveStats key={container.id} id={container.id} /> : (
        <div className="rounded-lg border p-3 sm:p-4"><h3 className="text-sm font-medium">{t('实时资源')}</h3><p className="mt-2 text-xs text-muted-foreground">{t('容器运行后显示实时资源。')}</p></div>
      )}
    </div>
  )
}

function usePollingResource<T>(load: (signal: AbortSignal) => Promise<T>, fallback: string, interval = 0) {
  const [data, setData] = useState<T>()
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [revision, setRevision] = useState(0)

  useEffect(() => {
    let timer: number | undefined
    const controller = new AbortController()
    const refresh = async () => {
      try {
        const value = await load(controller.signal)
        if (!controller.signal.aborted) {
          setData(value)
          setError('')
        }
      } catch (err) {
        if (!controller.signal.aborted) setError(errorMessage(err, fallback))
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          if (interval) timer = window.setTimeout(() => void refresh(), interval)
        }
      }
    }
    setLoading(true)
    void refresh()
    return () => {
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [load, fallback, interval, revision])

  return { data, loading, error, refresh: () => setRevision(current => current + 1) }
}

function LiveStats({ id }: { id: string }) {
  const { t } = useLanguage()
  const load = useCallback(async (signal: AbortSignal) => (await getPodmanStats(id, signal)).data.data, [id])
  const { data, loading, error } = usePollingResource(load, '无法读取容器资源', 5000)
  const metrics = data ? statsMetrics(data, t) : []
  return (
    <div className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="flex items-center gap-2 text-sm font-medium"><Activity className="size-4 text-muted-foreground" />{t('实时资源')}</h3>
        <span className="text-xs text-muted-foreground">{t('每 5 秒刷新')}</span>
      </div>
      {error && <ErrorAlert message={t(error)} />}
      {metrics.length ? <dl className="grid grid-cols-2 gap-2 sm:grid-cols-3">
        {metrics.map(([label, value]) => (
          <div key={label} className="min-w-0 space-y-1 rounded-md bg-muted/50 px-3 py-2">
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="break-words text-sm font-medium tabular-nums" data-no-translate>{value}</dd>
          </div>
        ))}
      </dl> : !error && <p role="status" className="text-sm text-muted-foreground">{t(loading ? '加载中...' : '暂无资源数据')}</p>}
    </div>
  )
}

function statsMetrics(metrics: PodmanStats, t: (text: string) => string) {
  const usage = metrics.memory_used_bytes == null ? '—' : formatBytes(metrics.memory_used_bytes)
  const memory = metrics.memory_limit_bytes ? usage + ' / ' + formatBytes(metrics.memory_limit_bytes) : usage
  const bytes = (value: number | null) => value == null ? '—' : formatBytes(value)
  return [
    ['CPU', metrics.cpu_percent == null ? '—' : metrics.cpu_percent.toFixed(1) + '%'],
    [t('内存'), memory],
    [t('进程数'), String(metrics.pids ?? '—')],
    [t('网络接收 / 发送'), bytes(metrics.network_rx_bytes) + ' / ' + bytes(metrics.network_tx_bytes)],
    [t('磁盘读取 / 写入'), bytes(metrics.block_read_bytes) + ' / ' + bytes(metrics.block_write_bytes)],
  ]
}

function ContainerLogs({ container }: { container: PodmanContainer }) {
  const { t } = useLanguage()
  const { alert } = useDialog()
  const [follow, setFollow] = useState(false)
  const sectionRef = useRef<HTMLElement | null>(null)
  const logRef = useRef<HTMLPreElement | null>(null)
  const load = useCallback(async (signal: AbortSignal) => (await getPodmanLogs(container.id, signal)).data.data?.logs || '', [container.id])
  const { data: logs = '', loading, error, refresh } = usePollingResource(load, '无法读取容器日志', follow ? 3000 : 0)

  useEffect(() => {
    const element = logRef.current
    if (follow && element) element.scrollTop = element.scrollHeight
  }, [logs, follow])

  const copy = async () => {
    try {
      if (await copyToClipboard(logs, sectionRef.current ?? undefined)) {
        await alert('复制成功', '日志已复制到剪贴板。')
        return
      }
    } catch {
      // The download action remains available when the browser blocks clipboard access.
    }
    await alert('复制失败', '可使用下载日志保存内容。')
  }

  const download = () => {
    const url = URL.createObjectURL(new Blob([logs], { type: 'text/plain;charset=utf-8' }))
    const link = document.createElement('a')
    link.href = url
    link.download = container.name.replace(/[^A-Za-z0-9_.-]+/g, '-') + '.log'
    link.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  return (
    <section ref={sectionRef} className="min-w-0 space-y-3" aria-label={t('容器日志')} aria-busy={loading}>
      <div className="flex flex-wrap items-center gap-2">
        <label className="mr-auto flex items-center gap-2 text-xs text-muted-foreground"><Switch checked={follow} onCheckedChange={setFollow} />{t('自动刷新')}</label>
        <Button variant="outline" size="sm" disabled={loading} onClick={refresh}><RefreshCw className="size-4" />{t('刷新')}</Button>
        <Button variant="outline" size="sm" disabled={!logs} onClick={() => void copy()}><Copy className="size-4" />{t('复制日志')}</Button>
        <Button variant="outline" size="sm" disabled={!logs} onClick={download}><ArrowDownToLine className="size-4" />{t('下载日志')}</Button>
      </div>
      {error && <ErrorAlert message={t(error)} />}
      {logs ? <pre ref={logRef} className="max-h-[48dvh] min-h-32 overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted/40 p-3 font-mono text-xs leading-relaxed" tabIndex={0} data-no-translate>{logs}</pre>
        : <div role="status" className="min-h-28 rounded-md border bg-muted/30 px-3 py-6 text-center text-sm text-muted-foreground">{t(loading ? '加载中...' : '该容器暂无日志。')}</div>}
    </section>
  )
}

function ErrorAlert({ message }: { message: string }) {
  return <Alert variant="destructive"><AlertCircle className="size-4" /><AlertDescription className="whitespace-pre-wrap break-words">{message}</AlertDescription></Alert>
}
