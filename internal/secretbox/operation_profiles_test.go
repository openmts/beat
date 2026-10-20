package secretbox

import (
	"bytes"
	"testing"
)

func TestOperationAADAndFingerprintsBindResources(t *testing.T) {
	cipher, err := NewDataCipher("0123456789abcdef0123456789abcdef", bytes.Repeat([]byte{7}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, aad := range []ResourceAAD{OperationSnippetAAD("snippet", 1, "sh"), OperationTaskAAD("task"), OperationOutputAAD("attempt", "stdout"), OperationTransferAAD("transfer"), OperationArtifactAAD("artifact")} {
		sealed, err := cipher.Seal([]byte("private"), aad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cipher.Open(sealed, aad); err != nil {
			t.Fatal(err)
		}
		aad.RowID += "other"
		if _, err := cipher.Open(sealed, aad); err == nil {
			t.Fatal("swapped resource accepted")
		}
	}
	fingerprint, err := cipher.OperationFingerprint([]byte("value"))
	if err != nil || !cipher.VerifyOperationFingerprint([]byte("value"), fingerprint) {
		t.Fatal(err)
	}
	if cipher.VerifyOperationFingerprint([]byte("other"), fingerprint) {
		t.Fatal("changed content matched")
	}
	if _, err := cipher.OperationFingerprint(make([]byte, MaximumSecretBytes+1)); err == nil {
		t.Fatal("oversized content accepted")
	}
}
