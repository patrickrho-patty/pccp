import { Routes, Route, Navigate } from 'react-router-dom'
import { useAuth, AuthProvider } from './hooks/useAuth'
import { ConfirmProvider } from './components/useConfirm'
import { ToastContainer } from './components/Toast'
import Login from './pages/Login'
import Bootstrap from './pages/Bootstrap'

// Layouts
import OpsLayout from './components/OpsLayout'
import CustomerLayout from './components/CustomerLayout'
import PortalLayout from './components/PortalLayout'

// Pages
import Dashboard from './pages/Dashboard'
import Users from './pages/Users'
import UserDetail from './pages/UserDetail'
import Harnesses from './pages/Harnesses'
import HarnessDetail from './pages/HarnessDetail'
import Projects from './pages/Projects'
import ProjectDetail from './pages/ProjectDetail'
import Repositories from './pages/Repositories'
import RepositoryDetail from './pages/RepositoryDetail'
import Sessions from './pages/Sessions'
import SessionDetail from './pages/SessionDetail'
import ModelDetail from './pages/ModelDetail'
import EndpointDetail from './pages/EndpointDetail'
import FindingDetail from './pages/FindingDetail'
import Policy from './pages/Policy'
import Provenance from './pages/Provenance'
import Audit from './pages/Audit'
import Fleet from './pages/Fleet'
import Security from './pages/Security'
import Compliance from './pages/Compliance'
import Tools from './pages/Tools'
import SREConsole from './pages/SREConsole'
import ModelInfra from './pages/ModelInfra'
import SubscriberManagement from './pages/SubscriberManagement'
import ServiceCommandCenter from './pages/ServiceCommandCenter'
import AccountPortal from './pages/AccountPortal'
import LiveView from './pages/LiveView'
import CodeExplorer from './pages/CodeExplorer'
import Analytics from './pages/Analytics'
import Communications from './pages/Communications'
import Sandboxes from './pages/Sandboxes'
import SandboxDetail from './pages/SandboxDetail'
import EnterpriseFeatures from './pages/EnterpriseFeatures'

export default function App() {
  return (
    <AuthProvider>
      <ConfirmProvider>
        <AppContent />
      </ConfirmProvider>
    </AuthProvider>
  )
}

function AppContent() {
  const { isAuthenticated, loading, profile } = useAuth()

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

  // Profile-aware routing
  const Layout = profile === 'patty_ops' ? OpsLayout : profile === 'portal' ? PortalLayout : CustomerLayout

  return (
    <>
    <Layout>
      <Routes>
        {/* Shared routes */}
        <Route path="/" element={profile === 'patty_ops' ? <ServiceCommandCenter /> : profile === 'portal' ? <AccountPortal /> : <Dashboard />} />

        {/* Patty Ops routes */}
        {profile === 'patty_ops' && (<>
          <Route path="/sre" element={<SREConsole />} />
          <Route path="/analytics" element={<Analytics />} />
          <Route path="/fleet" element={<Fleet />} />
		  <Route path="/live" element={<LiveView />} />
		  <Route path="/harnesses/:id" element={<HarnessDetail />} />
		  <Route path="/users/:id" element={<UserDetail />} />
          <Route path="/sessions" element={<Sessions />} />
		  <Route path="/sessions/:id" element={<SessionDetail />} />
		  <Route path="/sessions/:id/provenance" element={<Provenance />} />
          <Route path="/accounts" element={<SubscriberManagement />} />
          <Route path="/models" element={<ModelInfra />} />
		  <Route path="/models/:id" element={<ModelDetail />} />
          <Route path="/security" element={<Security />} />
          <Route path="/audit" element={<Audit />} />
        </>)}

        {/* Customer Console routes */}
        {profile === 'customer' && (<>
          <Route path="/users" element={<Users />} />
          <Route path="/users/:id" element={<UserDetail />} />
          <Route path="/harnesses" element={<Harnesses />} />
          <Route path="/harnesses/:id" element={<HarnessDetail />} />
          <Route path="/projects" element={<Projects />} />
          <Route path="/projects/:id" element={<ProjectDetail />} />
          <Route path="/repositories" element={<Repositories />} />
          <Route path="/repositories/:id" element={<RepositoryDetail />} />
          <Route path="/live" element={<LiveView />} />
          <Route path="/sessions" element={<Sessions />} />
          <Route path="/sessions/:id" element={<SessionDetail />} />
          <Route path="/sessions/:id/provenance" element={<Provenance />} />
          <Route path="/models" element={<ModelInfra />} />
          <Route path="/models/:id" element={<ModelDetail />} />
          <Route path="/endpoints/:id" element={<EndpointDetail />} />
          <Route path="/findings/:id" element={<FindingDetail />} />
          <Route path="/fleet" element={<Fleet />} />
          <Route path="/explorer" element={<CodeExplorer />} />
          <Route path="/analytics" element={<Analytics />} />
          <Route path="/policy" element={<Policy />} />
          <Route path="/security" element={<Security />} />
          <Route path="/compliance" element={<Compliance />} />
          <Route path="/tools" element={<Tools />} />
          <Route path="/communications" element={<Communications />} />
          <Route path="/sandboxes" element={<Sandboxes />} />
          <Route path="/sandboxes/:id" element={<SandboxDetail />} />
          <Route path="/enterprise" element={<EnterpriseFeatures />} />
          <Route path="/audit" element={<Audit />} />
        </>)}

        {/* Portal routes (minimal self-service) */}
        {profile === 'portal' && (<>
          <Route path="/portal" element={<AccountPortal />} />
          <Route path="/harnesses" element={<Harnesses />} />
        </>)}

        <Route path="*" element={<Navigate to="/" />} />
      </Routes>
    </Layout>
    <ToastContainer />
    </>
  )
}
