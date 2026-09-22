import { Fragment, useState, type FormEvent } from "react"
import { useParams } from "react-router-dom"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts"
import { ChevronDown, ChevronRight, Plus, RefreshCw, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { api, errMsg, type Video, type VideoAnalytics, type VideoCost, type YoutubeChannel, type YoutubeSyncResponse } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"
import { InfoTip } from "@/components/InfoTip"
import { StatCard } from "@/components/StatCard"

function centsToMoney(cents: number, currency = "USD") {
  return new Intl.NumberFormat("en-US", { style: "currency", currency }).format(cents / 100)
}
const fmtNum = (n: number) => n.toLocaleString()

const KIND_OPTIONS = [
  { value: "production", label: "Production" },
  { value: "ad_spend", label: "Ad spend" },
  { value: "other", label: "Other" },
]

const rangeItems = [
  { value: "7", label: "Last 7 days" },
  { value: "30", label: "Last 30 days" },
  { value: "90", label: "Last 90 days" },
]

const viewsChartConfig = { views: { label: "Views", color: "var(--chart-1)" } } satisfies ChartConfig

const fmtDay = (v: string) =>
  new Date(v + "T00:00:00Z").toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" })

interface CostForm {
  kind: string
  amount: string
  note: string
  incurredOn: string
}
const emptyCostForm: CostForm = { kind: "production", amount: "", note: "", incurredOn: "" }

export default function Videos() {
  const { clientId } = useParams()
  const qc = useQueryClient()
  const key = ["videos", clientId]
  const base = `/clients/${clientId}/videos`

  const videos = useQuery({ queryKey: key, queryFn: () => api<Video[]>(base) })
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const channelsKey = ["youtube-channels", clientId]
  const channels = useQuery({
    queryKey: channelsKey,
    queryFn: () => api<YoutubeChannel[]>(`/clients/${clientId}/youtube/channels`),
  })

  const sync = useMutation({
    mutationFn: () => api<YoutubeSyncResponse>(`/clients/${clientId}/youtube/sync`, { method: "POST" }),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: key })
      qc.invalidateQueries({ queryKey: channelsKey })
      if (res.failed.length === 0) {
        toast.success(`Synced ${res.synced.length} channel${res.synced.length === 1 ? "" : "s"}`)
      } else {
        toast.error(
          `${res.failed.length} channel${res.failed.length === 1 ? "" : "s"} failed to sync` +
            (res.synced.length ? ` (${res.synced.length} succeeded)` : ""),
        )
      }
    },
    onError: (e) => toast.error(errMsg(e)),
  })

  const lastSyncedAt = channels.data?.reduce<string | null>((latest, c) => {
    if (!c.last_synced_at) return latest
    if (!latest || c.last_synced_at > latest) return c.last_synced_at
    return latest
  }, null)

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            Videos
            <InfoTip text="Click a video to see its views, funnel performance and costs. Log what each video cost to produce or promote here -- it feeds the ROI numbers on this page and on Overview." />
          </h1>
          <p className="text-sm text-muted-foreground">Performance, revenue and cost, per video.</p>
        </div>
        <div className="flex flex-col items-end gap-1">
          <Button variant="outline" size="sm" onClick={() => sync.mutate()} disabled={sync.isPending}>
            <RefreshCw className={`size-4 ${sync.isPending ? "animate-spin" : ""}`} />
            {sync.isPending ? "Syncing…" : "Refresh from YouTube"}
          </Button>
          {lastSyncedAt && (
            <span className="text-xs text-muted-foreground">
              Last synced {new Date(lastSyncedAt).toLocaleString()}
            </span>
          )}
        </div>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-8" />
              <TableHead>Video</TableHead>
              <TableHead>Published</TableHead>
              <TableHead>Entries</TableHead>
              <TableHead>Total cost</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {videos.isLoading && (
              <TableRow>
                <TableCell colSpan={5}>
                  <Skeleton className="h-6 w-full" />
                </TableCell>
              </TableRow>
            )}
            {videos.isError && (
              <TableRow>
                <TableCell colSpan={5} className="text-destructive">
                  {errMsg(videos.error)}
                </TableCell>
              </TableRow>
            )}
            {videos.data?.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="py-8 text-center text-muted-foreground">
                  No videos yet. They'll show up here once a YouTube channel is connected.
                </TableCell>
              </TableRow>
            )}
            {videos.data?.map((v) => {
              const expanded = expandedId === v.id
              return (
                <Fragment key={v.id}>
                  <TableRow className="cursor-pointer" onClick={() => setExpandedId(expanded ? null : v.id)}>
                    <TableCell>
                      {expanded ? (
                        <ChevronDown className="size-4 text-muted-foreground" />
                      ) : (
                        <ChevronRight className="size-4 text-muted-foreground" />
                      )}
                    </TableCell>
                    <TableCell className="max-w-64 truncate font-medium" title={v.title || v.youtube_video_id}>
                      {v.title || v.youtube_video_id}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {v.published_at ? new Date(v.published_at).toLocaleDateString() : "—"}
                    </TableCell>
                    <TableCell>
                      <Badge variant="secondary">{v.cost_count}</Badge>
                    </TableCell>
                    <TableCell>{centsToMoney(v.total_cost_cents)}</TableCell>
                  </TableRow>
                  {expanded && (
                    <TableRow>
                      <TableCell colSpan={5} className="bg-muted/30 p-0">
                        <VideoExpansion
                          base={base}
                          video={v}
                          onCostsChanged={() => qc.invalidateQueries({ queryKey: key })}
                        />
                      </TableCell>
                    </TableRow>
                  )}
                </Fragment>
              )
            })}
          </TableBody>
        </Table>
      </Card>
    </div>
  )
}

function VideoExpansion({
  base,
  video,
  onCostsChanged,
}: {
  base: string
  video: Video
  onCostsChanged: () => void
}) {
  const [days, setDays] = useState("90")
  const videoBase = `${base}/${video.id}`

  const analytics = useQuery({
    queryKey: ["video-analytics", video.id, days],
    queryFn: () => api<VideoAnalytics>(`${videoBase}/analytics?days=${days}`),
  })

  const cost = video.total_cost_cents
  const revenue = analytics.data?.revenue_cents ?? 0
  const roi = analytics.data && cost > 0 ? ((revenue - cost) / cost) * 100 : null

  return (
    <div className="space-y-4 p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-muted-foreground">Performance</h3>
        <Select value={days} onValueChange={(v) => v && setDays(v)} items={rangeItems}>
          <SelectTrigger className="h-8 w-36 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="7">Last 7 days</SelectItem>
            <SelectItem value="30">Last 30 days</SelectItem>
            <SelectItem value="90">Last 90 days</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <StatCard
          title="Clicks"
          tip="How many times people clicked this video's tracked links. Bots are excluded."
          value={analytics.data && fmtNum(analytics.data.clicks)}
          loading={analytics.isLoading}
        />
        <StatCard
          title="Conversions"
          tip="Purchases, booked calls and form submissions traced back to a click on this video."
          value={analytics.data && fmtNum(analytics.data.conversions)}
          loading={analytics.isLoading}
        />
        <StatCard
          title="Revenue"
          tip="Money received from customers who came through this video's tracked links, in the selected period."
          value={analytics.data && centsToMoney(revenue)}
          loading={analytics.isLoading}
        />
        <StatCard title="Cost" tip="Lifetime production + ad spend logged for this video." value={centsToMoney(cost)} />
        <StatCard
          title="ROI"
          tip="Return on investment = (revenue - lifetime cost) / lifetime cost, for the selected period's revenue."
          value={analytics.data && (roi === null ? "-" : `${roi.toFixed(0)}%`)}
          sub={analytics.data && roi === null ? "Add costs to see ROI" : undefined}
          loading={analytics.isLoading}
        />
      </div>

      <Card>
        <CardHeader className="flex-row items-center gap-2 space-y-0 py-3">
          <CardTitle className="text-sm">Views over time</CardTitle>
        </CardHeader>
        <CardContent>
          <ChartContainer config={viewsChartConfig} className="h-56 w-full">
            <AreaChart data={analytics.data?.daily ?? []} margin={{ left: 4, right: 4 }}>
              <CartesianGrid vertical={false} />
              <XAxis dataKey="date" tickLine={false} axisLine={false} tickMargin={8} minTickGap={32} tickFormatter={fmtDay} />
              <ChartTooltip content={<ChartTooltipContent labelFormatter={(v) => fmtDay(String(v))} />} />
              <Area dataKey="views" type="monotone" stroke="var(--color-views)" fill="var(--color-views)" fillOpacity={0.15} />
            </AreaChart>
          </ChartContainer>
        </CardContent>
      </Card>

      <VideoCosts videoBase={videoBase} onChanged={onCostsChanged} />
    </div>
  )
}

function VideoCosts({ videoBase, onChanged }: { videoBase: string; onChanged: () => void }) {
  const costsBase = `${videoBase}/costs`
  const key = ["video-costs", videoBase]
  const qc = useQueryClient()
  const [form, setForm] = useState<CostForm>(emptyCostForm)

  const costs = useQuery({ queryKey: key, queryFn: () => api<VideoCost[]>(costsBase) })

  const refresh = () => {
    qc.invalidateQueries({ queryKey: key })
    onChanged()
  }

  const add = useMutation({
    mutationFn: () =>
      api<VideoCost>(costsBase, {
        body: {
          kind: form.kind,
          amount_cents: Math.round(Number(form.amount) * 100),
          note: form.note.trim(),
          ...(form.incurredOn ? { incurred_on: form.incurredOn } : {}),
        },
      }),
    onSuccess: () => {
      setForm(emptyCostForm)
      refresh()
      toast.success("Cost added")
    },
    onError: (e) => toast.error(errMsg(e)),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api<void>(`${costsBase}/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      refresh()
      toast.success("Cost removed")
    },
    onError: (e) => toast.error(errMsg(e)),
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const n = Number(form.amount)
    if (!n || n <= 0) {
      toast.error("Enter an amount greater than 0")
      return
    }
    add.mutate()
  }

  return (
    <Card>
      <CardHeader className="py-3">
        <CardTitle className="text-sm">Costs</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="max-h-64 space-y-2 overflow-y-auto">
          {costs.isLoading && <Skeleton className="h-6 w-full" />}
          {costs.data?.length === 0 && <p className="text-sm text-muted-foreground">No costs logged yet.</p>}
          {costs.data?.map((c) => (
            <div key={c.id} className="flex items-center justify-between gap-2 rounded-md border p-2.5 text-sm">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <Badge variant="secondary">{KIND_OPTIONS.find((k) => k.value === c.kind)?.label ?? c.kind}</Badge>
                  <span className="font-medium">{centsToMoney(c.amount_cents)}</span>
                  <span className="text-muted-foreground">{c.incurred_on}</span>
                </div>
                {c.note && <div className="truncate text-muted-foreground">{c.note}</div>}
              </div>
              <Button
                variant="ghost"
                size="icon"
                aria-label="Delete cost"
                onClick={() => remove.mutate(c.id)}
                disabled={remove.isPending}
              >
                <Trash2 className="size-4 text-destructive" />
              </Button>
            </div>
          ))}
        </div>

        <form onSubmit={submit} className="space-y-3 border-t pt-4">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label>Kind</Label>
              <Select
                items={KIND_OPTIONS}
                value={form.kind}
                onValueChange={(v) => v && setForm({ ...form, kind: v })}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="amount">Amount</Label>
              <Input
                id="amount"
                type="number"
                min="0.01"
                step="0.01"
                placeholder="0.00"
                value={form.amount}
                onChange={(e) => setForm({ ...form, amount: e.target.value })}
                required
              />
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="note">Note (optional)</Label>
            <Input id="note" placeholder="Editor invoice, YouTube ads..." value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="incurred">Date (optional, defaults to today)</Label>
            <Input id="incurred" type="date" value={form.incurredOn} onChange={(e) => setForm({ ...form, incurredOn: e.target.value })} />
          </div>
          <Button type="submit" disabled={add.isPending} className="w-full">
            <Plus className="size-4" /> Add cost
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}