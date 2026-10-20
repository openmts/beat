import { afterEach, expect, it, vi } from "vitest"
import MockAdapter from "axios-mock-adapter"
import { api } from "./api-client"
import * as operations from "./operations-api"
import * as transfers from "./operation-transfers"
import { operationPolicy, operationSnippet, operationTask, operationTransfer } from "@/test/operations-fixtures"
const mock=new MockAdapter(api)
afterEach(()=>{mock.reset();vi.unstubAllGlobals()})
it("roundtrips UTF-8 commands and large bounded output without placing content in URLs",async()=>{
 const content="中文 🌍 ".repeat(5000)
 expect(operations.operationDecode(operations.operationEncode(content))).toBe(content)
 expect(operations.operationDecode(null)).toBe("")
 mock.onPost("/admin/operations/tasks").reply(config=>{
  expect(JSON.parse(config.data).command).toBe(operations.operationEncode(content))
  expect(config.url).not.toContain(content)
  return [200,{code:200,data:{id:"created"}}]
 })
 expect(await operations.createOperationTask({dedupe_key:"request",policy_id:"policy",shell_family:"sh",command:operations.operationEncode(content),node_ids:["node"],group_ids:[],limits:{kind:"shell",targets:1,timeout_seconds:60,output_bytes:1024,run_as_root:false}})).toEqual({id:"created"})
})
it("generates valid UUIDs on both secure and plain HTTP browser contexts",()=>{
 expect(operations.operationRequestID()).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
 vi.stubGlobal("crypto",{getRandomValues:(bytes:Uint8Array)=>{for(let i=0;i<bytes.length;i++)bytes[i]=i;return bytes}})
 expect(operations.operationRequestID()).toBe("00010203-0405-4607-8809-0a0b0c0d0e0f")
})
it("preserves cursor, snapshot version and attempt identity through response envelopes",async()=>{
 const signal=new AbortController().signal,task=operationTask(),policy=operationPolicy(),snippet=operationSnippet()
 mock.onGet("/admin/operations/tasks").reply(config=>{expect(config.params).toEqual({cursor:"opaque",limit:50});return [200,{data:{items:[task],next_cursor:"next"},code:200}]})
 expect((await operations.getOperationTasks("opaque",signal)).next_cursor).toBe("next")
 mock.onGet("/admin/operations/tasks/task-one").reply(200,{code:200,data:task})
 expect(await operations.getOperationTask("task-one",signal)).toEqual(task)
 mock.onGet("/admin/operations/tasks/task-one/targets/target-one/output").reply(200,{code:200,data:{attempt_id:"immutable-attempt",state:"succeeded",stdout:"",stderr:""}})
 expect((await operations.getOperationOutput("task-one","target-one")).attempt_id).toBe("immutable-attempt")
 mock.onPut("/admin/operations/policies/policy-one").reply(config=>{expect(JSON.parse(config.data).version).toBe(1);return [200,{code:200,data:{id:policy.id}}]})
 expect((await operations.saveOperationPolicy(policy)).id).toBe(policy.id)
 mock.onPost("/admin/operations/policies").reply(200,{code:200,data:{id:"new"}})
 expect((await operations.saveOperationPolicy({...policy,id:"",version:0})).id).toBe("new")
 mock.onGet("/admin/operations/snippets/snippet-one").reply(config=>{expect(config.params.version).toBe(2);return [200,{code:200,data:{content:"plain UTF-8 内容"}}]})
 expect(await operations.getOperationSnippetContent(snippet)).toBe("plain UTF-8 内容")
})
it("uses bounded metadata endpoints and keeps mutation identifiers encoded",async()=>{
 const signal=new AbortController().signal
 mock.onGet().reply(config=>[200,{code:200,data:config.url?.endsWith("status")?{enabled:false}:[]}])
 for(const load of [operations.getExecutablePolicies,operations.getOperationPolicies,operations.getOperationSnippets,operations.getOperationNodes,operations.getOperationGroups]) expect(await load(signal)).toEqual([])
 expect((await operations.getOperationServerStatus(signal)).enabled).toBe(false)
 mock.onAny().reply(200,{code:200,data:{id:"record"}})
 await operations.cancelOperationTask("id/with?separator")
 await operations.retryOperationTarget("task","target",{reason:"verified",acknowledge_unknown_risk:true})
 await operations.disableOperationPolicy("policy")
 await operations.deleteOperationSnippet("snippet")
 await operations.approveOperationSnippet(operationSnippet())
 expect(mock.history.post.some(request=>request.url?.includes("id%2Fwith%3Fseparator"))).toBe(true)
 expect(JSON.parse(mock.history.post.find(request=>request.url?.endsWith("/retry"))!.data)).toEqual({reason:"verified",acknowledge_unknown_risk:true})
})
it("uploads immutable snippet versions and reads task content separately",async()=>{
 mock.onAny().reply(config=>[200,{code:200,data:config.method==="get"?{id:"task",payload:{command:"YQ=="}}:operationSnippet()}])
 const input={id:"",name:"Snippet",description:"",visibility:"private" as const,shell_family:"sh",content:"YQ==",expected_version:0}
 expect((await operations.saveOperationSnippet(input)).version).toBe(2)
 await operations.saveOperationSnippet({...input,id:"snippet-one",expected_version:2})
 expect(JSON.parse(mock.history.put[0].data).expected_version).toBe(2)
 expect((await operations.getOperationTaskContent("task")).payload.command).toBe("YQ==")
})
it("keeps transfer creation, chunk admission and finalization as distinct requests",async()=>{
 const signal=new AbortController().signal,item=operationTransfer()
 mock.onGet("/admin/operations/transfers").reply(200,{code:200,data:[item]})
 mock.onGet("/admin/operations/transfers/transfer-one").reply(200,{code:200,data:item})
 expect(await transfers.getOperationTransfers(signal)).toEqual([item])
 expect(await transfers.getOperationTransfer(item.id,signal)).toEqual(item)
 mock.onPost("/admin/operations/transfers").reply(200,{code:200,data:item})
 await transfers.createOperationTransfer({direction:"collect",policy_id:"policy",node_ids:["node"],group_ids:[],file:{root_id:"files",relative_path:"a.txt",size:0,digest:"",mode:384,overwrite:false}})
 expect(mock.history.post).toHaveLength(1)
 mock.onPost("/admin/operations/transfers/transfer-one/finalize").reply(200,{code:200,data:{task_id:"task"}})
 expect(await transfers.finalizeOperationTransfer(item.id)).toEqual({task_id:"task"})
 mock.onPost("/admin/operations/transfers/transfer-one/cancel").reply(200,{code:200,data:{}})
 await transfers.cancelOperationTransfer(item.id)
 expect(transfers.operationDownloadURL("id/unsafe")).toBe("/api/v1/admin/operations/transfers/id%2Funsafe/download")
})
