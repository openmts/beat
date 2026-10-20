package notificationqueue

import (
	"strings"
	"testing"
)

func TestEnqueueRequestRejectsUnsafeOrOversizedSourceData(t *testing.T) {
	valid := Request{SourceKey: "alert:event:triggered", SourceKind: "alert", SourceID: "event", EventKind: "alert.triggered", Subject: "Alert", Text: "Text", Data: map[string]string{"node.name": "node"}, UseSubscriptions: true}
	if err := validateRequest(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Request){
		func(r *Request) { r.SourceKey = "" }, func(r *Request) { r.SourceID = "id\n" }, func(r *Request) { r.SourceKind = strings.Repeat("x", 65) },
		func(r *Request) { r.EventKind = "security.login_unknown" }, func(r *Request) { r.Subject = "header\r\ninjection" },
		func(r *Request) { r.Text = strings.Repeat("x", MaximumTextBytes+1) }, func(r *Request) { r.Text = "\x00" },
		func(r *Request) { r.Data = map[string]string{"environment.PASSWORD": "secret"} },
		func(r *Request) {
			r.Data = map[string]string{"event.message": strings.Repeat("x", MaximumPayloadBytes)}
		},
		func(r *Request) { r.ChannelIDs = []string{"channel"} }, func(r *Request) { r.UseSubscriptions = false; r.ChannelIDs = []string{""} },
	} {
		copy := valid
		change(&copy)
		if err := validateRequest(copy); err == nil {
			t.Fatal("invalid enqueue request accepted")
		}
	}
}
