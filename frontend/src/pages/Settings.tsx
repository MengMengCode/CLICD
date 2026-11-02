import { Label } from '../components/ui/label'
import { Button } from '../components/ui/button'
import { Input } from '../components/ui/input'
import { Textarea } from '../components/ui/textarea'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/table'
import { Card } from '../components/ui/card'
import { Badge } from '../components/ui/badge'
import { Checkbox } from '../components/ui/checkbox'
import { Switch } from '../components/ui/switch'
import { Alert, AlertDescription } from '../components/ui/alert'
import { Dispatch, SetStateAction, useCallback, useEffect, useState } from 'react'
import { Clock, Globe, ListTodo, Lock, LogIn, Minus, Monitor, Plus, RefreshCw, Save, Shield, ShieldCheck, Terminal, Upload, UserCog } from 'lucide-react'
import {
  changePassword,
  changeUsername,
  getLoginLogs,
  getPanelAccessPolicy,
  getSSLSettings,
  getTaskQueueSettings,
  getWebSSHOriginSettings,
  LoginLog,
  PanelAccessPolicy,
  SSLSettings,
  TaskQueueSettings,
  updateTaskQueueSettings,
  updateSSLSettings,
  updatePanelAccessPolicy,
  updateWebSSHOriginSettings,
  WebSSHOriginSettings,
} from '../services/api'
import { useDialog } from '../components/Dialog'
import { useAuth } from '../contexts/AuthContext'
import { useLanguage } from '../contexts/LanguageContext'

type SettingsSection = 'tasks' | 'account' | 'access' | 'webssh' | 'ssl' | 'logs'

const settingsSections = [
  { id: 'tasks', label: '任务队列', icon: ListTodo },
  { id: 'account', label: '账号设置', icon: UserCog },
  { id: 'access', label: '访问来源', icon: Shield },
  { id: 'webssh', label: 'WebSSH 访问', icon: Terminal },
  { id: 'ssl', label: 'SSL 证书', icon: ShieldCheck },
  { id: 'logs', label: '登录日志', icon: LogIn },
] as const

export default function Settings() {
  const dialog = useDialog()
  const { username } = useAuth()
  const { t } = useLanguage()
  const [logs, setLogs] = useState<LoginLog[]>([])
  const [loading, setLoading] = useState(true)
  const [logPage, setLogPage] = useState(1)
  const pageSize = 10

  const [oldPwd, setOldPwd] = useState('')
  const [newPwd, setNewPwd] = useState('')
  const [newUsername, setNewUsername] = useState('')

  const [ssl, setSSL] = useState<SSLSettings | null>(null)
  const [sslEnabled, setSSLEnabled] = useState(false)
  const [sslMode, setSSLMode] = useState<SSLSettings['mode']>('disabled')
  const [sslTarget, setSSLTarget] = useState('')
  const [sslEmail, setSSLEmail] = useState('')
  const [certPEM, setCertPEM] = useState('')
  const [keyPEM, setKeyPEM] = useState('')
  const [applyNow, setApplyNow] = useState(true)
  const [savingSSL, setSavingSSL] = useState(false)
  const [webSSHOrigins, setWebSSHOrigins] = useState<WebSSHOriginSettings | null>(null)
  const [webSSHOriginsText, setWebSSHOriginsText] = useState('')
  const [savingWebSSHOrigins, setSavingWebSSHOrigins] = useState(false)
  const [taskQueue, setTaskQueue] = useState<TaskQueueSettings | null>(null)
  const [taskConcurrency, setTaskConcurrency] = useState(2)
  const [savingTaskQueue, setSavingTaskQueue] = useState(false)
  const [accessPolicy, setAccessPolicy] = useState<PanelAccessPolicy | null>(null)
  const [accessEnabled, setAccessEnabled] = useState(false)
  const [allowedSourcesText, setAllowedSourcesText] = useState('')
  const [trustedProxiesText, setTrustedProxiesText] = useState('')
  const [savingAccessPolicy, setSavingAccessPolicy] = useState(false)
  const [activeSection, setActiveSection] = useState<SettingsSection>('tasks')

  const fetchLogs = useCallback(async () => {
    try {
      const res = await getLoginLogs()
      if (res.data.data) setLogs(res.data.data)
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }, [])

  const fetchSSL = useCallback(async () => {
    try {
      const res = await getSSLSettings()
      const data = res.data.data
      if (!data) return
      setSSL(data)
      setSSLEnabled(data.enabled)
      setSSLMode(data.mode || 'disabled')
      setSSLTarget(data.target || data.detected_host || '')
      setSSLEmail(data.email || '')
    } catch (err) {
      console.error(err)
    }
  }, [])

  const fetchWebSSHOrigins = useCallback(async () => {
    try {
      const res = await getWebSSHOriginSettings()
      const data = res.data.data
      if (!data) return
      setWebSSHOrigins(data)
      setWebSSHOriginsText((data.origins || []).join('\n'))
    } catch (err) {
      console.error(err)
    }
  }, [])

  const fetchTaskQueue = useCallback(async () => {
    try {
      const res = await getTaskQueueSettings()
      const data = res.data.data
      if (!data) return
      setTaskQueue(data)
      setTaskConcurrency(data.concurrency)
    } catch (err) {
      console.error(err)
    }
  }, [])

  const fetchAccessPolicy = useCallback(async () => {
    try {
      const res = await getPanelAccessPolicy()
      const data = res.data.data
      if (!data) return
      setAccessPolicy(data)
      setAccessEnabled(data.enabled)
      setAllowedSourcesText((data.allowed_sources || []).join('\n'))
      setTrustedProxiesText((data.trusted_proxies || []).join('\n'))
    } catch (err) {
      console.error(err)
    }
  }, [])

  useEffect(() => {
    fetchLogs()
    fetchSSL()
    fetchWebSSHOrigins()
    fetchTaskQueue()
    fetchAccessPolicy()
    const logTimer = setInterval(fetchLogs, 15000)
    const taskTimer = setInterval(fetchTaskQueue, 5000)
    return () => {
      clearInterval(logTimer)
      clearInterval(taskTimer)
    }
  }, [fetchAccessPolicy, fetchLogs, fetchSSL, fetchTaskQueue, fetchWebSSHOrigins])

  const handleSaveTaskQueue = async () => {
    const concurrency = Math.max(1, Math.min(16, Math.round(taskConcurrency || 1)))
    setSavingTaskQueue(true)
    try {
      const res = await updateTaskQueueSettings(concurrency)
      const data = res.data.data
      if (data) {
        setTaskQueue(data)
        setTaskConcurrency(data.concurrency)
      }
      dialog.alert('完成', '任务队列并发设置已保存并立即生效')
    } catch (err: unknown) {
      const e = err as { response?: { data?: { message?: string } } }
      dialog.alert('失败', e.response?.data?.message || '任务队列设置保存失败')
    } finally {
      setSavingTaskQueue(false)
    }
  }

  const handleSSLModeChange = (mode: SSLSettings['mode']) => {
    setSSLMode(mode)
    const saved = ssl?.mode_certificates?.[mode]
    setSSLTarget(saved?.target || ssl?.detected_host || sslTarget)
    setSSLEmail(saved?.email || '')
  }

  const handleSaveSSL = async () => {
    setSavingSSL(true)
    try {
      const enabled = sslEnabled && sslMode !== 'disabled'
      const res = await updateSSLSettings({
        enabled,
        mode: enabled ? sslMode : 'disabled',
        target: sslTarget,
        email: sslEmail,
        cert_pem: certPEM,
        key_pem: keyPEM,
        apply_now: applyNow,
      })
      if (res.data.data) {
        setSSL(res.data.data)
        setCertPEM('')
        setKeyPEM('')
      }
      dialog.alert('完成', applyNow ? 'SSL 设置已保存，服务正在重启。稍后请用新的协议重新打开面板。' : 'SSL 设置已保存，重启 clicd 服务后生效。')
    } catch (err: unknown) {
      const e = err as { response?: { data?: { message?: string } } }
      dialog.alert('失败', e.response?.data?.message || 'SSL 设置保存失败')
    } finally {
      setSavingSSL(false)
    }
  }

  const handleSaveWebSSHOrigins = async () => {
    setSavingWebSSHOrigins(true)
    try {
      const origins = webSSHOriginsText.split(/\r?\n/).map(item => item.trim()).filter(Boolean)
      const res = await updateWebSSHOriginSettings(origins)
      const data = res.data.data
      if (data) {
        setWebSSHOrigins(data)
        setWebSSHOriginsText((data.origins || []).join('\n'))
      }
      dialog.alert('完成', 'Origin 白名单已保存')
    } catch (err: unknown) {
      const e = err as { response?: { data?: { message?: string } } }
      dialog.alert('失败', e.response?.data?.message || 'Origin 白名单保存失败')
    } finally {
      setSavingWebSSHOrigins(false)
    }
  }

  const handleAccessEnabledChange = (enabled: boolean) => {
    setAccessEnabled(enabled)
    if (enabled && !allowedSourcesText.trim() && accessPolicy?.current_source) {
      setAllowedSourcesText(accessPolicy.current_source)
    }
  }

  const handleSaveAccessPolicy = async () => {
    const splitEntries = (value: string) => value.split(/[\s,;]+/).map(item => item.trim()).filter(Boolean)
    setSavingAccessPolicy(true)
    try {
      const res = await updatePanelAccessPolicy({
        enabled: accessEnabled,
        allowed_sources: splitEntries(allowedSourcesText),
        trusted_proxies: splitEntries(trustedProxiesText),
      })
      const data = res.data.data
      if (data) {
        setAccessPolicy(data)
        setAccessEnabled(data.enabled)
        setAllowedSourcesText((data.allowed_sources || []).join('\n'))
        setTrustedProxiesText((data.trusted_proxies || []).join('\n'))
      }
      dialog.alert('完成', accessEnabled ? '面板访问来源策略已保存并立即生效' : '面板访问来源限制已关闭')
    } catch (err: unknown) {
      const e = err as { response?: { data?: { message?: string } } }
      dialog.alert('失败', e.response?.data?.message || '面板访问来源策略保存失败')
    } finally {
      setSavingAccessPolicy(false)
    }
  }

  const handleSaveAccount = async () => {
    if (!oldPwd) {
      dialog.alert('提示', '请输入当前密码以确认修改')
      return
    }
    if (!newPwd && !newUsername) {
      dialog.alert('提示', '至少填写新密码或新用户名中的一项')
      return
    }
    if (newPwd && newPwd.length < 6) {
      dialog.alert('提示', '新密码至少 6 位')
      return
    }
    if (newUsername && newUsername.length < 3) {
      dialog.alert('提示', '用户名至少 3 位')
      return
    }

    const results: string[] = []
    try {
      if (newUsername) {
        const res = await changeUsername(newUsername, oldPwd)
        results.push(res.data.success ? '用户名已修改' : '用户名修改失败')
      }
      if (newPwd) {
        const res = await changePassword(oldPwd, newPwd)
        results.push(res.data.success ? '密码已修改' : '密码修改失败')
      }
      if (results.length > 0) {
        dialog.alert('完成', `${results.join('，')}。下次登录生效`)
        setOldPwd('')
        setNewPwd('')
        setNewUsername('')
      }
    } catch (err: unknown) {
      const e = err as { response?: { data?: { message?: string } } }
      dialog.alert('失败', e.response?.data?.message || '修改失败')
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary"></div>
      </div>
    )
  }

  const totalPages = Math.ceil(logs.length / pageSize)

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-2xl font-bold text-foreground">面板设置</h1>
        <p className="mt-1 text-sm text-muted-foreground">任务队列、账号、访问控制、安全证书与访问记录</p>
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-[210px_minmax(0,1fr)]">
        <Card className="overflow-x-auto p-2 lg:sticky lg:top-4">
          <nav className="flex min-w-max gap-1 lg:min-w-0 lg:flex-col" aria-label="设置分类">
            {settingsSections.map((section) => {
              const Icon = section.icon
              const active = activeSection === section.id
              return (
                <Button
                  key={section.id}
                  type="button"
                  onClick={() => setActiveSection(section.id)}
                  variant={active ? 'default' : 'ghost'}
                  className="h-auto justify-start gap-2 px-3 py-2.5 text-left"
                >
                  <Icon className="h-4 w-4 flex-shrink-0" />
                  <span>{t(section.label)}</span>
                </Button>
              )
            })}
          </nav>
        </Card>

        <section className="min-w-0">
          {activeSection === 'tasks' && (
            <TaskQueueCard
              settings={taskQueue}
              concurrency={taskConcurrency}
              saving={savingTaskQueue}
              onConcurrencyChange={setTaskConcurrency}
              onRefresh={fetchTaskQueue}
              onSave={handleSaveTaskQueue}
            />
          )}

          {activeSection === 'account' && (
            <Card className="p-5">
              <h2 className="mb-4 flex items-center gap-2 text-sm font-semibold text-foreground">
                <UserCog className="h-4 w-4" />账号设置
              </h2>
              <div className="grid gap-4 md:grid-cols-2">
                <div>
                  <Label className="mb-1 block text-xs text-muted-foreground">当前用户名</Label>
                  <Input type="text" value={username || ''} disabled className="w-full text-sm" />
                </div>
                <div>
                  <Label className="mb-1 block text-xs text-muted-foreground">新用户名，留空则不修改</Label>
                  <Input type="text" value={newUsername} onChange={(e) => setNewUsername(e.target.value)} className="w-full text-sm" placeholder="至少 3 位" />
                </div>
                <div>
                  <Label className="mb-1 block text-xs text-muted-foreground">新密码，留空则不修改</Label>
                  <Input type="password" value={newPwd} onChange={(e) => setNewPwd(e.target.value)} className="w-full text-sm" placeholder="至少 6 位" />
                </div>
                <div>
                  <Label className="mb-1 block text-xs text-muted-foreground">当前密码，验证身份</Label>
                  <Input type="password" value={oldPwd} onChange={(e) => setOldPwd(e.target.value)} className="w-full text-sm" placeholder="输入当前密码以确认修改" />
                </div>
              </div>
              <div className="mt-4 flex justify-end">
                <Button size="sm" onClick={handleSaveAccount} className="px-4 py-2 text-sm">保存修改</Button>
              </div>
            </Card>
          )}

          {activeSection === 'webssh' && (
            <WebSSHOriginCard
              settings={webSSHOrigins}
              originsText={webSSHOriginsText}
              saving={savingWebSSHOrigins}
              onOriginsTextChange={setWebSSHOriginsText}
              onRefresh={fetchWebSSHOrigins}
              onSave={handleSaveWebSSHOrigins}
            />
          )}

          {activeSection === 'access' && (
            <PanelAccessPolicyCard
              policy={accessPolicy}
              enabled={accessEnabled}
              allowedSourcesText={allowedSourcesText}
              trustedProxiesText={trustedProxiesText}
              saving={savingAccessPolicy}
              onEnabledChange={handleAccessEnabledChange}
              onAllowedSourcesTextChange={setAllowedSourcesText}
              onTrustedProxiesTextChange={setTrustedProxiesText}
              onRefresh={fetchAccessPolicy}
              onSave={handleSaveAccessPolicy}
            />
          )}

          {activeSection === 'ssl' && (
            <SSLCard
              ssl={ssl}
              sslEnabled={sslEnabled}
              sslMode={sslMode}
              sslTarget={sslTarget}
              sslEmail={sslEmail}
              certPEM={certPEM}
              keyPEM={keyPEM}
              applyNow={applyNow}
              savingSSL={savingSSL}
              onRefresh={fetchSSL}
              onEnabledChange={setSSLEnabled}
              onModeChange={handleSSLModeChange}
              onTargetChange={setSSLTarget}
              onEmailChange={setSSLEmail}
              onCertChange={setCertPEM}
              onKeyChange={setKeyPEM}
              onApplyNowChange={setApplyNow}
              onSave={handleSaveSSL}
            />
          )}

          {activeSection === 'logs' && (
            <LoginLogCard logs={logs} logPage={logPage} pageSize={pageSize} totalPages={totalPages} setLogPage={setLogPage} />
          )}
        </section>
      </div>
    </div>
  )
}

interface TaskQueueCardProps {
  settings: TaskQueueSettings | null
  concurrency: number
  saving: boolean
  onConcurrencyChange: (value: number) => void
  onRefresh: () => void
  onSave: () => void
}

interface PanelAccessPolicyCardProps {
  policy: PanelAccessPolicy | null
  enabled: boolean
  allowedSourcesText: string
  trustedProxiesText: string
  saving: boolean
  onEnabledChange: (enabled: boolean) => void
  onAllowedSourcesTextChange: (value: string) => void
  onTrustedProxiesTextChange: (value: string) => void
  onRefresh: () => void
  onSave: () => void
}

function PanelAccessPolicyCard(props: PanelAccessPolicyCardProps) {
  return (
    <Card className="p-5">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
            <Shield className="h-4 w-4" />访问来源策略
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">限制可访问面板、登录和 API 的来源地址</p>
        </div>
        <Button variant="outline" size="icon" type="button" onClick={props.onRefresh} className="h-8 w-8" title="刷新">
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>

      <div className="flex items-center justify-between gap-4 border-y border-border py-3">
        <div>
          <div className="text-sm font-medium text-foreground">启用访问白名单</div>
          <div className="mt-0.5 text-xs text-muted-foreground">关闭后不限制访问来源</div>
        </div>
        <Switch
          checked={props.enabled}
          onCheckedChange={props.onEnabledChange}
          title={props.enabled ? '关闭访问白名单' : '启用访问白名单'}
          aria-label="启用访问白名单"
        />
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <div>
          <Label className="mb-1.5 block text-xs font-medium text-muted-foreground">允许的 IP / CIDR</Label>
          <Textarea
            value={props.allowedSourcesText}
            onChange={(event) => props.onAllowedSourcesTextChange(event.target.value)}
            rows={6}
            disabled={!props.enabled}
            className="w-full font-mono text-xs"
            placeholder={'203.0.113.10\n192.168.1.0/24\n2001:db8::/32'}
          />
        </div>
        <div>
          <Label className="mb-1.5 block text-xs font-medium text-muted-foreground">可信代理 IP / CIDR</Label>
          <Textarea
            value={props.trustedProxiesText}
            onChange={(event) => props.onTrustedProxiesTextChange(event.target.value)}
            rows={6}
            disabled={!props.enabled}
            className="w-full font-mono text-xs"
            placeholder={'127.0.0.1\n10.0.0.0/8'}
          />
          <p className="mt-1.5 text-xs text-muted-foreground">仅可信代理可提供真实客户端地址；未使用反向代理时留空</p>
        </div>
      </div>

      <Card className="mt-4 grid gap-2 p-3 text-xs sm:grid-cols-2">
        <div>
          <span className="text-muted-foreground">当前识别来源</span>
          <div className="mt-0.5 break-all font-mono text-foreground">{props.policy?.current_source || '-'}</div>
        </div>
        <div>
          <span className="text-muted-foreground">直接连接来源</span>
          <div className="mt-0.5 break-all font-mono text-foreground">{props.policy?.direct_source || '-'}</div>
        </div>
      </Card>

      <div className="mt-4 flex justify-end">
        <Button size="sm" type="button" onClick={props.onSave} disabled={props.saving} className="inline-flex items-center justify-center gap-2 px-4 py-2 text-sm">
          <Save className="h-4 w-4" />
          {props.saving ? '保存中...' : '保存访问策略'}
        </Button>
      </div>
    </Card>
  )
}

function TaskQueueCard(props: TaskQueueCardProps) {
  const setBounded = (value: number) => props.onConcurrencyChange(Math.max(1, Math.min(16, value)))
  return (
    <Card className="p-4">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
          <ListTodo className="h-4 w-4" />任务队列
        </h2>
        <Button variant="outline" size="icon" onClick={props.onRefresh} className="h-8 w-8" title="刷新">
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>
      <div className="grid grid-cols-2 divide-x divide-border border-y border-border bg-muted">
        <div className="px-3 py-2">
          <div className="text-[11px] text-muted-foreground">运行中</div>
          <div className="mt-0.5 text-lg font-semibold text-foreground">{props.settings?.active ?? 0}</div>
        </div>
        <div className="px-3 py-2">
          <div className="text-[11px] text-muted-foreground">等待中</div>
          <div className="mt-0.5 text-lg font-semibold text-foreground">{props.settings?.pending ?? 0}</div>
        </div>
      </div>
      <div className="mt-4">
        <Label className="mb-1.5 block text-xs text-muted-foreground">总并发上限</Label>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="icon" type="button" onClick={() => setBounded(props.concurrency - 1)} disabled={props.concurrency <= 1} className="h-8 w-8 flex w-10 items-center justify-center" title="减少并发">
            <Minus className="h-4 w-4" />
          </Button>
          <Input
            type="number"
            min={1}
            max={16}
            value={props.concurrency}
            onChange={(event) => setBounded(Number(event.target.value) || 1)}
            className="min-w-0 flex-1 text-center text-sm font-medium"
          />
          <Button variant="outline" size="icon" type="button" onClick={() => setBounded(props.concurrency + 1)} disabled={props.concurrency >= 16} className="h-8 w-8 flex w-10 items-center justify-center" title="增加并发">
            <Plus className="h-4 w-4" />
          </Button>
        </div>
      </div>
      <div className="mt-4 flex justify-end">
        <Button size="sm" onClick={props.onSave} disabled={props.saving} className="inline-flex items-center justify-center gap-2 px-4 py-2 text-sm">
          <Save className="h-4 w-4" />
          {props.saving ? '保存中...' : '保存队列设置'}
        </Button>
      </div>
    </Card>
  )
}

interface SSLCardProps {
  ssl: SSLSettings | null
  sslEnabled: boolean
  sslMode: SSLSettings['mode']
  sslTarget: string
  sslEmail: string
  certPEM: string
  keyPEM: string
  applyNow: boolean
  savingSSL: boolean
  onRefresh: () => void
  onEnabledChange: (enabled: boolean) => void
  onModeChange: (mode: SSLSettings['mode']) => void
  onTargetChange: (target: string) => void
  onEmailChange: (email: string) => void
  onCertChange: (cert: string) => void
  onKeyChange: (key: string) => void
  onApplyNowChange: (apply: boolean) => void
  onSave: () => void
}

interface WebSSHOriginCardProps {
  settings: WebSSHOriginSettings | null
  originsText: string
  saving: boolean
  onOriginsTextChange: (value: string) => void
  onRefresh: () => void
  onSave: () => void
}

function WebSSHOriginCard(props: WebSSHOriginCardProps) {
  return (
    <Card className="p-4">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
          <Terminal className="h-4 w-4" />WebSSH Origin 白名单
        </h2>
        <Button variant="outline" size="icon" onClick={props.onRefresh} className="h-8 w-8" title="刷新">
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>
      <div className="space-y-3">
        <div>
          <Label className="mb-1 block text-xs text-muted-foreground">允许的 Origin</Label>
          <Textarea
            value={props.originsText}
            onChange={(e) => props.onOriginsTextChange(e.target.value)}
            rows={4}
            className="w-full font-mono text-xs"
          />
        </div>
        <Alert>
          <AlertDescription className="text-xs">
            <div className="truncate font-mono" title={props.settings?.current_origin || ''}>当前面板来源：{props.settings?.current_origin || '-'}</div>
            <div className="mt-1">默认允许当前面板来源和本机回环来源；额外域名每行填写一个完整 Origin。</div>
          </AlertDescription>
        </Alert>
        <div className="flex justify-end">
          <Button size="sm" onClick={props.onSave} disabled={props.saving} className="inline-flex items-center justify-center gap-2 px-4 py-2 text-sm">
            <Upload className="h-4 w-4" />
            {props.saving ? '保存中...' : '保存 Origin 白名单'}
          </Button>
        </div>
      </div>
    </Card>
  )
}

function SSLCard(props: SSLCardProps) {
  const selectedSSL = props.ssl?.mode_certificates?.[props.sslMode]
  const modeOptions: Array<{ value: SSLSettings['mode']; label: string }> = [
    { value: 'letsencrypt', label: 'Let’s Encrypt' },
    { value: 'self_signed', label: '自签证书' },
    { value: 'uploaded', label: '上传证书' },
  ]

  return (
    <Card className="p-5">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
          <ShieldCheck className="h-4 w-4" />SSL 证书
        </h2>
        <Button variant="outline" size="icon" onClick={props.onRefresh} className="h-8 w-8" title="刷新">
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>
      <div className="space-y-4">
        <Label className="flex items-center gap-2 text-sm text-foreground">
          <Checkbox checked={props.sslEnabled} onCheckedChange={(checked) => props.onEnabledChange(checked === true)} />
          启用 HTTPS / WSS
        </Label>

        <div className="grid gap-2 sm:grid-cols-3">
          {modeOptions.map((option) => (
            <Button
              key={option.value}
              onClick={() => props.onModeChange(option.value)}
              variant={props.sslMode === option.value ? 'default' : 'outline'}
              className="px-3 py-2"
            >
              {option.label}
            </Button>
          ))}
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <div>
            <Label className="mb-1 block text-xs text-muted-foreground">IP / 域名</Label>
            <Input
              type="text"
              value={props.sslTarget}
              onChange={(e) => props.onTargetChange(e.target.value)}
              className="w-full text-sm"
              placeholder={props.ssl?.detected_host || '服务器公网 IP 或域名'}
            />
          </div>
          {props.sslMode === 'letsencrypt' && (
            <div>
              <Label className="mb-1 block text-xs text-muted-foreground">邮箱，可选</Label>
              <Input type="email" value={props.sslEmail} onChange={(e) => props.onEmailChange(e.target.value)} className="w-full text-sm" placeholder="admin@example.com" />
            </div>
          )}
        </div>

        {props.sslMode === 'letsencrypt' && (
          <Alert>
            <AlertDescription className="text-xs">纯 IP 证书需要服务器安装 Certbot 5.4+，且验证时 80 端口必须能被 Let’s Encrypt 访问。IP 证书是短有效期证书，certbot 需要保持自动续签。</AlertDescription>
          </Alert>
        )}

        {props.sslMode === 'self_signed' && (
          <Alert>
            <AlertDescription className="text-xs">自签证书可以加密面板和 VNC，但浏览器会提示证书不受信任；证书快到期时系统会自动重新签发。</AlertDescription>
          </Alert>
        )}

        {props.sslMode === 'uploaded' && (
          <div className="grid gap-3 lg:grid-cols-2">
            <div>
              <Label className="mb-1 block text-xs text-muted-foreground">证书 PEM / fullchain.pem</Label>
              <Textarea value={props.certPEM} onChange={(e) => props.onCertChange(e.target.value)} rows={7} className="w-full font-mono text-xs" placeholder="-----BEGIN CERTIFICATE-----" />
            </div>
            <div>
              <Label className="mb-1 block text-xs text-muted-foreground">私钥 PEM / privkey.pem</Label>
              <Textarea value={props.keyPEM} onChange={(e) => props.onKeyChange(e.target.value)} rows={7} className="w-full font-mono text-xs" placeholder="-----BEGIN PRIVATE KEY-----" />
            </div>
          </div>
        )}

        {selectedSSL?.certificate ? (
          <Card className="p-3 text-xs">
            <div className="flex items-center gap-2 text-foreground">
              <Lock className="h-3.5 w-3.5" />
              当前证书：{selectedSSL.certificate.valid ? '有效' : '已过期或未生效'}
            </div>
            <div className="mt-1 font-mono">到期时间：{selectedSSL.certificate.not_after}</div>
            <div className="mt-1 truncate font-mono" title={selectedSSL.cert_path}>证书路径：{selectedSSL.cert_path || '-'}</div>
            {selectedSSL.last_error && <div className="mt-1 text-red-600">最近错误：{selectedSSL.last_error}</div>}
          </Card>
        ) : (
          <Alert>
            <AlertDescription className="text-xs">{props.sslMode === 'uploaded' ? '上传来源还没有保存证书，请粘贴证书和私钥后保存。' : '当前来源还没有保存证书，保存 SSL 设置时会自动生成或申请。'}</AlertDescription>
          </Alert>
        )}

        <Label className="flex items-center gap-2 text-xs text-muted-foreground">
          <Checkbox checked={props.applyNow} onCheckedChange={(checked) => props.onApplyNowChange(checked === true)} />
          保存后自动重启服务并立即生效
        </Label>

        <div className="flex justify-end">
          <Button size="sm" onClick={props.onSave} disabled={props.savingSSL} className="inline-flex items-center justify-center gap-2 px-4 py-2 text-sm">
            <Upload className="h-4 w-4" />
            {props.savingSSL ? '保存中...' : '保存 SSL 设置'}
          </Button>
        </div>
      </div>
    </Card>
  )
}

interface LoginLogCardProps {
  logs: LoginLog[]
  logPage: number
  pageSize: number
  totalPages: number
  setLogPage: Dispatch<SetStateAction<number>>
}

function LoginLogCard({ logs, logPage, pageSize, totalPages, setLogPage }: LoginLogCardProps) {
  return (
    <Card className="p-5">
      <h2 className="mb-4 flex items-center gap-2 text-sm font-semibold text-foreground">
        <LogIn className="h-4 w-4" />登录日志
      </h2>
      {logs.length === 0 ? (
        <p className="text-sm text-muted-foreground">暂无登录记录</p>
      ) : (
        <>
          <div className="overflow-x-auto">
            <Table className="w-full text-xs">
              <TableHeader>
                <TableRow >
                  <TableHead className="w-40 py-2 text-left font-medium"><span className="inline-flex items-center gap-1"><Clock className="h-3 w-3" />时间</span></TableHead>
                  <TableHead className="py-2 text-left font-medium">用户名</TableHead>
                  <TableHead className="py-2 text-left font-medium"><span className="inline-flex items-center gap-1"><Globe className="h-3 w-3" />IP</span></TableHead>
                  <TableHead className="py-2 text-left font-medium"><span className="inline-flex items-center gap-1"><Monitor className="h-3 w-3" />设备</span></TableHead>
                  <TableHead className="py-2 text-left font-medium">结果</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody >
                {logs.slice((logPage - 1) * pageSize, logPage * pageSize).map((log, index) => (
                  <TableRow key={`${log.time}-${index}`}>
                    <TableCell className="whitespace-nowrap py-1.5 font-mono">{log.time}</TableCell>
                    <TableCell className="py-1.5">{log.username}</TableCell>
                    <TableCell className="py-1.5 font-mono">{log.ip}</TableCell>
                    <TableCell className="max-w-[180px] truncate py-1.5" title={log.user_agent}>{formatUA(log.user_agent)}</TableCell>
                    <TableCell className="py-1.5">
                      <Badge variant={log.success ? 'secondary' : 'destructive'}>
                        {log.success ? '成功' : '失败'}
                      </Badge>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          {logs.length > pageSize && (
            <div className="mt-3 flex items-center justify-between border-t border-border pt-3">
              <span className="text-xs text-muted-foreground">共 {logs.length} 条，第 {logPage}/{totalPages} 页</span>
              <div className="flex items-center gap-1">
                <Button variant="outline" size="sm" onClick={() => setLogPage(1)} disabled={logPage === 1} className="px-2 py-1 text-xs">首页</Button>
                <Button variant="outline" size="sm" onClick={() => setLogPage(p => Math.max(1, p - 1))} disabled={logPage === 1} className="px-2 py-1 text-xs">上一页</Button>
                <Button variant="outline" size="sm" onClick={() => setLogPage(p => Math.min(totalPages, p + 1))} disabled={logPage >= totalPages} className="px-2 py-1 text-xs">下一页</Button>
                <Button variant="outline" size="sm" onClick={() => setLogPage(totalPages)} disabled={logPage >= totalPages} className="px-2 py-1 text-xs">末页</Button>
              </div>
            </div>
          )}
        </>
      )}
    </Card>
  )
}

function formatUA(ua: string): string {
  const parts: string[] = []
  if (ua.includes('Windows NT')) parts.push('Windows')
  else if (ua.includes('Mac OS X')) parts.push('macOS')
  else if (ua.includes('Linux')) parts.push('Linux')
  else if (ua.includes('Android')) parts.push('Android')
  else if (ua.includes('iPhone') || ua.includes('iPad')) parts.push('iOS')

  if (ua.includes('Chrome') && !ua.includes('Edg')) parts.push('Chrome')
  else if (ua.includes('Firefox')) parts.push('Firefox')
  else if (ua.includes('Edg')) parts.push('Edge')
  else if (ua.includes('Safari') && !ua.includes('Chrome')) parts.push('Safari')

  return parts.join(' / ') || ua.substring(0, 40)
}
