export const TOKEN_KEY = "flow_token"
export const USER_KEY = "flow_user"

export type Role = "owner" | "agency_admin" | "client"

export interface AuthResponse {
  token: string
  user_id: string
  email: string
  name: string
  workspace_id: string
  role: Role
  client_id?: string
}

export interface Client {
  id: string
  workspace_id: string
  name: string
  contact_email: string
  timezone: string
  currency: string
  created_at: string
}

export interface Link {
  id: string
  client_id: string
  video_id: string | null
  slug: string
  name: string
  target_url: string
  is_active: boolean
  expires_at: string | null
  created_at: string
  updated_at: string
}

export interface OverviewStats {
  clicks: number
  unique_visitors: number
  conversions: number
  revenue_cents: number
  cost_cents: number // costs incurred during the selected period
  lifetime_cost_cents: number // total ever spent on this client's videos
  roi_percent: number | null // period revenue vs period cost
  conversion_rate: number
  currency: string
}

export interface DayPoint {
  date: string
  clicks: number
}

export interface Invite {
  id: string
  email: string
  expires_at: string
  invite_url: string
}

export interface Integration {
  id: string
  provider: "stripe" | "calendly" | "typeform"
  is_active: boolean
  has_secret: boolean
  webhook_url: string
  created_at: string
}

export interface Conversion {
  id: number
  event_type: "purchase" | "subscription_start" | "subscription_renewal" | "booked_call" | "lead" | "refund"
  source: string
  amount_cents: number
  currency: string
  email: string
  attribution_method: "track_id" | "email" | "none"
  occurred_at: string
  link_id: string | null
  link_slug: string
  link_name: string
}

// Row from ListVideosWithCostsByClient: video + a lifetime cost rollup.
export interface Video {
  id: string
  client_id: string
  channel_id: string | null
  youtube_video_id: string
  title: string
  thumbnail_url: string
  published_at: string | null
  created_at: string
  total_cost_cents: number
  cost_count: number
}

export interface VideoCost {
  id: string
  video_id: string
  kind: "production" | "ad_spend" | "other"
  amount_cents: number
  note: string
  incurred_on: string
  created_at: string
}

export interface VideoAnalytics {
  daily: { date: string; views: number; watch_minutes: number; subs_gained: number }[]
  clicks: number
  conversions: number
  revenue_cents: number
}

export interface YoutubeChannel {
  id: string
  client_id: string
  google_channel_id: string
  title: string
  thumbnail_url: string
  status: "connected" | "revoked"
  connected_at: string
  last_synced_at: string | null
}

export interface YoutubeSyncResponse {
  synced: string[]
  failed: { channel_id: string; error: string }[]
}

export interface ChannelAnalyticsOverview {
  views: number
  watch_minutes: number
  subs_gained: number
  subs_lost: number
  subs_net: number
  likes: number
  comments: number
}

export interface ChannelDayPoint {
  date: string
  views: number
  watch_minutes: number
  subs_gained: number
  subs_lost: number
}

export interface TopVideo {
  id: string
  title: string
  youtube_video_id: string
  thumbnail_url: string
  clicks: number
  conversions: number
  revenue_cents: number
  cost_cents: number
}

export interface ChannelAnalytics {
  overview: ChannelAnalyticsOverview
  daily: ChannelDayPoint[]
  top_videos: TopVideo[]
}

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

export function clearSession() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
}

export async function api<T>(
  path: string,
  opts: { method?: string; body?: unknown } = {},
): Promise<T> {
  const token = localStorage.getItem(TOKEN_KEY)
  const res = await fetch(`/api${path}`, {
    method: opts.method ?? (opts.body !== undefined ? "POST" : "GET"),
    headers: {
      ...(opts.body !== undefined ? { "Content-Type": "application/json" } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    if (res.status === 401 && token) {
      clearSession()
      window.location.href = "/login"
    }
    throw new ApiError(data.error ?? res.statusText, res.status)
  }
  return data as T
}

export function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : "Something went wrong"
}