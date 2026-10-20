package applicationsecrets

import (
	"context"
	"database/sql"

	"github.com/beat/backend/internal/secretbox"
)

func operationSourcesForDatabase(ctx context.Context, db databaseReader) ([]resourceSource, error) {
	const requiredTables = 13
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN (
		'operation_policies','operation_policy_nodes','operation_policy_groups','operation_snippets','operation_snippet_versions',
		'operation_tasks','operation_task_targets','operation_attempts','operation_capabilities','operation_transfers','operation_transfer_targets','operation_artifacts','operation_artifact_chunks')`).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	if count != requiredTables {
		return nil, ErrUnhealthy
	}
	return operationSources(), nil
}

func operationSources() []resourceSource {
	return []resourceSource{
		{name: "operation_snippet", query: resourceQuery("operation_snippet_versions", "id, snippet_id, version, shell_family, "+boundedColumn("content_encrypted"), oversizedColumn("content_encrypted")),
			update: "UPDATE operation_snippet_versions SET content_encrypted=? WHERE id=? AND content_encrypted IS ?", scan: scanOperationSnippet},
		operationSource(operationSourceProfile{"operation_task", "operation_tasks", "payload_encrypted", secretbox.OperationTaskAAD, true}),
		operationSource(operationSourceProfile{"operation_stdout", "operation_attempts", "stdout_encrypted", func(id string) secretbox.ResourceAAD { return secretbox.OperationOutputAAD(id, "stdout") }, false}),
		operationSource(operationSourceProfile{"operation_stderr", "operation_attempts", "stderr_encrypted", func(id string) secretbox.ResourceAAD { return secretbox.OperationOutputAAD(id, "stderr") }, false}),
		operationSource(operationSourceProfile{"operation_transfer", "operation_transfers", "path_encrypted", secretbox.OperationTransferAAD, true}),
		operationSource(operationSourceProfile{"operation_artifact", "operation_artifacts", "metadata_encrypted", secretbox.OperationArtifactAAD, true}),
	}
}

type operationSourceProfile struct {
	name, table, column string
	aad                 func(string) secretbox.ResourceAAD
	required            bool
}

func operationSource(profile operationSourceProfile) resourceSource {
	name, table, column, aad, required := profile.name, profile.table, profile.column, profile.aad, profile.required
	return resourceSource{name: name, query: resourceQuery(table, "id, "+boundedColumn(column), oversizedColumn(column)),
		update: "UPDATE " + table + " SET " + column + "=? WHERE id=? AND " + column + " IS ?",
		scan: func(rows *sql.Rows) (secretResource, error) {
			var resource secretResource
			err := rows.Scan(&resource.id, &resource.ciphertext, &resource.invalid)
			resource.required = required
			resource.aad = aad(resource.id)
			return resource, err
		}}
}

func scanOperationSnippet(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	var snippetID, shell string
	var version int
	err := rows.Scan(&resource.id, &snippetID, &version, &shell, &resource.ciphertext, &resource.invalid)
	resource.required = true
	resource.aad = secretbox.OperationSnippetAAD(snippetID, version, shell)
	return resource, err
}
