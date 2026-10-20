package beatcli

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	cli "github.com/urfave/cli/v3"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/releasemanifest"
)

const (
	specFlag        = "spec"
	keyFlag         = "key"
	outFlag         = "out"
	keyIDFlag       = "key-id"
	trustFlag       = "trust"
	serverTrustFlag = "server-trust"
	notBeforeFlag   = "not-before"
	expiresAtFlag   = "expires-at"
)

const defaultTrustValidity = 90 * 24 * time.Hour

// releaseCommand 是纯本地命令组：它只读写文件，不连接服务端也不使用服务令牌。
func (a *application) releaseCommand() *cli.Command {
	return &cli.Command{Name: "release", Usage: "build, sign and verify signed Agent release manifests (local files only)",
		Commands: []*cli.Command{a.releaseSignCommand(), a.releaseKeygenCommand()}}
}

// releaseSignCommand 把发布描述编译成 canonical manifest 并签名，写出协议要求的 payload/signature。
func (a *application) releaseSignCommand() *cli.Command {
	return &cli.Command{Name: "sign", Usage: "compile a release spec and sign the canonical manifest",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: specFlag, Required: true, Usage: "release spec JSON file"},
			&cli.StringFlag{Name: keyFlag, Required: true, Usage: "Ed25519 private key file (base64 or hex seed/private key)"},
			&cli.StringFlag{Name: outFlag, Required: true, Usage: "signed manifest output file"},
		},
		Action: func(_ context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return errors.New("release sign does not accept positional arguments")
			}
			spec, err := readReleaseSpec(command.String(specFlag))
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			manifest, err := releasemanifest.Build(spec, now)
			if err != nil {
				return fmt.Errorf("build manifest: %w", err)
			}
			payload, err := protocol.CanonicalManifest(manifest)
			if err != nil {
				return fmt.Errorf("canonical manifest: %w", err)
			}
			private, err := readPrivateKey(command.String(keyFlag))
			if err != nil {
				return err
			}
			signed, err := releasemanifest.Sign(payload, private)
			if err != nil {
				return err
			}
			if err := verifySignature(signed, manifest, private.Public().(ed25519.PublicKey), now); err != nil {
				return err
			}
			if err := writeFile(command.String(outFlag), encodeSigned(signed), false); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(a.options.Output, "manifest %s channel=%s artifacts=%d digest=%s\n",
				manifest.Version, manifest.Channel, len(manifest.Artifacts), protocol.ManifestDigest(payload))
			_, _ = fmt.Fprintf(a.options.Output, "wrote %s\n", command.String(outFlag))
			return nil
		}}
}

// releaseKeygenCommand 生成发布私钥、Agent 信任文件和 Server 侧 rollout 信任配置。
func (a *application) releaseKeygenCommand() *cli.Command {
	return &cli.Command{Name: "keygen", Usage: "create an Ed25519 release key with the matching Agent and Server trust files",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: keyIDFlag, Required: true, Usage: "signing key ID recorded in manifests"},
			&cli.StringFlag{Name: keyFlag, Required: true, Usage: "private key output file (base64 seed, 0600)"},
			&cli.StringFlag{Name: trustFlag, Required: true, Usage: "Agent trust file output"},
			&cli.StringFlag{Name: serverTrustFlag, Required: true, Usage: "Server BEAT_AGENT_ROLLOUT_CONFIG output"},
			&cli.StringFlag{Name: notBeforeFlag, Usage: "RFC3339 trust start (default: one hour ago)"},
			&cli.StringFlag{Name: expiresAtFlag, Usage: "RFC3339 trust expiry (default: 90 days)"},
		},
		Action: func(_ context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return errors.New("release keygen does not accept positional arguments")
			}
			now := time.Now().UTC()
			notBefore, expiresAt, err := trustWindow(command, now)
			if err != nil {
				return err
			}
			public, private, err := releasemanifest.GenerateKey()
			if err != nil {
				return err
			}
			seed, err := releasemanifest.EncodeSeed(private)
			if err != nil {
				return err
			}
			trust := []protocol.SigningTrust{{KeyID: command.String(keyIDFlag), PublicKey: public, NotBefore: notBefore, ExpiresAt: expiresAt}}
			server := serverTrustConfig{SchemaVersion: 1, Enabled: true, SignatureMode: pinnedSignatureMode,
				Keys: []serverTrustKey{{ID: command.String(keyIDFlag), PublicKey: public, NotBefore: notBefore, ExpiresAt: expiresAt}}}
			for path, content := range map[string][]byte{
				command.String(keyFlag):         []byte(seed + "\n"),
				command.String(trustFlag):       encodeJSON(trust),
				command.String(serverTrustFlag): encodeJSON(server),
			} {
				if err := writeFile(path, content, true); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(a.options.Output, "key %s id=%s valid %s..%s\n",
				command.String(keyFlag), command.String(keyIDFlag), notBefore.Format(time.RFC3339), expiresAt.Format(time.RFC3339))
			return nil
		}}
}

// pinnedSignatureMode 与服务端 cmd/server 的 rolloutSignatureModePinned 取值一致：
// keygen 只产出固定公钥信任锚，因此显式写入该模式，服务端不会去读不存在的 Sigstore 策略。
const pinnedSignatureMode = "pinned_ed25519"

// serverTrustConfig 对齐 cmd/server 的 BEAT_AGENT_ROLLOUT_CONFIG 结构。
type serverTrustConfig struct {
	SchemaVersion int              `json:"schema_version"`
	Enabled       bool             `json:"enabled"`
	SignatureMode string           `json:"signature_mode"`
	Keys          []serverTrustKey `json:"keys"`
}

type serverTrustKey struct {
	ID        string            `json:"id"`
	PublicKey ed25519.PublicKey `json:"public_key"`
	NotBefore time.Time         `json:"not_before"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// trustWindow 解析信任有效期，缺省为「一小时前生效、九十天有效」。
func trustWindow(command *cli.Command, now time.Time) (time.Time, time.Time, error) {
	notBefore, expiresAt := now.Add(-time.Hour), now.Add(defaultTrustValidity)
	for _, item := range []struct {
		flag  string
		value *time.Time
	}{{notBeforeFlag, &notBefore}, {expiresAtFlag, &expiresAt}} {
		raw := command.String(item.flag)
		if raw == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return notBefore, expiresAt, fmt.Errorf("%s must be RFC3339", item.flag)
		}
		*item.value = parsed.UTC()
	}
	if !expiresAt.After(notBefore) {
		return notBefore, expiresAt, errors.New("trust expiry must be after its start")
	}
	return notBefore, expiresAt, nil
}

// readReleaseSpec 读取发布描述文件；文件必须是普通文件且不跟随符号链接。
func readReleaseSpec(path string) (releasemanifest.Spec, error) {
	content, err := readPrivateFile(path)
	if err != nil {
		return releasemanifest.Spec{}, err
	}
	spec, err := releasemanifest.ParseSpec(content)
	if err != nil {
		return spec, fmt.Errorf("parse %s: %w", path, err)
	}
	return spec, nil
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	content, err := readPrivateFile(path)
	if err != nil {
		return nil, err
	}
	private, err := releasemanifest.ParsePrivateKey(content)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return private, nil
}

// verifySignature 在写出前自检签名与协议校验，避免产出无法导入的清单。
func verifySignature(signed protocol.SignedManifest, manifest protocol.Manifest, public ed25519.PublicKey, now time.Time) error {
	trust := protocol.SigningTrust{KeyID: manifest.SigningKeyID, PublicKey: public,
		NotBefore: now.Add(-time.Minute), ExpiresAt: manifest.ExpiresAt}
	if _, err := protocol.VerifyManifestSignature(signed, trust, now); err != nil {
		return fmt.Errorf("verify signed manifest: %w", err)
	}
	return nil
}

func encodeSigned(signed protocol.SignedManifest) []byte {
	return encodeJSON(signed)
}

// encodeJSON 统一以两空格缩进输出，便于人工核对与版本库归档。
func encodeJSON(value any) []byte {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return []byte("{}\n")
	}
	return append(encoded, '\n')
}

// readPrivateFile 读取 0600 的普通文件；拒绝符号链接，避免把秘密读到别处。
func readPrivateFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, fmt.Errorf("read %s: not a regular file", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s", path)
	}
	return content, nil
}

// writeFile 以 0600 落盘；exclusive 为真时拒绝覆盖既有文件（密钥与信任文件）。
func writeFile(path string, content []byte, exclusive bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return fmt.Errorf("write %s", path)
	}
	if _, err = file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s", path)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("write %s", path)
	}
	return nil
}
