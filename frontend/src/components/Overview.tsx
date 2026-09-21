import { useState } from "react"
import { useParams } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts"
import { api, errMsg, type Client, type DayPoint, type OverviewStats } from "@/lib/api"
import { money, num } from "@/lib/format"
import { StatCard } from "@/components/StatCard"
import { InfoTip } from "@/components/InfoTip"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

const chartConfig = { clicks: { label: "Clicks", color: "var(--chart-1)" } } satisfies ChartConfig

const rangeItems = [
  { value: "7", label: "Last 7 days" },
  { value: "30", label: "Last 30 days" },
  { value: "90", label: "Last 90 days" },
]

const fmtDay = (v: string) =>
  new Date(v + "T00:00:00Z").toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" })

export default function Overview() {
  const { clientId } = useParams()
  const [days, setDays] = useState("30")

  const client = useQuery({ queryKey: ["client", clientId], queryFn: () => api<Client>(`/clients/${clientId}`) })
  const stats = useQuery({
    queryKey: ["overview", clientId, days],
    queryFn: () => api<OverviewStats>(`/clients/${clientId}/stats/overview?days=${days}`),
  })
  const series = useQuery({
    queryKey: ["clicks-by-day", clientId, days],
    queryFn: () => api<DayPoint[]>(`/clients/${clientId}/stats/clicks-by-day?days=${days}`),
  })

  if (client.isError) return <p className="text-destructive">{errMsg(client.error)}</p>

  const s = stats.data
  const cur = s?.currency ?? "USD"

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">{client.data?.name ?? "Overview"}</h1>
          <p className="text-sm text-muted-foreground">How this client's YouTube links are performing.</p>
        </div>
        <Select value={days} onValueChange={(v) => v && setDays(v)} items={rangeItems}>
          <SelectTrigger className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="7">Last 7 days</SelectItem>
            <SelectItem value="30">Last 30 days</SelectItem>
            <SelectItem value="90">Last 90 days</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <StatCard
          title="Clicks"
          tip="How many times people clicked your tracked links from YouTube. Bots are excluded."
          value={s && num(s.clicks)}
          loading={stats.isLoading}
        />
        <StatCard
          title="Unique visitors"
          tip="Roughly how many different people clicked, counted by anonymized network address."
          value={s && num(s.unique_visitors)}
          loading={stats.isLoading}
        />
        <StatCard
          title="Conversions"
          tip="Purchases, booked calls and form submissions traced back to a click."
          value={s && num(s.conversions)}
          sub={s && `${(s.conversion_rate * 100).toFixed(1)}% of clicks`}
          loading={stats.isLoading}
        />
        <StatCard
          title="Revenue"
          tip="Money received from customers who came through your tracked links, after refunds."
          value={s && money(s.revenue_cents, cur)}
          loading={stats.isLoading}
        />
        <StatCard
          title="ROI"
          tip="Return on investment = (revenue - costs) / costs. 100% means you earned back double what you spent."
          value={s && (s.roi_percent === null ? "-" : `${s.roi_percent.toFixed(0)}%`)}
          sub={s && s.roi_percent === null ? "Add video costs to see ROI" : undefined}
          loading={stats.isLoading}
        />
      </div>

      <Card>
        <CardHeader className="flex-row items-center gap-2 space-y-0">
          <CardTitle className="text-base">Clicks over time</CardTitle>
          <InfoTip text="Daily clicks on all of this client's tracked links (UTC days)." />
        </CardHeader>
        <CardContent>
          <ChartContainer config={chartConfig} className="h-72 w-full">
            <AreaChart data={series.data ?? []} margin={{ left: 4, right: 4 }}>
              <CartesianGrid vertical={false} />
              <XAxis dataKey="date" tickLine={false} axisLine={false} tickMargin={8} minTickGap={32} tickFormatter={fmtDay} />
              <ChartTooltip content={<ChartTooltipContent labelFormatter={(v) => fmtDay(String(v))} />} />
              <Area dataKey="clicks" type="monotone" stroke="var(--color-clicks)" fill="var(--color-clicks)" fillOpacity={0.15} />
            </AreaChart>
          </ChartContainer>
        </CardContent>
      </Card>
    </div>
  )
}