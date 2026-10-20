package main

import (
	"context"
	"fmt"
	"io"
	"os"

	cli "github.com/urfave/cli/v3"

	"github.com/beat/backend/internal/theme"
)

func main() {
	os.Exit(commandExit(os.Args, os.Stdout, os.Stderr))
}

func commandExit(args []string, output, failures io.Writer) int {
	if err := newCommand(output).Run(context.Background(), args); err != nil {
		_, _ = fmt.Fprintf(failures, "theme: %v\n", err)
		return 1
	}
	return 0
}

func newCommand(output io.Writer) *cli.Command {
	return &cli.Command{Name: "beat-theme", Usage: "build, validate and publish Beat themes", Commands: []*cli.Command{
		initCommand(output),
		marketCommand(output),
		{Name: "pack", Usage: "pack source files using the server's validator", Flags: []cli.Flag{
			&cli.StringFlag{Name: "source", Required: true}, &cli.StringFlag{Name: "output", Required: true},
		}, Action: func(_ context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return fmt.Errorf("pack does not accept positional arguments")
			}
			raw, err := theme.BuildArchive(theme.PackOptions{Source: command.String("source"), Output: command.String("output")})
			if err != nil {
				return err
			}
			if err := theme.WriteArchive(command.String("output"), raw); err != nil {
				return err
			}
			return describeArchive(output, raw)
		}},
		{Name: "validate", Usage: "validate an existing ZIP with production rules", Flags: []cli.Flag{&cli.StringFlag{Name: "archive", Required: true}}, Action: func(_ context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return fmt.Errorf("validate does not accept positional arguments")
			}
			file, err := os.Open(command.String("archive"))
			if err != nil {
				return err
			}
			defer func() { _ = file.Close() }()
			raw, err := io.ReadAll(io.LimitReader(file, (25<<20)+1))
			if err != nil {
				return err
			}
			return describeArchive(output, raw)
		}},
	}}
}

func describeArchive(output io.Writer, raw []byte) error {
	validated, err := theme.ValidateArchive(raw)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "%s %s (%s), %d files, sha256 %s\n", validated.Manifest.ID, validated.Manifest.Version, validated.Manifest.Runtime, len(validated.Files), validated.Digest)
	return err
}
