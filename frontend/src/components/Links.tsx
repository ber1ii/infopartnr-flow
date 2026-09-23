import { useState, type FormEvent } from "react"
import { useParams } from "react-router-dom"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Copy, MoreHorizontal, Plus } from "lucide-react"
import { toast } from "sonner"
import { api, errMsg, type Link } from "@/lib/api"
import { useAuth } from "@/lib/Auth"
import { shortDate, shortUrl } from "@/lib/format"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { InfoTip } from "@/components/InfoTip"
import { LinkVariantsDialog } from "@/components/LinkVariants"

interface FormState {
  name: string
  target: string
  slug: string
}
const empty: FormState = { name: "", target: "", slug: "" }

export default function Links() {
  const { clientId } = useParams()
  const { user } = useAuth()
  const canEdit = user?.role !== "client"
  const qc = useQueryClient()
  const key = ["links", clientId]

  const links = useQuery({ queryKey: key, queryFn: () => api<Link[]>(`/clients/${clientId}/links`) })

  const [dialog, setDialog] = useState<"create" | Link | null>(null)
  const [form, setForm] = useState<FormState>(empty)
  const [ab, setAb] = useState<Link | null>(null)
  const editing = dialog && dialog !== "create" ? dialog : null

  const done = (msg: string) => {
    qc.invalidateQueries({ queryKey: key })
    toast.success(msg)
    setDialog(null)
  }
  const create = useMutation({
    mutationFn: () =>
      api<Link>(`/clients/${clientId}/links`, {
        body: { name: form.name, target_url: form.target, ...(form.slug ? { slug: form.slug } : {}) },
      }),
    onSuccess: () => done("Link created"),
    onError: (e) => toast.error(errMsg(e)),
  })
  const update = useMutation({
    mutationFn: (v: { id: string; body: Record<string, unknown> }) =>
      api<Link>(`/clients/${clientId}/links/${v.id}`, { method: "PATCH", body: v.body }),
    onSuccess: () => done("Link updated"),
    onError: (e) => toast.error(errMsg(e)),
  })
  const remove = useMutation({
    mutationFn: (id: string) => api<void>(`/clients/${clientId}/links/${id}`, { method: "DELETE" }),
    onSuccess: () => done("Link deleted"),
    onError: (e) => toast.error(errMsg(e)),
  })

  const openCreate = () => {
    setForm(empty)
    setDialog("create")
  }
  const openEdit = (l: Link) => {
    setForm({ name: l.name, target: l.target_url, slug: l.slug })
    setDialog(l)
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (editing) update.mutate({ id: editing.id, body: { name: form.name, target_url: form.target } })
    else create.mutate()
  }
  const copy = (text: string) => {
    navigator.clipboard.writeText(text)
    toast.success("Copied")
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            Links
            <InfoTip text="Put the short link in your YouTube video description. You can change where it points here anytime, without editing the video." />
          </h1>
          <p className="text-sm text-muted-foreground">Tracked short links for YouTube descriptions.</p>
        </div>
        {canEdit && (
          <Button onClick={openCreate}>
            <Plus className="size-4" /> New link
          </Button>
        )}
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Short link</TableHead>
              <TableHead>Sends people to</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Created</TableHead>
              {canEdit && <TableHead className="w-10" />}
            </TableRow>
          </TableHeader>
          <TableBody>
            {links.isLoading && (
              <TableRow>
                <TableCell colSpan={6}>
                  <Skeleton className="h-6 w-full" />
                </TableCell>
              </TableRow>
            )}
            {links.isError && (
              <TableRow>
                <TableCell colSpan={6} className="text-destructive">
                  {errMsg(links.error)}
                </TableCell>
              </TableRow>
            )}
            {links.data?.length === 0 && (
              <TableRow>
                <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                  No links yet.
                </TableCell>
              </TableRow>
            )}
            {links.data?.map((l) => (
              <TableRow key={l.id}>
                <TableCell className="font-medium">{l.name || "-"}</TableCell>
                <TableCell>
                  <button
                    className="inline-flex items-center gap-1.5 font-mono text-xs text-primary hover:underline"
                    onClick={() => copy(shortUrl(l.slug))}
                    title="Click to copy"
                  >
                    {shortUrl(l.slug).replace(/^https?:\/\//, "")}
                    <Copy className="size-3" />
                  </button>
                </TableCell>
                <TableCell className="max-w-64 truncate text-muted-foreground" title={l.target_url}>
                  {l.target_url}
                </TableCell>
                <TableCell>
                  <Badge variant={l.is_active ? "default" : "secondary"}>{l.is_active ? "Active" : "Paused"}</Badge>
                </TableCell>
                <TableCell className="text-muted-foreground">{shortDate(l.created_at)}</TableCell>
                {canEdit && (
                  <TableCell>
                    <DropdownMenu>
                      <DropdownMenuTrigger render={<Button variant="ghost" size="icon" aria-label="Actions" />}>
                        <MoreHorizontal className="size-4" />
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => openEdit(l)}>Edit</DropdownMenuItem>
                        <DropdownMenuItem onClick={() => setAb(l)}>A/B variants</DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => update.mutate({ id: l.id, body: { is_active: !l.is_active } })}
                        >
                          {l.is_active ? "Pause" : "Resume"}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          className="text-destructive"
                          onClick={() => confirm(`Delete "${l.name || l.slug}"? Its short link will stop working.`) && remove.mutate(l.id)}
                        >
                          Delete
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>

      <Dialog open={dialog !== null} onOpenChange={(o) => !o && setDialog(null)}>
        <DialogContent>
          <form onSubmit={submit} className="space-y-4">
            <DialogHeader>
              <DialogTitle>{editing ? "Edit link" : "New link"}</DialogTitle>
              <DialogDescription>
                {editing
                  ? "Changes apply to the short link immediately."
                  : "A short link is generated for you. Add a custom ending if you prefer."}
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-2">
              <Label htmlFor="lname">Name</Label>
              <Input id="lname" placeholder="e.g. Video 12: Sales funnel" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} autoFocus />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ltarget">Destination URL</Label>
              <Input id="ltarget" type="url" placeholder="https://..." value={form.target} onChange={(e) => setForm({ ...form, target: e.target.value })} required />
            </div>
            {!editing && (
              <div className="space-y-2">
                <Label htmlFor="lslug">Custom ending (optional)</Label>
                <Input id="lslug" placeholder="my-offer" value={form.slug} onChange={(e) => setForm({ ...form, slug: e.target.value })} />
              </div>
            )}
            <DialogFooter>
              <Button type="submit" disabled={create.isPending || update.isPending}>
                {editing ? "Save changes" : "Create link"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {ab && <LinkVariantsDialog clientId={clientId!} link={ab} onClose={() => setAb(null)} />}
    </div>
  )
}