import { useState, type ReactNode } from "react"
import { useNavigate, useParams } from "react-router-dom"
import { Copy, CreditCard, FileText, PhoneCall, Code2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"

// Swap these for the real YouTube video IDs once each walkthrough is
// recorded. Using "unlisted" YouTube videos: anyone with the link (i.e.
// embedded here) can watch, but they won't show up in search or on your
// channel. https://www.youtube.com/watch?v=<id> -> id goes here.
const VIDEO_ID_STRIPE = "n5Wy3leE6zM"
const VIDEO_ID_TYPEFORM = "1UMboKKqlLo"
const VIDEO_ID_CALENDLY = "dk8LJyVThhs"
const VIDEO_ID_CALENDLY_WIDGET = "VIDEO_ID_CALENDLY_WIDGET"

const WIDGET_SNIPPET = `Calendly.initInlineWidget({
  url: 'https://calendly.com/your-link/30min?utm_content=' + window.trakyo.getId(),
  parentElement: document.getElementById('calendly-embed'),
});`

function YouTubeEmbed({ videoId, title }: { videoId: string; title: string }) {
  const isPlaceholder = videoId.startsWith("VIDEO_ID_")
  if (isPlaceholder) {
    return (
      <div className="flex aspect-video items-center justify-center rounded-md border border-dashed bg-muted text-sm text-muted-foreground">
        Video coming soon
      </div>
    )
  }
  return (
    <div className="aspect-video overflow-hidden rounded-md border">
      <iframe
        className="size-full"
        src={`https://www.youtube.com/embed/${videoId}`}
        title={title}
        allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
        allowFullScreen
      />
    </div>
  )
}

function CodeBlock({ value }: { value: string }) {
  return (
    <div className="space-y-1.5">
      <div className="relative">
        <pre className="overflow-x-auto rounded-md border bg-muted p-3 text-xs">
          <code>{value}</code>
        </pre>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="absolute right-2 top-2"
          aria-label="Copy snippet"
          onClick={() => {
            navigator.clipboard.writeText(value)
            toast.success("Copied")
          }}
        >
          <Copy className="size-4" />
        </Button>
      </div>
    </div>
  )
}

function GuideSection({
  icon,
  title,
  subtitle,
  videoId,
  children,
}: {
  icon: ReactNode
  title: string
  subtitle: string
  videoId: string
  children: ReactNode
}) {
  return (
    <Card className="space-y-5 p-6">
      <div className="flex items-center gap-3">
        {icon}
        <div>
          <div className="font-medium">{title}</div>
          <div className="text-sm text-muted-foreground">{subtitle}</div>
        </div>
      </div>
      <YouTubeEmbed videoId={videoId} title={title} />
      <ol className="list-decimal space-y-2 pl-5 text-sm text-muted-foreground marker:text-foreground marker:font-medium">
        {children}
      </ol>
    </Card>
  )
}

export default function IntegrationsGuide() {
  const { clientId } = useParams()
  const navigate = useNavigate()
  const [showWidgetSection, setShowWidgetSection] = useState(true)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Setup guide</h1>
          <p className="text-sm text-muted-foreground">
            Step-by-step for connecting each tool. Follow along with the videos — takes a few minutes each.
          </p>
        </div>
        {clientId && (
          <Button variant="outline" onClick={() => navigate(`/c/${clientId}/integrations`)}>
            Back to Integrations
          </Button>
        )}
      </div>

      <GuideSection
        icon={<CreditCard className="size-5 text-primary" />}
        title="Stripe"
        subtitle="Connect payments so purchases and refunds show up automatically."
        videoId={VIDEO_ID_STRIPE}
      >
        <li>Open the Integrations page and click "Connect Stripe" — this creates your webhook URL below.</li>
        <li>In Stripe: go to Developers → Webhooks → Add endpoint.</li>
        <li>Paste in the Webhook URL shown on the Integrations page.</li>
        <li>Under "Select events", add the events listed on the Integrations page (checkout, invoice, refund, subscription cancel).</li>
        <li>Save the endpoint. Stripe will show a signing secret starting with <code>whsec_</code> — copy it immediately, it's only shown once.</li>
        <li>Paste that signing secret back into the Integrations page and save.</li>
      </GuideSection>

      <GuideSection
        icon={<FileText className="size-5 text-primary" />}
        title="Typeform"
        subtitle="Form submissions become tracked leads."
        videoId={VIDEO_ID_TYPEFORM}
      >
        <li>On the Integrations page, click "Connect Typeform" — this generates a webhook secret and URL for you.</li>
        <li>Copy the webhook secret shown (in the yellow box) right away — it won't be shown again.</li>
        <li>In Typeform: open your form → Connect panel → Webhooks → Add a webhook.</li>
        <li>Paste in the Webhook URL, and enter the secret from step 2 as the webhook's secret.</li>
        <li>Still in Typeform, add a hidden field (or URL parameter) named <code>trakyo_id</code> to the form — our tracking script fills this in automatically for visitors who click a tracked link.</li>
        <li>Make sure the form has an Email-type question — this is what lets us match a submission back to a later sale.</li>
      </GuideSection>

      <GuideSection
        icon={<PhoneCall className="size-5 text-primary" />}
        title="Calendly"
        subtitle="Booked calls, attributed automatically."
        videoId={VIDEO_ID_CALENDLY}
      >
        <li>You'll need a paid Calendly plan (webhooks aren't available on the free plan).</li>
        <li>Generate a personal access token in Calendly: Integrations → API & Webhooks → Personal Access Tokens.</li>
        <li>Paste that token into the Calendly card on the Integrations page and click Connect.</li>
        <li>That's it — we register the webhook on Calendly's side automatically. The token itself is never stored, only used once.</li>
        <li>Make sure your Calendly booking form has an Email field — this is standard, but double-check it's required.</li>
      </GuideSection>

      {showWidgetSection && (
        <GuideSection
          icon={<Code2 className="size-5 text-primary" />}
          title="Using Calendly's JS widget?"
          subtitle="If you embed Calendly with their inline/popup widget script (not a plain link or static iframe), add one line so tracking survives."
          videoId={VIDEO_ID_CALENDLY_WIDGET}
        >
          <li>
            If your site uses a plain Calendly link or a static <code>&lt;iframe&gt;</code>, skip this — it's handled automatically.
          </li>
          <li>
            If your developer embedded Calendly using <code>Calendly.initInlineWidget(...)</code>,{" "}
            <code>initPopupWidget</code>, or similar, the visitor's tracking id needs to be added to the URL manually,
            as shown below.
          </li>
          <li>Send this snippet to whoever manages your website code:</li>
        </GuideSection>
      )}
      {showWidgetSection && (
        <Card className="-mt-4 space-y-3 p-6">
          <CodeBlock value={WIDGET_SNIPPET} />
          <p className="text-sm text-muted-foreground">
            <code>window.trakyo.getId()</code> is provided automatically by our tracking script — no setup needed
            beyond adding this line to your widget code.
          </p>
          <Button variant="ghost" size="sm" onClick={() => setShowWidgetSection(false)}>
            Hide this section
          </Button>
        </Card>
      )}

      <div className="rounded-md border bg-muted/40 p-4 text-sm text-muted-foreground">
        Stuck on any step? Send us a screenshot of where things look different from the video and we'll help you
        finish setup.
      </div>
    </div>
  )
}