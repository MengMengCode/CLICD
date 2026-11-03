import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router'
import { CheckCircle2, Loader2, XCircle } from 'lucide-react'
import { getTasks, type Task } from '../services/api'
import { Alert } from './ui/alert'
import { Button } from './ui/button'
import { useLanguage } from '../contexts/LanguageContext'

export default function OperationTask({ id, onFinish }: { id: string; onFinish?: (task: Task) => void }) {
  const [task, setTask] = useState<Task | null>(null)
  const [error, setError] = useState('')
  const finished = useRef('')
  const callback = useRef(onFinish)
  callback.current = onFinish
  const { t } = useLanguage()
  useEffect(() => {
    let active = true
    const refresh = async () => {
      try {
        const response = await getTasks()
        if (!active) return
        const next = response.data.data?.find(item => item.id === id)
        if (next) {
          setTask(next)
          setError('')
          if (['done', 'failed', 'cancelled'].includes(next.status) && finished.current !== id) {
            finished.current = id
            window.clearInterval(timer)
            callback.current?.(next)
          }
        }
      } catch { if (active) setError(t('暂时无法读取任务，可在任务中心查看进度。')) }
    }
    void refresh()
    const timer = window.setInterval(() => void refresh(), 1500)
    return () => { active = false; window.clearInterval(timer) }
  }, [id, t])
  const done = task?.status === 'done'
  const failed = task?.status === 'failed' || task?.status === 'cancelled'
  const result = task?.result as { container_uuid?: string } | undefined
  return <Alert variant={failed ? 'destructive' : 'default'} className="space-y-2 text-sm" role="status">
    <div className="flex items-start gap-2">
      {done ? <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" /> : failed ? <XCircle className="mt-0.5 h-4 w-4 shrink-0" /> : <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin" />}
      <div className="min-w-0 flex-1 [overflow-wrap:anywhere]">
        <p className="font-medium">{t(done ? '操作已完成' : failed ? '操作未完成' : '任务处理中')} <span className="font-mono text-xs text-muted-foreground">{id}</span></p>
        <p className="mt-1 text-xs">{task?.error || task?.stage_detail || error || t('任务已提交，正在等待执行。')}</p>
      </div>
    </div>
    {task?.logs?.length ? <div className="space-y-1 pl-6 text-xs text-muted-foreground [overflow-wrap:anywhere]">{task.logs.slice(-4).map((log, i) => <p key={i}>{log.detail || log.stage}</p>)}</div> : null}
    <div className="flex flex-wrap gap-2 pl-6">
      <Button asChild size="sm" variant="outline"><Link to="/tasks">{t('查看任务中心')}</Link></Button>
      {done && result?.container_uuid && <Button asChild size="sm"><Link to={`/container/${result.container_uuid}`}>{t('查看新实例')}</Link></Button>}
    </div>
  </Alert>
}
