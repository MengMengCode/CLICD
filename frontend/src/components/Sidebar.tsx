import { Button } from './ui/button'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { cn } from '../lib/utils'
import { useEffect, useState, type ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router'
import {
  ChevronLeft,
  ChevronRight,
  Code2,
  Container,
  Cpu,
  Camera,
  HardDrive,
  LayoutDashboard,
  LogOut,
  Moon,
  Package,
  Route,
  ScrollText,
  Server,
  Sun,
  UserCog,
  Archive,
  Boxes,
  ListChecks,
} from 'lucide-react'
import { useAuth } from '../contexts/AuthContext'
import { useLanguage } from '../contexts/LanguageContext'
import { useTheme } from '../contexts/ThemeContext'
import { getVersion } from '../services/api'
import AppIcon from './AppIcon'

interface SidebarProps {
  collapsed: boolean
  onToggle: () => void
}

function GitHubIcon({ className = '' }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 1024 1024"
      version="1.1"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <path
        d="M512 42.666667A464.64 464.64 0 0 0 42.666667 502.186667 460.373333 460.373333 0 0 0 363.52 938.666667c23.466667 4.266667 32-9.813333 32-22.186667v-78.08c-130.56 27.733333-158.293333-61.44-158.293333-61.44a122.026667 122.026667 0 0 0-52.053334-67.413333c-42.666667-28.16 3.413333-27.733333 3.413334-27.733334a98.56 98.56 0 0 1 71.68 47.36 101.12 101.12 0 0 0 136.533333 37.973334 99.413333 99.413333 0 0 1 29.866667-61.44c-104.106667-11.52-213.333333-50.773333-213.333334-226.986667a177.066667 177.066667 0 0 1 47.36-124.16 161.28 161.28 0 0 1 4.693334-121.173333s39.68-12.373333 128 46.933333a455.68 455.68 0 0 1 234.666666 0c89.6-59.306667 128-46.933333 128-46.933333a161.28 161.28 0 0 1 4.693334 121.173333A177.066667 177.066667 0 0 1 810.666667 477.866667c0 176.64-110.08 215.466667-213.333334 226.986666a106.666667 106.666667 0 0 1 32 85.333334v125.866666c0 14.933333 8.533333 26.88 32 22.186667A460.8 460.8 0 0 0 981.333333 502.186667 464.64 464.64 0 0 0 512 42.666667"
        fill="currentColor"
      />
    </svg>
  )
}

function LanguageIcon({ className = '' }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 1024 1024" version="1.1" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
      <path
        d="M128 170.6496A42.6496 42.6496 0 0 0 128 256V170.6496zM640 256a42.6496 42.6496 0 1 0 0-85.3504V256zM426.6496 128a42.6496 42.6496 0 0 0-85.2992 0h85.2992zM341.3504 213.3504a42.6496 42.6496 0 0 0 85.2992 0H341.3504z m56.6784 434.944a42.6496 42.6496 0 0 0 61.44-59.2896l-61.44 59.2896zM312.8832 367.4112a42.6496 42.6496 0 0 0-78.592 33.1776l78.592-33.1776z m220.4672 357.888a42.6496 42.6496 0 1 0 0 85.3504v-85.2992z m298.6496 85.3504a42.6496 42.6496 0 1 0 0-85.2992v85.2992z m-400.8448 66.2528a42.6496 42.6496 0 1 0 76.3392 38.1952l-76.288-38.1952z m251.4944-407.552l38.1952-19.0976a42.6496 42.6496 0 0 0-76.3392 0l38.144 19.0976z m175.2064 445.7472a42.6496 42.6496 0 1 0 76.288-38.1952l-76.288 38.1952zM586.1376 220.3648a42.6496 42.6496 0 1 0-84.1728-14.08l84.1728 14.08zM109.0048 735.2832a42.6496 42.6496 0 0 0 37.9904 76.4416l-37.9904-76.4416zM128 256h512V170.6496h-512V256z m213.3504-128v85.3504h85.2992V128H341.3504z m118.0672 461.0048a726.3232 726.3232 0 0 1-146.5344-221.5936l-78.592 33.1776a811.6224 811.6224 0 0 0 163.7376 247.7056l61.44-59.2896z m73.9328 221.696h298.6496v-85.3504h-298.6496v85.2992z m-25.856 104.3968l213.3504-426.7008-76.3392-38.144-213.3504 426.6496 76.3392 38.1952z m137.0112-426.7008l213.3504 426.7008 76.288-38.1952-213.2992-426.6496-76.3392 38.144zM501.9648 206.336C463.0016 438.6304 313.3952 633.7536 109.056 735.232l37.9904 76.4416c228.2496-113.4592 395.52-331.3152 439.1424-591.36L501.9648 206.336z"
        fill="currentColor"
      />
    </svg>
  )
}

function SidebarHint({ collapsed, label, children }: { collapsed: boolean; label: string; children: ReactNode }) {
  if (!collapsed) return children
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  )
}

export default function Sidebar({ collapsed, onToggle }: SidebarProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const { logout, isSubUser } = useAuth()
  const { theme, toggleTheme } = useTheme()
  const { toggleLanguage, t } = useLanguage()
  const [version, setVersion] = useState('')

  useEffect(() => {
    getVersion()
      .then(res => {
        if (res.data?.data?.version) setVersion(res.data.data.version)
      })
      .catch(() => {})
  }, [])

  const navItems = [
    { path: '/', label: '控制面板', icon: LayoutDashboard, active: location.pathname === '/', admin: true },
    { path: '/containers', label: '容器管理', icon: Server, active: location.pathname.startsWith('/container'), admin: false },
    { path: '/podman', label: 'Podman 容器', icon: Container, active: location.pathname === '/podman', admin: true },
    { path: '/images', label: '镜像管理', icon: Package, active: location.pathname.startsWith('/images'), admin: true },
    { path: '/snapshots', label: '快照管理', icon: Camera, active: location.pathname.startsWith('/snapshots'), admin: true },
    { path: '/backups', label: '备份与恢复', icon: Archive, active: location.pathname.startsWith('/backups'), admin: true },
    { path: '/instance-templates', label: '实例模板', icon: Boxes, active: location.pathname.startsWith('/instance-templates'), admin: true },
    { path: '/tasks', label: '任务中心', icon: ListChecks, active: location.pathname.startsWith('/tasks'), admin: false },
    { path: '/routing', label: '路由管理', icon: Route, active: location.pathname.startsWith('/routing'), admin: true },
    { path: '/storage', label: '存储管理', icon: HardDrive, active: location.pathname.startsWith('/storage'), admin: true },
    { path: '/audit-logs', label: '操作日志', icon: ScrollText, active: location.pathname.startsWith('/audit-logs'), admin: true },
    { path: '/sub-users', label: '子用户管理', icon: UserCog, active: location.pathname.startsWith('/sub-users'), admin: true },
    { path: '/api-integration', label: 'API 集成', icon: Code2, active: location.pathname.startsWith('/api-integration'), admin: true },
    { path: '/host-report', label: '宿主机信息', icon: Cpu, active: location.pathname.startsWith('/host-report'), admin: true },
    { path: '/settings', label: '面板设置', icon: UserCog, active: location.pathname.startsWith('/settings'), admin: true },
  ]
  const themeLabel = t(theme === 'dark' ? '切换亮色模式' : '切换暗黑模式')

  return (
    <TooltipProvider delayDuration={150}>
      <aside className={cn('fixed left-0 top-0 z-30 flex h-full flex-col border-r bg-card text-card-foreground transition-all duration-300', collapsed ? 'w-16' : 'w-60')}>
        <div className={cn('flex h-14 shrink-0 items-center justify-between border-b', collapsed ? 'px-1' : 'px-4')}>
          <div className="flex min-w-0 items-center gap-2">
            <div className="flex h-7 w-7 shrink-0 items-center justify-center">
              <AppIcon className="w-5 h-5" />
            </div>
            {!collapsed && <span className="text-sm font-bold">CLICD</span>}
          </div>
          <SidebarHint collapsed={collapsed} label={t('切换侧边栏')}>
            <Button variant="ghost" size="icon" onClick={onToggle} className="h-6 w-6 shrink-0" aria-label={t('切换侧边栏')}>
              {collapsed ? <ChevronRight className="w-4 h-4" /> : <ChevronLeft className="w-4 h-4" />}
            </Button>
          </SidebarHint>
        </div>

        <nav className="min-h-0 flex-1 space-y-1 overflow-y-auto px-2 py-4" aria-label={t('主导航')}>
          {navItems.filter(item => !item.admin || !isSubUser).map(item => (
            <SidebarHint key={item.path} collapsed={collapsed} label={t(item.label)}>
              <Button variant={item.active ? 'secondary' : 'ghost'} onClick={() => navigate(item.path)} aria-label={t(item.label)} aria-current={item.active ? 'page' : undefined}
                className={cn('h-auto w-full gap-3 px-3 py-2.5 text-sm', collapsed ? 'justify-center' : 'justify-start')}>
                <item.icon className="w-4 h-4" />
                {!collapsed && <span>{item.label}</span>}
              </Button>
            </SidebarHint>
          ))}
        </nav>

        <div className="shrink-0 space-y-1 border-t p-2">
          <div className={collapsed ? 'space-y-1' : 'flex items-center gap-1'}>
            <SidebarHint collapsed={collapsed} label={themeLabel}>
              <Button variant="ghost" onClick={toggleTheme} aria-label={themeLabel}
                className={cn('h-auto gap-3 px-3 py-2.5 text-sm', collapsed ? 'w-full justify-center' : 'min-w-0 flex-1 justify-start')}>
                {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
                {!collapsed && <span>{theme === 'dark' ? '亮色模式' : '暗黑模式'}</span>}
              </Button>
            </SidebarHint>
            <SidebarHint collapsed={collapsed} label="Language">
              <Button variant="ghost" onClick={() => { void toggleLanguage() }} aria-label="Language"
                className={cn('h-auto justify-center gap-2 px-3 py-2.5 text-sm', collapsed ? 'w-full' : 'min-w-0 flex-1')}>
                <LanguageIcon className="h-4 w-4 shrink-0" />
                {!collapsed && <span>Language</span>}
              </Button>
            </SidebarHint>
          </div>

          {version && (
            <div className={cn('px-3 py-2 text-xs text-muted-foreground', collapsed && 'text-center')}>
              {collapsed ? (
                <SidebarHint collapsed label={`CLICD v${version}`}>
                  <Button asChild variant="link" className="h-auto p-0">
                    <a href="https://github.com/MengMengCode/CLICD" target="_blank" rel="noreferrer" aria-label={`CLICD v${version}`}>
                      <GitHubIcon className="h-4 w-4" />
                    </a>
                  </Button>
                </SidebarHint>
              ) : (
                <div className="flex min-w-0 items-center gap-2">
                  <Button asChild variant="link" className="h-auto shrink-0 gap-1 p-0 text-xs">
                    <a href="https://github.com/MengMengCode/CLICD" target="_blank" rel="noreferrer" aria-label="CLICD">
                      <GitHubIcon className="h-3.5 w-3.5 shrink-0" />
                      <span className="truncate">CLICD</span>
                    </a>
                  </Button>
                  <span className="min-w-0 truncate font-mono text-[10px]" title={`v${version}`}>v{version}</span>
                </div>
              )}
            </div>
          )}

          <SidebarHint collapsed={collapsed} label={t('退出登录')}>
            <Button variant="ghost" onClick={logout} aria-label={t('退出登录')}
              className={cn('h-auto w-full gap-3 px-3 py-2.5 text-sm', collapsed ? 'justify-center' : 'justify-start')}>
              <LogOut className="w-4 h-4" />
              {!collapsed && <span>退出登录</span>}
            </Button>
          </SidebarHint>
        </div>
      </aside>
    </TooltipProvider>
  )
}
