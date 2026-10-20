package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	cli "github.com/urfave/cli/v3"

	"github.com/beat/backend/internal/theme"
)

func marketCommand(output io.Writer) *cli.Command {
	return &cli.Command{Name: "market", Usage: "publish signed theme catalogs", Commands: []*cli.Command{
		{Name: "keygen", Usage: "create a private signing key and public trust key", Flags: []cli.Flag{
			&cli.StringFlag{Name: "private-key", Required: true}, &cli.StringFlag{Name: "public-key", Required: true},
		}, Action: func(_ context.Context, command *cli.Command) error { return generateMarketKeys(command, output) }},
		{Name: "identity", Usage: "print the stable public source identity", Flags: []cli.Flag{
			&cli.StringFlag{Name: "public-key", Required: true},
		}, Action: func(_ context.Context, command *cli.Command) error { return describeMarketIdentity(command, output) }},
		{Name: "publish", Usage: "validate archives and sign a catalog; outputs must not exist", Flags: []cli.Flag{
			&cli.StringFlag{Name: "private-key", Required: true}, &cli.StringFlag{Name: "entries", Required: true},
			&cli.StringFlag{Name: "output", Required: true}, &cli.StringFlag{Name: "signature", Required: true},
			&cli.DurationFlag{Name: "expires-in", Value: 24 * time.Hour},
		}, Action: func(_ context.Context, command *cli.Command) error { return publishMarket(command, output) }},
	}}
}

func generateMarketKeys(command *cli.Command, output io.Writer) error {
	if command.Args().Len() != 0 {
		return fmt.Errorf("keygen does not accept positional arguments")
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	publicText := base64.StdEncoding.EncodeToString(public)
	identity, err := theme.MarketIdentity(publicText)
	if err != nil {
		return err
	}
	files := []marketOutput{{command.String("private-key"), []byte(base64.StdEncoding.EncodeToString(private) + "\n")},
		{command.String("public-key"), []byte(publicText + "\n")}}
	if err := writeMarketOutputs(files); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "source_id %s\npublic_key %s\n", identity, publicText)
	return err
}

func describeMarketIdentity(command *cli.Command, output io.Writer) error {
	if command.Args().Len() != 0 {
		return fmt.Errorf("identity does not accept positional arguments")
	}
	raw, err := readMarketFile(command.String("public-key"), marketKeyFileLimit)
	if err != nil {
		return err
	}
	identity, err := theme.MarketIdentity(string(raw))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, identity)
	return err
}

func publishMarket(command *cli.Command, output io.Writer) error {
	if command.Args().Len() != 0 {
		return fmt.Errorf("publish does not accept positional arguments")
	}
	key, err := readMarketPrivateKey(command.String("private-key"))
	if err != nil {
		return err
	}
	packages, err := readMarketInputs(command.String("entries"))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	catalog, err := theme.BuildMarketCatalog(theme.MarketPublishOptions{PrivateKey: key, Packages: packages, Now: now, ExpiresAt: now.Add(command.Duration("expires-in"))})
	if err != nil {
		return err
	}
	if err := writeMarketOutputs([]marketOutput{{command.String("output"), catalog.Body}, {command.String("signature"), []byte(catalog.Signature + "\n")}}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "source_id %s\n%d themes; serve the signature as X-Beat-Catalog-Signature\n", catalog.SourceID, len(packages))
	return err
}

func readMarketPrivateKey(filename string) (ed25519.PrivateKey, error) {
	raw, err := readMarketPrivateFile(filename)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Ed25519 private key file")
	}
	return ed25519.PrivateKey(decoded), nil
}
