package ingress

// Limits 是类别的有效数值上限，只读地暴露给 Operations 视图与指标。
type Limits struct {
	GlobalRatePerSecond   float64 `json:"global_rate_per_second"`
	GlobalBurst           int     `json:"global_burst"`
	GlobalInFlight        int     `json:"global_in_flight"`
	IdentityRatePerSecond float64 `json:"identity_rate_per_second"`
	IdentityBurst         int     `json:"identity_burst"`
	IdentityInFlight      int     `json:"identity_in_flight"`
}

// ReasonCount 是固定拒绝原因及其计数。
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  uint64 `json:"count"`
}

// ClassView 是单个类别的只读状态，不包含任何客户端身份。
type ClassView struct {
	Class           string        `json:"class"`
	Admitted        uint64        `json:"admitted"`
	Rejected        uint64        `json:"rejected"`
	Reasons         []ReasonCount `json:"reasons"`
	InFlight        int           `json:"in_flight"`
	IdentityEntries int           `json:"identity_entries"`
	Limits          Limits        `json:"limits"`
}

// Snapshot 是准入控制的完整只读快照。
type Snapshot struct {
	IdentityEntries      int         `json:"identity_entries"`
	IdentityEntriesLimit int         `json:"identity_entries_limit"`
	IdentityTTLSeconds   float64     `json:"identity_ttl_seconds"`
	Classes              []ClassView `json:"classes"`
}

// Snapshot 汇总当前计数与有效上限；调用频率由指标抓取与只读视图决定。
func (limiter *Limiter) Snapshot() Snapshot {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	entries := [classCount]int{}
	for key := range limiter.identities {
		if key.class.Valid() {
			entries[key.class]++
		}
	}
	view := Snapshot{
		IdentityEntries: len(limiter.identities), IdentityEntriesLimit: limiter.capacity.IdentityEntries,
		IdentityTTLSeconds: limiter.capacity.IdentityTTL.Seconds(), Classes: make([]ClassView, 0, classCount),
	}
	for _, class := range Classes() {
		view.Classes = append(view.Classes, limiter.classView(class, entries[class]))
	}
	return view
}

func (limiter *Limiter) classView(class Class, entries int) ClassView {
	policy := limiter.capacity.Policies[class]
	reasons := make([]ReasonCount, 0, reasonCount)
	for reason := Reason(0); reason < reasonCount; reason++ {
		reasons = append(reasons, ReasonCount{Reason: reason.String(), Count: limiter.rejectedReasons[class][reason]})
	}
	return ClassView{
		Class: class.String(), Admitted: limiter.admitted[class], Rejected: limiter.rejected[class],
		Reasons: reasons, InFlight: limiter.globalInFlight[class], IdentityEntries: entries,
		Limits: Limits{
			GlobalRatePerSecond: policy.GlobalRate.PerSecond, GlobalBurst: policy.GlobalRate.Burst,
			GlobalInFlight: policy.GlobalInFlight, IdentityRatePerSecond: policy.IdentityRate.PerSecond,
			IdentityBurst: policy.IdentityRate.Burst, IdentityInFlight: policy.IdentityInFlight,
		},
	}
}
