import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router'
import {
  Download,
  Trash2,
  RefreshCw,
  CheckCircle2,
  XCircle,
  ToggleLeft,
  ToggleRight,
  Loader2,
  AlertCircle,
  X,
  Plus,
  Unlink,
  CloudDownload,
} from 'lucide-react'
import {
  getImages,
  getStorageInfo,
  downloadImage,
  cancelImageDownload,
  deleteImage,
  toggleImage,
  createCustomKVMImage,
  removeCustomKVMImage,
  ImageInfo,
  StorageInfo,
  CustomKVMImageInput,
} from '../services/api'
import { useDialog } from '../components/Dialog'
import { useLanguage } from '../contexts/LanguageContext'

export default function ImageManagement() {
  const dialog = useDialog()
  const { t } = useLanguage()
  const navigate = useNavigate()
  const [images, setImages] = useState<ImageInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [actionLoading, setActionLoading] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [storageInfo, setStorageInfo] = useState<StorageInfo | null>(null)
  const [storageLoading, setStorageLoading] = useState(true)
  const [customModalOpen, setCustomModalOpen] = useState<'lxc' | 'kvm' | null>(null)

  const fetchImages = useCallback(async () => {
    try {
      const res = await getImages()
      setImages(res.data.data || [])
      setError('')
    } catch {
      setError('获取镜像列表失败')
    } finally {
      setLoading(false)
    }
  }, [])

  const fetchStorage = useCallback(async () => {
    setStorageLoading(true)
    try {
      const res = await getStorageInfo()
      setStorageInfo(res.data.data || null)
    } catch {
      setStorageInfo(null)
    } finally {
      setStorageLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchImages()
    fetchStorage()
  }, [fetchImages, fetchStorage])

  useEffect(() => {
    const hasDownloads = images.some((img) => img.downloading)
    const interval = setInterval(fetchImages, hasDownloads ? 1500 : 5000)
    return () => clearInterval(interval)
  }, [fetchImages, images])

  const handleDownload = async (templateId: string) => {
    setActionLoading(templateId)
    setError('')
    try {
      await downloadImage(templateId)
      await fetchImages()
    } catch (err: unknown) {
      setError(apiErrorMessage(err, '下载失败'))
    } finally {
      setActionLoading(null)
    }
  }

  const handleCancelDownload = async (templateId: string) => {
    setActionLoading(templateId)
    setError('')
    try {
      await cancelImageDownload(templateId)
      await fetchImages()
    } catch (err: unknown) {
      setError(apiErrorMessage(err, '取消失败'))
    } finally {
      setActionLoading(null)
    }
  }

  const handleDelete = async (templateId: string) => {
    if (!(await dialog.confirm('删除镜像', '确定要删除该镜像缓存吗？删除后需要重新下载才能使用。'))) return
    setActionLoading(templateId)
    setError('')
    try {
      await deleteImage(templateId)
      await fetchImages()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : '删除失败'
      setError(msg)
    } finally {
      setActionLoading(null)
    }
  }

  const handleToggle = async (templateId: string, enabled: boolean) => {
    setActionLoading(templateId)
    setError('')
    try {
      await toggleImage(templateId, !enabled)
      await fetchImages()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : '操作失败'
      setError(msg)
    } finally {
      setActionLoading(null)
    }
  }

  const handleRemoveCustom = async (templateId: string) => {
    if (!(await dialog.confirm('移除第三方镜像', '确定移除该镜像源和已下载的缓存吗？正在使用该镜像的虚拟机不会允许移除。'))) return
    setActionLoading(templateId)
    setError('')
    try {
      await removeCustomKVMImage(templateId)
      await fetchImages()
      dialog.alert('完成', '第三方镜像已移除')
    } catch (err: unknown) {
      dialog.alert('失败', apiErrorMessage(err, '移除第三方镜像失败'))
    } finally {
      setActionLoading(null)
    }
  }

  const handleCustomCreated = async (payload: CustomKVMImageInput) => {
    const response = await createCustomKVMImage(payload)
    const image = response.data.data
    if (!image) throw new Error('镜像源保存成功，但服务器没有返回镜像 ID')
    try {
      await downloadImage(image.id)
      dialog.alert('完成', '第三方镜像已添加，下载任务已启动')
    } catch (err: unknown) {
      dialog.alert('提示', `镜像源已保存，但下载未启动：${apiErrorMessage(err, '请在列表中重试')}`)
    }
    await fetchImages()
  }

  const downloadedCount = images.filter((img) => img.downloaded).length
  const lxcImages = images.filter((img) => img.type === 'lxc')
  const kvmImages = images.filter((img) => img.type === 'kvm')
  const imageStorageReady = (storageInfo?.pools || []).some((pool) =>
    pool.enabled !== false && pool.available !== false && (pool.content_types || []).includes('images')
  )

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-black"></div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-black">镜像管理</h1>
          <p className="text-sm text-gray-500 mt-1">
            管理 LXC / KVM 系统镜像，下载后的镜像才能用于创建容器/虚拟机。
            已下载 {downloadedCount}/{images.length}
          </p>
        </div>
        <button
          onClick={() => { fetchImages(); fetchStorage() }}
          className="flex items-center gap-1.5 px-3 py-1.5 border border-gray-300 text-gray-700 rounded-md hover:bg-gray-50 transition-colors text-xs font-medium"
        >
          <RefreshCw className="w-3.5 h-3.5" />
          刷新
        </button>
      </div>

      {error && (
        <div className="flex items-center gap-2 bg-red-50 border border-red-200 rounded-lg px-4 py-3 text-sm text-red-700">
          <AlertCircle className="w-4 h-4 flex-shrink-0" />
          {error}
        </div>
      )}

      {storageLoading && (
        <div className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-sm text-gray-600">
          <Loader2 className="h-4 w-4 shrink-0 animate-spin" />
          正在检查存储配置...
        </div>
      )}

      {!storageLoading && !imageStorageReady && (
        <div className="flex items-center justify-between gap-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
          <div className="flex items-center gap-2">
            <AlertCircle className="h-4 w-4 shrink-0" />
            尚未开启镜像缓存存储，无法下载新镜像。
          </div>
          <button onClick={() => navigate('/storage')} className="shrink-0 rounded-md border border-amber-300 bg-white px-3 py-1.5 text-xs font-medium hover:bg-amber-100">
            去开启
          </button>
        </div>
      )}

      <ImageTable
        title="LXC 容器镜像"
        images={lxcImages}
        actionLoading={actionLoading}
        downloadedCount={lxcImages.filter((img) => img.downloaded).length}
        totalCount={lxcImages.length}
        onDownload={handleDownload}
        onCancelDownload={handleCancelDownload}
        onDelete={handleDelete}
        onToggle={handleToggle}
        onRemoveCustom={handleRemoveCustom}
        storageReady={imageStorageReady}
        storageLoading={storageLoading}
        headerAction={(
          <button
            type="button"
            onClick={() => setCustomModalOpen('lxc')}
            disabled={storageLoading || !imageStorageReady}
            title={storageLoading ? t('正在检查存储配置...') : imageStorageReady ? t('下载第三方 LXC 镜像') : t('请先在存储管理中开启镜像缓存存储')}
            className="inline-flex items-center gap-1.5 rounded-md bg-black px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-gray-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-white dark:text-black dark:hover:bg-gray-200"
          >
            <Plus className="h-3.5 w-3.5" />
            {t('第三方镜像')}
          </button>
        )}
      />

      {kvmImages.length > 0 && (
        <ImageTable
          title="KVM 虚拟机镜像"
          images={kvmImages}
          actionLoading={actionLoading}
          downloadedCount={kvmImages.filter((img) => img.downloaded).length}
          totalCount={kvmImages.length}
          onDownload={handleDownload}
          onCancelDownload={handleCancelDownload}
          onDelete={handleDelete}
          onToggle={handleToggle}
          onRemoveCustom={handleRemoveCustom}
          storageReady={imageStorageReady}
          storageLoading={storageLoading}
          headerAction={(
            <button
              type="button"
              onClick={() => setCustomModalOpen('kvm')}
              disabled={storageLoading || !imageStorageReady}
              title={storageLoading ? t('正在检查存储配置...') : imageStorageReady ? t('下载第三方 KVM 镜像') : t('请先在存储管理中开启镜像缓存存储')}
              className="inline-flex items-center gap-1.5 rounded-md bg-black px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-gray-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-white dark:text-black dark:hover:bg-gray-200"
            >
              <Plus className="h-3.5 w-3.5" />
              {t('第三方镜像')}
            </button>
          )}
        />
      )}
      {customModalOpen !== null && (
        <CustomKVMImageModal
          virtualization={customModalOpen}
          arch={(customModalOpen === 'lxc' ? lxcImages[0]?.arch : kvmImages[0]?.arch) || 'amd64'}
          onClose={() => setCustomModalOpen(null)}
          onSubmit={handleCustomCreated}
        />
      )}
    </div>
  )
}

const emptyCustomImage = (arch: string, virtualization: 'lxc' | 'kvm'): CustomKVMImageInput => ({
  type: virtualization,
  name: '',
  description: '',
  distro: '',
  release: '',
  arch,
  url: '',
  provisioner: virtualization === 'lxc' ? 'lxc-rootfs' : 'linux-cloud-init',
  sha256: '',
})

function CustomKVMImageModal({
  virtualization,
  arch,
  onClose,
  onSubmit,
}: {
  virtualization: 'lxc' | 'kvm'
  arch: string
  onClose: () => void
  onSubmit: (payload: CustomKVMImageInput) => Promise<void>
}) {
  const { t } = useLanguage()
  const [form, setForm] = useState<CustomKVMImageInput>(() => emptyCustomImage(arch, virtualization))
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState('')
  const windows = virtualization === 'kvm' && form.provisioner !== 'linux-cloud-init'

  useEffect(() => {
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !submitting) onClose()
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [onClose, submitting])

  const updateProvisioner = (provisioner: 'linux-cloud-init' | 'windows-10' | 'windows-11') => {
    setForm((current) => ({
      ...current,
      provisioner,
      distro: provisioner === 'linux-cloud-init' ? (current.distro === 'windows' ? '' : current.distro) : 'windows',
      release: provisioner === 'windows-10' ? '10' : provisioner === 'windows-11' ? '11' : (current.distro === 'windows' ? '' : current.release),
    }))
  }

  const submit = async () => {
    if (!form.name.trim() || !form.distro.trim() || !form.release.trim() || !form.url.trim()) {
      setFormError(t('请填写名称、发行版、版本和下载地址'))
      return
    }
    if (form.sha256 && !/^[a-fA-F0-9]{64}$/.test(form.sha256.trim())) {
      setFormError(t('SHA-256 必须是 64 位十六进制字符串'))
      return
    }
    setSubmitting(true)
    setFormError('')
    try {
      await onSubmit({
        ...form,
        name: form.name.trim(),
        description: form.description.trim(),
        distro: form.distro.trim().toLowerCase(),
        release: form.release.trim().toLowerCase(),
        url: form.url.trim(),
        sha256: form.sha256?.trim().toLowerCase(),
      })
      onClose()
    } catch (err: unknown) {
      setFormError(apiErrorMessage(err, t('添加第三方镜像失败')))
    } finally {
      setSubmitting(false)
    }
  }

  const inputClass = 'mt-1.5 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-black outline-none focus:border-black focus:ring-2 focus:ring-black/10 dark:border-gray-700 dark:bg-gray-950 dark:text-white dark:focus:border-white dark:focus:ring-white/10'

  return (
    <div className="fixed inset-0 z-[90] flex items-center justify-center bg-black/55 p-4 dark:bg-black/75">
      <div className="w-full max-w-2xl overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl dark:border-gray-700 dark:bg-gray-900">
        <div className="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-700">
          <div>
            <h3 className="text-base font-semibold text-gray-900 dark:text-white">{t(virtualization === 'lxc' ? '下载第三方 LXC 镜像' : '下载第三方 KVM 镜像')}</h3>
            <p className="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {t(virtualization === 'lxc' ? '支持 tar、tar.gz、tar.xz、tar.zst 格式的 Linux rootfs' : '镜像格式必须与所选无人值守安装模板匹配')}
            </p>
          </div>
          <button type="button" onClick={onClose} disabled={submitting} className="rounded p-1.5 text-gray-400 hover:bg-gray-100 hover:text-black disabled:opacity-50 dark:hover:bg-gray-800 dark:hover:text-white" title={t('关闭')}>
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="max-h-[72vh] space-y-5 overflow-y-auto px-5 py-4">
          {virtualization === 'kvm' && <div>
            <label className="text-sm font-medium text-gray-700 dark:text-gray-200">{t('无人值守安装模板')}</label>
            <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-3">
              {([
                ['linux-cloud-init', 'Linux cloud-init', 'QCOW2 / IMG'],
                ['windows-10', 'Windows 10', '安装 ISO'],
                ['windows-11', 'Windows 11', '安装 ISO'],
              ] as const).map(([value, label, hint]) => (
                <button
                  key={value}
                  type="button"
                  onClick={() => updateProvisioner(value)}
                  disabled={arch !== 'amd64' && value !== 'linux-cloud-init'}
                  className={`rounded-md border px-3 py-2 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${
                    form.provisioner === value
                      ? 'border-black bg-gray-50 dark:border-white dark:bg-gray-800'
                      : 'border-gray-200 hover:border-gray-400 dark:border-gray-700 dark:hover:border-gray-500'
                  }`}
                >
                  <span className="block text-sm font-medium text-gray-900 dark:text-white">{label}</span>
                  <span className="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">{t(hint)}</span>
                </button>
              ))}
            </div>
          </div>}

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <label className="text-sm text-gray-700 dark:text-gray-200">
              {t('镜像名称')}
              <input className={inputClass} value={form.name} maxLength={100} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder={virtualization === 'lxc' ? 'Alpine Custom Rootfs' : windows ? 'Windows 11 Custom' : 'Ubuntu Custom Cloud'} />
            </label>
            <label className="text-sm text-gray-700 dark:text-gray-200">
              {t('架构')}
              <select className={inputClass} value={form.arch} disabled>
                <option value={arch}>{arch}</option>
              </select>
            </label>
            <label className="text-sm text-gray-700 dark:text-gray-200">
              {t('发行版')}
              <input className={inputClass} value={form.distro} disabled={windows} maxLength={64} onChange={(event) => setForm({ ...form, distro: event.target.value })} placeholder="ubuntu" />
            </label>
            <label className="text-sm text-gray-700 dark:text-gray-200">
              {t('版本 / 代号')}
              <input className={inputClass} value={form.release} disabled={windows} maxLength={64} onChange={(event) => setForm({ ...form, release: event.target.value })} placeholder="noble" />
            </label>
          </div>

          <label className="block text-sm text-gray-700 dark:text-gray-200">
            {t('备注')}
            <textarea className={`${inputClass} min-h-20 resize-y`} value={form.description} maxLength={500} onChange={(event) => setForm({ ...form, description: event.target.value })} placeholder={t('镜像来源、版本或用途')} />
          </label>

          <label className="block text-sm text-gray-700 dark:text-gray-200">
            {t('镜像下载地址')}
            <input className={`${inputClass} font-mono text-xs`} value={form.url} onChange={(event) => setForm({ ...form, url: event.target.value })} placeholder={virtualization === 'lxc' ? 'https://example.com/rootfs.tar.xz' : windows ? 'https://example.com/windows.iso' : 'https://example.com/image.qcow2'} />
          </label>

          <label className="block text-sm text-gray-700 dark:text-gray-200">
            SHA-256 <span className="text-xs text-gray-400">({t('可选')})</span>
            <input className={`${inputClass} font-mono text-xs`} value={form.sha256 || ''} maxLength={64} onChange={(event) => setForm({ ...form, sha256: event.target.value })} placeholder={t('用于校验下载文件完整性')} />
          </label>

          {formError && (
            <div className="flex items-start gap-2 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
              {formError}
            </div>
          )}
        </div>

        <div className="flex justify-end gap-2 border-t border-gray-200 bg-gray-50 px-5 py-3 dark:border-gray-700 dark:bg-gray-800">
          <button type="button" onClick={onClose} disabled={submitting} className="rounded-md px-4 py-2 text-sm text-gray-700 hover:bg-gray-200 disabled:opacity-50 dark:text-gray-300 dark:hover:bg-gray-700">{t('取消')}</button>
          <button type="button" onClick={submit} disabled={submitting} className="inline-flex items-center gap-2 rounded-md bg-black px-4 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50 dark:bg-white dark:text-black dark:hover:bg-gray-200">
            {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <CloudDownload className="h-4 w-4" />}
            {submitting ? t('正在添加...') : t('添加并下载')}
          </button>
        </div>
      </div>
    </div>
  )
}

function ImageTable({
  title,
  images,
  actionLoading,
  downloadedCount,
  totalCount,
  onDownload,
  onCancelDownload,
  onDelete,
  onToggle,
  onRemoveCustom,
  storageReady,
  storageLoading,
  headerAction,
}: {
  title: string
  images: ImageInfo[]
  actionLoading: string | null
  downloadedCount: number
  totalCount: number
  onDownload: (id: string) => void
  onCancelDownload: (id: string) => void
  onDelete: (id: string) => void
  onToggle: (id: string, enabled: boolean) => void
  onRemoveCustom: (id: string) => void
  storageReady: boolean
  storageLoading: boolean
  headerAction?: ReactNode
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-semibold text-gray-800 dark:text-gray-100">{title}</h2>
          <span className="text-xs text-gray-400">
            已下载 {downloadedCount}/{totalCount}
          </span>
        </div>
        {headerAction}
      </div>
      <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 bg-gray-50">
                <th className="text-left px-4 py-3 text-[11px] font-medium text-gray-500 uppercase whitespace-nowrap">
                  系统镜像
                </th>
                <th className="text-left px-4 py-3 text-[11px] font-medium text-gray-500 uppercase whitespace-nowrap">
                  发行版
                </th>
                <th className="text-left px-4 py-3 text-[11px] font-medium text-gray-500 uppercase whitespace-nowrap">
                  架构
                </th>
                <th className="text-left px-4 py-3 text-[11px] font-medium text-gray-500 uppercase whitespace-nowrap">
                  大小
                </th>
                <th className="text-left px-4 py-3 text-[11px] font-medium text-gray-500 uppercase whitespace-nowrap">
                  状态
                </th>
                <th className="text-right px-4 py-3 text-[11px] font-medium text-gray-500 uppercase whitespace-nowrap">
                  操作
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {images.map((img) => {
                const isBusy = actionLoading === img.id
                return (
                  <tr key={img.id} className="hover:bg-gray-50 transition-colors">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-3">
                        <span className="w-8 h-8 flex items-center justify-center flex-shrink-0">
                          {getTemplateIcon(img.id, img.distro, img.custom)}
                        </span>
                        <div>
                          <span className="font-medium text-gray-900 text-sm dark:text-gray-100">{img.name}</span>
                          {img.custom && <span className="ml-2 rounded bg-blue-50 px-1.5 py-0.5 text-[10px] font-medium text-blue-700 dark:bg-blue-950 dark:text-blue-300">第三方</span>}
                          <p className="text-[11px] text-gray-400">{img.description}</p>

                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-600 font-mono">
                      {img.distro} {img.release}
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-500 font-mono">
                      {img.arch}
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-600 tabular-nums">
                      {formatSize(img.size_bytes)}
                    </td>
                    <td className="px-4 py-3">
                      <StatusBadge img={img} />
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center justify-end gap-2">
                        {!img.downloaded && !img.downloading && (
                          <button
                            onClick={() => onDownload(img.id)}
                            disabled={isBusy || storageLoading || !storageReady}
                            title={storageLoading ? '正在检查存储配置...' : storageReady ? '下载镜像' : '请先在存储管理中开启镜像缓存存储'}
                            className="inline-flex items-center gap-1.5 px-3 py-1.5 bg-black text-white rounded-md hover:bg-gray-800 transition-colors text-xs font-medium disabled:opacity-50"
                          >
                            {isBusy ? (
                              <Loader2 className="w-3.5 h-3.5 animate-spin" />
                            ) : (
                              <Download className="w-3.5 h-3.5" />
                            )}
                            {isBusy ? '下载中...' : '下载'}
                          </button>
                        )}

                        {img.downloading && (
                          <button
                            onClick={() => onCancelDownload(img.id)}
                            disabled={isBusy}
                            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md border border-red-200 text-red-600 hover:bg-red-50 transition-colors text-xs font-medium disabled:opacity-50"
                            title="取消下载并清理临时文件"
                          >
                            {isBusy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <X className="w-3.5 h-3.5" />}
                            {isBusy ? '取消中...' : '取消'}
                          </button>
                        )}

                        {img.downloaded && !img.downloading && (
                          <>
                            <button
                              onClick={() => onToggle(img.id, img.enabled)}
                              disabled={isBusy || storageLoading || !storageReady}
                              title={storageLoading ? '正在检查存储配置...' : storageReady ? (img.enabled ? '禁用镜像' : '启用镜像') : '请先在存储管理中开启镜像缓存存储'}
                              className={`inline-flex items-center gap-1 px-2.5 py-1.5 rounded-md text-xs font-medium transition-colors disabled:opacity-50 ${
                                img.enabled
                                  ? 'bg-emerald-50 text-emerald-700 border border-emerald-200 hover:bg-emerald-100'
                                  : 'bg-gray-50 text-gray-500 border border-gray-200 hover:bg-gray-100'
                              }`}
                            >
                              {img.enabled ? <ToggleRight className="w-3.5 h-3.5" /> : <ToggleLeft className="w-3.5 h-3.5" />}
                              {img.enabled ? '启用' : '禁用'}
                            </button>
                            <button
                              onClick={() => onDelete(img.id)}
                              disabled={isBusy}
                              className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-md border border-red-200 text-red-600 hover:bg-red-50 transition-colors text-xs font-medium disabled:opacity-50"
                              title="删除镜像缓存"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </>
                        )}
                        {img.custom && !img.downloading && (
                          <button
                            onClick={() => onRemoveCustom(img.id)}
                            disabled={isBusy}
                            className="inline-flex items-center rounded-md border border-gray-200 p-1.5 text-gray-500 hover:border-red-200 hover:bg-red-50 hover:text-red-600 disabled:opacity-50 dark:border-gray-700 dark:text-gray-400 dark:hover:border-red-900 dark:hover:bg-red-950 dark:hover:text-red-300"
                            title="移除第三方镜像源和缓存"
                          >
                            <Unlink className="h-3.5 w-3.5" />
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
      </div>
    </div>
  )
}

function StatusBadge({ img }: { img: ImageInfo }) {
  if (img.downloading) {
    const progress = Math.max(0, Math.min(100, img.progress || 0))
    const showProgress = img.stage === 'downloading' && (progress > 0 || img.downloaded_bytes > 0)
    return (
      <div className="inline-flex flex-col gap-1">
        <span
          className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-amber-50 text-amber-700"
          title={downloadStatusTitle(img)}
        >
          <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" />
          {downloadStatusLabel(img)}
        </span>
        {showProgress && (
          <span className="block h-1 w-24 overflow-hidden rounded-full bg-amber-100">
            <span
              className={`block h-full rounded-full bg-amber-500 transition-all ${progress <= 0 ? 'animate-pulse' : ''}`}
              style={{ width: progress > 0 ? `${progress}%` : '35%' }}
            />
          </span>
        )}
      </div>
    )
  }
  if (img.error) {
    return (
      <span
        className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-red-50 text-red-600"
        title={img.error}
      >
        <AlertCircle className="w-3 h-3" />
        下载失败
      </span>
    )
  }
  if (img.downloaded && img.enabled) {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-emerald-50 text-emerald-700">
        <CheckCircle2 className="w-3 h-3" />
        可用
      </span>
    )
  }
  if (img.downloaded && !img.enabled) {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-gray-100 text-gray-500">
        <XCircle className="w-3 h-3" />
        已禁用
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-red-50 text-red-600">
      <XCircle className="w-3 h-3" />
      未下载
    </span>
  )
}

function downloadStatusLabel(img: ImageInfo) {
  if (img.stage === 'canceling') return '取消中'
  if (img.stage === 'converting') return '转换中'
  if (img.stage === 'validating') return '校验中'
  if (img.stage === 'lxc-create') return '下载中'
  if (img.progress > 0) return `下载中 ${Math.min(100, img.progress)}%`
  if (img.downloaded_bytes > 0) return `下载中 · ${formatSize(img.downloaded_bytes)}`
  return '下载中'
}

function downloadStatusTitle(img: ImageInfo) {
  const parts = [downloadStatusLabel(img)]
  if (img.stage) parts.push(`阶段：${img.stage}`)
  if (img.downloaded_bytes > 0 || img.total_bytes > 0) {
    parts.push(`${formatSize(img.downloaded_bytes)} / ${formatSize(img.total_bytes)}`)
  }
  return parts.join('，')
}

function isWindowsImage(img: ImageInfo) {
  return img.distro === 'windows' || img.id.toLowerCase().includes('windows')
}

function getTemplateIcon(id: string, distro = '', custom = false): ReactNode {
  const size = 'w-5 h-5'
  id = (custom && distro ? distro : id).toLowerCase()
  id = id.startsWith('kvm-') ? id.slice(4) : id
  if (id.startsWith('debian')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M935.473 375.359a558.602 558.602 0 0 0-22.351-114.655l13.308 4.436c-35.66-81.385-90.086-163.623-153.556-199.282-8.701-5.118-35.147 4.948-26.616-12.113s-37.536-8.19-56.816-4.778c-26.275 4.266-30.028-29.175-75.071-35.83-25.593-3.582-32.247 18.427-44.702 13.309-23.545-9.384-20.816-27.64-57.669-9.384-18.427 9.042 11.602-26.105-49.138-4.607L457.744 0C349.23 41.63 318.69 76.266 288.15 79.337c-6.996 0-34.124 32.759-53.574 53.062-17.062 17.062-26.275 36.512-49.138 39.583l-17.062 70.636A136.494 136.494 0 0 0 119.41 339.7a66.711 66.711 0 0 1 4.436-52.892c-17.062 6.825-45.896 17.062-29.687 96.91 12.796 63.13-5.29 135.13 10.066 204.742 4.777 20.986 0 40.095 6.142 51.185 107.66 235.794 208.836 392.08 472.44 384.06l4.436-8.872c-28.152-6.825-55.11-17.062-111.584-30.711-18.597-4.436-23.033-34.124-40.265-44.19-9.384-5.46-28.323-4.095-37.195-9.896s4.266-21.668-19.962-14.332c-8.531 2.56-13.82-10.92-20.133-17.061s0-23.716-23.375-24.74-18.426-29.687-19.791-44.702c-12.114 1.536-1.195-1.535-13.308 4.436a63.64 63.64 0 0 1-23.887-31.735c-10.237-48.967-10.578-21.497-15.014-32.417a322.297 322.297 0 0 0-19.28-42.142l26.787 8.872h4.436l4.436-13.309-26.616-8.701h31.223c-7.678 13.99 2.047 5.29-13.479 8.872v13.308l22.35-8.872v-13.308c-20.644-10.237-28.663-13.308-49.137-22.01l9.043 8.872v4.436h-49.138c-22.01-14.843-13.99-31.734-17.915-53.062 17.062 0 9.213 6.655 17.062-13.137l-17.062 8.872 13.308-33.953-13.308 13.138c-29.176-38.73-16.209-97.764-11.943-152.02A180.684 180.684 0 0 1 211.2 372.97c8.872-10.067 5.119-25.251 5.46-37.195l31.223-26.445H265.8c7.678 17.061 4.777 5.46 0 22.01l8.872 4.435c7.166-8.701 6.142-5.971 8.872-22.01-10.066-10.578-6.995-9.895-26.616-13.308A119.432 119.432 0 0 1 368.51 243.13l4.436-13.308-17.915 9.043-4.436-13.138a109.536 109.536 0 0 1 76.095-27.128c6.313 0 6.996-17.062 12.797-19.45 161.574-60.57 309.33 9.383 371.093 147.413a324.173 324.173 0 0 1 8.19 34.123c17.061 56.987-7.167 121.48 9.725 155.604-7.849 36-36.683 13.82-40.266 30.881-8.531 41.29-14.844 59.717-40.778 78.826a196.38 196.38 0 0 1-30.711 22.35 84.285 84.285 0 0 0 22.35-39.753c-106.294 111.584-262.58 63.981-290.049-105.954a101.176 101.176 0 0 1 35.147-93.157c92.987-87.527 150.144-52.38 205.765-20.474l-8.872-30.711c-32.93-24.398-17.062-19.792-9.043-57.328v-4.436l-17.915-13.137c2.56 10.066 1.024 5.289 9.043 17.061-4.436 16.039 0 9.043-8.872 17.062-15.014 9.725-23.716 7.337-44.702 4.436l4.436-13.308-13.308-13.308c0 11.773-4.095 2.73 0 17.062-126.086 9.896-218.05 80.02-178.636 260.191a220.608 220.608 0 0 0 8.872 44.19l-8.872 8.702-4.436-26.446h-13.48l-4.435 13.308c-12.626-25.763-0.853 10.75 40.265 52.892a149.29 149.29 0 0 0 12.797 12.625c47.773 34.124 113.29 81.385 201.328 49.138h9.043v-4.436l-102.37-13.308-4.436-8.701c106.806 24.74 176.93-8.531 236.646-48.456 13.138-17.062 11.431-24.057 22.18-9.043 19.28-17.061 3.925-26.786 13.48-44.019 6.483-11.772 32.587-17.062 44.7-35.318l40.096-136.494h-17.062c3.071-14.332 22.522-34.123-4.436-48.455-2.559-1.536 9.043-1.365 8.872-4.266a145.537 145.537 0 0 0-22.18-66.37c33.1 21.669 36.342 68.247 53.574 105.783v8.872h4.436V375.36zM453.308 595.455l-9.555-26.446 62.446 57.328zM146.196 211.736l-23.204-4.436v39.754c16.72-10.578 18.939-10.407 22.35-35.318z m574.981 176.419a57.498 57.498 0 0 0-17.062 44.19l13.48 8.701a37.877 37.877 0 0 0 4.435-52.891zM868.42 555.872c26.275-11.602 54.598-58.01 35.83-97.081l-35.83 96.91z m-174.03-79.508c-15.697 11.773-19.791 13.308-22.35 39.754l13.307 8.872 17.915-8.872a60.228 60.228 0 0 0 4.436-48.455c-8.36 13.478-2.559 20.644-13.308 8.701z m-67.053 79.508c15.868-10.92 11.944-14.844 17.915-22.18v-4.778a292.097 292.097 0 0 1-62.446 0c-13.137-13.99-13.308-29.346-31.223-39.583 17.062 35.147 3.242 38.218 31.223 61.764a158.162 158.162 0 0 0 40.095 4.436c1.536 0-6.824-1.024 4.436 0zM207.79 520.554H194.31l-8.872 8.702c9.555 10.237 5.46 7.166 13.308-4.436L212.225 547l4.436-17.062-8.872-8.701z m17.062 57.328l4.436-8.873c-10.067-8.701 0-3.583-13.308 0l-13.309-17.061 4.436 17.061v8.873h17.062z" fill="#CE0C48"/></svg>
  if (id.startsWith('ubuntu')) return <svg className={size} viewBox="0 0 1024 1024"><circle cx="512" cy="512" r="511" fill="#DD4814"/><path d="M164.532 442.532c-37.676 0-68.2 30.524-68.2 68.2 0 37.656 30.524 68.184 68.2 68.184 37.66 0 68.184-30.528 68.184-68.184 0-37.676-30.524-68.2-68.184-68.2z m486.86 309.912c-32.612 18.84-43.8 60.52-24.96 93.116 18.82 32.616 60.5 43.796 93.116 24.96 32.612-18.82 43.796-60.5 24.96-93.12-18.82-32.592-60.524-43.772-93.116-24.956z m-338.744-241.712c0-67.384 33.472-126.92 84.684-162.968L347.48 264.268c-59.656 39.88-104.048 100.816-122.496 172.188 21.528 17.56 35.304 44.3 35.304 74.272 0 29.956-13.776 56.696-35.304 74.26C243.408 656.376 287.8 717.32 347.48 757.2l49.852-83.52c-51.212-36.028-84.684-95.56-84.684-162.948z m199.168-199.188c104.052 0 189.42 79.776 198.38 181.52l97.16-1.432c-4.776-75.112-37.592-142.544-88.008-192.128-25.928 9.796-55.88 8.296-81.76-6.624-25.932-14.964-42.192-40.208-46.636-67.608a297.04 297.04 0 0 0-79.14-10.76 295.148 295.148 0 0 0-131.276 30.652l47.38 84.908a198.384 198.384 0 0 1 83.9-18.528z m0 398.36a198.404 198.404 0 0 1-83.896-18.528l-47.38 84.9a294.848 294.848 0 0 0 131.28 30.684 296.16 296.16 0 0 0 79.136-10.788c4.444-27.4 20.708-52.62 46.632-67.608 25.904-14.948 55.836-16.42 81.76-6.624 50.42-49.584 83.232-117.016 88.016-192.128l-97.188-1.432c-8.94 101.772-94.304 181.52-198.36 181.52z m139.552-440.924c32.616 18.832 74.3 7.68 93.116-24.936 18.84-32.616 7.68-74.3-24.936-93.14-32.616-18.816-74.296-7.64-93.14 24.976-18.812 32.6-7.632 74.28 24.96 93.1z" fill="#FFF"/></svg>
  if (id.startsWith('alpine')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M255.914667 68.565333L0 512l255.914667 443.434667h512.170666L1024 512 768.085333 68.565333H255.914667zM425.173333 303.786667L540.16 422.4l68.181333 68.053333 0.085334-0.085333 102.826666 100.821333c-8.533333 5.973333-16.469333 10.752-24.021333 14.677334a160.256 160.256 0 0 1-21.162667 9.258666 115.285333 115.285333 0 0 1-18.133333 4.736c-5.589333 0.981333-10.666667 1.450667-15.274667 1.450667-5.546667 0-10.325333-0.597333-14.421333-1.450667a56.192 56.192 0 0 1-10.24-3.072 40.533333 40.533333 0 0 1-8.533333-4.821333l-45.312-46.592-129.664-129.749333-46.933334 44.928-130.986666 131.072a41.557333 41.557333 0 0 1-8.533334 4.736 54.357333 54.357333 0 0 1-10.112 3.114666 70.826667 70.826667 0 0 1-14.421333 1.408c-4.608 0-9.685333-0.384-15.274667-1.322666a115.2 115.2 0 0 1-18.133333-4.864 159.914667 159.914667 0 0 1-21.162667-9.258667 223.061333 223.061333 0 0 1-24.021333-14.634667L425.173333 303.786667z m201.386667 33.493333l195.370667 196.181333 58.965333 57.728a223.573333 223.573333 0 0 1-24.064 14.677334 159.146667 159.146667 0 0 1-21.077333 9.258666 115.072 115.072 0 0 1-18.176 4.736c-5.546667 0.981333-10.709333 1.450667-15.36 1.450667-5.504 0-10.282667-0.597333-14.378667-1.450667a54.826667 54.826667 0 0 1-16.426667-6.229333 10.197333 10.197333 0 0 1-2.261333-1.706667l-52.565333-51.968-90.069334-90.069333-14.250666 14.250667L545.706667 418.133333l80.896-80.938666z m-254.549333 175.786667v107.904a90.794667 90.794667 0 0 1-15.189334-1.493334 117.973333 117.973333 0 0 1-18.005333-4.949333 158.208 158.208 0 0 1-20.821333-9.130667 222.592 222.592 0 0 1-23.68-14.506666l77.653333-77.866667z" fill="#0D597F"/></svg>
  if (id.startsWith('centos')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M153.650377 358.349623v112.005247h-3.694326v-108.310921l3.694326-3.694326z" fill="#932279"/><path d="M453.058708 512l-29.554608 29.554608H137.86553v108.310922L0 512l137.86553-137.86553v108.310922h285.63857l29.554608 29.554608zM738.529354 226.529354L553.64513 411.413578V149.956051h108.310921l3.694326 3.694326 72.878977 72.878977z" fill="#932279"/><path d="M649.86553 137.86553h-108.310922v285.63857l-29.554608 29.554608-29.554608-29.554608V137.86553h-108.310922L512 0l137.86553 137.86553zM874.043949 553.64513v108.310921l-3.694326 3.694326-72.878977 72.878977-184.884224-184.884224h261.457527z" fill="#EFA724"/><path d="M886.13447 361.036405v13.098065l-6.04526-6.04526-6.045261-6.045261v108.310921h-3.694326v-125.103312l3.694326 3.694326 6.045261 6.045261 6.04526 6.04526z" fill="#262577"/><path d="M886.13447 649.86553v-108.310922H600.4959L570.941292 512l29.554608-29.554608h285.63857v-108.310922l137.86553 137.86553-137.86553 137.86553z" fill="#262577"/><path d="M411.413578 470.35487H149.956051v-108.310921l3.694326-3.694326L226.529354 285.470646 411.413578 470.35487zM470.35487 149.956051V411.413578L285.470646 226.529354l72.878977-72.878977 3.694326-3.694326h108.310921z" fill="#9CCD2A"/><path d="M738.529354 797.470646L553.64513 612.586422v261.457527h108.310921l3.694326-3.694326 72.878977-72.878977z" fill="#EFA724"/><path d="M649.86553 886.13447h-108.310922V600.4959L512 570.941292l-29.554608 29.554608v285.63857h-108.310922l137.86553 137.86553 137.86553-137.86553z" fill="#9CCD2A"/><path d="M470.35487 874.043949V612.586422L285.470646 797.470646l72.878977 72.878977 3.694326 3.694326h108.310921z" fill="#262577"/><path d="M470.35487 428.541817v41.813053h-41.813053L226.529354 268.342407l-76.573303 76.573303V149.956051h194.959659l-76.573303 76.573303 202.012463 202.012463z" fill="#9CCD2A"/><path d="M880.08921 143.91079v224.17842l-6.045261-6.045261v108.310921H612.586422L797.470646 285.470646l72.878977 72.878977v-13.098065l3.694326 3.694326v-4.030174l-76.573303-76.573303-202.012463 202.012463h-41.813053v-41.813053L755.657593 226.529354l-82.618564-82.618564h207.050181z" fill="#932279"/><path d="M666.993768 137.86553l12.090522 12.090521h194.959659v212.087898l6.045261 6.045261 6.04526 6.04526V137.86553z" fill="#FFF"/><path d="M874.043949 679.08429v194.959659H679.08429L755.657593 797.470646 553.64513 595.458183v-41.813053h41.813053L797.470646 755.657593l76.573303-76.573303z" fill="#EFA724"/><path d="M411.413578 553.64513L226.529354 738.529354l-72.878977-72.878977-3.694326-3.694326v-108.310921H411.413578z" fill="#262577"/><path d="M470.35487 595.458183L268.342407 797.470646l76.573303 76.573303H149.956051V679.08429L226.529354 755.657593l202.012463-202.012463h41.813053v41.813053z" fill="#262577"/><path d="M874.043949 344.91571v4.030174l-3.694326-3.694326v13.098065l3.694326 3.694326 6.045261 6.045261 6.04526 6.04526v-16.792391z" fill="#FFF"/></svg>
  if (id.startsWith('archlinux')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M504.149333 7.850667c-44.373333 108.544-70.997333 179.2-120.149333 284.330666 30.037333 32.085333 67.242667 69.290667 127.317333 111.274667-64.512-26.624-108.544-53.248-141.653333-80.896-63.146667 131.413333-161.792 318.464-361.813333 678.229333 157.696-90.794667 279.552-146.773333 393.216-168.277333-4.778667-21.162667-7.509333-43.690667-7.509334-67.584l0.341334-5.12c2.389333-100.693333 54.954667-178.517333 117.077333-173.056s110.592 91.477333 107.861333 192.170667c-0.341333 18.090667-2.389333 36.522667-6.485333 54.272 112.64 21.845333 233.130667 77.824 388.437333 167.594666l-83.968-155.648c-40.96-31.744-83.968-73.386667-171.349333-118.101333 60.074667 15.701333 103.082667 33.792 136.533333 53.930667-265.557333-493.909333-287.061333-559.786667-377.856-773.12z" fill="#1793D1"/></svg>
  if (id.startsWith('fedora')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M512 0C229.344 0 0.224 229.024 0 511.648V907.84a116.384 116.384 0 0 0 116.384 116.128h395.808c282.656-0.128 511.776-229.28 511.776-512 0-282.752-229.248-512-512-512z m196.064 237.952c-16.16 0-22.016-3.104-45.728-3.104a126.848 126.848 0 0 0-126.848 126.624v110.208c0 9.888 8.032 17.92 17.92 17.92h83.328c31.072 0 56.16 24.736 56.16 55.904 0 31.328-25.344 55.968-56.736 55.968h-100.608v127.36a240.32 240.32 0 0 1-240.288 240.288h-1.248a190.944 190.944 0 0 1-53.216-7.52l1.344 0.32c-27.168-7.072-49.376-29.408-49.376-55.296 0-31.328 22.752-54.112 56.736-54.112 16.128 0 22.016 3.072 45.696 3.072a126.848 126.848 0 0 0 126.848-126.624v-110.208a17.92 17.92 0 0 0-17.92-17.888h-83.328a55.808 55.808 0 0 1-56.096-55.904c0-31.328 25.344-55.968 56.736-55.968h100.576v-127.36a240.32 240.32 0 0 1 240.288-240.288c20.128 0 34.432 2.272 53.088 7.136 27.168 7.136 49.408 29.44 49.408 55.296 0 31.36-22.752 54.144-56.736 54.144z" fill="#294172"/></svg>
  if (id.startsWith('rockylinux')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M995.498667 680.362667c18.474667-52.778667 28.501333-109.568 28.501333-168.704C1024 229.077333 794.752 0 512 0S0 229.077333 0 511.658667c0 139.818667 56.106667 266.496 147.114667 358.826666L666.453333 351.530667l128.213334 128.170666 200.832 200.704z m-93.525334 162.816l-235.52-235.349334-368.896 368.597334A510.506667 510.506667 0 0 0 512 1023.274667c156.16 0 296.106667-69.888 389.973333-180.053334h0.042667z" fill="#10B981"/></svg>
  if (id.startsWith('windows')) return <svg className={size} viewBox="0 0 1024 1024"><path d="M56.888889 227.555556l398.222222-70.542223V512H56.888889V227.555556z m0 625.777777l398.222222 70.542223V568.888889H56.888889v284.444444zM512 147.342222L1024 56.888889v455.111111H512V147.342222z m0 786.204445L1024 1024v-455.111111H512v364.657778z" fill="#16C6FE"/></svg>
  if (custom) return <CloudDownload className={`${size} text-blue-600 dark:text-blue-300`} />
  return null
}

function apiErrorMessage(err: unknown, fallback: string) {
  const error = err as { response?: { data?: { message?: string } }; message?: string }
  return error.response?.data?.message || error.message || fallback
}

function formatSize(bytes: number): string {
  if (bytes <= 0) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}
