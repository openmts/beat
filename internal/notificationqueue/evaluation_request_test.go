package notificationqueue

import "testing"

func TestEvaluationHealthRequestsUseFixedFieldsWithoutNumericValue(t *testing.T) {
	for _, kind := range []string{"alert.no_data", "alert.evaluation_error", "alert.evaluation_recovered"} {
		request := Request{SourceKey: "alert-evaluation:epoch:kind", SourceKind: "alert_evaluation", SourceID: "event", EventKind: kind, UseSubscriptions: true,
			Data: map[string]string{"evaluation.status": "no_data", "evaluation.reason": "missing_value", "evaluation.epoch": "epoch"}}
		if err := validateRequest(request); err != nil {
			t.Fatal(kind, err)
		}
		request.Data["evaluation.untrusted"] = "extra"
		if err := validateRequest(request); err == nil {
			t.Fatal("unknown evaluation field accepted")
		}
	}
}
