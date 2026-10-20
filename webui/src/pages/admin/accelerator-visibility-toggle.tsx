import { useEffect, useState } from "react"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
} from "@/components/ui/field"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/context/locale"
import { updateAcceleratorVisibility } from "@/lib/accelerator-api"

interface Props {
  nodeID: string
  visible: boolean
  globalVisible: boolean
  saved: () => void
}

// 节点开关只控制指标页展示；关闭全局总开关时，节点设置保留但暂不生效。
export function AcceleratorVisibilityToggle(props: Props) {
  const { t } = useLocale()
  const [checked, setChecked] = useState(props.visible)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setChecked(props.visible)
    setError(null)
  }, [props.nodeID, props.visible])

  const save = async (next: boolean) => {
    setChecked(next)
    setError(null)
    setSaving(true)
    try {
      await updateAcceleratorVisibility(props.nodeID, next)
      props.saved()
    } catch (reason) {
      setChecked(props.visible)
      setError(reason instanceof Error ? reason.message : t("app.error"))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-3" aria-busy={saving}>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldLabel htmlFor="accelerator-node-visibility">
            {t("settings.accelerator_node_visibility")}
          </FieldLabel>
          <FieldDescription>
            {t("settings.accelerator_node_visibility_hint")}
          </FieldDescription>
        </FieldContent>
        <Switch
          id="accelerator-node-visibility"
          checked={checked}
          disabled={saving}
          onCheckedChange={(next) => void save(next)}
        />
      </Field>
      {!props.globalVisible && (
        <p role="status" className="text-sm text-muted-foreground">
          {t("settings.accelerator_node_global_off")}
        </p>
      )}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    </div>
  )
}
