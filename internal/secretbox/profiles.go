package secretbox

import "strconv"

func TOTPSecretAAD(userID string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "admin_user", RowID: userID,
		Field: "totp_secret", PayloadVersion: "v1"}
}

func SSHPrivateKeyAAD(keyID string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "ssh_key", RowID: keyID,
		Field: "private_key", PayloadVersion: "v1"}
}

func OIDCProviderAAD(providerID string, version int) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "oidc_provider", RowID: providerID,
		Field: "client_secret", Revision: strconv.Itoa(version), PayloadVersion: "v1"}
}

func OIDCTransactionAAD(transactionID, providerID string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "oidc_transaction", RowID: transactionID,
		Field: "pkce_verifier", Revision: providerID, PayloadVersion: "v1"}
}
