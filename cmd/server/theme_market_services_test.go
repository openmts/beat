package main

import "testing"

// 私网放通开关默认关闭，只有显式布尔 true 才放通；非法值必须让启动失败。
func TestThemeMarketAllowPrivateFollowsExplicitOperatorSwitch(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		allowed bool
		failed  bool
	}{
		{name: "未设置", value: "", allowed: false},
		{name: "显式关闭", value: "false", allowed: false},
		{name: "显式开启", value: "true", allowed: true},
		{name: "大写开启", value: "TRUE", allowed: true},
		{name: "非法值", value: "yes", allowed: false, failed: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("BEAT_THEME_MARKET_ALLOW_PRIVATE", testCase.value)
			allowed, err := themeMarketAllowPrivate()
			if failed := err != nil; failed != testCase.failed {
				t.Fatalf("expected failed=%v, actual failed=%v (err=%v)", testCase.failed, failed, err)
			}
			if allowed != testCase.allowed {
				t.Fatalf("expected allowed=%v, actual allowed=%v", testCase.allowed, allowed)
			}
		})
	}
}
