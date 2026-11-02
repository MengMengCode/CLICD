import { Modal } from '../components/ui/modal'
import { Card } from '../components/ui/card'
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '../components/ui/table'
import { Button } from '../components/ui/button'
import { Label } from '../components/ui/label'
import { Checkbox } from '../components/ui/checkbox'
import { Badge } from '../components/ui/badge'
import { NativeSelect } from '../components/ui/native-select'
import { useCallback, useEffect, useState } from 'react'
import { Copy, HardDrive, KeyRound, LogIn, RefreshCw, Save, ScrollText, UserCog } from 'lucide-react'
import { useDialog } from '../components/Dialog'
import { useLanguage } from '../contexts/LanguageContext'
import api, { AuditLog, ImageInfo, LoginLog, getImages, updateSubUserImages } from '../services/api'
import { copyToClipboard } from '../utils/clipboard'

interface SubUserItem {
  id: string
  username: string
  container_names: string[]
  container_uuids: string[]
  allowed_image_ids?: string[]
  image_limit_configured?: boolean
  current_image_ids?: string[]
  container_name: string
  container_uuid: string
  access_code: string
  password?: string
  created_at: string
  last_login: string
  last_login_ip: string
  last_login_ua: string
}

interface AuditLogExt extends AuditLog {
  ip?: string
  user_agent?: string
  success?: boolean
  error?: string
}

export default function SubUserManagement() {
  const dialog = useDialog()
  const { t } = useLanguage()
  const [users, setUsers] = useState<SubUserItem[]>([])
  const [loading, setLoading] = useState(true)
  const [auditLogs, setAuditLogs] = useState<AuditLogExt[] | null>(null)
  const [loginLogs, setLoginLogs] = useState<LoginLog[] | null>(null)
  const [modalTitle, setModalTitle] = useState('')
  const [passwordUser, setPasswordUser] = useState<SubUserItem | null>(null)
  const [imageUser, setImageUser] = useState<SubUserItem | null>(null)
  const [images, setImages] = useState<ImageInfo[]>([])
  const [selectedImageIDs, setSelectedImageIDs] = useState<string[]>([])
  const [imagesLoading, setImagesLoading] = useState(false)
  const [savingImages, setSavingImages] = useState(false)
  const [rotatingPassword, setRotatingPassword] = useState(false)
  const [logPage, setLogPage] = useState(1)
  const [logPageSize, setLogPageSize] = useState(10)

  const fetchUsers = useCallback(async () => {
    try {
      const res = await api.get<{ success: boolean; data: SubUserItem[] }>('/sub-users')
      setUsers(res.data.data || [])
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchUsers() }, [fetchUsers])

  const managementUrl = (user: SubUserItem) => `${window.location.origin}/login?code=${user.access_code}`

  const copyText = async (text: string) => {
    await copyToClipboard(text)
  }

  const rotatePassword = async (user: SubUserItem) => {
    setRotatingPassword(true)
    try {
      const res = await api.post(`/sub-users/${user.id}/rotate-password`)
      const data = res.data.data
      const updatedUser = {
        ...user,
        username: data?.username || user.username,
        access_code: data?.access_code || user.access_code,
        password: data?.password || '',
      }
      setUsers((prev) => prev.map((item) => (item.id === user.id ? updatedUser : item)))
      setPasswordUser(updatedUser)
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('轮换失败', error.response?.data?.message || '请稍后重试')
    } finally {
      setRotatingPassword(false)
    }
  }

  const openImageLimit = async (user: SubUserItem) => {
    setImageUser(user)
    setSelectedImageIDs(user.allowed_image_ids || [])
    setImagesLoading(true)
    try {
      const res = await getImages()
      const currentIDs = new Set(user.current_image_ids || [])
      setImages((res.data.data || []).filter((image) => image.downloaded && (image.enabled || currentIDs.has(image.id))))
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('加载失败', error.response?.data?.message || '获取镜像列表失败')
    } finally {
      setImagesLoading(false)
    }
  }

  const toggleImageID = (id: string) => {
    setSelectedImageIDs((prev) => prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id])
  }

  const saveImageLimit = async () => {
    if (!imageUser) return
    setSavingImages(true)
    try {
      const res = await updateSubUserImages(imageUser.id, selectedImageIDs)
      const updated = {
        ...imageUser,
        allowed_image_ids: res.data.data?.allowed_image_ids || selectedImageIDs,
        image_limit_configured: true,
      }
      setUsers((prev) => prev.map((item) => (item.id === imageUser.id ? { ...item, allowed_image_ids: updated.allowed_image_ids, image_limit_configured: true } : item)))
      setImageUser(null)
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('保存失败', error.response?.data?.message || '保存可用镜像失败')
    } finally {
      setSavingImages(false)
    }
  }

  const showAuditLogs = async (user: SubUserItem) => {
    try {
      const res = await api.get(`/sub-users/${user.id}/audit-logs`)
      setAuditLogs(res.data.data || [])
      setLoginLogs(null)
      setModalTitle(`${user.username} - 操作日志`)
      setLogPage(1)
    } catch {
      dialog.alert('错误', '获取操作日志失败')
    }
  }

  const showLoginLogs = async (user: SubUserItem) => {
    try {
      const res = await api.get(`/sub-users/${user.id}/login-logs`)
      setLoginLogs(res.data.data || [])
      setAuditLogs(null)
      setModalTitle(`${user.username} - 登录日志`)
      setLogPage(1)
    } catch {
      dialog.alert('错误', '获取登录日志失败')
    }
  }

  const closeModal = () => {
    setAuditLogs(null)
    setLoginLogs(null)
  }

  const currentLogTotal = auditLogs?.length ?? loginLogs?.length ?? 0
  const logTotalPages = Math.max(1, Math.ceil(currentLogTotal / logPageSize))
  const currentLogPage = Math.min(logPage, logTotalPages)
  const logStart = (currentLogPage - 1) * logPageSize
  const currentAuditLogs = auditLogs?.slice(logStart, logStart + logPageSize)
  const currentLoginLogs = loginLogs?.slice(logStart, logStart + logPageSize)

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
      </div>
    )
  }

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-xl font-semibold text-foreground">{t('子用户管理')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {t('容器分配的子用户列表，共')} {users.length} {t('个')}
        </p>
      </div>

      <Card className="overflow-hidden">
        {users.length === 0 ? (
          <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
            <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-lg bg-muted">
              <UserCog className="h-7 w-7 text-muted-foreground" />
            </div>
            <div className="text-sm font-medium text-foreground">暂无子用户</div>
          </div>
        ) : (
          <Table className="w-full min-w-[820px] text-sm">
            <TableHeader className="border-b border-border bg-muted text-xs text-muted-foreground">
              <TableRow>
                <TableHead className="h-auto px-4 py-3 text-left font-medium w-12">#</TableHead>
                <TableHead className="h-auto px-4 py-3 text-left font-medium">容器名称</TableHead>
                <TableHead className="h-auto px-4 py-3 text-left font-medium">UUID</TableHead>
                <TableHead className="h-auto px-4 py-3 text-left font-medium">最后登录</TableHead>
                <TableHead className="h-auto px-4 py-3 text-center font-medium">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody className="divide-y divide-border">
              {users.map((user, index) => (
                <TableRow key={user.id} className="hover:bg-muted/50">
                  <TableCell className="px-4 py-3 text-muted-foreground">{index + 1}</TableCell>
                  <TableCell className="px-4 py-3 font-medium text-foreground">{user.container_name || '-'}</TableCell>
                  <TableCell className="px-4 py-3 font-mono text-xs text-muted-foreground">{user.container_uuid || '-'}</TableCell>
                  <TableCell className="px-4 py-3 text-muted-foreground">
                    {user.last_login ? (
                      <div>
                        <div className="text-xs">{user.last_login}</div>
                        <div className="text-xs text-muted-foreground">{user.last_login_ip}</div>
                      </div>
                    ) : (
                      <span className="text-muted-foreground">从未登录</span>
                    )}
                  </TableCell>
                  <TableCell className="px-4 py-3">
                    <div className="flex items-center justify-center gap-1">
                      <Button variant="ghost"
                        onClick={() => setPasswordUser(user)}
                        className="h-auto inline-flex items-center gap-1 px-2 py-1.5 text-xs"
                        title="查看密码"
                      >
                        <KeyRound className="w-3.5 h-3.5" />
                        查看密码
                      </Button>
                      <Button variant="ghost"
                        onClick={() => showAuditLogs(user)}
                        className="h-auto inline-flex items-center gap-1 px-2 py-1.5 text-xs"
                        title="查看操作日志"
                      >
                        <ScrollText className="w-3.5 h-3.5" />
                        操作日志
                      </Button>
                      <Button variant="ghost"
                        onClick={() => showLoginLogs(user)}
                        className="h-auto inline-flex items-center gap-1 px-2 py-1.5 text-xs"
                        title="查看登录日志"
                      >
                        <LogIn className="w-3.5 h-3.5" />
                        登录日志
                      </Button>
                      <Button variant="ghost"
                        onClick={() => openImageLimit(user)}
                        className="h-auto inline-flex items-center gap-1 px-2 py-1.5 text-xs"
                        title="可用镜像"
                      >
                        <HardDrive className="w-3.5 h-3.5" />
                        可用镜像
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      {passwordUser && (
        <Modal size="lg" onClose={() => setPasswordUser(null)} title="查看密码" extra={<Button variant="ghost"
                  onClick={() => rotatePassword(passwordUser)}
                  disabled={rotatingPassword}
                  className="h-auto inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs"
                  title="轮换密码"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${rotatingPassword ? 'animate-spin' : ''}`} />
                  {rotatingPassword ? '轮换中...' : '轮换密码'}
                </Button>} flush>
<div className="p-5">
              <div className="bg-muted rounded-lg p-4 text-sm space-y-3">
                <div className="flex items-start justify-between gap-3">
                  <span className="shrink-0 text-muted-foreground">用户</span>
                  <span className="min-w-0 text-right font-medium text-foreground break-all">{passwordUser.username}</span>
                </div>
                <div className="flex items-start justify-between gap-3">
                  <span className="shrink-0 text-muted-foreground">地址</span>
                  <div className="flex min-w-0 items-center gap-1">
                    <span className="font-mono text-xs text-foreground break-all">{managementUrl(passwordUser)}</span>
                    <Button variant="ghost" onClick={() => copyText(managementUrl(passwordUser))} className="h-auto shrink-0 p-0.5" title="复制">
                      <Copy className="w-3 h-3" />
                    </Button>
                  </div>
                </div>
                <div className="flex items-start justify-between gap-3">
                  <span className="shrink-0 text-muted-foreground">密码</span>
                  <div className="flex min-w-0 items-center gap-1">
                    <span className="font-mono text-xs text-foreground break-all">
                      {passwordUser.password || '未保存，请轮换生成新密码'}
                    </span>
                    {passwordUser.password && (
                      <Button variant="ghost" onClick={() => copyText(passwordUser.password || '')} className="h-auto shrink-0 p-0.5" title="复制">
                        <Copy className="w-3 h-3" />
                      </Button>
                    )}
                  </div>
                </div>
              </div>
            </div>
</Modal>
      )}

      {imageUser && (
        <Modal size="2xl" onClose={() => setImageUser(null)} title="可用镜像" description={<>{imageUser.username} · 默认勾选当前系统，取消后将禁止重装该系统</>} flush footer={<div className="flex w-full items-center justify-between gap-3">
              <span className="text-xs text-muted-foreground">已选择 {selectedImageIDs.length} 个镜像</span>
              <div className="flex items-center gap-2">
                <Button variant="ghost" onClick={() => setImageUser(null)} className="h-auto px-3 py-2 text-sm">
                  取消
                </Button>
                <Button variant="default"
                  onClick={saveImageLimit}
                  disabled={savingImages || imagesLoading}
                  className="h-auto inline-flex items-center gap-1.5 px-3 py-2 text-sm"
                >
                  <Save className="h-4 w-4" />
                  {savingImages ? '保存中...' : '保存'}
                </Button>
              </div>
            </div>}>
<div className="flex-1 overflow-y-auto p-5">
              {imagesLoading ? (
                <div className="flex items-center justify-center py-12">
                  <div className="h-7 w-7 animate-spin rounded-full border-b-2 border-primary" />
                </div>
              ) : images.length === 0 ? (
                <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
                  暂无已下载并启用的镜像
                </div>
              ) : (
                <div className="grid gap-2 sm:grid-cols-2">
                  {images.map((image) => {
                    const checked = selectedImageIDs.includes(image.id)
                    const current = (imageUser.current_image_ids || []).includes(image.id)
                    return (
                      <Label
                        key={image.id}
                        className={`flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-3 text-sm leading-normal ${checked ? 'border-primary bg-accent' : 'border-input hover:bg-accent'}`}
                      >
                        <Checkbox
                          checked={checked}
                          onCheckedChange={() => toggleImageID(image.id)}
                          className="mt-1 h-4 w-4"
                        />
                        <span className="min-w-0 flex-1">
                          <span className="block truncate font-medium text-foreground">{image.name}{current ? '（当前系统）' : ''}</span>
                          <span className="mt-1 block text-xs text-muted-foreground">
                            {image.type.toUpperCase()} · {image.arch} · {image.distro} {image.release}
                          </span>
                        </span>
                      </Label>
                    )
                  })}
                </div>
              )}
            </div>
</Modal>
      )}

      {/* Log Modal */}
      {(auditLogs || loginLogs) && (
        <Modal size="3xl" onClose={closeModal} title={modalTitle} flush footer={currentLogTotal > 0 && (
              <div className="flex w-full flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <span>
                    显示 {logStart + 1}-{Math.min(logStart + logPageSize, currentLogTotal)} / {currentLogTotal}
                  </span>
                  <NativeSelect
                    value={logPageSize}
                    onChange={(event) => {
                      setLogPageSize(Number(event.target.value))
                      setLogPage(1)
                    }}
                    className="h-7 pl-2 pr-8 text-xs"
                    aria-label="每页日志数量"
                  >
                    <option value={10}>10 / 页</option>
                    <option value={20}>20 / 页</option>
                    <option value={50}>50 / 页</option>
                  </NativeSelect>
                </div>
                <div className="flex items-center gap-1">
                  <Button variant="outline" onClick={() => setLogPage(1)} disabled={currentLogPage === 1} className="h-auto px-2.5 py-1.5 text-xs">首页</Button>
                  <Button variant="outline" onClick={() => setLogPage((page) => Math.max(1, page - 1))} disabled={currentLogPage === 1} className="h-auto px-2.5 py-1.5 text-xs">上一页</Button>
                  <span className="px-2 text-xs text-muted-foreground">{currentLogPage} / {logTotalPages}</span>
                  <Button variant="outline" onClick={() => setLogPage((page) => Math.min(logTotalPages, page + 1))} disabled={currentLogPage === logTotalPages} className="h-auto px-2.5 py-1.5 text-xs">下一页</Button>
                  <Button variant="outline" onClick={() => setLogPage(logTotalPages)} disabled={currentLogPage === logTotalPages} className="h-auto px-2.5 py-1.5 text-xs">末页</Button>
                </div>
              </div>
            )}>
<div className="overflow-auto flex-1">
              {auditLogs && (
                <Table className="w-full text-sm">
                  <TableHeader className="border-b border-border bg-muted text-xs text-muted-foreground sticky top-0">
                    <TableRow>
                      <TableHead className="h-auto px-4 py-2 text-left">操作时间</TableHead>
                      <TableHead className="h-auto px-4 py-2 text-left">操作</TableHead>
                      <TableHead className="h-auto px-4 py-2 text-left">IP</TableHead>
                      <TableHead className="h-auto px-4 py-2 text-left">UA</TableHead>
                      <TableHead className="h-auto px-4 py-2 text-center">结果</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody className="divide-y divide-border">
                    {auditLogs.length === 0 ? (
                      <TableRow><TableCell colSpan={5} className="px-4 py-8 text-center text-muted-foreground">暂无操作日志</TableCell></TableRow>
                    ) : currentAuditLogs?.map((log, i) => (
                      <TableRow key={i} className="hover:bg-muted/50">
                        <TableCell className="px-4 py-2 text-xs text-muted-foreground whitespace-nowrap">{log.time}</TableCell>
                        <TableCell className="px-4 py-2 text-xs text-foreground">{log.action}</TableCell>
                        <TableCell className="px-4 py-2 text-xs font-mono text-muted-foreground">{log.ip || '-'}</TableCell>
                        <TableCell className="px-4 py-2 text-xs text-muted-foreground max-w-[200px] truncate" title={log.user_agent}>{log.user_agent || '-'}</TableCell>
                        <TableCell className="px-4 py-2 text-center">
                          {log.success !== undefined ? (
                            log.success ? (
                              <Badge variant="secondary" className="inline-flex px-2 py-0.5 text-xs">成功</Badge>
                            ) : (
                              <Badge variant="destructive" className="inline-flex px-2 py-0.5 text-xs" title={log.error}>{log.error ? '失败' : '失败'}</Badge>
                            )
                          ) : (
                            <span className="text-muted-foreground">-</span>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
              {loginLogs && (
                <Table className="w-full text-sm">
                  <TableHeader className="border-b border-border bg-muted text-xs text-muted-foreground sticky top-0">
                    <TableRow>
                      <TableHead className="h-auto px-4 py-2 text-left">登录时间</TableHead>
                      <TableHead className="h-auto px-4 py-2 text-left">登录 IP</TableHead>
                      <TableHead className="h-auto px-4 py-2 text-left">UA</TableHead>
                      <TableHead className="h-auto px-4 py-2 text-center">结果</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody className="divide-y divide-border">
                    {loginLogs.length === 0 ? (
                      <TableRow><TableCell colSpan={4} className="px-4 py-8 text-center text-muted-foreground">暂无登录日志</TableCell></TableRow>
                    ) : currentLoginLogs?.map((log, i) => (
                      <TableRow key={i} className="hover:bg-muted/50">
                        <TableCell className="px-4 py-2 text-xs text-muted-foreground whitespace-nowrap">{log.time}</TableCell>
                        <TableCell className="px-4 py-2 text-xs font-mono text-muted-foreground">{log.ip}</TableCell>
                        <TableCell className="px-4 py-2 text-xs text-muted-foreground max-w-[250px] truncate" title={log.user_agent}>{log.user_agent}</TableCell>
                        <TableCell className="px-4 py-2 text-center">
                          {log.success ? (
                            <Badge variant="secondary" className="inline-flex px-2 py-0.5 text-xs">成功</Badge>
                          ) : (
                            <Badge variant="destructive" className="inline-flex px-2 py-0.5 text-xs">失败</Badge>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </div>
</Modal>
      )}
    </div>
  )
}
