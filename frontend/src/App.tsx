import { Navigate, Outlet, Route, Routes } from "react-router-dom"
import { useAuth } from "./lib/Auth"
import Layout from "./components/Layout"
import Login from "./components/Login"
import Invite from "./components/Invite"
import Clients from "./components/Clients"
import Overview from "./components/Overview"
import Links from "./components/Links"
import Conversions from "./components/Conversions"
import Integrations from "./components/Integrations"
import Videos from "./components/Videos"
import ChannelAnalytics from "./components/ChannelAnalytics"
import NotificationChannels from "./components/NotificationChannels"

function Home() {
  const { user } = useAuth()
  if (!user) return <Navigate to="/login" replace />
  if (user.role === "client" && user.client_id) return <Navigate to={`/c/${user.client_id}/overview`} replace />
  return <Navigate to="/clients" replace />
}

function Protected({ staffOnly }: { staffOnly?: boolean }) {
  const { user } = useAuth()
  if (!user) return <Navigate to="/login" replace />
  if (staffOnly && user.role === "client") return <Navigate to="/" replace />
  return <Outlet />
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/invite/:token" element={<Invite />} />
      <Route path="/" element={<Home />} />
      <Route element={<Protected />}>
        <Route element={<Layout />}>
          <Route element={<Protected staffOnly />}>
            <Route path="/clients" element={<Clients />} />
            <Route path="/notifications" element={<NotificationChannels />} />
            <Route path="/c/:clientId/videos" element={<Videos />} />
            <Route path="/c/:clientId/channel-analytics" element={<ChannelAnalytics />} />
          </Route>
          <Route path="/c/:clientId/overview" element={<Overview />} />
          <Route path="/c/:clientId/links" element={<Links />} />
          <Route path="/c/:clientId/conversions" element={<Conversions />} />
          <Route path="/c/:clientId/integrations" element={<Integrations />} />
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}