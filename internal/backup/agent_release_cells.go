package backup

import (
	"context"
	"crypto/ed25519"
	"database/sql"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/releasesignature"
)

const maximumArchivedTimestampBytes = 128

func validateArchivedReleaseCells(ctx context.Context, database *sql.DB) error {
	if err := boundedAgentRows(ctx, database); err != nil {
		return err
	}
	const query = `SELECT EXISTS(SELECT 1 FROM agent_releases WHERE
 typeof(expires_at)<>'text' OR length(CAST(expires_at AS BLOB)) NOT BETWEEN 1 AND ?
 OR typeof(verified)<>'integer' OR verified NOT IN(0,1)
 OR typeof(sigstore_bundle)<>'blob' OR length(sigstore_bundle) NOT BETWEEN 1 AND ?)
 OR EXISTS(SELECT 1 FROM agent_release_artifacts WHERE
 typeof(size)<>'integer' OR size<1 OR size>?)
 OR EXISTS(SELECT 1 FROM agent_release_trust WHERE typeof(public_key)<>'blob' OR length(public_key)<>?)`
	var invalid bool
	if err := database.QueryRowContext(ctx, query, maximumArchivedTimestampBytes, releasesignature.MaximumBundleBytes, protocol.MaximumArtifactBytes, ed25519.PublicKeySize).Scan(&invalid); err != nil {
		return err
	}
	if invalid {
		return errAgentReleaseArchive
	}
	return nil
}

func boundedAgentRows(ctx context.Context, database *sql.DB) error {
	bounds := []struct {
		table   string
		maximum int
	}{{"agent_releases", maximumArchivedAgentReleases}, {"agent_release_artifacts", maximumArchivedAgentArtifacts}, {"agent_release_trust", maximumArchivedAgentReleases}}
	for _, bound := range bounds {
		var count int
		query := "SELECT COUNT(*) FROM (SELECT 1 FROM " + bound.table + " LIMIT ?)"
		if err := database.QueryRowContext(ctx, query, bound.maximum+1).Scan(&count); err != nil {
			return err
		}
		if count > bound.maximum {
			return errAgentReleaseArchive
		}
	}
	return nil
}
