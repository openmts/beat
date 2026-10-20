package agentrollout

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/releasesignature"
)

const maximumPublicRootBytes = 1 << 20

const maximumPolicyDocumentBytes = 2 << 20

type signaturePolicyReader interface {
	Policy() releasesignature.PolicyMetadata
}

type signaturePolicySnapshot struct {
	revision, rootDigest string
	document             []byte
}

func snapshotSignaturePolicy(verifier SignatureVerifier) (*signaturePolicySnapshot, error) {
	if verifier == nil {
		return nil, ErrSignature
	}
	reader, ok := verifier.(signaturePolicyReader)
	if !ok {
		return nil, ErrSignature
	}
	policy := reader.Policy()
	if err := validateSignaturePolicy(policy); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(policy)
	if err != nil || len(encoded) > maximumPolicyDocumentBytes {
		return nil, ErrSignature
	}
	return &signaturePolicySnapshot{revision: policy.Revision, rootDigest: policy.RootSHA256, document: encoded}, nil
}

func validateSignaturePolicy(policy releasesignature.PolicyMetadata) error {
	if policy.Version != 1 || len(policy.Root) == 0 || len(policy.Root) > maximumPublicRootBytes || !json.Valid(policy.Root) {
		return ErrSignature
	}
	digest := sha256.Sum256(policy.Root)
	if hex.EncodeToString(digest[:]) != policy.RootSHA256 || !protocol.ValidSHA256(policy.VerifierSHA256) {
		return ErrSignature
	}
	if !validPolicyRules(policy) {
		return ErrSignature
	}
	revision := sha256.Sum256([]byte(strings.Join([]string{"1", policy.RootSHA256, policy.VerifierSHA256, policy.CertificateIdentity, policy.ReleaseWorkflow, policy.OIDCIssuer}, "\x00")))
	if hex.EncodeToString(revision[:]) != policy.Revision {
		return ErrSignature
	}
	return nil
}

func validPolicyRules(policy releasesignature.PolicyMetadata) bool {
	if (policy.CertificateIdentity == "") == (policy.ReleaseWorkflow == "") {
		return false
	}
	if policy.OIDCIssuer == "" {
		return false
	}
	for _, value := range []string{policy.CertificateIdentity, policy.ReleaseWorkflow, policy.OIDCIssuer} {
		if len(value) > 2048 || strings.ContainsAny(value, "\x00\r\n") {
			return false
		}
	}
	return true
}

func (tx *transaction) persistSignaturePolicy(policy signaturePolicySnapshot) error {
	var root string
	var document []byte
	err := tx.db.QueryRowContext(tx.ctx, "SELECT root_sha256,CASE WHEN length(policy_json)<=? THEN policy_json ELSE NULL END FROM agent_release_signature_policies WHERE revision=?", maximumPolicyDocumentBytes, policy.revision).Scan(&root, &document)
	if err == nil && (root != policy.rootDigest || !bytes.Equal(document, policy.document)) {
		return ErrSignature
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrUnavailable
	}
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.exec("INSERT INTO agent_release_signature_policies(revision,root_sha256,policy_json,created_at) VALUES(?,?,?,?)", policy.revision, policy.rootDigest, policy.document, tx.at); err != nil {
			return err
		}
	}
	return tx.activateSignaturePolicy(policy.revision)
}

func (tx *transaction) activateSignaturePolicy(revision string) error {
	var previous string
	err := tx.db.QueryRowContext(tx.ctx, "SELECT policy_revision FROM agent_release_signature_state WHERE id=1").Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrUnavailable
	}
	if previous == revision {
		return nil
	}
	if err := tx.exec("INSERT INTO agent_release_signature_state(id,policy_revision,updated_at) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET policy_revision=excluded.policy_revision,updated_at=excluded.updated_at", revision, tx.at); err != nil {
		return err
	}
	if err := tx.exec("UPDATE agent_releases SET verified=0"); err != nil {
		return err
	}
	if err := tx.exec("UPDATE agent_rollouts SET state='paused',reason='trust_policy_changed',updated_at=? WHERE state NOT IN('completed','aborted')", tx.at); err != nil {
		return err
	}
	return tx.audit(audit{Action: "release.signature_policy.loaded", Kind: "release_signature_policy", ID: revision})
}

func (s *Service) signaturePolicyRevision(tx *transaction) (string, error) {
	if s.signaturePolicy == nil || (!s.pinnedOnly && s.sigstore == nil) {
		return "", ErrSignature
	}
	var revision string
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT policy_revision FROM agent_release_signature_state WHERE id=1").Scan(&revision); err != nil {
		return "", ErrSignature
	}
	if revision != s.signaturePolicy.revision {
		return "", ErrSignature
	}
	return revision, nil
}

func (s *Service) requireSignaturePolicyRevision(tx *transaction, expected string) error {
	revision, err := s.signaturePolicyRevision(tx)
	if err != nil {
		return err
	}
	if revision != expected {
		return ErrSignature
	}
	return nil
}
