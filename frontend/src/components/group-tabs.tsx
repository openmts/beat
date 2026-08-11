import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import type { Group } from "@/types"
import { useLocale } from "@/context/locale"

interface GroupTabsProps {
  groups: Group[]
  value: string
  onChange: (groupId: string) => void
}

export function GroupTabs({ groups, value, onChange }: GroupTabsProps) {
  const { t } = useLocale()
  return (
    <Tabs value={value} onValueChange={onChange}>
      <TabsList className="max-w-full overflow-x-auto scrollbar-none">
        <TabsTrigger value="">{t("group.all")}</TabsTrigger>
        {groups.map((group) => (
          <TabsTrigger key={group.id} value={group.id}>
            {group.name}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}
