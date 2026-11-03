import { useEffect, useState } from 'react'
import { getStorageInfo, type ContainerIdentifier, type StoragePool } from '../services/api'
import { cloneInstance, createBackup, operationError, saveInstanceTemplate } from '../services/operations'
import { useLanguage } from '../contexts/LanguageContext'
import OperationTask from './OperationTask'
import { Modal } from './ui/modal'
import { Alert } from './ui/alert'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { NativeSelect } from './ui/native-select'

export default function InstanceTransferModal({ containerId, containerName, runtime, initialKind = 'backup', onClose, onUpdated }: { containerId: ContainerIdentifier; containerName: string; runtime: string; initialKind?: 'backup' | 'clone' | 'template'; onClose: () => void; onUpdated: () => void | Promise<void> }) {
  const { t } = useLanguage()
  const [kind, setKind] = useState(initialKind)
  const [name, setName] = useState(initialKind === 'clone' ? `${containerName}-copy` : `${containerName}-${initialKind}`)
  const [pool, setPool] = useState('')
  const [pools, setPools] = useState<StoragePool[]>([])
  const [ports, setPorts] = useState('2')
  const [submitting, setSubmitting] = useState(false)
  const [taskId, setTaskId] = useState('')
  const [error, setError] = useState('')
  useEffect(() => { void getStorageInfo().then(r => setPools((r.data.data?.pools || []).filter(p => p.enabled && p.available))).catch(e => setError(operationError(e))) }, [])
  const submit = async () => {
    if (!name.trim()) { setError(t('请输入名称')); return }
    if (kind === 'clone' && !/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/.test(name.trim())) { setError(t('实例名称只能包含字母、数字、点、下划线和短横线，最长 63 字符。')); return }
    if (kind === 'clone' && (!Number.isInteger(Number(ports)) || Number(ports) < 1 || Number(ports) > 64)) { setError(t('NAT 端口配额请输入 1–64 的整数')); return }
    setSubmitting(true); setError('')
    try {
      const response = kind === 'backup' ? await createBackup(containerId, { name: name.trim(), storage_pool_id: pool || undefined }) : kind === 'template' ? await saveInstanceTemplate(containerId, { name: name.trim(), storage_pool_id: pool || undefined }) : await cloneInstance(containerId, { name: name.trim(), storage_pool_id: pool || undefined, assign_nat: true, port_mapping_count: Number(ports) })
      const id = response.data.data?.task_id
      if (!id) throw new Error('服务器未返回任务标识')
      setTaskId(id)
    } catch (e) { setError(operationError(e)) }
    finally { setSubmitting(false) }
  }
  const content = kind === 'clone' ? runtime : 'backups'
  return <Modal title={t('备份、克隆与模板')} description={containerName} onClose={onClose} size="2xl" dismissible={!submitting} footer={<>
    <Button variant="outline" disabled={submitting} onClick={onClose}>{t(taskId ? '后台继续' : '取消')}</Button>
    {!taskId && <Button disabled={submitting || !name.trim()} onClick={() => void submit()}>{t(submitting ? '正在提交…' : '提交任务')}</Button>}
  </>}>
    <div className="space-y-4">
      {!taskId && <>
        <div className="grid grid-cols-3 gap-2">{(['backup', 'clone', 'template'] as const).map(value => <Button key={value} type="button" variant={kind === value ? 'secondary' : 'outline'} disabled={submitting} className="h-auto min-h-9 whitespace-normal text-xs" aria-pressed={kind === value} onClick={() => { setKind(value); setPool(''); setName(`${containerName}-${value === 'clone' ? 'copy' : value}`) }}>{t(value === 'backup' ? '完整备份' : value === 'clone' ? '完整克隆' : '保存模板')}</Button>)}</div>
        <Alert className="text-xs leading-5">{t('任务会先关闭源实例，复制系统盘和全部数据盘，再恢复源实例原来的运行状态。请为业务安排可中断的时间。')}</Alert>
        <Label className="block">{t(kind === 'clone' ? '新实例名称' : kind === 'template' ? '模板名称' : '备份名称')}<Input className="mt-1.5" maxLength={kind === 'clone' ? 63 : 100} value={name} disabled={submitting} onChange={e => setName(e.target.value)} /></Label>
        <Label className="block">{t(kind === 'clone' ? '新实例存储位置' : '备份与模板存储位置')}<NativeSelect wrapperClassName="mt-1.5 w-full" value={pool} disabled={submitting} onChange={e => setPool(e.target.value)}><option value="">{t('自动选择可用存储')}</option>{pools.filter(p => p.content_types.includes(content)).map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Label>
        {kind === 'clone' && <Label className="block">{t('新实例 NAT 端口配额')}<Input className="mt-1.5" type="number" min={1} max={64} value={ports} disabled={submitting} onChange={e => setPorts(e.target.value)} /></Label>}
        {kind !== 'backup' && <p className="text-xs leading-5 text-muted-foreground">{t('完整副本独立于原实例。新实例使用新的 UUID、MAC 和端口，默认保持关机；系统账号保留，请通过 VNC 检查网络与应用身份。')}</p>}
      </>}
      {error && <Alert variant="destructive" role="alert">{error}</Alert>}
      {taskId && <OperationTask id={taskId} onFinish={() => void onUpdated()} />}
    </div>
  </Modal>
}
