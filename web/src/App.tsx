import { Routes, Route, Navigate } from 'react-router-dom'
import { useAuth, AuthProvider } from './hooks/useAuth'
import Login from './pages/Login'
import Layout from './components/Layout'
import Dashboard from './pages/Dashboard'
import Users from './pages/Users'
import Harnesses from './pages/Harnesses'
import Projects from './pages/Projects'
import Repositories from './pages/Repositories'
import Sessions from './pages/Sessions'
import Models from './pages/Models'
import Endpoints from './pages/Endpoints'
import Policy from './pages/Policy'
import Provenance from './pages/Provenance'
import Audit from './pages/Audit'
import Fleet from './pages/Fleet'
import Security from './pages/Security'
import Compliance from './pages/Compliance'
import Tools from './pages/Tools'
import ModelCatalog from './pages/ModelCatalog'
import SREConsole from './pages/SREConsole'
import AccountPortal from './pages/AccountPortal'
import LiveView from './pages/LiveView'
import CodeExplorer from './pages/CodeExplorer'
import Analytics from './pages/Analytics'
import Communications from './pages/Communications'
import Sandboxes from './pages/Sandboxes'
import Bootstrap from './pages/Bootstrap'

export default function App() {
  return (
    <AuthProvider>
      <AppContent />
    </AuthProvider>
  )
}

function AppContent() {
  const { isAuthenticated, loading } = useAuth()

  if (loading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <div className="text-gray-500">로딩 중...</div>
      </div>
    )
  }

  if (!isAuthenticated) {
    return (
      <Routes>
        <Route path="/bootstrap" element={<Bootstrap />} />
        <Route path="*" element={<Login />} />
      </Routes>
    )
  }

  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/users" element={<Users />} />
        <Route path="/harnesses" element={<Harnesses />} />
        <Route path="/projects" element={<Projects />} />
        <Route path="/repositories" element={<Repositories />} />
        <Route path="/sessions" element={<Sessions />} />
        <Route path="/sessions/:id/provenance" element={<Provenance />} />
        <Route path="/models" element={<Models />} />
        <Route path="/endpoints" element={<Endpoints />} />
        <Route path="/policy" element={<Policy />} />
        <Route path="/audit" element={<Audit />} />
        <Route path="/fleet" element={<Fleet />} />
        <Route path="/security" element={<Security />} />
        <Route path="/compliance" element={<Compliance />} />
        <Route path="/tools" element={<Tools />} />
        <Route path="/catalog" element={<ModelCatalog />} />
        <Route path="/sre" element={<SREConsole />} />
        <Route path="/portal" element={<AccountPortal />} />
        <Route path="/live" element={<LiveView />} />
        <Route path="/explorer" element={<CodeExplorer />} />
        <Route path="/analytics" element={<Analytics />} />
        <Route path="/communications" element={<Communications />} />
          <Route path="/sandboxes" element={<Sandboxes />} />
        <Route path="*" element={<Navigate to="/" />} />
      </Routes>
    </Layout>
  )
}
