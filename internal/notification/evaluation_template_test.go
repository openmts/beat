package notification

import "testing"

func TestEvaluationTemplateFieldsAreExplicitlyAllowed(t *testing.T) {
	for _, field := range []string{"evaluation.status", "evaluation.reason", "evaluation.epoch"} {
		if _, err := CompileTemplate("{{"+field+"}}", false); err != nil {
			t.Fatal(field, err)
		}
	}
	if _, err := CompileTemplate("{{evaluation.untrusted}}", false); err == nil {
		t.Fatal("unknown field accepted")
	}
}
