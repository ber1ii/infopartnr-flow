import { useState, type FormEvent } from "react"
import { Link } from "react-router-dom"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Copy, Plus } from "lucide-react"
import { toast } from "sonner"
import { api, errMsg, type Client, type Invite } from "@/lib/api"
import { shortDate } from "@/lib/format"
import { Button, buttonVariants } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

export default function Clients() {
  const qc = useQueryClient()
  const clients = useQuery({ queryKey: ["clients"], queryFn: () => api<Client[]>("/clients") })

  const [addOpen, setAddOpen] = useState(false)
  const [name, setName] = useState("")
  const [contact, setContact] = useState("")
  const addClient = useMutation({
    mutationFn: () =>
      api<Client>("/clients", {
        body: { name, contact_email: contact, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["clients"] })
      toast.success("Client added")
      setAddOpen(false)
      setName("")
      setContact("")
    },
    onError: (e) => toast.error(errMsg(e)),
  })

  const [inviteFor, setInviteFor] = useState<Client | null>(null)
  const [inviteEmail, setInviteEmail] = useState("")
  const [invite, setInvite] = useState<Invite | null>(null)
  const createInvite = useMutation({
    mutationFn: () => api<Invite>(`/clients/${inviteFor!.id}/invites`, { body: { email: inviteEmail } }),
    onSuccess: setInvite,
    onError: (e) => toast.error(errMsg(e)),
  })
  const openInvite = (c: Client) => {
    setInviteFor(c)
    setInviteEmail(c.contact_email)
    setInvite(null)
  }

  const copy = (text: string) => {
    navigator.clipboard.writeText(text)
    toast.success("Copied")
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Clients</h1>
          <p className="text-sm text-muted-foreground">Everyone whose YouTube funnel you track.</p>
        </div>
        <Button onClick={() => setAddOpen(true)}>
          <Plus className="size-4" /> Add client
        </Button>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Contact</TableHead>
              <TableHead>Added</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {clients.isLoading && (
              <TableRow>
                <TableCell colSpan={4}>
                  <Skeleton className="h-6 w-full" />
                </TableCell>
              </TableRow>
            )}
            {clients.data?.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="py-8 text-center text-muted-foreground">
                  No clients yet. Add your first one.
                </TableCell>
              </TableRow>
            )}
            {clients.data?.map((c) => (
              <TableRow key={c.id}>
                <TableCell className="font-medium">{c.name}</TableCell>
                <TableCell className="text-muted-foreground">{c.contact_email || "-"}</TableCell>
                <TableCell className="text-muted-foreground">{shortDate(c.created_at)}</TableCell>
                <TableCell className="space-x-2 text-right">
                  <Button variant="outline" size="sm" onClick={() => openInvite(c)}>
                    Invite
                  </Button>
                  <Link to={`/c/${c.id}/overview`} className={buttonVariants({ size: "sm" })}>
                    Open
                  </Link>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent>
          <form
            onSubmit={(e: FormEvent) => {
              e.preventDefault()
              addClient.mutate()
            }}
            className="space-y-4"
          >
            <DialogHeader>
              <DialogTitle>Add client</DialogTitle>
              <DialogDescription>You can invite them to their own dashboard afterwards.</DialogDescription>
            </DialogHeader>
            <div className="space-y-2">
              <Label htmlFor="cname">Name</Label>
              <Input id="cname" value={name} onChange={(e) => setName(e.target.value)} required autoFocus />
            </div>
            <div className="space-y-2">
              <Label htmlFor="cemail">Contact email (optional)</Label>
              <Input id="cemail" type="email" value={contact} onChange={(e) => setContact(e.target.value)} />
            </div>
            <DialogFooter>
              <Button type="submit" disabled={addClient.isPending}>
                Add client
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={!!inviteFor} onOpenChange={(o) => !o && setInviteFor(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Invite to {inviteFor?.name}</DialogTitle>
            <DialogDescription>
              Creates a one-time link (valid 7 days). Send it to your client yourself for now.
            </DialogDescription>
          </DialogHeader>
          {!invite ? (
            <form
              onSubmit={(e: FormEvent) => {
                e.preventDefault()
                createInvite.mutate()
              }}
              className="space-y-4"
            >
              <div className="space-y-2">
                <Label htmlFor="iemail">Client email</Label>
                <Input id="iemail" type="email" value={inviteEmail} onChange={(e) => setInviteEmail(e.target.value)} required />
              </div>
              <DialogFooter>
                <Button type="submit" disabled={createInvite.isPending}>
                  Generate link
                </Button>
              </DialogFooter>
            </form>
          ) : (
            <div className="space-y-2">
              <Label>Invite link (shown once)</Label>
              <div className="flex gap-2">
                <Input readOnly value={invite.invite_url} onFocus={(e) => e.target.select()} />
                <Button variant="outline" size="icon" onClick={() => copy(invite.invite_url)} aria-label="Copy">
                  <Copy className="size-4" />
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}