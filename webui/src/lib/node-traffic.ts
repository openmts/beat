import type { TrafficLimitType } from "@/types"

const gib = 1024 ** 3

export interface EditableNode {
  id: string
  alias: string
  groupId: string
  sshPublicKey: string
  trafficLimitGiB: string
  trafficLimitType: TrafficLimitType
  trafficResetDay: string
  sortOrder: string
  tags: string
  isPublic: boolean
  publicRemark: string
  privateRemark: string
}

export interface NodeUpdatePayload {
  alias: string
  group_id: string
  ssh_public_key: string
  traffic_limit: number
  traffic_limit_type: TrafficLimitType
  traffic_reset_day: number
  sort_order: number
  tags: string[]
  is_public: boolean
  public_remark: string
  private_remark: string
}

export function toNodeUpdatePayload(node: EditableNode): NodeUpdatePayload {
  return {
    alias: node.alias,
    group_id: node.groupId,
    ssh_public_key: node.sshPublicKey,
    traffic_limit: Math.round(Number(node.trafficLimitGiB) * gib),
    traffic_limit_type: node.trafficLimitType,
    traffic_reset_day: Number(node.trafficResetDay),
    sort_order: Number(node.sortOrder),
    tags: parseNodeTags(node.tags),
    is_public: node.isPublic,
    public_remark: node.publicRemark.trim(),
    private_remark: node.privateRemark.trim(),
  }
}

export function parseNodeTags(value: string): string[] {
  const tags: string[] = []
  for (const part of value.split(",")) {
    const tag = part.trim()
    if (tag && !tags.some((current) => current.toLocaleLowerCase() === tag.toLocaleLowerCase())) {
      tags.push(tag)
    }
  }
  return tags
}
