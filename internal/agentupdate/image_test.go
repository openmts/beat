//go:build linux

package agentupdate

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"testing"
)

func TestExecutableFormatValidation(t *testing.T) {
	for _, kind := range []string{"short", "magic", "class", "endian", "abi", "version", "header-size", "kind", "machine", "arch", "entry", "count", "width", "offset", "bounds", "no-execute", "segment-size", "overflow", "arm64", "pie"} {
		t.Run(kind, func(t *testing.T) {
			b := testELF(512, 1)
			arch := "amd64"
			o := binary.LittleEndian
			switch kind {
			case "short":
				b = b[:20]
			case "magic":
				b[0] = 0
			case "class":
				b[4] = 1
			case "endian":
				b[5] = 2
			case "abi":
				b[7] = 9
			case "version":
				b[20] = 0
			case "header-size":
				b[52] = 0
			case "kind":
				b[16] = 0
			case "machine":
				b[18] = 0
			case "arch":
				arch = "386"
			case "entry":
				o.PutUint64(b[24:], 0)
			case "count":
				o.PutUint16(b[56:], 0)
			case "width":
				o.PutUint16(b[54:], 1)
			case "offset":
				o.PutUint64(b[32:], 9999)
			case "bounds":
				o.PutUint64(b[32:], 500)
			case "no-execute":
				b[68] = 0
			case "segment-size":
				o.PutUint64(b[96:], 9999)
			case "overflow":
				o.PutUint64(b[80:], ^uint64(0))
			case "arm64":
				arch = "arm64"
				o.PutUint16(b[18:], 183)
			case "pie":
				o.PutUint16(b[16:], 3)
			}
			e := verifyELF(bytes.NewReader(b), int64(len(b)), arch)
			if kind == "arm64" || kind == "pie" {
				must(t, e)
			} else {
				wantError(t, ErrFormat, e)
			}
		})
	}
	b := testELF(512, 1)
	wantError(t, ErrFormat, verifyELF(bytes.NewReader(b[:64]), 512, "amd64"))
}

func TestImageIntegrityAndContext(t *testing.T) {
	f := newFixture(t)
	file, e := os.Open(f.u.config.Executable)
	must(t, e)
	defer func() { _ = file.Close() }()
	wantError(t, ErrIntegrity, imageVerifier("amd64").verifyImage(context.Background(), file, imageOf(f.next, "v2.0.0")))
	image := imageOf(f.old, "v1.0.0")
	image.SHA256 = imageOf(f.next, "v2.0.0").SHA256
	wantError(t, ErrIntegrity, imageVerifier("amd64").verifyImage(context.Background(), file, image))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wantError(t, context.Canceled, imageVerifier("amd64").verifyImage(ctx, file, f.u.config.Installed))
	must(t, file.Close())
	if e = imageVerifier("amd64").verifyImage(context.Background(), file, f.u.config.Installed); e == nil {
		t.Fatal("closed file accepted")
	}
}
