import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import * as api from "@/lib/operations-api"
import * as runtimeApi from "@/lib/runtime-api"
import { operationNode, operationPolicy, operationTask } from "@/test/operations-fixtures"
import Operations from "./operations"
vi.mock("@/lib/operations-api")
vi.mock("@/lib/runtime-api")
vi.mock("@/context/auth", () => ({useAuth:()=>({principal:{user:{id:"owner",role:"owner"},session:{reauthenticated_until:new Date(Date.now()+600000).toISOString()}}})}))
const runtimeStatus=()=>({server_time:"2026-10-14T00:00:00Z",stopping:false,workers:[{name:"alerts",configured:true,started:true,running:true,state:"healthy",failures:0,consecutive_failures:0,restarts:0,interval_seconds:60,scheduler_lag_seconds:0.5,started_at:"2026-10-14T00:00:00Z",stopped_at:"",last_attempt:"2026-10-14T00:00:00Z",last_success:"2026-10-14T00:00:00Z",last_failure:""}],metrics:{generation:1,timestamp:"2026-10-14T00:00:00Z",age_seconds:1,stale:false,available:true,connections:2,reserved:0,tracked_ips:3,builds:10,failures:0,dropped:0,build_duration_seconds:0.02,skipped_cadences:0,closed:false}})
beforeEach(()=>{
 vi.resetAllMocks()
 vi.mocked(api.getOperationServerStatus).mockResolvedValue({enabled:true,spool_configured:true,scanner_configured:false,maximum_file_bytes:2147483648,chunk_bytes:4194304})
 vi.mocked(api.getOperationTasks).mockResolvedValue({items:[operationTask()],next_cursor:""})
 vi.mocked(api.getOperationTask).mockResolvedValue(operationTask())
 vi.mocked(api.getExecutablePolicies).mockResolvedValue([operationPolicy()])
 vi.mocked(api.getOperationNodes).mockResolvedValue([operationNode()])
 vi.mocked(api.operationRequestID).mockReturnValue("request-one")
 vi.mocked(api.operationEncode).mockImplementation(value=>btoa(value))
 vi.mocked(api.operationDecode).mockImplementation(value=>value ? atob(value) : "")
 vi.mocked(runtimeApi.getRuntimeStatus).mockResolvedValue(runtimeStatus())
})
it("keeps disabled features explanatory without requesting task data",async()=>{
 vi.mocked(api.getOperationServerStatus).mockResolvedValue({enabled:false,spool_configured:true,scanner_configured:false,maximum_file_bytes:2147483648,chunk_bytes:4194304})
 render(<LocaleProvider><Operations /></LocaleProvider>)
 expect(await screen.findByText(/Remote operations are disabled/)).toBeInTheDocument()
 expect(api.getOperationTasks).not.toHaveBeenCalled()
})
it("requires deliberate risk acceptance for unknown outcome retry",async()=>{
 const user=userEvent.setup()
 render(<LocaleProvider><Operations /></LocaleProvider>)
 await user.click(await screen.findByRole("button",{name:"Details"}))
 await user.type(await screen.findByRole("textbox",{name:"Reason for retry"}),"Operator checked target")
 await user.click(screen.getByRole("button",{name:"Retry"}))
 const confirm=screen.getByRole("button",{name:"Confirm"})
 expect(confirm).toBeDisabled()
 await user.click(screen.getByRole("checkbox"))
 await user.click(confirm)
 await waitFor(()=>expect(api.retryOperationTarget).toHaveBeenCalledWith("task-one","target-one",{reason:"Operator checked target",acknowledge_unknown_risk:true}))
})
it("creates a bounded task only after review and retains the request identity",async()=>{
 const user=userEvent.setup()
 vi.mocked(api.createOperationTask).mockResolvedValue({id:"task-one"})
 render(<LocaleProvider><Operations /></LocaleProvider>)
 await user.click(await screen.findByRole("button",{name:"Create task"}))
 await user.click(screen.getByRole("combobox",{name:"Execution policy"}))
 await user.click(await screen.findByRole("option",{name:"Production policy"}))
 await user.click(screen.getByRole("checkbox",{name:/Production host/}))
 fireEvent.change(screen.getByRole("textbox",{name:"Command"}),{target:{value:"uname -a"}})
 await user.click(screen.getByRole("button",{name:"Review and dispatch"}))
 expect(api.createOperationTask).not.toHaveBeenCalled()
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 await waitFor(()=>expect(api.createOperationTask).toHaveBeenCalledWith(expect.objectContaining({dedupe_key:"request-one",command:btoa("uname -a"),node_ids:["node-one"],limits:{kind:"shell",targets:1,timeout_seconds:60,output_bytes:65536,run_as_root:false}})))
})

it("shows exact attempt output as text and clears sensitive content",async()=>{
 const user=userEvent.setup()
 vi.mocked(api.getOperationOutput).mockResolvedValue({attempt_id:"attempt-history",state:"succeeded",exit_code:0,stdout:btoa("<img src=x onerror=alert(1)>"),stderr:btoa("private stderr"),truncated:true})
 vi.mocked(api.getOperationTaskContent).mockResolvedValue({id:"task-one",payload:{dedupe_key:"key",policy_id:"policy-one",shell_family:"sh",command:btoa("private command"),node_ids:[],group_ids:[],limits:{kind:"shell",targets:1,timeout_seconds:60,output_bytes:64,run_as_root:false}}})
 render(<LocaleProvider><Operations /></LocaleProvider>)
 await user.click(await screen.findByRole("button",{name:"Details"}))
 await user.click(await screen.findByRole("button",{name:"Output"}))
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 expect(await screen.findByText("<img src=x onerror=alert(1)>")).toBeInTheDocument()
 expect(screen.queryByRole("img")).not.toBeInTheDocument()
 expect(screen.getByText(/attempt-history/)).toBeInTheDocument()
 expect(screen.getByText("Output was truncated")).toBeInTheDocument()
 await user.click(screen.getByRole("button",{name:"Clear output"}))
 expect(screen.queryByText("private stderr")).not.toBeInTheDocument()
 await user.click(screen.getByRole("button",{name:"View task content"}))
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 expect(await screen.findByText("private command")).toBeInTheDocument()
 await user.click(screen.getByRole("button",{name:"Clear content"}))
 expect(screen.queryByText("private command")).not.toBeInTheDocument()
 expect(localStorage.length).toBe(0)
})

it("keeps cancellation explicit and follows cursor navigation",async()=>{
 const user=userEvent.setup()
 vi.mocked(api.getOperationTasks).mockResolvedValue({items:[operationTask()],next_cursor:"second"})
 render(<LocaleProvider><Operations /></LocaleProvider>)
 await user.click(await screen.findByRole("button",{name:"Next"}))
 await waitFor(()=>expect(api.getOperationTasks).toHaveBeenCalledWith("second",expect.any(AbortSignal)))
 await user.click(screen.getByRole("button",{name:"Previous"}))
 await user.click(await screen.findByRole("button",{name:"Details"}))
 await user.click(await screen.findByRole("button",{name:"Cancel task"}))
 expect(api.cancelOperationTask).not.toHaveBeenCalled()
 await user.click(screen.getByRole("button",{name:"Confirm"}))
 await waitFor(()=>expect(api.cancelOperationTask).toHaveBeenCalledWith("task-one"))
})

it("preserves removed-node history without offering an impossible retry",async()=>{
 const user=userEvent.setup(),task=operationTask()
 task.targets![0].node_id=""
 task.targets![0].restore_risk=true
 vi.mocked(api.getOperationTask).mockResolvedValue(task)
 render(<LocaleProvider><Operations /></LocaleProvider>)
 await user.click(await screen.findByRole("button",{name:"Details"}))
 await screen.findByRole("textbox",{name:"Reason for retry"})
 expect(screen.queryByRole("button",{name:"Retry"})).not.toBeInTheDocument()
 expect(screen.getByRole("button",{name:"Output"})).toBeInTheDocument()
})
