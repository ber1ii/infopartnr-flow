import { useState, type FormEvent } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Trash2 } from "lucide-react"
import { toast } from "sonner"
import { api, errMsg, type Link, type LinkVariant } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

// TODO: swap for your money helper in lib/format.ts (uses client currency).
const money = (c: number) => (c / 100).toLocaleString(undefined, { style: "currency", currency: "USD" })

export function LinkVariantsDialog({
  clientId,
  link,
  onClose,
}: {
  clientId: string
  link: Link
  onClose: () => void
}) {
  const qc = useQueryClient()
  const key = ["link-variants", link.id]
  const base = `/clients/${clientId}/links/${link.id}/variants`
  const q = useQuery({ queryKey: key, queryFn: () => api<LinkVariant[]>(base) })
  const refresh = () => qc.invalidateQueries({ queryKey: key })

  const [url, setUrl] = useState("")
  const [weight, setWeight] = useState("1")

  const add = useMutation({
    mutationFn: () => api<LinkVariant>(base, { body: { target_url: url, weight: Number(weight) || 1 } }),
    onSuccess: () => {
      refresh()
      setUrl("")
      setWeight("1")
    },
    onError: (e) => toast.error(errMsg(e)),
  })
  const patch = useMutation({
    mutationFn: (v: { id: string; body: Record<string, unknown> }) =>
      api<LinkVariant>(`${base}/${v.id}`, { method: "PATCH", body: v.body }),
    onSuccess: refresh,
    onError: (e) => {
      toast.error(errMsg(e))
      refresh()
    },
  })
  const remove = useMutation({
    mutationFn: (id: string) => api<void>(`${base}/${id}`, { method: "DELETE" }),
    onSuccess: refresh,
    onError: (e) => toast.error(errMsg(e)),
  })

  const rows = q.data ?? []
  const activeTotal = rows.filter((v) => v.is_active).reduce((s, v) => s + v.weight, 0)
  const anyActive = activeTotal > 0

  const submit = (e: FormEvent) => {
    e.preventDefault()
    add.mutate()
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>A/B destinations: {link.name || link.slug}</DialogTitle>
          <DialogDescription>
            {anyActive
              ? "Traffic is split by weight. The main destination is ignored while any variant is active. Visitors stick to the same variant on repeat clicks."
              : "No active variants: all traffic goes to the main destination. To test, add your current page as a variant too, plus the alternatives."}
          </DialogDescription>
        </DialogHeader>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Destination</TableHead>
              <TableHead className="w-20">Weight</TableHead>
              <TableHead className="w-16">Share</TableHead>
              <TableHead className="text-right">Clicks</TableHead>
              <TableHead className="text-right">Conv.</TableHead>
              <TableHead className="text-right">Rate</TableHead>
              <TableHead className="text-right">Revenue</TableHead>
              <TableHead className="w-24" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {q.isLoading && (
              <TableRow>
                <TableCell colSpan={8}>
                  <Skeleton className="h-6 w-full" />
                </TableCell>
              </TableRow>
            )}
            {q.isError && (
              <TableRow>
                <TableCell colSpan={8} className="text-destructive">
                  {errMsg(q.error)}
                </TableCell>
              </TableRow>
            )}
            {q.data?.length === 0 && (
              <TableRow>
                <TableCell colSpan={8} className="py-6 text-center text-muted-foreground">
                  No variants yet.
                </TableCell>
              </TableRow>
            )}
            {rows.map((v) => (
              <TableRow key={v.id} className={v.is_active ? "" : "opacity-50"}>
                <TableCell className="max-w-64 truncate" title={v.target_url}>
                  {v.target_url}
                </TableCell>
                <TableCell>
                  <Input
                    key={`${v.id}-${v.weight}`}
                    type="number"
                    min={1}
                    max={1000}
                    defaultValue={v.weight}
                    className="h-8 w-16"
                    onBlur={(e) => {
                      const n = Number(e.target.value)
                      if (n !== v.weight && n >= 1) patch.mutate({ id: v.id, body: { weight: n } })
                    }}
                  />
                </TableCell>
                <TableCell>
                  {v.is_active && anyActive ? `${Math.round((v.weight / activeTotal) * 100)}%` : "-"}
                </TableCell>
                <TableCell className="text-right">{v.clicks}</TableCell>
                <TableCell className="text-right">{v.conversions}</TableCell>
                <TableCell className="text-right">
                  {v.clicks > 0 ? `${((v.conversions / v.clicks) * 100).toFixed(1)}%` : "-"}
                </TableCell>
                <TableCell className="text-right">{money(v.revenue_cents)}</TableCell>
                <TableCell>
                  <div className="flex items-center justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => patch.mutate({ id: v.id, body: { is_active: !v.is_active } })}
                    >
                      {v.is_active ? "Pause" : <Badge variant="secondary">Resume</Badge>}
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Delete variant"
                      onClick={() =>
                        confirm("Delete this variant? Its past clicks lose their variant label. Pausing keeps history.") &&
                        remove.mutate(v.id)
                      }
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        <form onSubmit={submit} className="flex items-end gap-2">
          <div className="flex-1">
            <Input type="url" placeholder="https://landing-page-b..." value={url} onChange={(e) => setUrl(e.target.value)} required />
          </div>
          <Input
            type="number"
            min={1}
            max={1000}
            value={weight}
            onChange={(e) => setWeight(e.target.value)}
            className="w-20"
            aria-label="Weight"
          />
          <Button type="submit" disabled={add.isPending}>
            Add variant
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  )
}