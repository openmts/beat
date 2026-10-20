package store

import (
	"encoding/json"
	"fmt"

	"github.com/beat/backend/internal/model"
)

func decodePreviousDashboard(value string) (model.DashboardDefinition, error) {
	var definition model.DashboardDefinition
	if err := json.Unmarshal([]byte(value), &definition); err != nil {
		return definition, fmt.Errorf("decode previous metric dashboard: %w", err)
	}
	return definition, model.ValidateDashboard(definition)
}
