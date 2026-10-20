package applicationsecrets

import (
	"context"
	"errors"
)

func operationFrameReferences(ctx context.Context, database databaseReader, config validationConfig) (references map[string]int, resultErr error) {
	sources, err := operationSourcesForDatabase(ctx, database)
	if err != nil || len(sources) == 0 {
		return nil, err
	}
	rows, err := database.QueryContext(ctx, "SELECT data_key_id,COUNT(*) FROM operation_artifact_chunks GROUP BY data_key_id LIMIT ?", maximumDataKeys+1)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	references = map[string]int{}
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		if config.ring.ciphers[id] == nil || count < 1 {
			return nil, ErrUnhealthy
		}
		references[id] = count
	}
	return references, rows.Err()
}
