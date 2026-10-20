package adminauth

import (
	"errors"
	"fmt"
	"testing"
)

func TestLoginBudgetCountsRequestsBeforeFailures(t *testing.T) {
	const requests = 121
	service := newTestService(t)
	for index := range requests {
		_, err := service.Login(t.Context(), LoginRequest{Username: fmt.Sprintf("absent-%d", index),
			Password: "incorrect", IPAddress: fmt.Sprintf("198.51.100.%d", index)})
		if index == requests-1 && !errors.Is(err, ErrLocked) {
			t.Fatalf("want global request budget rejection, got %v", err)
		}
	}
}
