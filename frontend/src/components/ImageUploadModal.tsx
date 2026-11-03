import { useRef, useState } from 'react'
import { Upload, X, Loader2, FileImage } from 'lucide-react'
import { type StoragePool } from '../services/api'
import { formatBytes, operationError, uploadLocalImage } from '../services/operations'
import { useLanguage } from '../contexts/LanguageContext'
import { Modal } from './ui/modal'
import { Alert } from './ui/alert'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { NativeSelect } from './ui/native-select'
import { Progress } from './ui/progress'

export default function ImageUploadModal({ arch, pools, onClose, onUploaded }: { arch: string; pools: StoragePool[]; onClose: () => void; onUploaded: () => void | Promise<void> }) {
  const { t } = useLanguage()
  const [file, setFile] = useState<File | null>(null)
  const [name, setName] = useState('')
  const [provisioner, setProvisioner] = useState('manual-iso')
  const [distro, setDistro] = useState('')
  const [release, setRelease] = useState('')
  const [pool, setPool] = useState('')
  const [sha, setSHA] = useState('')
  const [busy, setBusy] = useState(false)
  const [percent, setPercent] = useState(0)
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  const isISO = provisioner === 'manual-iso' || provisioner.startsWith('windows-')
  const submit = async () => {
    if (!file || !name.trim()) { setError(t('请选择镜像文件并填写名称')); return }
    if (file.size <= 0 || file.size > 32 * 1024 ** 3) { setError(t('请选择不超过 32 GiB 的有效文件')); return }
    if (sha.trim() && !/^[a-f0-9]{64}$/i.test(sha.trim())) { setError(t('SHA-256 必须是 64 位十六进制字符串')); return }
    setBusy(true); setError(''); setPercent(0)
    const abort = new AbortController(); controller.current = abort
    try {
      await uploadLocalImage(file, { name: name.trim(), provisioner, arch, distro: distro.trim(), release: release.trim(), sha256: sha.trim() }, pool, setPercent, abort.signal)
      setPercent(100); await onUploaded(); onClose()
    } catch (e) {
      setError(abort.signal.aborted ? t('上传已取消') : operationError(e))
      if (abort.signal.aborted) await onUploaded()
    } finally { setBusy(false); controller.current = null }
  }
  return <Modal title={t('上传本地 KVM 镜像')} description={t('支持 ISO 与独立 QCOW2 文件，上传完成后校验并自动启用。')} onClose={onClose} size="2xl" dismissible={!busy} bodyClassName="image-management__form-scroll space-y-4" footer={<>
    <Button variant="outline" onClick={() => busy ? controller.current?.abort() : onClose()}>{busy ? <X className="h-4 w-4" /> : null}{t(busy ? '取消上传' : '取消')}</Button>
    <Button onClick={() => void submit()} disabled={busy || !file || !name.trim()}>{busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}{t(busy ? '上传并校验中…' : '上传并启用')}</Button>
  </>}>
    <Label className="block">{t('镜像文件')}<Input type="file" className="mt-1.5 h-auto py-2" accept=".iso,.qcow2,.img" disabled={busy} onChange={e => { const next = e.target.files?.[0] || null; setFile(next); if (next) { if (!name) setName(next.name.replace(/\.[^.]+$/, '')); setProvisioner(/\.iso$/i.test(next.name) ? 'manual-iso' : 'manual-disk') }; setError('') }} /></Label>
    {file && <p className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground"><FileImage className="h-4 w-4 shrink-0" /><span className="break-all">{file.name} · {formatBytes(file.size)}</span></p>}
    <div className="grid gap-4 sm:grid-cols-2">
      <Label>{t('镜像名称')}<Input className="mt-1.5" value={name} maxLength={100} disabled={busy} onChange={e => setName(e.target.value)} /></Label>
      <Label>{t('安装方式')}<NativeSelect wrapperClassName="mt-1.5 w-full" value={provisioner} disabled={busy} onChange={e => setProvisioner(e.target.value)}>
        <option value="manual-iso">{t('通用 ISO · 手动安装')}</option><option value="manual-disk">{t('QCOW2 · 保留原系统')}</option><option value="linux-cloud-init">{t('QCOW2 · cloud-init 初始化')}</option>
        {arch === 'amd64' && <><option value="windows-10">Windows 10 ISO</option><option value="windows-11">Windows 11 ISO</option></>}
      </NativeSelect></Label>
      <Label>{t('发行版')}<Input className="mt-1.5" value={distro} disabled={busy} onChange={e => setDistro(e.target.value)} placeholder={t('例如 ubuntu，可选')} /></Label>
      <Label>{t('版本')}<Input className="mt-1.5" value={release} disabled={busy} onChange={e => setRelease(e.target.value)} placeholder={t('例如 noble，可选')} /></Label>
    </div>
    <Label className="block">{t('镜像存储位置')}<NativeSelect wrapperClassName="mt-1.5 w-full" value={pool} disabled={busy} onChange={e => setPool(e.target.value)}><option value="">{t('自动选择可用存储')}</option>{pools.filter(p => p.enabled && p.available && p.content_types.includes('images')).map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Label>
    <Label className="block">SHA-256 ({t('可选')})<Input className="mt-1.5 font-mono text-xs" value={sha} maxLength={64} disabled={busy} onChange={e => setSHA(e.target.value)} placeholder={t('上传后核对文件完整性')} /></Label>
    <Alert className="text-xs leading-5">{t(isISO ? 'ISO 用于虚拟光驱。通用 ISO 在 VNC 中安装；Windows 自动安装使用对应模板。' : provisioner === 'manual-disk' ? '预装磁盘保留原系统账号与配置，请通过 VNC 检查网络。QCOW2 必须是无外部依赖的完整磁盘。' : '请选择已安装 cloud-init 的 Linux 磁盘，创建时将注入新的账号和网络配置。')}</Alert>
    {busy && <div className="space-y-2"><Progress value={percent} /><p className="text-xs text-muted-foreground">{percent >= 99 ? t('传输完成，正在验证镜像完整性…') : `${t('上传进度')} ${percent}%`}</p></div>}
    {error && <Alert variant="destructive" role="alert">{error}</Alert>}
  </Modal>
}
