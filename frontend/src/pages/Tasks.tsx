import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router'
import { Download, ListChecks, RefreshCw } from 'lucide-react'
import { getTasks, type Task } from '../services/api'
import { useLanguage } from '../contexts/LanguageContext'
import { actionLabel } from '../utils/labels'
import { operationError } from '../services/operations'
import { Button } from '../components/ui/button'
import { Card } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { NativeSelect } from '../components/ui/native-select'
import { Badge } from '../components/ui/badge'
import { Alert } from '../components/ui/alert'
import { Modal } from '../components/ui/modal'

const operationNames: Record<string, string> = { 'disk-add': '添加数据盘', 'disk-resize': '磁盘扩容', 'disk-remove': '删除数据盘', 'disk-attach': '修改磁盘挂载', backup: '完整备份', clone: '完整克隆', 'save-template': '保存模板', 'restore-backup': '备份恢复', 'instantiate-template': '从模板创建' }
export function taskOperationLabel(type: string) { return operationNames[type] || actionLabel(type) || type }
const statusNames: Record<string, string> = { pending: '排队中', running: '执行中', done: '已完成', failed: '失败', cancelled: '已取消' }
function duration(task: Task) {
  const milliseconds = task.duration_ms || (task.started_at ? Math.max(0, Date.parse(task.finished_at || new Date().toISOString()) - Date.parse(task.started_at)) : 0)
  return milliseconds >= 60000 ? `${(milliseconds / 60000).toFixed(1)} min` : `${Math.max(0, milliseconds / 1000).toFixed(1)} s`
}

export default function Tasks() {
  const { t } = useLanguage()
  const [tasks, setTasks] = useState<Task[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('all')
  const [page, setPage] = useState(0)
  const [selected, setSelected] = useState('')
  const refresh = useCallback(async () => {
    try { setTasks((await getTasks()).data.data || []); setError('') }
    catch (e) { setError(operationError(e)) }
    finally { setLoading(false) }
  }, [])
  useEffect(() => { void refresh(); const timer = window.setInterval(() => void refresh(), 4000); return () => window.clearInterval(timer) }, [refresh])
  const filtered = tasks.filter(task => (status === 'all' || status === task.status) && [task.id, task.container_name, task.stage_detail, taskOperationLabel(task.type)].some(value => value?.toLowerCase().includes(search.toLowerCase())))
  const visible = filtered.slice(page * 30, page * 30 + 30)
  const current = tasks.find(task => task.id === selected)
  const result = current?.result as { container_uuid?: string; artifact_id?: string; name?: string } | undefined
  const downloadLog = (task: Task) => {
    const text = [`${task.id} ${taskOperationLabel(task.type)} ${task.container_name}`, `${task.status} ${task.started_at || task.created_at} → ${task.finished_at || ''}`, ...(task.logs || []).map(line => `${line.time} [${line.stage}] ${line.detail}`), task.error || ''].join('\n')
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }))
    const link = document.createElement('a'); link.href = url; link.download = `${task.id.replace(/[^a-z0-9_-]/gi, '_')}.log`; link.click(); window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  return <div className="space-y-5">
    <div className="flex flex-wrap items-center justify-between gap-3"><div><h1 className="flex items-center gap-2 text-xl font-semibold"><ListChecks className="h-5 w-5" />{t('任务中心')}</h1><p className="mt-1 text-sm text-muted-foreground">{t('查看正在执行与历史任务，服务重启后仍可查询完成结果和阶段日志。')}</p></div><Button variant="outline" size="sm" onClick={() => void refresh()}><RefreshCw className="h-4 w-4" />{t('刷新')}</Button></div>
    {error && <Alert variant="destructive" role="alert">{error}</Alert>}
    <div className="flex flex-col gap-3 sm:flex-row"><Input className="sm:max-w-md" placeholder={t('搜索任务或实例')} value={search} onChange={e => { setSearch(e.target.value); setPage(0) }} /><NativeSelect wrapperClassName="w-full sm:w-40" value={status} onChange={e => { setStatus(e.target.value); setPage(0) }} aria-label={t('任务状态')}><option value="all">{t('全部状态')}</option>{Object.entries(statusNames).map(([key, value]) => <option value={key} key={key}>{t(value)}</option>)}</NativeSelect></div>
    {loading ? <p className="text-sm text-muted-foreground">{t('正在读取任务…')}</p> : !filtered.length ? <Card className="p-10 text-center text-sm text-muted-foreground">{t('没有匹配的任务')}</Card> : <div className="space-y-2">{visible.map(task => <Card key={task.id} className="flex min-w-0 flex-col justify-between gap-3 p-4 sm:flex-row sm:items-center">
      <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><Badge variant={task.status === 'failed' ? 'destructive' : task.status === 'running' ? 'default' : 'secondary'}>{t(statusNames[task.status] || task.status)}</Badge><span className="text-sm font-medium">{t(taskOperationLabel(task.type))}</span><span className="min-w-0 break-all font-mono text-xs text-muted-foreground">{task.container_name || task.id}</span></div><p className="mt-2 break-words text-xs text-muted-foreground">{task.stage_detail || task.error || t('查看详情了解执行结果')}</p><p className="mt-1 text-xs text-muted-foreground">{task.created_at} · {t('耗时')} {duration(task)}</p></div>
      <Button variant="outline" size="sm" onClick={() => setSelected(task.id)}>{t('详情与日志')}</Button>
    </Card>)}</div>}
    <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground"><span>{t('共')} {filtered.length} {t('个任务')} · {t('默认保留最近 30 天内最多 1000 个已结束任务')}</span><div className="flex gap-2"><Button variant="outline" size="sm" disabled={page === 0} onClick={() => setPage(value => value - 1)}>{t('上一页')}</Button><Button variant="outline" size="sm" disabled={(page + 1) * 30 >= filtered.length} onClick={() => setPage(value => value + 1)}>{t('下一页')}</Button></div></div>
    {current && <Modal title={`${t('任务详情')} · ${current.id}`} onClose={() => setSelected('')} size="3xl" footer={<><Button variant="outline" onClick={() => downloadLog(current)}><Download className="h-4 w-4" />{t('下载日志')}</Button><Button variant="outline" onClick={() => setSelected('')}>{t('关闭')}</Button></>}>
      <div className="space-y-4"><div className="grid gap-3 text-sm sm:grid-cols-2"><p>{t('操作')}：{t(taskOperationLabel(current.type))}</p><p>{t('状态')}：{t(statusNames[current.status] || current.status)}</p><p className="break-all">{t('实例')}：{current.container_name || '—'}</p><p>{t('耗时')}：{duration(current)}</p><p>{t('开始')}：{current.started_at || '—'}</p><p>{t('结束')}：{current.finished_at || '—'}</p></div>
      {current.error && <Alert variant="destructive" className="whitespace-pre-wrap break-words">{current.error}</Alert>}
      {result?.container_uuid && <Button asChild size="sm"><Link to={`/container/${result.container_uuid}`}>{t('查看新实例')}</Link></Button>}
      {result?.artifact_id && <p className="break-all text-xs text-muted-foreground">{t('生成的备份或模板')}：{result.artifact_id}</p>}
      <div className="space-y-3 rounded-md border p-3">{current.logs?.length ? current.logs.map((line, index) => <div key={index} className="grid min-w-0 gap-1 border-b pb-3 text-xs last:border-0 last:pb-0 sm:grid-cols-[150px_minmax(0,1fr)]"><time className="text-muted-foreground">{line.time}</time><div className="min-w-0"><p className="font-medium">{line.stage}</p><p className="mt-1 whitespace-pre-wrap break-words text-muted-foreground [overflow-wrap:anywhere]">{line.detail}</p></div></div>) : <p className="text-xs text-muted-foreground">{current.stage_detail || t('此任务暂无阶段日志')}</p>}</div></div>
    </Modal>}
  </div>
}
