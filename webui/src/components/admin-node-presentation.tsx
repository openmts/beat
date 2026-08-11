import {
  ArrowDownIcon,
  ArrowUpIcon,
  BanIcon,
  FileJsonIcon,
  KeyRoundIcon,
  MoreHorizontalIcon,
  Trash2Icon,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useLocale } from "@/context/locale"
import type { ManagedNode } from "@/types"

export function AdminNodePresentation({ node }: { node: ManagedNode }) {
  const { t } = useLocale()
  return (
    <>
      <div className="flex min-w-0 flex-wrap gap-1.5">
        <Badge variant={node.is_public ? "outline" : "secondary"}>
          {node.is_public ? t("node.public") : t("node.hidden")}
        </Badge>
        {(node.tags ?? []).map((tag) => <Badge key={tag} variant="outline">{tag}</Badge>)}
      </div>
      {node.public_remark ? (
        <p className="line-clamp-2 text-xs leading-relaxed text-muted-foreground" title={node.public_remark}>
          {node.public_remark}
        </p>
      ) : null}
      {node.private_remark ? (
        <div className="min-w-0 border-l-2 border-muted pl-2">
          <p className="text-xs text-muted-foreground">{t("node.private_remark")}</p>
          <p className="line-clamp-2 text-xs" title={node.private_remark}>{node.private_remark}</p>
        </div>
      ) : null}
    </>
  )
}

interface AdminNodeActionsProps {
  canMoveUp: boolean
  canMoveDown: boolean
  onMoveUp: () => void
  onMoveDown: () => void
  onInstall: () => void
  onRotate: () => void
  onRevoke: () => void
  onDelete: () => void
}

export function AdminNodeActions(props: AdminNodeActionsProps) {
  const { t } = useLocale()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon-sm" aria-label={t("app.actions")} title={t("app.actions")} />}
      >
        <MoreHorizontalIcon />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuGroup>
          <DropdownMenuItem disabled={!props.canMoveUp} onClick={props.onMoveUp}>
            <ArrowUpIcon />{t("node.move_up")}
          </DropdownMenuItem>
          <DropdownMenuItem disabled={!props.canMoveDown} onClick={props.onMoveDown}>
            <ArrowDownIcon />{t("node.move_down")}
          </DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem onClick={props.onInstall}>
            <FileJsonIcon />{t("agent.install_config")}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={props.onRotate}>
            <KeyRoundIcon />{t("agent.rotate")}
          </DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onClick={props.onRevoke}>
            <BanIcon />{t("agent.revoke")}
          </DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem variant="destructive" onClick={props.onDelete}>
            <Trash2Icon />{t("app.delete")}
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
