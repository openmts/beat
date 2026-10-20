package applicationsecrets

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/beat/backend/internal/secretbox"
)

// diagnosticSourcesForDatabase 把诊断跳身份登记到部署级密钥生命周期；缺失该
// 表时（早期 schema）直接跳过。跳表没有 id 列，游标使用 job#hop 复合键。
func diagnosticSourcesForDatabase(ctx context.Context, db databaseReader) ([]resourceSource, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='diagnostic_hop_identities'`).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	key := "job_id || '#' || hop_index"
	return []resourceSource{{name: "diagnostic_hop",
		query: "SELECT " + key + ", " + boundedColumn("identity_json") + ", " + oversizedColumn("identity_json") +
			" FROM diagnostic_hop_identities WHERE (" + key + " > ? OR ? = '') ORDER BY " + key + " LIMIT ?",
		update: "UPDATE diagnostic_hop_identities SET identity_json=? WHERE " + key + "=? AND identity_json IS ?",
		scan:   scanDiagnosticHop}}, nil
}

func scanDiagnosticHop(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	var key string
	err := rows.Scan(&key, &resource.ciphertext, &resource.invalid)
	resource.id = key
	resource.required = false
	jobID, index, splitErr := splitHopKey(key)
	if splitErr != nil {
		return resource, splitErr
	}
	resource.aad = secretbox.DiagnosticHopAAD(jobID, index)
	return resource, err
}

func splitHopKey(key string) (string, int, error) {
	for position := len(key) - 1; position >= 0; position-- {
		if key[position] != '#' {
			continue
		}
		index, err := strconv.Atoi(key[position+1:])
		if err != nil || index < 1 || index > 64 {
			return "", 0, ErrUnhealthy
		}
		return key[:position], index, nil
	}
	return "", 0, ErrUnhealthy
}
