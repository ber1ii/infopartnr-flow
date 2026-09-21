import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { InfoTip } from "./InfoTip"

interface Props {
  title: string
  tip: string
  value?: string
  sub?: string
  loading?: boolean
}

export function StatCard({ title, tip, value, sub, loading }: Props) {
  return (
    <Card>
      <CardContent className="space-y-2 p-5">
        <div className="flex items-center gap-1.5 text-sm text-muted-foreground">
          {title}
          <InfoTip text={tip} />
        </div>
        {loading ? <Skeleton className="h-8 w-24" /> : <div className="text-2xl font-semibold tabular-nums">{value}</div>}
        {sub && !loading && <div className="text-xs text-muted-foreground">{sub}</div>}
      </CardContent>
    </Card>
  )
}