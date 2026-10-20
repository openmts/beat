import MockAdapter from "axios-mock-adapter"
import { afterEach, describe, expect, it } from "vitest"
import { api } from "./api-client"
import { getAcceleratorInventory, getAcceleratorPolicy, updateAcceleratorPolicy, updateAcceleratorVisibility } from "./accelerator-api"
import { acceleratorInventory, acceleratorPolicy } from "@/test/accelerator-fixtures"
import { acceleratorPolicyWrite } from "./accelerator-policy"
const mock = new MockAdapter(api)
afterEach(() => mock.reset())
describe("GPU inventory and policy API", () => {
 it("encodes node IDs and forwards aborts for inventory reads", async () => {
  const signal = new AbortController().signal, expected = acceleratorInventory()
  mock.onGet("/admin/nodes/node%2Fone/accelerators").reply(config => { expect(config.signal).toBe(signal); return [200, expected] })
  expect(await getAcceleratorInventory("node/one", signal)).toEqual(expected)
 })
 it("keeps node overrides and enrollment defaults distinct and uses revision CAS", async () => {
  const signal = new AbortController().signal, value = acceleratorPolicy()
  for (const [scope, path] of [[{ kind: "node", id: "a/b" }, "/admin/nodes/a%2Fb/accelerator-policy"], [{ kind: "enrollment_policy" }, "/admin/accelerator-enrollment-policy"]] as const) {
   mock.onGet(path).reply(200, value); expect(await getAcceleratorPolicy(scope, signal)).toEqual(value)
   mock.onPut(path).reply(config => { expect(JSON.parse(config.data).expected_revision).toBe(0); return [200, { ...value, configured: true, revision: 1 }] }); expect((await updateAcceleratorPolicy(scope, acceleratorPolicyWrite(value))).revision).toBe(1)
  }
 })
 it("saves node visibility through an encoded node-scoped endpoint", async () => {
  mock.onPut("/admin/nodes/node%2Fone/accelerator-visibility").reply(config => {
    expect(JSON.parse(config.data)).toEqual({ visible: false })
    return [200, { code: 200, data: { node_id: "node/one", visible: false } }]
  })
  await expect(updateAcceleratorVisibility("node/one", false)).resolves.toEqual({ node_id: "node/one", visible: false })
 })
 it("preserves conflict and forbidden errors without optimistic success", async () => {
  mock.onPut("/admin/nodes/node/accelerator-policy").reply(409, { error: "GPU policy changed" })
  await expect(updateAcceleratorPolicy({ kind: "node", id: "node" }, acceleratorPolicyWrite(acceleratorPolicy()))).rejects.toMatchObject({ status: 409 })
  mock.onGet("/admin/nodes/node/accelerators").reply(403, { error: "Forbidden" }); await expect(getAcceleratorInventory("node", new AbortController().signal)).rejects.toMatchObject({ status: 403 })
 })
})
