import { APIError } from "@/lib/api-error"
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { reauthenticateAdmin } from "@/lib/security-api"
import { OperationConfirm } from "./operation-confirm"
const auth=vi.hoisted(()=>({until:"",totp:false}))
vi.mock("@/context/auth",()=>({useAuth:()=>({principal:{user:{role:"admin",totp_enabled_at:auth.totp ? "enabled" : null},session:{reauthenticated_until:auth.until}}})}))
vi.mock("@/lib/security-api",()=>({reauthenticateAdmin:vi.fn()}))
beforeEach(()=>{vi.resetAllMocks();auth.until="";auth.totp=false})
function Harness({execute}:{execute:()=>Promise<void>}) {
 const flow=useConfirmedOperation()
 return <><button onClick={()=>flow.request({title:"Run operation",description:"Review target",execute})}>Open</button><OperationConfirm flow={flow} /></>
}
it("requires fresh password and TOTP before an ordinary admin action",async()=>{
 auth.totp=true
 const execute=vi.fn(async()=>undefined),user=userEvent.setup()
 vi.mocked(reauthenticateAdmin).mockResolvedValue({reauthenticated_until:new Date(Date.now()+600000).toISOString()} as Awaited<ReturnType<typeof reauthenticateAdmin>>)
 render(<LocaleProvider><Harness execute={execute}/></LocaleProvider>)
 await user.click(screen.getByRole("button",{name:"Open"}))
 expect(screen.getByRole("button",{name:"Confirm"})).toBeDisabled()
 await user.type(screen.getByLabelText("Current password"),"private password")
 await user.type(screen.getByLabelText("Authenticator code"),"123456")
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 await waitFor(()=>expect(execute).toHaveBeenCalledOnce())
 expect(reauthenticateAdmin).toHaveBeenCalledWith("private password","123456")
 expect(screen.queryByLabelText("Current password")).not.toBeInTheDocument()
})
it("clears credentials after failed proof without executing or auto-retrying",async()=>{
 const execute=vi.fn(async()=>undefined),user=userEvent.setup()
 vi.mocked(reauthenticateAdmin).mockRejectedValue(new Error("proof rejected"))
 render(<LocaleProvider><Harness execute={execute}/></LocaleProvider>)
 await user.click(screen.getByRole("button",{name:"Open"}))
 await user.type(screen.getByLabelText("Current password"),"wrong password")
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 expect(await screen.findByRole("alert")).toHaveTextContent("proof rejected")
 expect(screen.getByLabelText("Current password")).toHaveValue("")
 expect(execute).not.toHaveBeenCalled()
 await user.click(screen.getByRole("button",{name:"Cancel"}))
 expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
})
it("does not dispatch a second action while the first is pending",async()=>{
 auth.until=new Date(Date.now()+600000).toISOString()
 let resolve!:()=>void
 const execute=vi.fn(()=>new Promise<void>(done=>{resolve=done})),user=userEvent.setup()
 render(<LocaleProvider><Harness execute={execute}/></LocaleProvider>)
 await user.click(screen.getByRole("button",{name:"Open"}))
 const form=screen.getByRole("button",{name:"Confirm"}).closest("form")!
 fireEvent.submit(form);fireEvent.submit(form)
 expect(execute).toHaveBeenCalledOnce()
 resolve()
 await waitFor(()=>expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
})

it("does not issue an operation after navigating away during identity proof",async()=>{
 let resolve!: (session: Awaited<ReturnType<typeof reauthenticateAdmin>>)=>void
 vi.mocked(reauthenticateAdmin).mockImplementation(()=>new Promise(done=>{resolve=done}))
 const execute=vi.fn(async()=>undefined),user=userEvent.setup()
 const page=render(<LocaleProvider><Harness execute={execute}/></LocaleProvider>)
 await user.click(screen.getByRole("button",{name:"Open"}))
 await user.type(screen.getByLabelText("Current password"),"private password")
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 page.unmount()
 await act(async()=>resolve({reauthenticated_until:new Date(Date.now()+600000).toISOString()} as Awaited<ReturnType<typeof reauthenticateAdmin>>))
 await waitFor(()=>expect(reauthenticateAdmin).toHaveBeenCalledOnce())
 expect(execute).not.toHaveBeenCalled()
})

it("requests proof after a server 428 without silently repeating the operation", async () => {
 auth.until = new Date(Date.now() + 600000).toISOString()
 const execute = vi.fn().mockRejectedValueOnce(new APIError("Proof expired", 428)).mockResolvedValue(undefined)
 const user = userEvent.setup()
 vi.mocked(reauthenticateAdmin).mockResolvedValue({ reauthenticated_until: auth.until } as Awaited<ReturnType<typeof reauthenticateAdmin>>)
 render(<LocaleProvider><Harness execute={execute} /></LocaleProvider>)
 await user.click(screen.getByRole("button", { name: "Open" }))
 await user.click(screen.getByRole("button", { name: "Confirm" }))
 expect(await screen.findByLabelText("Current password")).toHaveValue("")
 expect(execute).toHaveBeenCalledTimes(1)
 await user.type(screen.getByLabelText("Current password"), "renewed proof")
 await user.click(screen.getByRole("button", { name: "Confirm" }))
 await waitFor(() => expect(execute).toHaveBeenCalledTimes(2))
 expect(reauthenticateAdmin).toHaveBeenCalledWith("renewed proof", "")
})
