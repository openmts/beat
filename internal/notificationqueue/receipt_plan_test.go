package notificationqueue

import (
	"strings"
	"testing"
)

func TestSourceReceiptUsesIndexedMessageLookup(t *testing.T) {
	service, _ := queueFixture(t)
	rows, err := service.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+messageDeliveryReceiptQuery, "message")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	indexed := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "SCAN d") {
			t.Fatalf("receipt scans all deliveries: %s", detail)
		}
		if strings.Contains(detail, "SEARCH d USING") && strings.Contains(detail, "(message_id=?)") {
			indexed = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !indexed {
		t.Fatal("receipt lookup has no index for message_id across original and retried deliveries")
	}
}
