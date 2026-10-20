package ingress

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	maximumRatePerSecond   = 100000
	maximumBurst           = 1 << 20
	maximumInFlight        = 4096
	minimumIdentityEntries = 16
	maximumIdentityEntries = 65536
	minimumIdentityTTL     = time.Second
	maximumIdentityTTL     = time.Hour
	defaultIdentityEntries = 4096
	defaultIdentityTTL     = 10 * time.Minute
)

// Rate 是一个令牌桶的速率与突发上限。
type Rate struct {
	PerSecond float64
	Burst     int
}

// Policy 描述一个类别的全局预算与单身份预算。
type Policy struct {
	GlobalRate       Rate
	IdentityRate     Rate
	GlobalInFlight   int
	IdentityInFlight int
}

// Capacity 是启动时构造一次的不可变容量配置；变更需要重启。
type Capacity struct {
	Policies        [classCount]Policy
	IdentityEntries int
	IdentityTTL     time.Duration
}

// DefaultCapacity 返回代码自带的安全默认值；运行手册发布同一份数值。
func DefaultCapacity() Capacity {
	return Capacity{
		Policies: [classCount]Policy{
			PublicStatic:   policy(400, 800, 40, 80, 512, 16),
			PublicSnapshot: policy(200, 400, 20, 40, 256, 8),
			PublicHistory:  policy(20, 40, 4, 8, 32, 2),
			AuthKDF:        policy(8, 16, 1, 4, 8, 1),
			AgentRead:      policy(500, 1000, 20, 40, 256, 4),
			AgentWrite:     policy(200, 400, 10, 20, 128, 1),
			AdminRead:      policy(50, 100, 10, 20, 64, 4),
			AdminWrite:     policy(20, 40, 5, 10, 32, 2),
			AdminExpensive: policy(4, 8, 1, 2, 4, 1),
		},
		IdentityEntries: defaultIdentityEntries,
		IdentityTTL:     defaultIdentityTTL,
	}
}

func policy(globalRate float64, globalBurst int, identityRate float64, identityBurst, globalInFlight, identityInFlight int) Policy {
	return Policy{
		GlobalRate:     Rate{PerSecond: globalRate, Burst: globalBurst},
		IdentityRate:   Rate{PerSecond: identityRate, Burst: identityBurst},
		GlobalInFlight: globalInFlight, IdentityInFlight: identityInFlight,
	}
}

// Validate 拒绝零值、负值、越界与内部不一致的配置，避免静默关闭保护。
func (capacity Capacity) Validate() error {
	for _, class := range Classes() {
		if err := capacity.Policies[class].validate(class); err != nil {
			return err
		}
	}
	if capacity.IdentityEntries < minimumIdentityEntries || capacity.IdentityEntries > maximumIdentityEntries {
		return fmt.Errorf("ingress capacity: identity entries %d out of range", capacity.IdentityEntries)
	}
	if capacity.IdentityTTL < minimumIdentityTTL || capacity.IdentityTTL > maximumIdentityTTL {
		return fmt.Errorf("ingress capacity: identity ttl %s out of range", capacity.IdentityTTL)
	}
	return nil
}

func (policy Policy) validate(class Class) error {
	if err := validateRate(policy.GlobalRate, "global"); err != nil {
		return classError(class, err)
	}
	if err := validateRate(policy.IdentityRate, "identity"); err != nil {
		return classError(class, err)
	}
	if policy.IdentityRate.PerSecond > policy.GlobalRate.PerSecond {
		return classError(class, fmt.Errorf("identity rate exceeds global rate"))
	}
	if policy.IdentityRate.Burst > policy.GlobalRate.Burst {
		return classError(class, fmt.Errorf("identity burst exceeds global burst"))
	}
	if policy.GlobalInFlight < 1 || policy.GlobalInFlight > maximumInFlight {
		return classError(class, fmt.Errorf("global in-flight %d out of range", policy.GlobalInFlight))
	}
	if policy.IdentityInFlight < 1 || policy.IdentityInFlight > policy.GlobalInFlight {
		return classError(class, fmt.Errorf("identity in-flight %d out of range", policy.IdentityInFlight))
	}
	return nil
}

func classError(class Class, err error) error {
	return fmt.Errorf("ingress capacity %s: %w", class, err)
}

func validateRate(rate Rate, scope string) error {
	if math.IsNaN(rate.PerSecond) || math.IsInf(rate.PerSecond, 0) ||
		rate.PerSecond <= 0 || rate.PerSecond > maximumRatePerSecond {
		return fmt.Errorf("%s rate %g out of range", scope, rate.PerSecond)
	}
	if rate.Burst < 1 || rate.Burst > maximumBurst {
		return fmt.Errorf("%s burst %d out of range", scope, rate.Burst)
	}
	return nil
}

// 环境变量后缀到策略字段的映射；部署规模不同时只覆盖需要调整的字段。
var rateOverrideFields = []struct {
	suffix string
	apply  func(*Policy, string) error
}{
	{"_GLOBAL_RATE", func(policy *Policy, raw string) error {
		return setRateField(&policy.GlobalRate.PerSecond, raw)
	}},
	{"_GLOBAL_BURST", func(policy *Policy, raw string) error {
		return setIntField(&policy.GlobalRate.Burst, raw)
	}},
	{"_IDENTITY_RATE", func(policy *Policy, raw string) error {
		return setRateField(&policy.IdentityRate.PerSecond, raw)
	}},
	{"_IDENTITY_BURST", func(policy *Policy, raw string) error {
		return setIntField(&policy.IdentityRate.Burst, raw)
	}},
	{"_GLOBAL_INFLIGHT", func(policy *Policy, raw string) error {
		return setIntField(&policy.GlobalInFlight, raw)
	}},
	{"_IDENTITY_INFLIGHT", func(policy *Policy, raw string) error {
		return setIntField(&policy.IdentityInFlight, raw)
	}},
}

// FromEnvironment 在默认值之上应用有界覆盖；任何非法取值都在启动时失败。
func FromEnvironment(get func(string) string) (Capacity, error) {
	capacity := DefaultCapacity()
	for _, class := range Classes() {
		prefix := "BEAT_INGRESS_" + strings.ToUpper(class.String())
		policy := capacity.Policies[class]
		for _, field := range rateOverrideFields {
			raw := strings.TrimSpace(get(prefix + field.suffix))
			if raw == "" {
				continue
			}
			if err := field.apply(&policy, raw); err != nil {
				return Capacity{}, classError(class, err)
			}
		}
		capacity.Policies[class] = policy
	}
	if err := applyScalarOverrides(&capacity, get); err != nil {
		return Capacity{}, err
	}
	if err := capacity.Validate(); err != nil {
		return Capacity{}, err
	}
	return capacity, nil
}

func applyScalarOverrides(capacity *Capacity, get func(string) string) error {
	if raw := strings.TrimSpace(get("BEAT_INGRESS_IDENTITY_ENTRIES")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("ingress capacity: identity entries %q is not an integer", raw)
		}
		capacity.IdentityEntries = value
	}
	if raw := strings.TrimSpace(get("BEAT_INGRESS_IDENTITY_TTL")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("ingress capacity: identity ttl %q is not a duration", raw)
		}
		capacity.IdentityTTL = value
	}
	return nil
}

func setRateField(target *float64, raw string) error {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("rate %q is not a number", raw)
	}
	*target = value
	return nil
}

func setIntField(target *int, raw string) error {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("value %q is not an integer", raw)
	}
	*target = value
	return nil
}
