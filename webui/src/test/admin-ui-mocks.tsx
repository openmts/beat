import { Children, createContext, isValidElement, useContext, type ReactNode } from "react"

type ChangeContext = { value?: string; change?: (value: string) => void }

const TabsContext = createContext<ChangeContext>({})

export function MockTabs({
  children,
  value,
  onValueChange,
}: {
  children: ReactNode
  value?: string
  onValueChange?: (value: string) => void
}) {
  return (
    <TabsContext.Provider value={{ value, change: onValueChange }}>
      {children}
    </TabsContext.Provider>
  )
}

export function MockTabsTrigger({ children, value }: { children: ReactNode; value: string }) {
  const tabs = useContext(TabsContext)
  return <button onClick={() => tabs.change?.(value)}>{children}</button>
}

export function MockTabsContent({ children, value }: { children: ReactNode; value: string }) {
  const tabs = useContext(TabsContext)
  return tabs.value === value ? <div>{children}</div> : null
}

export function MockSelect({
  children,
  value,
  onValueChange,
  disabled,
  items,
}: {
  children: ReactNode
  value?: string
  onValueChange?: (value: string) => void
  disabled?: boolean
  items?: ReadonlyArray<{ label: ReactNode; value: string }>
}) {
  const selectedLabel = items?.find((item) => item.value === value)?.label
  return (
    <select
      data-testid="select"
      data-selected-label={typeof selectedLabel === "string" ? selectedLabel : undefined}
      value={value}
      disabled={disabled}
      onChange={(event) => onValueChange?.(event.target.value)}
    >
      {collectOptions(children)}
    </select>
  )
}

function collectOptions(children: ReactNode): ReactNode[] {
  const options: ReactNode[] = []
  Children.forEach(children, (child) => {
    if (!isValidElement<{ children?: ReactNode; value?: string }>(child)) return
    if (child.type === MockSelectItem) {
      options.push(<option key={child.props.value} value={child.props.value}>{child.props.children}</option>)
      return
    }
    options.push(...collectOptions(child.props.children))
  })
  return options
}

export function MockSelectItem({ children }: { children: ReactNode; value: string }) {
  return <>{children}</>
}

export function MockDialog({ children, open }: { children: ReactNode; open?: boolean }) {
  return open ? <div role="dialog">{children}</div> : null
}

export function PassThrough({ children }: { children?: ReactNode }) {
  return <>{children}</>
}
