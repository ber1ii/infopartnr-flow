import { useState, type FormEvent } from "react"
import { useParams } from "react-router-dom"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { api, errMsg, type Video, type VideoCost } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { InfoTip } from "@/components/InfoTip"

function centsToMoney(cents: number, currency = "USD") {
  return new Intl.NumberFormat("en-US", { style: "currency", currency }).format(cents / 100)
}

const KIND_OPTIONS = [
  { value: "production", label: "Production" },
  { value: "ad_spend", label: "Ad spend" },
  { value: "other", label: "Other" },
]

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
  const [openVideo, setOpenVideo] = useState<Video | null>(null)

  return (
    <div className="space-y-6">
      <div>
        <h1 className="flex items-center gap-2 text-2xl font-semibold">
          Video costs
          <InfoTip text="Log what each video cost to produce or promote. This feeds directly into the ROI numbers on the Overview page." />
        </h1>
        <p className="text-sm text-muted-foreground">Production and ad spend, tracked per video.</p>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Video</TableHead>
              <TableHead>Published</TableHead>
              <TableHead>Entries</TableHead>
              <TableHead>Total cost</TableHead>
              <TableHead className="w-10" />
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
            {videos.data?.map((v) => (
              <TableRow key={v.id}>
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
                <TableCell>
                  <Button variant="ghost" size="sm" onClick={() => setOpenVideo(v)}>
                    Manage
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>

      <CostsDialog
        video={openVideo}
        base={base}
        onClose={() => setOpenVideo(null)}
        onChanged={() => qc.invalidateQueries({ queryKey: key })}
      />
    </div>
  )
}

function CostsDialog({
  video,
  base,
  onClose,
  onChanged,
}: {
  video: Video | null
  base: string
  onClose: () => void
  onChanged: () => void
}) {
  const costsBase = video ? `${base}/${video.id}/costs` : ""
  const key = ["video-costs", video?.id]
  const qc = useQueryClient()
  const [form, setForm] = useState<CostForm>(emptyCostForm)

  const costs = useQuery({
    queryKey: key,
    queryFn: () => api<VideoCost[]>(costsBase),
    enabled: !!video,
  })

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
    <Dialog open={!!video} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{video?.title || video?.youtube_video_id}</DialogTitle>
          <DialogDescription>Costs are lifetime and factor into this video's ROI immediately.</DialogDescription>
        </DialogHeader>

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
      </DialogContent>
    </Dialog>
  )
}