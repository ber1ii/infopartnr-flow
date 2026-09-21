import { useParams } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import { api, errMsg, type Conversion } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { InfoTip } from "@/components/InfoTip"

const TYPE_LABEL: Record<Conversion["event_type"], string> = {
  purchase: "Purchase",
  subscription_start: "Subscription started",
  subscription_renewal: "Renewal",
  booked_call: "Booked call",
  lead: "Lead",
  refund: "Refund",
}

const METHOD_LABEL: Record<Conversion["attribution_method"], string> = {
  track_id: "Direct",
  email: "Email match",
  none: "Unattributed",
}

function money(cents: number, currency: string) {
  try {
    return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(cents / 100)
  } catch {
    return `${(cents / 100).toFixed(2)} ${currency}`
  }
}

export default function Conversions() {
  const { clientId } = useParams()
  const q = useQuery({
    queryKey: ["conversions", clientId],
    queryFn: () => api<Conversion[]>(`/clients/${clientId}/conversions?limit=100`),
    refetchInterval: 15000,
  })

  return (
    <div className="space-y-6">
      <div>
        <h1 className="flex items-center gap-2 text-2xl font-semibold">
          Conversions
          <InfoTip text="Every sale, renewal, refund or booking we received, and which link it came from. 'Direct' means the buyer was tracked from the click; 'Email match' means we matched them by email; 'Unattributed' means we couldn't tell." />
        </h1>
        <p className="text-sm text-muted-foreground">Latest 100 events. Refreshes automatically.</p>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>When</TableHead>
              <TableHead>Event</TableHead>
              <TableHead className="text-right">Amount</TableHead>
              <TableHead>Customer</TableHead>
              <TableHead>Came from</TableHead>
              <TableHead>Attribution</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {q.isLoading && (
              <TableRow>
                <TableCell colSpan={6}>
                  <Skeleton className="h-6 w-full" />
                </TableCell>
              </TableRow>
            )}
            {q.isError && (
              <TableRow>
                <TableCell colSpan={6} className="text-destructive">
                  {errMsg(q.error)}
                </TableCell>
              </TableRow>
            )}
            {q.data?.length === 0 && (
              <TableRow>
                <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                  No conversions yet.
                </TableCell>
              </TableRow>
            )}
            {q.data?.map((c) => (
              <TableRow key={c.id}>
                <TableCell className="whitespace-nowrap text-muted-foreground">
                  {new Date(c.occurred_at).toLocaleString()}
                </TableCell>
                <TableCell>
                  <Badge variant={c.event_type === "refund" ? "secondary" : "default"}>{TYPE_LABEL[c.event_type]}</Badge>
                </TableCell>
                <TableCell className={"text-right font-mono " + (c.amount_cents < 0 ? "text-destructive" : "")}>
                  {c.event_type === "booked_call" || c.event_type === "lead" ? "-" : money(c.amount_cents, c.currency)}
                </TableCell>
                <TableCell className="text-muted-foreground">{c.email || "-"}</TableCell>
                <TableCell>{c.link_id ? c.link_name || c.link_slug : <span className="text-muted-foreground">-</span>}</TableCell>
                <TableCell>
                  <Badge variant="secondary">{METHOD_LABEL[c.attribution_method]}</Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  )
}