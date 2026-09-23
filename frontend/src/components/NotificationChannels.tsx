import { useState, type FormEvent } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Bell, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { api, errMsg, type NotificationChannel } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { InfoTip } from "@/components/InfoTip"

const EVENTS: { value: string; label: string }[] = [
  { value: "purchase", label: "One-time purchase" },
  { value: "subscription_start", label: "New subscriber" },
  { value: "subscription_renewal", label: "Subscription renewal" },
  { value: "booked_call", label: "Booked call" },
  { value: "lead", label: "New lead" },
  { value: "refund", label: "Refund" },
]

const base = "/notification-channels"

export default function NotificationChannels() {
  const qc = useQueryClient()
  const key = ["notification-channels"]
  const list = useQuery({ queryKey: key, queryFn: () => api<NotificationChannel[]>(base) })
  const refresh = () => qc.invalidateQueries({ queryKey: key })

  const remove = useMutation({
    mutationFn: (id: string) => api<void>(`${base}/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      refresh()
      toast.success("Channel removed")
    },
    onError: (e) => toast.error(errMsg(e)),
  })

  const toggleActive = useMutation({
    mutationFn: (ch: NotificationChannel) =>
      api<NotificationChannel>(`${base}/${ch.id}`, {
        method: "PATCH",
        body: {
          webhook_url: ch.webhook_url,
          min_amount_cents: ch.min_amount_cents,
          events: ch.events,
          is_active: !ch.is_active,
        },
      }),
    onSuccess: refresh,
    onError: (e) => toast.error(errMsg(e)),
  })

  return (
    <div className="space-y-6">
      <div>
        <h1 className="flex items-center gap-2 text-2xl font-semibold">
          Notifications
          <InfoTip text="Get a Slack (or Discord) message whenever a sale, renewal, or other event happens across any client." />
        </h1>
        <p className="text-sm text-muted-foreground">Applies workspace-wide, across all clients.</p>
      </div>

      {list.isLoading && <Skeleton className="h-32 w-full" />}

      {!list.isLoading && list.data && list.data.length > 0 && (
        <div className="space-y-3">
          {list.data.map((ch) => (
            <Card key={ch.id} className="flex items-center justify-between gap-4 p-4">
              <div className="min-w-0 space-y-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium capitalize">{ch.kind}</span>
                  {ch.is_active ? <Badge>Active</Badge> : <Badge variant="secondary">Paused</Badge>}
                </div>
                <div className="truncate text-xs text-muted-foreground">{ch.webhook_url}</div>
                <div className="text-xs text-muted-foreground">
                  {ch.events.join(", ")} · min ${(ch.min_amount_cents / 100).toFixed(2)}
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Button variant="outline" size="sm" onClick={() => toggleActive.mutate(ch)} disabled={toggleActive.isPending}>
                  {ch.is_active ? "Pause" : "Resume"}
                </Button>
                <Button
                  variant="outline"
                  size="icon"
                  className="text-destructive"
                  aria-label="Remove"
                  onClick={() => confirm("Remove this notification channel?") && remove.mutate(ch.id)}
                  disabled={remove.isPending}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      <AddChannelCard refresh={refresh} />
    </div>
  )
}

function AddChannelCard({ refresh }: { refresh: () => void }) {
  const [webhookUrl, setWebhookUrl] = useState("")
  const [minAmount, setMinAmount] = useState("0")
  const [events, setEvents] = useState<string[]>(["purchase", "subscription_start", "subscription_renewal"])

  const create = useMutation({
    mutationFn: () =>
      api<NotificationChannel>(base, {
        body: {
          kind: "slack",
          webhook_url: webhookUrl.trim(),
          min_amount_cents: Math.round(parseFloat(minAmount || "0") * 100),
          events,
        },
      }),
    onSuccess: () => {
      setWebhookUrl("")
      setMinAmount("0")
      setEvents(["purchase", "subscription_start", "subscription_renewal"])
      refresh()
      toast.success("Notification channel added")
    },
    onError: (e) => toast.error(errMsg(e)),
  })

  const toggleEvent = (value: string) => {
    setEvents((prev) => (prev.includes(value) ? prev.filter((e) => e !== value) : [...prev, value]))
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (webhookUrl.trim() && events.length > 0) create.mutate()
  }

  return (
    <Card className="space-y-5 p-6">
      <div className="flex items-center gap-3">
        <Bell className="size-5 text-primary" />
        <div>
          <div className="font-medium">Add a Slack webhook</div>
          <div className="text-sm text-muted-foreground">Paste an Incoming Webhook URL from your Slack app.</div>
        </div>
      </div>

      <form onSubmit={submit} className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="webhook-url">Webhook URL</Label>
          <Input
            id="webhook-url"
            type="password"
            autoComplete="off"
            placeholder="https://hooks.slack.com/services/..."
            value={webhookUrl}
            onChange={(e) => setWebhookUrl(e.target.value)}
          />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="min-amount">Minimum amount ($) — 0 notifies on everything</Label>
          <Input
            id="min-amount"
            type="number"
            min="0"
            step="0.01"
            value={minAmount}
            onChange={(e) => setMinAmount(e.target.value)}
            className="max-w-40"
          />
        </div>

        <div className="space-y-1.5">
          <Label>Notify on</Label>
          <div className="grid grid-cols-2 gap-2">
            {EVENTS.map((ev) => (
              <label key={ev.value} className="flex items-center gap-2 text-sm">
                <Checkbox checked={events.includes(ev.value)} onCheckedChange={() => toggleEvent(ev.value)} />
                {ev.label}
              </label>
            ))}
          </div>
        </div>

        <Button type="submit" disabled={!webhookUrl.trim() || events.length === 0 || create.isPending}>
          Add channel
        </Button>
      </form>
    </Card>
  )
}