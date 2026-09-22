export const money = (cents: number, currency = "USD") =>
  new Intl.NumberFormat(undefined, { style: "currency", currency }).format(cents / 100)

export const num = (n: number) => new Intl.NumberFormat().format(n)

// Redirects are served by the Go backend. In production this is your short domain.
const SHORT_BASE: string = import.meta.env.VITE_SHORT_URL_BASE ?? "http://localhost:8081"
export const shortUrl = (slug: string) => `${SHORT_BASE}/${slug}`

export const shortDate = (iso: string) =>
  new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" })