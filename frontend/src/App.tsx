import { Routes, Route, Navigate } from 'react-router'
import { useAuth } from './contexts/AuthContext'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Containers from './pages/Containers'
import ContainerDetail from './pages/ContainerDetail'

import Security from './pages/Security'
import AuditLogs from './pages/AuditLogs'
import ApiIntegration from './pages/ApiIntegration'
import HostReport from './pages/HostReport'
import Settings from './pages/Settings'
import ImageManagement from './pages/ImageManagement'
import Snapshots from './pages/Snapshots'
import Routing from './pages/Routing'
import Storage from './pages/Storage'
import SubUserManagement from './pages/SubUserManagement'
import Layout from './components/Layout'

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, isLoading } = useAuth()

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-white dark:bg-gray-950">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-black dark:border-white"></div>
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}

function HomeRoute() {
  const { isSubUser, containerIdentifiers } = useAuth()
  if (isSubUser) {
    const firstContainer = containerIdentifiers[0]
    return <Navigate to={firstContainer ? `/container/${encodeURIComponent(firstContainer)}` : '/containers'} replace />
  }
  return <Dashboard />
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={
          <ProtectedRoute>
            <Layout />
          </ProtectedRoute>
        }
      >
        <Route index element={<HomeRoute />} />
        <Route path="containers" element={<Containers />} />
        <Route path="images" element={<ImageManagement />} />
        <Route path="container/:id" element={<ContainerDetail />} />

        <Route path="security" element={<Security />} />
        <Route path="snapshots" element={<Snapshots />} />
        <Route path="routing" element={<Routing />} />
        <Route path="storage" element={<Storage />} />
        <Route path="audit-logs" element={<AuditLogs />} />
        <Route path="api-integration" element={<ApiIntegration />} />
        <Route path="host-report" element={<HostReport />} />
        <Route path="sub-users" element={<SubUserManagement />} />
        <Route path="settings" element={<Settings />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

export default App
