import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import * as api from "@/lib/operation-transfers"
import { operationTransfer } from "@/test/operations-fixtures"
import { OperationTransferDetail } from "./operation-transfer-detail"
vi.mock("@/lib/operation-transfers")
vi.mock("@/context/auth",()=>({useAuth:()=>({principal:{user:{id:"owner",role:"owner"},session:{reauthenticated_until:new Date(Date.now()+600000).toISOString()}}})}))
beforeEach(()=>{vi.resetAllMocks();vi.mocked(api.getOperationTransfer).mockResolvedValue(operationTransfer())})
afterEach(()=>vi.restoreAllMocks())
it("shows download for a completed collection only when its artifact is available",async()=>{
 const user=userEvent.setup(), click=vi.spyOn(HTMLAnchorElement.prototype,"click").mockImplementation(()=>undefined)
 vi.mocked(api.getOperationTransfer).mockResolvedValue({...operationTransfer(),direction:"collect",state:"succeeded",download_available:true,task_id:"task-one"})
 vi.mocked(api.operationDownloadURL).mockReturnValue("/api/v1/admin/operations/transfers/transfer-one/download")
 render(<LocaleProvider><OperationTransferDetail id="transfer-one" close={vi.fn()} task={vi.fn()} /></LocaleProvider>)
 await user.click(await screen.findByRole("button",{name:"Download"}))
 expect(click).not.toHaveBeenCalled()
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 await waitFor(()=>expect(click).toHaveBeenCalledOnce())
 expect((click.mock.instances[0] as HTMLAnchorElement).getAttribute("href")).toBe("/api/v1/admin/operations/transfers/transfer-one/download")
})
it("does not offer download for a succeeded task whose artifact was removed",async()=>{
 vi.mocked(api.getOperationTransfer).mockResolvedValue({...operationTransfer(),direction:"collect",state:"succeeded",download_available:false})
 render(<LocaleProvider><OperationTransferDetail id="transfer-one" close={vi.fn()} task={vi.fn()} /></LocaleProvider>)
 await screen.findByText("Succeeded")
 expect(screen.queryByRole("button",{name:"Download"})).not.toBeInTheDocument()
})
it("refetches missing chunks before resume and finalizes only after upload",async()=>{
 const user=userEvent.setup(), file=new File(["abc"],"data.txt")
 vi.mocked(api.uploadOperationFile).mockImplementation(async(_transfer,_file,options)=>{options.progress(3)})
 vi.mocked(api.finalizeOperationTransfer).mockResolvedValue({task_id:"task-one"})
 render(<LocaleProvider><OperationTransferDetail id="transfer-one" initialFile={file} close={vi.fn()} task={vi.fn()} /></LocaleProvider>)
 await user.click(await screen.findByRole("button",{name:"Upload / resume"}))
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 await waitFor(()=>expect(api.finalizeOperationTransfer).toHaveBeenCalledWith("transfer-one"))
 expect(vi.mocked(api.getOperationTransfer).mock.calls.length).toBeGreaterThanOrEqual(2)
 expect(vi.mocked(api.uploadOperationFile).mock.invocationCallOrder[0]).toBeLessThan(vi.mocked(api.finalizeOperationTransfer).mock.invocationCallOrder[0])
})
it("pauses locally without canceling the durable transfer or finalizing it",async()=>{
 const user=userEvent.setup(), file=new File(["abc"],"data.txt")
 vi.mocked(api.uploadOperationFile).mockImplementation(async(_transfer,_file,options)=>new Promise((_resolve,reject)=>{options.signal.addEventListener("abort",()=>reject(new Error("paused"))) }))
 render(<LocaleProvider><OperationTransferDetail id="transfer-one" initialFile={file} close={vi.fn()} task={vi.fn()} /></LocaleProvider>)
 await user.click(await screen.findByRole("button",{name:"Upload / resume"}))
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 await user.click(await screen.findByRole("button",{name:"Pause upload"}))
 expect(await screen.findByRole("alert")).toHaveTextContent("Upload paused")
 expect(api.cancelOperationTransfer).not.toHaveBeenCalled()
 expect(api.finalizeOperationTransfer).not.toHaveBeenCalled()
})

it("does not finalize after leaving the page at the last upload acknowledgement",async()=>{
 const user=userEvent.setup(),file=new File(["abc"],"data.txt")
 let leave: () => void = ()=>undefined
 vi.mocked(api.uploadOperationFile).mockImplementation(async()=>{leave()})
 const page=render(<LocaleProvider><OperationTransferDetail id="transfer-one" initialFile={file} close={vi.fn()} task={vi.fn()} /></LocaleProvider>)
 leave=page.unmount
 await user.click(await screen.findByRole("button",{name:"Upload / resume"}))
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 await waitFor(()=>expect(api.uploadOperationFile).toHaveBeenCalledOnce())
 expect(api.finalizeOperationTransfer).not.toHaveBeenCalled()
})
