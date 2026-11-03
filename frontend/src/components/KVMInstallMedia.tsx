import { useCallback, useEffect, useRef, useState } from 'react'
import { Disc3, Monitor, RefreshCw, Save } from 'lucide-react'
import { getInstallationMedia, updateInstallationMedia, type ContainerIdentifier, type InstallationMediaState } from '../services/api'
import { useLanguage } from '../contexts/LanguageContext'
import { useDialog } from './Dialog'
import { Alert } from './ui/alert'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Modal } from './ui/modal'
import { Label } from './ui/label'
import { NativeSelect } from './ui/native-select'

type MediaDraft = { image_id: string; boot: 'cdrom' | 'disk' }

export default function KVMInstallMedia({ open, onClose, containerId, status, taskBusy, isSubUser, canOpenVNC, onOpenVNC, onManualInstallChange, onUpdated }: {
  open: boolean
  onClose: () => void
  containerId: ContainerIdentifier
  status: string
  taskBusy: boolean
  isSubUser: boolean
  canOpenVNC: boolean
  onOpenVNC: () => void
  onManualInstallChange: (manual: boolean) => void
  onUpdated: () => void | Promise<void>
}) {
  const { t } = useLanguage()
  const dialog = useDialog()
  const [state, setState] = useState<InstallationMediaState | null>(null)
  const [draft, setDraft] = useState<MediaDraft>({ image_id: '', boot: 'disk' })
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const stateRef = useRef<InstallationMediaState | null>(null)
  const dirtyRef = useRef(false)
  const editableRef = useRef(false)
  const savingRef = useRef(false)
  const requestID = useRef(0)

  const acceptState = useCallback((next: InstallationMediaState) => {
    const previous = stateRef.current
    if (!previous || previous.image_id !== next.image_id || previous.boot !== next.boot) {
      if (previous && dirtyRef.current) setNotice(t('安装介质配置已更新，请重新选择。'))
      dirtyRef.current = false
    }
    if (!dirtyRef.current) setDraft({ image_id: next.image_id || '', boot: next.boot })
    stateRef.current = next
    setState(next)
    onManualInstallChange(next.manual_install)
  }, [onManualInstallChange, t])

  const load = useCallback(async () => {
    if (savingRef.current) return
    const request = ++requestID.current
    try {
      const response = await getInstallationMedia(containerId)
      if (request !== requestID.current) return
      if (!response.data.data || !Array.isArray(response.data.data.available)) throw new Error('Missing installation media state')
      acceptState(response.data.data)
      setLoadError('')
    } catch (failure) {
      if (request === requestID.current) setLoadError(apiError(failure, t('无法读取安装介质。')))
    } finally {
      if (request === requestID.current) setLoading(false)
    }
  }, [acceptState, containerId, t])

  useEffect(() => {
    void load()
    const timer = window.setInterval(() => { void load() }, 5000)
    return () => { ++requestID.current; window.clearInterval(timer) }
  }, [load, status, taskBusy])

  useEffect(() => {
    if (open) void load()
  }, [open, load])

  const canEdit = !!state?.supported && !state.running && status === 'stopped' && !taskBusy && !saving && !loadError
  editableRef.current = canEdit
  const validImage = !draft.image_id || !!state?.available.some((image) => image.id === draft.image_id)
  const changed = !!state && (draft.image_id !== state.image_id || draft.boot !== state.boot)
  const canSave = canEdit && validImage && changed && (draft.boot !== 'cdrom' || !!draft.image_id)
  const editDraft = (next: MediaDraft) => {
    dirtyRef.current = true
    setDraft(next)
    setError('')
    setNotice('')
  }

  const save = async (next: MediaDraft) => {
    if (!editableRef.current || savingRef.current) return
    if (next.boot === 'cdrom' && !next.image_id) return
    if (next.image_id && !stateRef.current?.available.some((image) => image.id === next.image_id)) {
      setError(t('请选择当前可用的本地 ISO。'))
      return
    }
    savingRef.current = true
    ++requestID.current
    setSaving(true)
    setError('')
    setNotice('')
    try {
      const response = await updateInstallationMedia(containerId, next)
      if (!response.data.data || !Array.isArray(response.data.data.available)) throw new Error('Missing installation media state')
      dirtyRef.current = false
      acceptState(response.data.data)
      setNotice(t('安装介质与启动顺序已保存，请手动开机。'))
      await onUpdated()
    } catch (failure) {
      setError(apiError(failure, t('安装介质保存失败。')))
    } finally {
      savingRef.current = false
      setSaving(false)
      await load()
    }
  }

  const finishInstallation = async () => {
    if (!canEdit || !state?.image_id) return
    const mounted = state.image_id
    if (!await dialog.confirm(t('完成系统安装'), t('确认已在虚拟机内完成系统安装？此操作将弹出 ISO 并设为磁盘启动，不会自动重启。'))) return
    if (mounted !== stateRef.current?.image_id) {
      setError(t('安装介质配置已更新，请重新选择。'))
      return
    }
    await save({ image_id: '', boot: 'disk' })
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={<span className="flex items-center gap-2"><Disc3 className="h-4 w-4 shrink-0 text-muted-foreground" />{t('KVM 安装介质')}</span>}
      size="2xl"
      dismissible={!saving}
      className="[container-type:inline-size]"
      bodyClassName="min-w-0 space-y-4 p-4 sm:p-5"
      extra={
        <>
          <Button type="button" variant="outline" size="sm" onClick={() => void load()} disabled={loading || saving}><RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />{t('刷新')}</Button>
          <Button type="button" variant="outline" size="sm" onClick={onOpenVNC} disabled={!canOpenVNC || saving}><Monitor className="h-3.5 w-3.5" />{t('打开 VNC')}</Button>
        </>
      }
      footer={state && (
        <div className="flex w-full min-w-0 flex-wrap items-center justify-between gap-2">
          <Button type="button" variant="outline" size="sm" className="h-auto min-h-8 w-full min-w-0 whitespace-normal py-2 text-left [@container(min-width:440px)]:w-auto" disabled={!canEdit || !state.image_id} onClick={() => void finishInstallation()}><Disc3 className="h-3.5 w-3.5 shrink-0" />{t('安装完成，弹出并从磁盘启动')}</Button>
          <Button type="button" size="sm" className="h-auto min-h-8 w-full min-w-0 whitespace-normal py-2 text-left [@container(min-width:440px)]:w-auto" disabled={!canSave} onClick={() => void save(draft)}><Save className="h-3.5 w-3.5 shrink-0" />{saving ? t('保存中…') : t('保存介质与启动顺序')}</Button>
        </div>
      )}
    >
      {loading && !state && <p className="text-xs text-muted-foreground">{t('正在读取安装介质…')}</p>}
      {state && <>
        <div className="flex min-w-0 flex-wrap items-center gap-x-5 gap-y-2 text-xs text-muted-foreground">
          <span className="min-w-0 max-w-full [overflow-wrap:anywhere]">{t('当前介质')}：<span className="font-medium text-foreground">{state.image_name || t('未挂载 ISO')}</span></span>
          <span className="flex max-w-full flex-wrap items-center gap-1">{t('启动顺序')}：<Badge variant="secondary">{state.boot === 'cdrom' ? t('光驱优先') : t('磁盘优先')}</Badge></span>
        </div>
        {state.manual_install && <Alert className="text-xs leading-5">{t(state.image_id ? '此 ISO 将通过 VNC 手动安装系统，磁盘分区、账号和网络需要自行设置，不会注入 SSH 密码或云初始化。' : '此系统保留原有账号和网络配置，请通过 VNC 检查，不会注入新密码。')}</Alert>}
        {(!canEdit && !saving) && <p className="text-xs leading-5 text-muted-foreground">{t('更改安装介质前请先关闭虚拟机并等待当前任务结束；面板不会自动关机或重启。')}</p>}
        <div className="grid min-w-0 gap-4 [@container(min-width:440px)]:grid-cols-[minmax(0,2fr)_minmax(160px,1fr)]">
          <div className="min-w-0 space-y-1.5">
            <Label htmlFor={`installation-iso-${containerId}`}>{t('本地 ISO')}</Label>
            <NativeSelect id={`installation-iso-${containerId}`} wrapperClassName="min-w-0 w-full" value={draft.image_id} disabled={!canEdit} onChange={(event) => editDraft({ image_id: event.target.value, boot: event.target.value ? draft.boot : 'disk' })}>
              <option value="">{t('不挂载 ISO')}</option>
              {state.image_id && !state.available.some((image) => image.id === state.image_id) && <option value={state.image_id} disabled>{t('当前介质不可重新选择')}</option>}
              {state.available.map((image) => <option key={image.id} value={image.id}>{image.name} · {[image.distro, image.release, image.arch].filter(Boolean).join(' ')}</option>)}
            </NativeSelect>
          </div>
          <div className="min-w-0 space-y-1.5">
            <Label htmlFor={`installation-boot-${containerId}`}>{t('启动顺序')}</Label>
            <NativeSelect id={`installation-boot-${containerId}`} wrapperClassName="w-full" value={draft.boot} disabled={!canEdit} onChange={(event) => editDraft({ ...draft, boot: event.target.value as 'cdrom' | 'disk' })}>
              <option value="disk">{t('磁盘优先')}</option>
              <option value="cdrom" disabled={!draft.image_id}>{t('光驱优先')}</option>
            </NativeSelect>
          </div>
        </div>
        {state.available.length === 0 && <p className="text-xs text-muted-foreground">{isSubUser ? t('没有已授权且已下载的本地 ISO，请联系管理员。') : t('没有可用的本地 ISO，请先在镜像管理中下载并启用 ISO。')}</p>}
      </>}
      {(error || loadError) && <Alert variant="destructive" className="text-xs" role="alert">{error || loadError}</Alert>}
      {notice && <Alert className="text-xs" role="status">{notice}</Alert>}
    </Modal>
  )
}

function apiError(error: unknown, fallback: string) {
  return (error as { response?: { data?: { message?: string } } })?.response?.data?.message || fallback
}
