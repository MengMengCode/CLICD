import { Outlet } from 'react-router'
import Sidebar from './Sidebar'
import { useEffect, useState } from 'react'
import AutoTranslate from './AutoTranslate'
import BrowserDialogTranslator from './BrowserDialogTranslator'

const compactSidebarQuery = '(max-width: 1023px)'

export default function Layout() {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => window.matchMedia(compactSidebarQuery).matches)

  useEffect(() => {
    const query = window.matchMedia(compactSidebarQuery)
    const collapseOnNarrowScreen = () => {
      if (query.matches) setSidebarCollapsed(true)
    }
    query.addEventListener('change', collapseOnNarrowScreen)
    return () => query.removeEventListener('change', collapseOnNarrowScreen)
  }, [])

  return (
    <div className="min-h-screen bg-muted/40 text-foreground flex">
      <AutoTranslate />
      <BrowserDialogTranslator />
      <Sidebar collapsed={sidebarCollapsed} onToggle={() => setSidebarCollapsed(collapsed => !collapsed)} />
      <main className={`min-w-0 flex-1 transition-all duration-300 ${sidebarCollapsed ? 'ml-16' : 'ml-60'}`}>
        <div className="p-4 sm:p-6">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
