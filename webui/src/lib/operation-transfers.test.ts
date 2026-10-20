import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest"
import MockAdapter from "axios-mock-adapter"
import { api } from "./api-client"
import { hashOperationFile, uploadOperationFile, operationChunkBytes } from "./operation-transfers"
import type { OperationTransfer } from "@/types/operations"
const mock = new MockAdapter(api)
afterEach(() => mock.reset())
const originalArrayBuffer = Object.getOwnPropertyDescriptor(Blob.prototype, "arrayBuffer")
beforeAll(() => Object.defineProperty(Blob.prototype, "arrayBuffer", { configurable:true, value: function(this: Blob) {
 return new Promise<ArrayBuffer>((resolve,reject) => { const reader=new FileReader(); reader.onload=()=>resolve(reader.result as ArrayBuffer); reader.onerror=()=>reject(reader.error); reader.readAsArrayBuffer(this) })
} }))
afterAll(() => { if(originalArrayBuffer) Object.defineProperty(Blob.prototype,"arrayBuffer",originalArrayBuffer); else Reflect.deleteProperty(Blob.prototype,"arrayBuffer") })
const fileOf = (bytes: Uint8Array | string) => new File([typeof bytes === "string" ? bytes : new Uint8Array(bytes).buffer], "sample.bin")
const options = () => ({ signal: new AbortController().signal, progress: vi.fn() })

it("hashes standard vectors including empty files without network requests", async () => {
 expect(await hashOperationFile(fileOf("abc"), options())).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
 expect(await hashOperationFile(fileOf(""), options())).toBe("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
 expect(mock.history.post).toHaveLength(0)
})

it("resumes only a missing final chunk with exact content-range and digest", async () => {
 const bytes = new Uint8Array(operationChunkBytes + 3); bytes.set([97,98,99], operationChunkBytes)
 const file = fileOf(bytes), upload = options()
 const transfer: OperationTransfer = { id: "resume", direction: "deploy", state: "uploading", size: file.size, digest: await hashOperationFile(file, options()), expires_at: Date.now()+60000, missing_chunks: [1] }
 mock.onPost("/admin/operations/transfers/resume/chunks").reply(config => {
  expect(config.headers?.["Content-Range"]).toBe(`bytes ${operationChunkBytes}-${operationChunkBytes+2}/${file.size}`)
  expect(config.headers?.["X-Beat-Chunk-Index"]).toBe("1")
  expect(config.headers?.["X-Content-SHA256"]).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
  return [200, {}]
 })
 await uploadOperationFile(transfer, file, upload)
 expect(mock.history.post).toHaveLength(1)
 expect(upload.progress).toHaveBeenNthCalledWith(1, operationChunkBytes)
 expect(upload.progress).toHaveBeenLastCalledWith(file.size)
})

it("rejects a changed file before any upload and stops when paused", async () => {
 const file = fileOf("abc"), controller = new AbortController()
 const transfer: OperationTransfer = { id:"reject", direction:"deploy", state:"uploading", size:3, digest:"0".repeat(64),expires_at:0,missing_chunks:[0] }
 await expect(uploadOperationFile(transfer,file,options())).rejects.toThrow("does not match")
 controller.abort()
 await expect(hashOperationFile(file,{signal:controller.signal,progress:vi.fn()})).rejects.toThrow()
 expect(mock.history.post).toHaveLength(0)
})

it("propagates chunk failure without finalizing or reporting full progress", async () => {
 const file=fileOf("abc"), upload=options()
 const transfer: OperationTransfer={id:"failed",direction:"deploy",state:"uploading",size:3,digest:await hashOperationFile(file,options()),expires_at:0,missing_chunks:[0]}
 mock.onPost().reply(409,{message:"chunk conflict"})
 await expect(uploadOperationFile(transfer,file,upload)).rejects.toThrow("chunk conflict")
 expect(upload.progress).toHaveBeenLastCalledWith(0)
 expect(mock.history.post).toHaveLength(1)
})
