export function element(tag, className, text) {
  const node = document.createElement(tag)
  if (className) node.className = className
  if (text !== undefined && text !== null) node.textContent = String(text)
  return node
}

export function append(parent, ...children) {
  for (const child of children) if (child) parent.append(child)
  return parent
}
