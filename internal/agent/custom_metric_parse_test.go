package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestParseMetricValueClassifiesFailures(t *testing.T) {
	for _, test := range []struct {
		output string
		want   error
	}{
		{"", errMetricEmpty},
		{"  \n", errMetricEmpty},
		{"0.66,", errMetricNotANumber},
		{"not-a-number", errMetricNotANumber},
		{"NaN", errMetricValue},
		{"Inf", errMetricValue},
		{"1e999", errMetricNotANumber},
	} {
		if _, err := parseMetricValue(test.output); !errors.Is(err, test.want) {
			t.Errorf("parse %q error = %v, want %v", test.output, err, test.want)
		}
	}
}

// 非法输出只回显有界前缀，避免把整段命令输出写进日志。
func TestParseMetricValueBoundsEchoedToken(t *testing.T) {
	_, err := parseMetricValue(strings.Repeat("x", 512))
	if err == nil {
		t.Fatal("invalid output was accepted")
	}
	if strings.Contains(err.Error(), strings.Repeat("x", maxCustomMetricOutputToken+1)) {
		t.Fatalf("error echoed more than %d characters", maxCustomMetricOutputToken)
	}
	if !strings.Contains(err.Error(), strings.Repeat("x", maxCustomMetricOutputToken)) {
		t.Fatalf("error lost the offending prefix: %v", err)
	}
}
