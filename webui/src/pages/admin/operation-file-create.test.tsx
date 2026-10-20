import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import * as sources from "@/lib/operations-api"
import * as transfers from "@/lib/operation-transfers"
import { operationNode, operationPolicy, operationTransfer } from "@/test/operations-fixtures"
import { OperationTransferCreate } from "./operation-transfer-create"
vi.mock("@/lib/operations-api")
vi.mock("@/lib/operation-transfers")
vi.mock("@/context/auth",()=>({useAuth:()=>({principal:{user:{id:"owner",role:"owner"},session:{reauthenticated_until:new Date(Date.now()+600000).toISOString()}}})}))
beforeEach(()=>{
 vi.resetAllMocks()
 vi.mocked(sources.getExecutablePolicies).mockResolvedValue([operationPolicy()])
 vi.mocked(sources.getOperationNodes).mockResolvedValue([operationNode()])
 vi.mocked(transfers.createOperationTransfer).mockResolvedValue(operationTransfer())
 vi.mocked(transfers.hashOperationFile).mockImplementation(async(file,options)=>{options.progress(file.size);return "digest"})
})
async function selectTargets(user:ReturnType<typeof userEvent.setup>) {
 await user.click(screen.getByRole("combobox",{name:"Policy"}))
 await user.click(await screen.findByRole("option",{name:"Production policy"}))
 await user.click(screen.getByRole("combobox",{name:"Root"}))
 await user.click(await screen.findByRole("option",{name:"Shared files"}))
 await user.click(screen.getByRole("checkbox",{name:"Production host"}))
 fireEvent.change(screen.getByRole("textbox",{name:"Relative path"}),{target:{value:"app.txt"}})
}
it("creates a deployment with explicit file permissions after hashing and confirmation",async()=>{
 const user=userEvent.setup(),created=vi.fn(),file=new File(["abc"],"data.txt")
 render(<LocaleProvider><OperationTransferCreate close={vi.fn()} created={created}/></LocaleProvider>)
 await selectTargets(user)
 await user.upload(screen.getByLabelText("Local file"),file)
 await user.click(screen.getByRole("checkbox",{name:"Allow replacing target file"}))
 await user.click(screen.getByRole("checkbox",{name:"Make executable"}))
 await user.click(screen.getByRole("button",{name:"Continue"}))
 expect(transfers.createOperationTransfer).not.toHaveBeenCalled()
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 await waitFor(()=>expect(created).toHaveBeenCalledWith("transfer-one",file))
 expect(transfers.createOperationTransfer).toHaveBeenCalledWith({direction:"deploy",filename:"data.txt",policy_id:"policy-one",node_ids:["node-one"],group_ids:[],file:{root_id:"files",relative_path:"app.txt",size:3,digest:"digest",mode:448,overwrite:true}})
})
it("creates a collection without reading a browser file",async()=>{
 const user=userEvent.setup(),created=vi.fn()
 render(<LocaleProvider><OperationTransferCreate close={vi.fn()} created={created}/></LocaleProvider>)
 await user.click(screen.getByRole("combobox",{name:"Direction"}))
 await user.click(await screen.findByRole("option",{name:"Collect file"}))
 await selectTargets(user)
 expect(screen.queryByLabelText("Local file")).not.toBeInTheDocument()
 await user.click(screen.getByRole("button",{name:"Continue"}))
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 await waitFor(()=>expect(created).toHaveBeenCalledWith("transfer-one",undefined))
 expect(transfers.hashOperationFile).not.toHaveBeenCalled()
 expect(transfers.createOperationTransfer).toHaveBeenCalledWith(expect.objectContaining({direction:"collect",file:{root_id:"files",relative_path:"app.txt",size:0,digest:"",mode:384,overwrite:false}}))
})
it("rejects an oversized file locally and preserves a failed creation for review",async()=>{
 const user=userEvent.setup(),created=vi.fn(),policy=operationPolicy();policy.policy.roots[0].maximum_bytes=2
 vi.mocked(sources.getExecutablePolicies).mockResolvedValue([policy])
 vi.mocked(transfers.createOperationTransfer).mockRejectedValue(new Error("quota rejected"))
 render(<LocaleProvider><OperationTransferCreate close={vi.fn()} created={created}/></LocaleProvider>)
 await selectTargets(user)
 await user.upload(screen.getByLabelText("Local file"),new File(["abc"],"large.txt"))
 expect(screen.getByRole("button",{name:"Continue"})).toBeDisabled()
 await user.upload(screen.getByLabelText("Local file"),new File(["a"],"small.txt"))
 await user.click(screen.getByRole("button",{name:"Continue"}))
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 expect(await screen.findByRole("alert")).toHaveTextContent("quota rejected")
 expect(created).not.toHaveBeenCalled()
})
