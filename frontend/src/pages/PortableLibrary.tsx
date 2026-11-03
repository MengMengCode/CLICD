import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router'
import { Archive, Boxes, Download, RefreshCw, RotateCcw, Trash2, Upload, X } from 'lucide-react'
import { getStorageInfo, type StoragePool } from '../services/api'
import { backupDownloadTicket, deleteArtifact, formatBytes, importBackup, listBackups, listInstanceTemplates, operationError, restoreArtifact, type PortableArtifact } from '../services/operations'
import { useLanguage } from '../contexts/LanguageContext'
import { useAuth } from '../contexts/AuthContext'
import { useDialog } from '../components/Dialog'
import OperationTask from '../components/OperationTask'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { Modal } from '../components/ui/modal'
import { NativeSelect } from '../components/ui/native-select'
import { Progress } from '../components/ui/progress'

export default function PortableLibrary({ kind }: { kind: 'backup' | 'template' }) {
  const { t } = useLanguage()
  const { isSubUser } = useAuth()
  const dialog = useDialog()
  const [artifacts, setArtifacts] = useState<PortableArtifact[]>([])
  const [pools, setPools] = useState<StoragePool[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<PortableArtifact | null>(null)
  const [name, setName] = useState('')
  const [pool, setPool] = useState('')
  const [ports, setPorts] = useState('2')
  const [copies, setCopies] = useState('1')
  const [taskIds, setTaskIds] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [restoreError, setRestoreError] = useState('')
  const [uploadOpen, setUploadOpen] = useState(false)
  const [file, setFile] = useState<File | null>(null)
  const [percent, setPercent] = useState(0)
  const controller = useRef<AbortController | null>(null)
  const refresh = useCallback(async () => {
    if (isSubUser) return
    try { const response = kind === 'backup' ? await listBackups() : await listInstanceTemplates(); setArtifacts(response.data.data || []); setError('') }
    catch (e) { setError(operationError(e)) }
    finally { setLoading(false) }
  }, [kind, isSubUser])
  useEffect(() => { setSelected(null); setUploadOpen(false); setTaskIds([]); setLoading(true); void refresh(); if (!isSubUser) void getStorageInfo().then(r => setPools((r.data.data?.pools || []).filter(p => p.enabled && p.available))).catch(() => {}); const timer = window.setInterval(() => void refresh(), 6000); return () => window.clearInterval(timer) }, [refresh, isSubUser])
  const copyCount = kind === 'template' ? Number(copies) : 1
  const copyNames = Number.isInteger(copyCount) && copyCount >= 1 && copyCount <= 10
    ? Array.from({ length: copyCount }, (_, index) => copyCount === 1 ? name.trim() : `${name.trim()}-${index + 1}`) : []
  const restore = async () => {
    if (!selected || busy) return
    if (!copyNames.length) { setRestoreError(t('复制数量请输入 1–10 的整数')); return }
    if (copyNames.some(value => !/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/.test(value))) { setRestoreError(t('每个生成的实例名称必须有效且不超过 63 字符')); return }
    const count = Number(ports)
    if (!Number.isInteger(count) || count < 1 || count > 64) { setRestoreError(t('NAT 端口配额请输入 1–64 的整数')); return }
    setBusy(true); setRestoreError('')
    const submitted: string[] = [], failures: string[] = []
    for (const copyName of copyNames) {
      try {
        const id = (await restoreArtifact(kind, selected.id, { name: copyName, storage_pool_id: pool || undefined, assign_nat: true, port_mapping_count: count })).data.data?.task_id
        if (!id) throw new Error(t('服务器未返回任务标识'))
        submitted.push(id)
        setTaskIds([...submitted])
      } catch (e) { failures.push(`${copyName}: ${operationError(e)}`) }
    }
    if (failures.length) setRestoreError(`${t('已提交任务')} ${submitted.length}/${copyNames.length}\n${failures.join('\n')}`)
    setBusy(false)
  }
  const upload = async () => {
    if (!file || busy) return
    if (file.size > 64 * 1024 ** 3 || !file.size) { setRestoreError(t('备份包必须非空且不超过 64 GiB')); return }
    const abort = new AbortController(); controller.current = abort
    setBusy(true); setPercent(0); setRestoreError('')
    try { await importBackup(file, pool, setPercent, abort.signal); setPercent(100); await refresh(); setUploadOpen(false) }
    catch (e) { setRestoreError(abort.signal.aborted ? t('上传已取消') : operationError(e)); await refresh() }
    finally { setBusy(false); controller.current = null }
  }
  const download = async (artifact: PortableArtifact) => {
    setError('')
    try {
      const response = await backupDownloadTicket(artifact.id)
      if (!response.data.data?.url) throw new Error('服务器未返回下载地址')
      const url = new URL(response.data.data.url, window.location.origin)
      if (url.origin !== window.location.origin || !url.pathname.startsWith('/api/backup-download/')) throw new Error(t('下载地址无效'))
      const link = document.createElement('a'); link.href = url.href; link.download = ''; document.body.appendChild(link); link.click(); link.remove()
    } catch (e) { setError(operationError(e)) }
  }
  if (isSubUser) return <Alert>{t('此功能仅管理员可用')}</Alert>
  const filtered = artifacts.filter(item => [item.name, item.source_name, item.runtime].some(value => value.toLowerCase().includes(search.toLowerCase())))
  return <div className="space-y-5">
    <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start"><div><h1 className="flex items-center gap-2 text-xl font-semibold">{kind === 'backup' ? <Archive className="h-5 w-5" /> : <Boxes className="h-5 w-5" />}{t(kind === 'backup' ? '备份与恢复' : '实例模板')}</h1><p className="mt-1 text-sm leading-6 text-muted-foreground">{t(kind === 'backup' ? '备份包含系统盘、数据盘和配置，可导出并在同架构宿主机恢复成新实例。' : '保存已配置的实例环境，通过完整独立副本创建新实例。')}</p></div><div className="flex shrink-0 flex-wrap gap-2"><Button variant="outline" size="sm" onClick={() => void refresh()}><RefreshCw className="h-4 w-4" />{t('刷新')}</Button>{kind === 'backup' && <Button size="sm" onClick={() => { setUploadOpen(true); setFile(null); setPool(''); setRestoreError(''); setPercent(0) }}><Upload className="h-4 w-4" />{t('导入备份')}</Button>}</div></div>
    <Card className="flex flex-wrap items-center justify-between gap-3 p-4"><p className="text-xs leading-5 text-muted-foreground">{t(kind === 'backup' ? '在实例详情中选择“备份、克隆与模板”创建完整备份。' : '在实例详情中选择“备份、克隆与模板”，将系统和应用保存为模板。')}</p><Button asChild size="sm" variant="outline"><Link to="/containers">{t('前往实例管理')}</Link></Button></Card>
    {error && <Alert variant="destructive" role="alert">{error}</Alert>}
    <Input className="sm:max-w-md" value={search} onChange={e => setSearch(e.target.value)} placeholder={t('搜索名称、来源或虚拟化类型')} />
    {loading ? <p className="text-sm text-muted-foreground">{t('正在读取…')}</p> : !filtered.length ? <Card className="p-10 text-center text-sm text-muted-foreground">{t(kind === 'backup' ? '暂无匹配的备份' : '暂无匹配的模板')}</Card> : <div className="grid gap-4 xl:grid-cols-2">{filtered.map(artifact => <Card key={artifact.id} className="min-w-0 space-y-4 p-4">
      <div className="flex flex-wrap items-start justify-between gap-2"><div className="min-w-0"><h2 className="break-all text-sm font-semibold">{artifact.name}</h2><p className="mt-1 break-all text-xs text-muted-foreground">{t('来源')}：{artifact.source_name}</p></div><Badge variant="secondary">{artifact.runtime.toUpperCase()} · {artifact.arch}</Badge></div>
      <div className="grid grid-cols-2 gap-2 text-xs text-muted-foreground"><span>{t('大小')}：{formatBytes(artifact.size_bytes)}</span><span>{t('磁盘数量')}：{artifact.disk_count}</span><span className="col-span-2">{t('创建时间')}：{artifact.created_at}</span></div>
      <div className="flex flex-wrap gap-2"><Button size="sm" onClick={() => { setSelected(artifact); setName(`${artifact.source_name}-copy`); setPool(''); setPorts('2'); setCopies('1'); setTaskIds([]); setRestoreError('') }}><RotateCcw className="h-3.5 w-3.5" />{t(kind === 'backup' ? '恢复为新实例' : '创建实例')}</Button>{kind === 'backup' && <Button size="sm" variant="outline" onClick={() => void download(artifact)}><Download className="h-3.5 w-3.5" />{t('下载')}</Button>}<Button size="sm" variant="destructive" onClick={async () => { if (!await dialog.confirm(t(kind === 'backup' ? '删除备份' : '删除模板'), t('确定删除这个独立副本吗？已有实例不会被删除。'))) return; try { await deleteArtifact(kind, artifact.id); await refresh() } catch (e) { setError(operationError(e)) } }}><Trash2 className="h-3.5 w-3.5" />{t('删除')}</Button></div>
    </Card>)}</div>}
    {selected && <Modal title={t(kind === 'backup' ? '恢复为新实例' : '从模板创建实例')} description={selected.name} onClose={() => setSelected(null)} size="2xl" dismissible={!busy} footer={<><Button variant="outline" disabled={busy} onClick={() => setSelected(null)}>{t(taskIds.length ? '后台继续' : '取消')}</Button>{!taskIds.length && <Button disabled={busy || !name.trim()} onClick={() => void restore()}>{t(busy ? '正在提交…' : '创建独立副本')}</Button>}</>}>
      <div className="space-y-4">{!taskIds.length && <><Alert className="text-xs leading-5">{t('新实例默认保持关机，系统账号保留，UUID、MAC 和 NAT 端口重新分配。启动前请通过控制台检查网络及应用身份。')}</Alert><Label className="block">{t(kind === 'template' && copyCount > 1 ? '实例名称前缀' : '新实例名称')}<Input className="mt-1.5" maxLength={63} value={name} disabled={busy} onChange={e => setName(e.target.value)} /></Label>{kind === 'template' && <><Label className="block">{t('复制数量')}<Input className="mt-1.5" type="number" min={1} max={10} value={copies} disabled={busy} onChange={e => setCopies(e.target.value)} /></Label><div className="space-y-1 rounded-md border p-3 text-xs"><p className="font-medium">{t('实例名称预览')}</p><div className="space-y-1 font-mono text-muted-foreground">{copyNames.map(copyName => <p key={copyName} className="break-all">{copyName || '—'}</p>)}</div></div></>}<Label className="block">{t('目标存储')}<NativeSelect wrapperClassName="mt-1.5 w-full" value={pool} disabled={busy} onChange={e => setPool(e.target.value)}><option value="">{t('自动选择可用存储')}</option>{pools.filter(p => p.content_types.includes(selected.runtime)).map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Label><Label className="block">{t('NAT 端口配额')}<Input className="mt-1.5" type="number" min={1} max={64} value={ports} disabled={busy} onChange={e => setPorts(e.target.value)} /></Label></>}{restoreError && <Alert variant="destructive" role="alert" className="whitespace-pre-wrap break-words">{restoreError}</Alert>}{taskIds.map(id => <OperationTask key={id} id={id} onFinish={() => void refresh()} />)}</div>
    </Modal>}
    {uploadOpen && <Modal title={t('导入完整备份')} description={t('选择 CLICD 导出的自包含备份包，校验成功后可恢复为新实例。')} onClose={() => setUploadOpen(false)} dismissible={!busy} size="2xl" footer={<><Button variant="outline" onClick={() => busy ? controller.current?.abort() : setUploadOpen(false)}>{busy && <X className="h-4 w-4" />}{t(busy ? '取消上传' : '取消')}</Button><Button disabled={busy || !file} onClick={() => void upload()}><Upload className="h-4 w-4" />{t(busy ? '上传并校验中…' : '上传并校验')}</Button></>}>
      <div className="space-y-4"><Label className="block">{t('备份包')}<Input className="mt-1.5 h-auto py-2" type="file" disabled={busy} onChange={e => setFile(e.target.files?.[0] || null)} /></Label>{file && <p className="break-all text-xs text-muted-foreground">{file.name} · {formatBytes(file.size)}</p>}<Label className="block">{t('保存位置')}<NativeSelect wrapperClassName="mt-1.5 w-full" value={pool} disabled={busy} onChange={e => setPool(e.target.value)}><option value="">{t('自动选择可用存储')}</option>{pools.filter(p => p.content_types.includes('backups')).map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Label><p className="text-xs text-muted-foreground">{t('单包及展开后的数据最多 64 GiB。导入会验证格式、磁盘清单和校验和。')}</p>{busy && <div className="space-y-2"><Progress value={percent} /><p className="text-xs text-muted-foreground">{percent >= 99 ? t('传输完成，正在校验备份…') : `${t('上传进度')} ${percent}%`}</p></div>}{restoreError && <Alert variant="destructive" role="alert">{restoreError}</Alert>}</div>
    </Modal>}
  </div>
}
