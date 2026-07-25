import { Outlet } from 'react-router'
import Sidebar from './Sidebar'
import { useState } from 'react'
import AutoTranslate from './AutoTranslate'
import BrowserDialogTranslator from './BrowserDialogTranslator'

export default function Layout() {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)

  return (
    <div className="min-h-screen bg-gray-50 flex dark:bg-gray-950">
      <AutoTranslate />
      <BrowserDialogTranslator />
      <Sidebar collapsed={sidebarCollapsed} onToggle={() => setSidebarCollapsed(!sidebarCollapsed)} />
      <main className={`min-w-0 flex-1 transition-all duration-300 ${sidebarCollapsed ? 'ml-16' : 'ml-60'}`}>
        <div className="p-6">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
