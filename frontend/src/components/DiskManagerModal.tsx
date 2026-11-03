import { useCallback, useEffect, useState } from 'react'
import { HardDrive, Plus, RefreshCw, Unplug, Plug, Trash2, Expand } from 'lucide-react'
import { type ContainerIdentifier, getStorageInfo, type StoragePool } from '../services/api'
import { addDataDisk, attachDataDisk, deleteDataDisk, formatBytes, getDisks, operationError, resizeDisk, type DiskInventory } from '../services/operations'
import { useLanguage } from '../contexts/LanguageContext'
import { useDialog } from './Dialog'
import OperationTask from './OperationTask'
import { Modal } from './ui/modal'
import { Button } from './ui/button'
import { Card } from './ui/card'
import { Alert } from './ui/alert'
import { Badge } from './ui/badge'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { NativeSelect } from './ui/native-select'

export default function DiskManagerModal({ containerId, runtime, readOnly = false, onClose, onUpdated }: { containerId: ContainerIdentifier; runtime: string; readOnly?: boolean; onClose: () => void; onUpdated: () => void | Promise<void> }) {
  const { t } = useLanguage()
  const dialog = useDialog()
  const [inventory, setInventory] = useState<DiskInventory | null>(null)
  const [pools, setPools] = useState<StoragePool[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [taskId, setTaskId] = useState('')
  const [editor, setEditor] = useState<{ kind: 'add' | 'resize'; id: string; current: number } | null>(null)
  const [name, setName] = useState('')
  const [size, setSize] = useState('10')
  const [pool, setPool] = useState('')
  const [mount, setMount] = useState('/mnt/data1')
  const refresh = useCallback(async () => {
    try { const data = (await getDisks(containerId)).data.data; if (!data) throw new Error('磁盘数据不可用'); setInventory(data); setError('') }
    catch (e) { setError(operationError(e)) }
    finally { setLoading(false) }
  }, [containerId])
  useEffect(() => { void refresh(); if (!readOnly) void getStorageInfo().then(r => setPools((r.data.data?.pools || []).filter(p => p.enabled && p.available && p.content_types.includes(runtime)))).catch(() => {}) }, [refresh, readOnly, runtime])
  const editable = !!inventory?.can_edit && !busy && !readOnly
  const run = async (operation: () => Promise<{ data: { data?: { task_id: string } } }>) => {
    if (!editable) return
    setBusy(true); setError('')
    try { const response = await operation(); const id = response.data.data?.task_id; if (!id) throw new Error('服务器未返回任务标识'); setTaskId(id); setEditor(null) }
    catch (e) { setError(operationError(e)); setBusy(false); void refresh() }
  }
  const openResize = (id: string, current: number) => { setEditor({ kind: 'resize', id, current }); setSize(String(Math.ceil(current + 1))); setError('') }
  const save = () => {
    const amount = Number(size)
    if (!Number.isFinite(amount) || amount < 0.125 || amount > 16384 || (editor?.kind === 'resize' && amount <= editor.current)) { setError(t('请输入更大的有效容量，磁盘仅支持扩容。')); return }
    if (editor?.kind === 'add') {
      if (!name.trim()) { setError(t('请输入数据盘名称')); return }
      void run(() => addDataDisk(containerId, { name: name.trim(), size_gb: amount, storage_pool_id: pool || undefined, mount_path: runtime === 'lxc' ? mount : undefined }))
    } else if (editor) void run(() => resizeDisk(containerId, editor.id, amount))
  }
  return <Modal title={t('磁盘管理')} description={t('扩容系统盘，管理独立数据盘及挂载状态。')} onClose={onClose} size="3xl" footer={<Button variant="outline" onClick={onClose}>{t(busy ? '后台继续' : '关闭')}</Button>}>
    <div className="space-y-4">
      <div className="flex flex-wrap justify-between gap-2">
        <p className="text-xs leading-5 text-muted-foreground">{t(runtime === 'kvm' ? '修改磁盘前请先关机。虚拟磁盘扩容后，仍需在客户机内扩展分区和文件系统。' : '修改磁盘前请先停止容器。受管 ext4 磁盘扩容会同步扩展文件系统。')}</p>
        <Button variant="outline" size="sm" disabled={busy || loading} onClick={() => void refresh()}><RefreshCw className="h-3.5 w-3.5" />{t('刷新')}</Button>
      </div>
      {error && <Alert variant="destructive" role="alert">{error}</Alert>}
      {taskId && <OperationTask id={taskId} onFinish={() => { setBusy(false); void refresh(); void onUpdated() }} />}
      {loading ? <p className="text-sm text-muted-foreground">{t('正在读取磁盘…')}</p> : inventory && <>
        {!inventory.can_edit && <Alert className="text-xs">{inventory.reason || t('实例运行或有任务执行时，只能查看磁盘。')}</Alert>}
        <Card className="flex min-w-0 flex-wrap items-center justify-between gap-3 p-4">
          <div className="flex min-w-0 items-start gap-3"><HardDrive className="mt-1 h-4 w-4 shrink-0 text-muted-foreground" /><div className="min-w-0"><p className="text-sm font-medium">{t('系统盘')} <Badge variant="secondary">{inventory.system.format}</Badge></p><p className="mt-1 text-xs text-muted-foreground">{inventory.system.size_gb} GiB · {t('已占用')} {formatBytes(inventory.system.allocated_bytes)}</p>{inventory.system.error && <p role="alert" className="mt-2 text-xs leading-5 text-destructive [overflow-wrap:anywhere]">{inventory.system.error}</p>}</div></div>
          {!readOnly && <Button variant="outline" size="sm" disabled={!editable || !inventory.system.can_resize} onClick={() => openResize('system', inventory.system.size_gb)}><Expand className="h-3.5 w-3.5" />{t('扩容')}</Button>}
        </Card>
        <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-sm font-semibold">{t('数据盘')} <span className="font-normal text-muted-foreground">{inventory.data_disks.length}/{inventory.limits.max_data_disks}</span></h3>{!readOnly && <Button size="sm" disabled={!editable || inventory.data_disks.length >= inventory.limits.max_data_disks} onClick={() => { setEditor({ kind: 'add', id: '', current: 0 }); setName(''); setSize('10'); setMount(`/mnt/data${inventory.data_disks.length + 1}`); setError('') }}><Plus className="h-3.5 w-3.5" />{t('添加数据盘')}</Button>}</div>
        {!inventory.data_disks.length && <Card className="p-5 text-center text-sm text-muted-foreground">{t('尚未添加数据盘')}</Card>}
        {inventory.data_disks.map(disk => <Card key={disk.id} className="min-w-0 space-y-3 p-4">
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><p className="min-w-0 break-words text-sm font-medium">{disk.name}</p><Badge variant={disk.attached ? 'secondary' : 'outline'}>{t(disk.attached ? '已挂载' : '已卸载')}</Badge></div>
          <p className="break-words text-xs text-muted-foreground">{disk.size_gb} GiB · {t('已占用')} {formatBytes(disk.allocated_bytes)} · {disk.mount_path || `${disk.target} (${disk.bus})`}</p>
          {disk.error && <p role="alert" className="text-xs leading-5 text-destructive [overflow-wrap:anywhere]">{disk.error}</p>}
          {!readOnly && <div className="flex flex-wrap gap-2">
            <Button size="sm" variant="outline" disabled={!editable || disk.can_resize === false || !!disk.error} onClick={() => openResize(disk.id, disk.size_gb)}><Expand className="h-3.5 w-3.5" />{t('扩容')}</Button>
            <Button size="sm" variant="outline" disabled={!editable} onClick={() => void run(() => attachDataDisk(containerId, disk.id, !disk.attached))}>{disk.attached ? <Unplug className="h-3.5 w-3.5" /> : <Plug className="h-3.5 w-3.5" />}{t(disk.attached ? '卸载' : '挂载')}</Button>
            <Button size="sm" variant="destructive" disabled={!editable} onClick={async () => { if (await dialog.confirm(t('删除数据盘'), t('此操作会永久删除该数据盘及其中的数据，无法撤销。请确认已有可用备份。'))) void run(() => deleteDataDisk(containerId, disk.id)) }}><Trash2 className="h-3.5 w-3.5" />{t('删除')}</Button>
          </div>}
        </Card>)}
        {!!inventory.data_disks.length && <p className="text-xs leading-5 text-muted-foreground">{t('完整备份会包含数据盘。包含数据盘的实例请使用完整备份进行回滚。')}</p>}
      </>}
      {editor && <Card className="space-y-4 border-primary/30 p-4">
        <h3 className="text-sm font-semibold">{t(editor.kind === 'add' ? '添加数据盘' : '扩容磁盘')}</h3>
        <div className="grid gap-4 sm:grid-cols-2">
          {editor.kind === 'add' && <Label>{t('名称')}<Input className="mt-1.5" value={name} maxLength={64} disabled={busy} onChange={e => setName(e.target.value)} placeholder={t('例如：应用数据')} /></Label>}
          <Label>{t('容量')} (GiB)<Input className="mt-1.5" type="number" min={editor.kind === 'resize' ? editor.current + 0.125 : 0.125} max={16384} step={0.125} value={size} disabled={busy} onChange={e => setSize(e.target.value)} /></Label>
          {editor.kind === 'add' && <Label>{t('存储位置')}<NativeSelect wrapperClassName="mt-1.5 w-full" value={pool} disabled={busy} onChange={e => setPool(e.target.value)}><option value="">{t('自动选择可用存储')}</option>{pools.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Label>}
          {editor.kind === 'add' && runtime === 'lxc' && <Label>{t('容器内挂载目录')}<Input className="mt-1.5" value={mount} disabled={busy} onChange={e => setMount(e.target.value)} placeholder="/mnt/data1" /></Label>}
        </div>
        {runtime === 'kvm' && editor.kind === 'add' && <p className="text-xs text-muted-foreground">{t('新数据盘为空白盘，需要在客户机内分区、格式化并挂载。')}</p>}
        <div className="flex justify-end gap-2"><Button variant="outline" disabled={busy} onClick={() => setEditor(null)}>{t('取消')}</Button><Button disabled={!editable} onClick={save}>{t('提交任务')}</Button></div>
      </Card>}
    </div>
  </Modal>
}
