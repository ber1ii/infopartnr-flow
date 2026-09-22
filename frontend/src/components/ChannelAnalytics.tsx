import { useState } from "react";
import { useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts";
import { RefreshCw } from "lucide-react";
import { toast } from "sonner";
import {
  api,
  errMsg,
  type ChannelAnalytics,
  type YoutubeChannel,
  type YoutubeSyncResponse,
} from "@/lib/api";
import { StatCard } from "@/components/StatCard";
import { InfoTip } from "@/components/InfoTip";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";

function centsToMoney(cents: number, currency = "USD") {
  return new Intl.NumberFormat("en-US", { style: "currency", currency }).format(
    cents / 100,
  );
}
const fmtNum = (n: number) => n.toLocaleString();
const fmtHours = (mins: number) =>
  (mins / 60).toLocaleString(undefined, { maximumFractionDigits: 0 });

const rangeItems = [
  { value: "7", label: "Last 7 days" },
  { value: "30", label: "Last 30 days" },
  { value: "90", label: "Last 90 days" },
];

const viewsChartConfig = {
  views: { label: "Views", color: "var(--chart-1)" },
} satisfies ChartConfig;

const fmtDay = (v: string) =>
  new Date(v + "T00:00:00Z").toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });

export default function ChannelAnalytics() {
  const { clientId } = useParams();
  const qc = useQueryClient();
  const [days, setDays] = useState("30");

  const analytics = useQuery({
    queryKey: ["channel-analytics", clientId, days],
    queryFn: () =>
      api<ChannelAnalytics>(
        `/clients/${clientId}/youtube/analytics?days=${days}`,
      ),
  });

  const channelsKey = ["youtube-channels", clientId];
  const channels = useQuery({
    queryKey: channelsKey,
    queryFn: () =>
      api<YoutubeChannel[]>(`/clients/${clientId}/youtube/channels`),
  });

  const sync = useMutation({
    mutationFn: () =>
      api<YoutubeSyncResponse>(`/clients/${clientId}/youtube/sync`, {
        method: "POST",
      }),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ["channel-analytics", clientId] });
      qc.invalidateQueries({ queryKey: channelsKey });
      if (res.failed.length === 0) {
        toast.success(
          `Synced ${res.synced.length} channel${res.synced.length === 1 ? "" : "s"}`,
        );
      } else {
        toast.error(
          `${res.failed.length} channel${res.failed.length === 1 ? "" : "s"} failed to sync` +
            (res.synced.length ? ` (${res.synced.length} succeeded)` : ""),
        );
      }
    },
    onError: (e) => toast.error(errMsg(e)),
  });

  const lastSyncedAt = channels.data?.reduce<string | null>((latest, c) => {
    if (!c.last_synced_at) return latest;
    if (!latest || c.last_synced_at > latest) return c.last_synced_at;
    return latest;
  }, null);

  const o = analytics.data?.overview;

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            Channel analytics
            <InfoTip text="Channel-wide YouTube performance (views, watch time, subscribers), plus which videos are actually driving revenue through this client's funnel." />
          </h1>
          <p className="text-sm text-muted-foreground">
            Across all connected channels for this client.
          </p>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex flex-col items-end gap-1">
            <Button
              variant="outline"
              size="sm"
              onClick={() => sync.mutate()}
              disabled={sync.isPending}
            >
              <RefreshCw
                className={`size-4 ${sync.isPending ? "animate-spin" : ""}`}
              />
              {sync.isPending ? "Syncing…" : "Refresh from YouTube"}
            </Button>
            {lastSyncedAt && (
              <span className="text-xs text-muted-foreground">
                Last synced {new Date(lastSyncedAt).toLocaleString()}
              </span>
            )}
          </div>
          <Select
            value={days}
            onValueChange={(v) => v && setDays(v)}
            items={rangeItems}
          >
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
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <StatCard
          title="Views"
          tip="Total views across all connected channels in the selected period."
          value={o && fmtNum(o.views)}
          loading={analytics.isLoading}
        />
        <StatCard
          title="Watch time"
          tip="Total watch time, in hours."
          value={o && `${fmtHours(o.watch_minutes)}h`}
          loading={analytics.isLoading}
        />
        <StatCard
          title="Net subscribers"
          tip="Subscribers gained minus subscribers lost in the selected period."
          value={o && `${o.subs_net >= 0 ? "+" : ""}${fmtNum(o.subs_net)}`}
          sub={
            o && `${fmtNum(o.subs_gained)} gained · ${fmtNum(o.subs_lost)} lost`
          }
          loading={analytics.isLoading}
        />
        <StatCard
          title="Likes"
          tip="Total likes across all connected channels in the selected period."
          value={o && fmtNum(o.likes)}
          loading={analytics.isLoading}
        />
        <StatCard
          title="Comments"
          tip="Total comments across all connected channels in the selected period."
          value={o && fmtNum(o.comments)}
          loading={analytics.isLoading}
        />
      </div>

      <Card>
        <CardHeader className="flex-row items-center gap-2 space-y-0">
          <CardTitle className="text-base">Views over time</CardTitle>
          <InfoTip text="Daily views summed across all connected channels (UTC days)." />
        </CardHeader>
        <CardContent>
          <ChartContainer config={viewsChartConfig} className="h-72 w-full">
            <AreaChart
              data={analytics.data?.daily ?? []}
              margin={{ left: 4, right: 4 }}
            >
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="date"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                minTickGap={32}
                tickFormatter={fmtDay}
              />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    labelFormatter={(v) => fmtDay(String(v))}
                  />
                }
              />
              <Area
                dataKey="views"
                type="monotone"
                stroke="var(--color-views)"
                fill="var(--color-views)"
                fillOpacity={0.15}
              />
            </AreaChart>
          </ChartContainer>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-center gap-2 space-y-0">
          <CardTitle className="text-base">Top videos</CardTitle>
          <InfoTip text="Ranked by revenue, then clicks, for the selected period. This is the funnel data YouTube Studio can't show you." />
        </CardHeader>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Video</TableHead>
              <TableHead>Clicks</TableHead>
              <TableHead>Conversions</TableHead>
              <TableHead>Revenue</TableHead>
              <TableHead>Cost</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {analytics.isLoading && (
              <TableRow>
                <TableCell colSpan={5}>
                  <Skeleton className="h-6 w-full" />
                </TableCell>
              </TableRow>
            )}
            {analytics.data?.top_videos.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={5}
                  className="py-8 text-center text-muted-foreground"
                >
                  No video activity in this period yet.
                </TableCell>
              </TableRow>
            )}
            {analytics.data?.top_videos.map((v) => (
              <TableRow key={v.id}>
                <TableCell
                  className="max-w-64 truncate font-medium"
                  title={v.title || v.youtube_video_id}
                >
                  {v.title || v.youtube_video_id}
                </TableCell>
                <TableCell>{fmtNum(v.clicks)}</TableCell>
                <TableCell>{fmtNum(v.conversions)}</TableCell>
                <TableCell>{centsToMoney(v.revenue_cents)}</TableCell>
                <TableCell>{centsToMoney(v.cost_cents)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}
