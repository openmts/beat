import {
  BookOpenTextIcon,
  CalendarClockIcon,
  GaugeIcon,
  ListChecksIcon,
  RouteIcon,
  type LucideIcon,
} from "lucide-react"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useLocale } from "@/context/locale"

interface GuideSection {
  title: string
  body: string
  icon: LucideIcon
}

export function NetworkQualityGuide() {
  const { t } = useLocale()
  const sections: GuideSection[] = [
    { title: "network.guide.direction_title", body: "network.guide.direction", icon: RouteIcon },
    { title: "network.guide.metrics_title", body: "network.guide.metrics", icon: GaugeIcon },
    { title: "network.guide.schedule_title", body: "network.guide.schedule", icon: CalendarClockIcon },
    { title: "network.guide.checklist_title", body: "network.guide.checklist", icon: ListChecksIcon },
  ]

  return (
    <Card role="region" aria-labelledby="network-quality-guide-title" className="bg-muted/30">
      <CardHeader>
        <CardTitle id="network-quality-guide-title" className="flex items-center gap-2">
          <BookOpenTextIcon className="size-4 text-primary" aria-hidden />
          {t("network.guide.title")}
        </CardTitle>
        <CardDescription className="max-w-4xl">{t("network.guide.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        <dl className="grid gap-3 md:grid-cols-2">
          {sections.map(({ title, body, icon: Icon }) => (
            <div key={title} className="rounded-lg border bg-background/70 p-3">
              <dt className="flex items-center gap-2 font-medium">
                <Icon className="size-4 text-muted-foreground" aria-hidden />
                {t(title)}
              </dt>
              <dd className="mt-1.5 text-sm leading-6 text-muted-foreground">{t(body)}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}
