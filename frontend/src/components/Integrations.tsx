import { useEffect, useState, type FormEvent } from "react"
import { useNavigate, useParams, useSearchParams } from "react-router-dom"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { BookOpen, Copy, CreditCard, FileText, PhoneCall } from "lucide-react"
import { toast } from "sonner"
import { api, errMsg, type Integration, type YoutubeChannel } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { InfoTip } from "@/components/InfoTip"

const STRIPE_EVENTS = [
  "checkout.session.completed",
  "checkout.session.async_payment_succeeded",
  "invoice.payment_succeeded",
  "refund.created",
  "customer.subscription.deleted",
]

function YoutubeIcon(props: React.SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...props}>
      <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
    </svg>
  )
}

function genSecret(bytes = 24) {
  const arr = new Uint8Array(bytes)
  crypto.getRandomValues(arr)
  return Array.from(arr, (b) => b.toString(16).padStart(2, "0")).join("")
}

function CopyRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate rounded-md border bg-muted px-3 py-2 text-xs">{value}</code>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={`Copy ${label}`}
          onClick={() => {
            navigator.clipboard.writeText(value)
            toast.success("Copied")
          }}
        >
          <Copy className="size-4" />
        </Button>
      </div>
    </div>
  )
}

function StatusBadge({ loading, active }: { loading: boolean; active: "yes" | "no" | "partial" }) {
  if (loading) return <Skeleton className="h-6 w-24" />
  if (active === "yes") return <Badge>Active</Badge>
  if (active === "partial") return <Badge variant="secondary">Needs signing secret</Badge>
  return <Badge variant="secondary">Not connected</Badge>
}

export default function Integrations() {
  const { clientId } = useParams()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const key = ["integrations", clientId]
  const base = `/clients/${clientId}/integrations`

  const list = useQuery({ queryKey: key, queryFn: () => api<Integration[]>(base) })
  const refresh = () => qc.invalidateQueries({ queryKey: key })

  const stripe = list.data?.find((i) => i.provider === "stripe")
  const typeform = list.data?.find((i) => i.provider === "typeform")
  const calendly = list.data?.find((i) => i.provider === "calendly")

  const remove = useMutation({
    mutationFn: (id: string) => api<void>(`${base}/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      refresh()
      toast.success("Disconnected")
    },
    onError: (e) => toast.error(errMsg(e)),
  })

  // The YouTube connect flow leaves the SPA entirely (full browser redirect
  // to Google and back), so it reports back via a query param instead of
  // a mutation's onSuccess.
  const [searchParams, setSearchParams] = useSearchParams()
  useEffect(() => {
    const yt = searchParams.get("youtube")
    if (!yt) return
    if (yt === "connected") toast.success("YouTube channel connected")
    else if (yt === "denied") toast.error("YouTube connection was cancelled")
    else toast.error("Couldn't connect YouTube — try again")
    setSearchParams(
      (p) => {
        p.delete("youtube")
        return p
      },
      { replace: true },
    )
  }, [searchParams, setSearchParams])

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            Integrations
            <InfoTip text="Connect the tools this client uses to get paid, book calls and capture leads. Each one tells us when something happens, and we match it back to the video link the person clicked." />
          </h1>
          <p className="text-sm text-muted-foreground">Connect payments, calls, forms and YouTube itself.</p>
        </div>
        <Button variant="outline" onClick={() => navigate(`/c/${clientId}/integrations/guide`)}>
          <BookOpen className="size-4" />
          Setup guide
        </Button>
      </div>

      <YouTubeCard base={`/clients/${clientId}/youtube`} />
      <StripeCard base={base} stripe={stripe} loading={list.isLoading} isError={list.isError} error={list.error} refresh={refresh} remove={remove} />
      <TypeformCard base={base} typeform={typeform} loading={list.isLoading} refresh={refresh} remove={remove} />
      <CalendlyCard base={base} calendly={calendly} loading={list.isLoading} refresh={refresh} remove={remove} />
    </div>
  )
}

type RemoveMutation = ReturnType<typeof useMutation<void, unknown, string>>

// ===== YouTube =====

function YouTubeCard({ base }: { base: string }) {
  const key = ["youtube-channels", base]
  const qc = useQueryClient()
  const channels = useQuery({ queryKey: key, queryFn: () => api<YoutubeChannel[]>(`${base}/channels`) })
  const refresh = () => qc.invalidateQueries({ queryKey: key })

  const connect = useMutation({
    mutationFn: () => api<{ auth_url: string }>(`${base}/connect`, { method: "POST" }),
    onSuccess: (d) => {
      window.location.href = d.auth_url // full navigation to Google -- leaves the SPA
    },
    onError: (e) => toast.error(errMsg(e)),
  })
  const disconnect = useMutation({
    mutationFn: (id: string) => api<void>(`${base}/channels/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      refresh()
      toast.success("Channel disconnected")
    },
    onError: (e) => toast.error(errMsg(e)),
  })

  const connected = channels.data?.filter((c) => c.status === "connected") ?? []

  return (
    <Card className="space-y-5 p-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <YoutubeIcon className="size-5 text-primary" />
          <div>
            <div className="font-medium">YouTube</div>
            <div className="text-sm text-muted-foreground">Syncs videos and writes the tracked link into descriptions.</div>
          </div>
        </div>
        <StatusBadge loading={channels.isLoading} active={connected.length > 0 ? "yes" : "no"} />
      </div>

      {!channels.isLoading && connected.length === 0 && (
        <div className="space-y-3">
          <p className="text-sm text-muted-foreground">
            You'll be sent to Google to sign in and approve access, then brought back here.
          </p>
          <Button onClick={() => connect.mutate()} disabled={connect.isPending}>
            Connect YouTube
          </Button>
        </div>
      )}

      {connected.length > 0 && (
        <div className="space-y-3">
          {connected.map((ch) => (
            <div key={ch.id} className="flex items-center justify-between gap-3 rounded-md border p-3">
              <div className="flex min-w-0 items-center gap-3">
                {ch.thumbnail_url && <img src={ch.thumbnail_url} alt="" className="size-8 shrink-0 rounded-full" />}
                <div className="truncate text-sm font-medium">{ch.title || ch.google_channel_id}</div>
              </div>
              <Button
                variant="outline"
                size="sm"
                className="shrink-0 text-destructive"
                onClick={() => confirm(`Disconnect ${ch.title || "this channel"}? Syncing and description updates will stop.`) && disconnect.mutate(ch.id)}
                disabled={disconnect.isPending}
              >
                Disconnect
              </Button>
            </div>
          ))}
          <Button variant="outline" onClick={() => connect.mutate()} disabled={connect.isPending}>
            Connect another channel
          </Button>
        </div>
      )}
    </Card>
  )
}

// ===== Stripe (unchanged from before) =====

function StripeCard({
  base,
  stripe,
  loading,
  isError,
  error,
  refresh,
  remove,
}: {
  base: string
  stripe?: Integration
  loading: boolean
  isError: boolean
  error: unknown
  refresh: () => void
  remove: RemoveMutation
}) {
  const [secret, setSecret] = useState("")

  const connect = useMutation({
    mutationFn: () => api<Integration>(base, { body: { provider: "stripe" } }),
    onSuccess: () => {
      refresh()
      toast.success("Stripe integration created")
    },
    onError: (e) => toast.error(errMsg(e)),
  })
  const saveSecret = useMutation({
    mutationFn: (id: string) => api<void>(`${base}/${id}/secret`, { method: "PUT", body: { signing_secret: secret.trim() } }),
    onSuccess: () => {
      setSecret("")
      refresh()
      toast.success("Signing secret saved")
    },
    onError: (e) => toast.error(errMsg(e)),
  })

  const submitSecret = (e: FormEvent) => {
    e.preventDefault()
    if (stripe) saveSecret.mutate(stripe.id)
  }

  return (
    <Card className="space-y-5 p-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <CreditCard className="size-5 text-primary" />
          <div>
            <div className="font-medium">Stripe</div>
            <div className="text-sm text-muted-foreground">Payments, subscriptions and refunds.</div>
          </div>
        </div>
        <StatusBadge loading={loading} active={!stripe ? "no" : stripe.has_secret ? "yes" : "partial"} />
      </div>

      {isError && <p className="text-sm text-destructive">{errMsg(error)}</p>}

      {!loading && !stripe && (
        <Button onClick={() => connect.mutate()} disabled={connect.isPending}>
          Connect Stripe
        </Button>
      )}

      {stripe && (
        <div className="space-y-5">
          <div className="space-y-3 rounded-md border p-4">
            <div className="text-sm font-medium">1. Add a webhook in Stripe</div>
            <p className="text-sm text-muted-foreground">
              Stripe Dashboard → Developers → Webhooks → Add endpoint. Paste this URL and select the events below.
            </p>
            <CopyRow label="Webhook URL" value={stripe.webhook_url} />
            <CopyRow label="Events to send" value={STRIPE_EVENTS.join(",")} />
          </div>

          <form onSubmit={submitSecret} className="space-y-3 rounded-md border p-4">
            <div className="text-sm font-medium">2. Paste the signing secret</div>
            <p className="text-sm text-muted-foreground">
              After saving the endpoint, Stripe shows a signing secret starting with <code>whsec_</code>. It is stored encrypted and never shown again.
            </p>
            <div className="flex gap-2">
              <Input
                type="password"
                autoComplete="off"
                placeholder={stripe.has_secret ? "Saved. Paste a new one to replace it" : "whsec_..."}
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
              />
              <Button type="submit" disabled={!secret.trim() || saveSecret.isPending}>
                Save
              </Button>
            </div>
          </form>

          <div className="space-y-1 text-sm text-muted-foreground">
            <div className="font-medium text-foreground">Tracking tips</div>
            <p>Stripe Payment Links (buy.stripe.com) are tagged automatically by track.js on the client's site.</p>
            <p>For custom Checkout Sessions, set client_reference_id to the visitor's trakyo_id when creating the session.</p>
          </div>

          <Button
            variant="outline"
            className="text-destructive"
            onClick={() => confirm("Disconnect Stripe? New sales will stop being tracked.") && remove.mutate(stripe.id)}
            disabled={remove.isPending}
          >
            Disconnect
          </Button>
        </div>
      )}
    </Card>
  )
}

// ===== Typeform =====

function TypeformCard({
  base,
  typeform,
  loading,
  refresh,
  remove,
}: {
  base: string
  typeform?: Integration
  loading: boolean
  refresh: () => void
  remove: RemoveMutation
}) {
  const [generated, setGenerated] = useState<string | null>(null)

  const connect = useMutation({
    mutationFn: (signing_secret: string) => api<Integration>(base, { body: { provider: "typeform", signing_secret } }),
    onSuccess: () => {
      refresh()
      toast.success("Typeform integration created")
    },
    onError: (e) => {
      setGenerated(null)
      toast.error(errMsg(e))
    },
  })

  const handleConnect = () => {
    const secret = genSecret()
    setGenerated(secret)
    connect.mutate(secret)
  }

  return (
    <Card className="space-y-5 p-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <FileText className="size-5 text-primary" />
          <div>
            <div className="font-medium">Typeform</div>
            <div className="text-sm text-muted-foreground">Form submissions become leads.</div>
          </div>
        </div>
        <StatusBadge loading={loading} active={typeform ? "yes" : "no"} />
      </div>

      {!loading && !typeform && (
        <Button onClick={handleConnect} disabled={connect.isPending}>
          Connect Typeform
        </Button>
      )}

      {typeform && (
        <div className="space-y-5">
          {generated && (
            <div className="space-y-3 rounded-md border border-amber-300 bg-amber-50 p-4 dark:border-amber-900 dark:bg-amber-950">
              <div className="text-sm font-medium">Copy this secret now — it won't be shown again</div>
              <CopyRow label="Webhook secret" value={generated} />
            </div>
          )}

          <div className="space-y-3 rounded-md border p-4">
            <div className="text-sm font-medium">Set up the webhook in Typeform</div>
            <p className="text-sm text-muted-foreground">
              In your form's Connect panel, add a Webhook pointing at this URL{generated ? ", using the secret above as the webhook secret" : ""}.
            </p>
            <CopyRow label="Webhook URL" value={typeform.webhook_url} />
          </div>

          <div className="space-y-1 text-sm text-muted-foreground">
            <div className="font-medium text-foreground">Setup requirements</div>
            <p>Add a URL parameter named <code>trakyo_id</code> to the form — track.js fills it in automatically.</p>
            <p>Add an Email-type question so the submission includes an email answer.</p>
          </div>

          <Button
            variant="outline"
            className="text-destructive"
            onClick={() => confirm("Disconnect Typeform? New submissions will stop being tracked.") && remove.mutate(typeform.id)}
            disabled={remove.isPending}
          >
            Disconnect
          </Button>
        </div>
      )}
    </Card>
  )
}

// ===== Calendly =====

function CalendlyCard({
  base,
  calendly,
  loading,
  refresh,
  remove,
}: {
  base: string
  calendly?: Integration
  loading: boolean
  refresh: () => void
  remove: RemoveMutation
}) {
  const [token, setToken] = useState("")

  const connect = useMutation({
    mutationFn: () => api<Integration>(base, { body: { provider: "calendly", personal_access_token: token.trim() } }),
    onSuccess: () => {
      setToken("")
      refresh()
      toast.success("Calendly connected")
    },
    onError: (e) => toast.error(errMsg(e)),
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (token.trim()) connect.mutate()
  }

  return (
    <Card className="space-y-5 p-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <PhoneCall className="size-5 text-primary" />
          <div>
            <div className="font-medium">Calendly</div>
            <div className="text-sm text-muted-foreground">Booked calls, attributed automatically.</div>
          </div>
        </div>
        <StatusBadge loading={loading} active={calendly ? "yes" : "no"} />
      </div>

      {!loading && !calendly && (
        <form onSubmit={submit} className="space-y-3">
          <p className="text-sm text-muted-foreground">
            Paste an org admin's Calendly personal access token. We use it once to register the webhook and generate our own
            signing key — the token itself is never stored. Requires a paid Calendly plan and a publicly reachable server
            (Calendly rejects localhost; use ngrok or deploy first).
          </p>
          <Label htmlFor="calendly-token">Personal access token</Label>
          <div className="flex gap-2">
            <Input
              id="calendly-token"
              type="password"
              autoComplete="off"
              placeholder="eyJraWQ..."
              value={token}
              onChange={(e) => setToken(e.target.value)}
            />
            <Button type="submit" disabled={!token.trim() || connect.isPending}>
              Connect
            </Button>
          </div>
        </form>
      )}

      {calendly && (
        <div className="space-y-5">
          <div className="space-y-1 text-sm text-muted-foreground">
            <div className="font-medium text-foreground">How attribution works</div>
            <p>track.js tags Calendly links and embeds with the visitor's trakyo_id, mirrored into utm_content as a safety net.</p>
            <p>Rescheduled bookings update the original booked call instead of creating a duplicate.</p>
          </div>

          <Button
            variant="outline"
            className="text-destructive"
            onClick={() =>
              confirm(
                "Disconnect Calendly? This removes it here, but the webhook subscription stays on Calendly's side until it eventually gets disabled from failed deliveries.",
              ) && remove.mutate(calendly.id)
            }
            disabled={remove.isPending}
          >
            Disconnect
          </Button>
        </div>
      )}
    </Card>
  )
}