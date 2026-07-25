import { useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router'
import {
  ChevronLeft,
  ChevronRight,
  Code2,
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

  ShieldAlert,
  Sun,
  UserCog,
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
        if (res.data?.data?.version) {
          setVersion(res.data.data.version)
        }
      })
      .catch(() => {})
  }, [])

  const isContainerPage =
    location.pathname.startsWith('/containers') ||
    location.pathname.startsWith('/container')

  const isImagesPage = location.pathname.startsWith('/images')

  const isSnapshotsPage = location.pathname.startsWith('/snapshots')
  const isRoutingPage = location.pathname.startsWith('/routing')
  const isStoragePage = location.pathname.startsWith('/storage')
  const isAuditLogsPage = location.pathname.startsWith('/audit-logs')
  const isApiIntegrationPage = location.pathname.startsWith('/api-integration')
  const isHostReportPage = location.pathname.startsWith('/host-report')
  const isSecurityPage = location.pathname.startsWith('/security')
  const isSettingsPage = location.pathname.startsWith('/settings')

  return (
    <aside
      className={`fixed left-0 top-0 h-full bg-white border-r border-gray-200 flex flex-col transition-all duration-300 z-30 dark:bg-gray-900 dark:border-gray-700 ${
        collapsed ? 'w-16' : 'w-60'
      }`}
    >
      <div className="flex items-center justify-between h-14 px-4 border-b border-gray-200 dark:border-gray-700">
        {!collapsed && (
          <div className="flex items-center gap-2">
            <div className="w-7 h-7 flex items-center justify-center">
              <AppIcon className="w-5 h-5" />
            </div>
            <span className="font-bold text-black text-sm dark:text-white">CLICD</span>
          </div>
        )}
        {collapsed && (
          <div className="w-7 h-7 flex items-center justify-center mx-auto">
            <AppIcon className="w-5 h-5" />
          </div>
        )}
        <button
          onClick={onToggle}
          className="p-1 rounded hover:bg-gray-100 text-gray-500 dark:hover:bg-gray-800 dark:text-gray-400"
          title={t('切换侧边栏')}
        >
          {collapsed ? (
            <ChevronRight className="w-4 h-4" />
          ) : (
            <ChevronLeft className="w-4 h-4" />
          )}
        </button>
      </div>

      <nav className="flex-1 py-4 px-2 space-y-1">
        {!isSubUser && (
          <button
            onClick={() => navigate('/')}
            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
              location.pathname === '/'
                ? 'bg-black text-white dark:bg-white dark:text-black'
                : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
            }`}
          >
            <LayoutDashboard className="w-4 h-4" />
            {!collapsed && <span>控制面板</span>}
          </button>
        )}

        <button
          onClick={() => navigate('/containers')}
          className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
            isContainerPage
              ? 'bg-black text-white dark:bg-white dark:text-black'
              : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
          }`}
        >
          <Server className="w-4 h-4" />
          {!collapsed && <span>容器管理</span>}
        </button>

        {!isSubUser && (
          <button
            onClick={() => navigate('/images')}
            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
              isImagesPage
                ? 'bg-black text-white dark:bg-white dark:text-black'
                : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
            }`}
          >
            <Package className="w-4 h-4" />
            {!collapsed && <span>镜像管理</span>}
          </button>
        )}

        {!isSubUser && (
          <>
            <button
              onClick={() => navigate('/security')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
                isSecurityPage
                  ? 'bg-black text-white dark:bg-white dark:text-black'
                  : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
              }`}
            >
              <ShieldAlert className="w-4 h-4" />
              {!collapsed && <span>安全告警</span>}
            </button>

            <button
              onClick={() => navigate('/snapshots')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
                isSnapshotsPage
                  ? 'bg-black text-white dark:bg-white dark:text-black'
                  : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
              }`}
            >
              <Camera className="w-4 h-4" />
              {!collapsed && <span>快照管理</span>}
            </button>

            <button
              onClick={() => navigate('/routing')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
                isRoutingPage
                  ? 'bg-black text-white dark:bg-white dark:text-black'
                  : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
              }`}
            >
              <Route className="w-4 h-4" />
              {!collapsed && <span>路由管理</span>}
            </button>

            <button
              onClick={() => navigate('/storage')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
                isStoragePage
                  ? 'bg-black text-white dark:bg-white dark:text-black'
                  : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
              }`}
            >
              <HardDrive className="w-4 h-4" />
              {!collapsed && <span>存储管理</span>}
            </button>

            <button
              onClick={() => navigate('/audit-logs')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
                isAuditLogsPage
                  ? 'bg-black text-white dark:bg-white dark:text-black'
                  : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
              }`}
            >
              <ScrollText className="w-4 h-4" />
              {!collapsed && <span>操作日志</span>}
            </button>

            <button
              onClick={() => navigate('/sub-users')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
                location.pathname.startsWith('/sub-users')
                  ? 'bg-black text-white dark:bg-white dark:text-black'
                  : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
              }`}
            >
              <UserCog className="w-4 h-4" />
              {!collapsed && <span>子用户管理</span>}
            </button>

            <button
              onClick={() => navigate('/api-integration')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
                isApiIntegrationPage
                  ? 'bg-black text-white dark:bg-white dark:text-black'
                  : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
              }`}
            >
              <Code2 className="w-4 h-4" />
              {!collapsed && <span>API 集成</span>}
            </button>

            <button
              onClick={() => navigate('/host-report')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
                isHostReportPage
                  ? 'bg-black text-white dark:bg-white dark:text-black'
                  : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
              }`}
            >
              <Cpu className="w-4 h-4" />
              {!collapsed && <span>宿主机信息</span>}
            </button>

            <button
              onClick={() => navigate('/settings')}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors ${
                isSettingsPage
                  ? 'bg-black text-white dark:bg-white dark:text-black'
                  : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
              }`}
            >
              <UserCog className="w-4 h-4" />
              {!collapsed && <span>面板设置</span>}
            </button>
          </>
        )}
      </nav>

      <div className="border-t border-gray-200 dark:border-gray-700 p-2 space-y-1">
        {/* Theme Toggle */}
        <div className={collapsed ? 'space-y-1' : 'flex items-center gap-1'}>
          <button
            onClick={toggleTheme}
            className={`${collapsed ? 'w-full justify-center' : 'flex-1'} flex items-center gap-3 px-3 py-2.5 rounded-md text-sm text-gray-600 hover:bg-gray-100 transition-colors dark:text-gray-400 dark:hover:bg-gray-800`}
            title={t(theme === 'dark' ? '切换亮色模式' : '切换暗黑模式')}
          >
            {theme === 'dark' ? (
              <Sun className="w-4 h-4" />
            ) : (
              <Moon className="w-4 h-4" />
            )}
            {!collapsed && <span>{theme === 'dark' ? '亮色模式' : '暗黑模式'}</span>}
          </button>

          <button
            onClick={() => { void toggleLanguage() }}
            className={`${collapsed ? 'w-full justify-center' : 'flex-1 justify-center'} flex items-center gap-2 rounded-md px-3 py-2.5 text-sm text-gray-600 hover:bg-gray-100 transition-colors dark:text-gray-400 dark:hover:bg-gray-800`}
            title="Language"
          >
            <LanguageIcon className="h-4 w-4 shrink-0" />
            {!collapsed && <span>Language</span>}
          </button>
        </div>

        {/* Version */}
        {version && (
          <div className={`px-3 py-2 text-xs text-gray-400 dark:text-gray-500 ${collapsed ? 'text-center' : ''}`}>
            {collapsed ? (
              <a
                href="https://github.com/MengMengCode/CLICD"
                target="_blank"
                rel="noreferrer"
                title={`CLICD v${version}`}
                className="inline-flex items-center justify-center rounded text-gray-400 transition-colors hover:text-gray-900 dark:text-gray-500 dark:hover:text-white"
              >
                <GitHubIcon className="h-4 w-4" />
              </a>
            ) : (
              <div className="flex min-w-0 items-center gap-2">
                <a
                  href="https://github.com/MengMengCode/CLICD"
                  target="_blank"
                  rel="noreferrer"
                  title="CLICD"
                  className="inline-flex min-w-0 items-center gap-1 rounded text-gray-500 transition-colors hover:text-gray-950 dark:text-gray-400 dark:hover:text-white"
                >
                  <GitHubIcon className="h-3.5 w-3.5 shrink-0" />
                  <span className="truncate">CLICD</span>
                </a>
                <span className="shrink-0">v{version}</span>
              </div>
            )}
          </div>
        )}

        {/* Logout */}
        <button
          onClick={logout}
          className="w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-sm text-gray-600 hover:bg-gray-100 transition-colors dark:text-gray-400 dark:hover:bg-gray-800"
        >
          <LogOut className="w-4 h-4" />
          {!collapsed && <span>退出登录</span>}
        </button>
      </div>
    </aside>
  )
}
