import { NavLink, Outlet, useLocation, useMatch, useNavigate } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import { BarChart3, Bell, Link2, LogOut, Plug, Receipt, TrendingUp, Users, Video } from "lucide-react"
import { api, type Client } from "@/lib/api"
import { useAuth } from "@/lib/Auth"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

export default function Layout() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const clientId = useMatch("/c/:clientId/*")?.params.clientId
  const isStaff = user?.role !== "client"
  const section = ["links", "conversions", "integrations", "videos", "channel-analytics"].find((s) => pathname.endsWith(`/${s}`)) ?? "overview"

  const clients = useQuery({ queryKey: ["clients"], queryFn: () => api<Client[]>("/clients"), enabled: isStaff })
  const current = useQuery({
    queryKey: ["client", clientId],
    queryFn: () => api<Client>(`/clients/${clientId}`),
    enabled: !!clientId,
  })

  const nav = [
    ...(isStaff
      ? [
          { to: "/clients", label: "Clients", icon: Users },
          { to: "/notifications", label: "Notifications", icon: Bell },
        ]
      : []),
    ...(clientId
      ? [
          { to: `/c/${clientId}/overview`, label: "Overview", icon: BarChart3 },
          { to: `/c/${clientId}/links`, label: "Links", icon: Link2 },
          { to: `/c/${clientId}/conversions`, label: "Conversions", icon: Receipt },
          ...(isStaff ? [{ to: `/c/${clientId}/videos`, label: "Videos", icon: Video }] : []),
          ...(isStaff ? [{ to: `/c/${clientId}/channel-analytics`, label: "Channel analytics", icon: TrendingUp }] : []),
          { to: `/c/${clientId}/integrations`, label: "Integrations", icon: Plug },
        ]
      : []),
  ]

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      <aside className="hidden w-56 shrink-0 flex-col border-r bg-card md:flex">
        <div className="px-5 py-5 text-lg font-semibold">
          Infopartnr <span className="text-primary">Flow</span>
        </div>
        <nav className="flex flex-col gap-1 px-3">
          {nav.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-accent hover:text-foreground",
                  isActive && "bg-accent font-medium text-primary",
                )
              }
            >
              <Icon className="size-4" />
              {label}
            </NavLink>
          ))}
        </nav>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center justify-between gap-4 border-b px-6">
          {isStaff ? (
            <Select
              value={clientId ?? null}
              onValueChange={(id) => id && navigate(`/c/${id}/${section}`)}
              items={clients.data?.map((c) => ({ value: c.id, label: c.name })) ?? []}
            >
              <SelectTrigger className="w-56">
                <SelectValue placeholder="Select a client" />
              </SelectTrigger>
              <SelectContent>
                {clients.data?.map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <div className="font-medium">{current.data?.name}</div>
          )}
          <div className="flex items-center gap-3 text-sm text-muted-foreground">
            <span className="hidden sm:inline">{user?.email}</span>
            <Button variant="ghost" size="sm" onClick={logout}>
              <LogOut className="size-4" /> Log out
            </Button>
          </div>
        </header>
        <main className="flex-1 p-6">
          <Outlet />
        </main>
      </div>
    </div>
  )
}