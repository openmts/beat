package secretbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestOperationFramesBindAllMetadataAndRejectCorruption(t *testing.T) {
	cipher, err := NewDataCipher("0123456789abcdef0123456789abcdef", bytes.Repeat([]byte{7}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("private frame")
	digest := sha256.Sum256(plain)
	frame := OperationFrame{ArtifactID: "artifact", Direction: "deploy", Index: 0, TotalBytes: int64(len(plain)), Digest: hex.EncodeToString(digest[:])}
	sealed, err := cipher.SealOperationFrame(plain, frame)
	if err != nil || bytes.Contains(sealed, plain) {
		t.Fatal(err)
	}
	actual, err := cipher.OpenOperationFrame(sealed, frame)
	if err != nil || !bytes.Equal(plain, actual) {
		t.Fatal(err)
	}
	for _, mutate := range []func(*OperationFrame){func(f *OperationFrame) { f.ArtifactID = "other" }, func(f *OperationFrame) { f.Direction = "collect" },
		func(f *OperationFrame) { f.Index = 1 }, func(f *OperationFrame) { f.TotalBytes++ }, func(f *OperationFrame) { f.Digest = hex.EncodeToString(make([]byte, 32)) }} {
		changed := frame
		mutate(&changed)
		if _, err := cipher.OpenOperationFrame(sealed, changed); err == nil {
			t.Fatal("frame metadata substitution accepted")
		}
	}
	corrupt := bytes.Clone(sealed)
	corrupt[len(corrupt)-1] ^= 1
	if _, err := cipher.OpenOperationFrame(corrupt, frame); err == nil {
		t.Fatal("corrupt frame accepted")
	}
	for _, bad := range [][]byte{nil, sealed[:8], bytes.Repeat([]byte{0}, 100), append(bytes.Repeat([]byte{0}, OperationFrameBytes+128), 1)} {
		if _, err := OperationFrameKeyID(bad); err == nil {
			t.Fatal("bad frame header accepted")
		}
	}
	if _, err := cipher.SealOperationFrame(make([]byte, OperationFrameBytes+1), frame); err == nil {
		t.Fatal("frame limit")
	}
	for _, bad := range []OperationFrame{{}, {ArtifactID: "a", Direction: "deploy", Index: 4096}, {ArtifactID: "a", Direction: "deploy", Digest: "bad"}} {
		if _, err := cipher.SealOperationFrame(nil, bad); err == nil {
			t.Fatal("invalid frame declaration")
		}
	}
}
