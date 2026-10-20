package secretbox

import "strconv"

// DiagnosticHopAAD 绑定作业与跳索引；跳地址只在管理员详情中解密。
func DiagnosticHopAAD(jobID string, hopIndex int) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "diagnostic_hop", RowID: jobID, Field: "identity",
		Revision: strconv.Itoa(hopIndex), PayloadVersion: "v1"}
}
