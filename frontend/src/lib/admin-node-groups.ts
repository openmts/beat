import type { Group, Node } from "@/types"

export interface NodeSection<T extends Node = Node> {
  id: string
  name: string
  nodes: T[]
}

interface BuildNodeSectionsOptions<T extends Node> {
  nodes: T[]
  groups: Pick<Group, "id" | "name">[]
  search: string
  unassignedName: string
}

export function buildNodeSections<T extends Node>({
  nodes,
  groups,
  search,
  unassignedName,
}: BuildNodeSectionsOptions<T>): NodeSection<T>[] {
  const query = search.trim().toLowerCase()
  const grouped = new Map(groups.map((group) => [group.id, [] as T[]]))
  const unassigned: T[] = []
  for (const node of nodes) {
    if (query && !matchesSearch(node, query)) continue
    const groupNodes = grouped.get(node.group_id)
    if (groupNodes) groupNodes.push(node)
    else unassigned.push(node)
  }
  for (const sectionNodes of grouped.values()) sectionNodes.sort(compareNodes)
  unassigned.sort(compareNodes)
  const sections = groups
    .map((group) => ({ ...group, nodes: grouped.get(group.id) ?? [] }))
    .filter((section) => section.nodes.length > 0)
  if (unassigned.length > 0) {
    sections.push({ id: "unassigned", name: unassignedName, nodes: unassigned })
  }
  return sections
}

export function getSSHKeyName(
  node: Node,
  keyNames: Map<string, string>,
  labels: { notAssigned: string; unknown: string },
): string {
  if (!node.ssh_public_key) return labels.notAssigned
  return keyNames.get(node.ssh_public_key) ?? labels.unknown
}

export function toEditNode(node: Node) {
  return {
    id: node.id,
    alias: node.alias || "",
    groupId: node.group_id,
    sshPublicKey: node.ssh_public_key || "",
    trafficLimitGiB: formatGiB(node.traffic_limit ?? 0),
    trafficLimitType: node.traffic_limit_type ?? "sum",
    trafficResetDay: String(node.traffic_reset_day ?? 1),
    sortOrder: String(node.sort_order ?? 0),
    tags: (node.tags ?? []).join(", "),
    isPublic: node.is_public ?? true,
    publicRemark: node.public_remark ?? "",
    privateRemark: "private_remark" in node ? String(node.private_remark ?? "") : "",
  }
}

function formatGiB(bytes: number): string {
  const value = bytes / 1024 ** 3
  return Number.isInteger(value) ? String(value) : value.toFixed(2).replace(/0+$/, "").replace(/\.$/, "")
}

function matchesSearch(node: Node, query: string): boolean {
  const privateRemark = "private_remark" in node ? String(node.private_remark ?? "") : ""
  return [node.name, node.alias, node.host, node.public_remark, privateRemark, ...(node.tags ?? [])]
    .some((value) => value.toLowerCase().includes(query))
}

function compareNodes(left: Node, right: Node): number {
  return (left.sort_order ?? 0) - (right.sort_order ?? 0) || left.name.localeCompare(right.name)
}
