import { Info } from "lucide-react"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

// Plain-English explanation next to a metric name.
export function InfoTip({ text }: { text: string }) {
  return (
    <Tooltip>
      <TooltipTrigger className="text-muted-foreground hover:text-foreground" aria-label="What is this?">
        <Info className="size-3.5" />
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">{text}</TooltipContent>
    </Tooltip>
  )
}