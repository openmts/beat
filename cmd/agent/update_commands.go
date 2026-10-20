package main

import (
	"context"
	"errors"

	cli "github.com/urfave/cli/v3"

	"github.com/beat/backend/internal/agentinstall"
)

func agentEnableUpdatesCommand(actions agentActions) *cli.Command {
	return &cli.Command{Name: "enable-updates", Usage: "enable signed Linux updates with an independent watchdog (requires root)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Value: agentinstall.DefaultConfigPath},
			&cli.StringFlag{Name: "manifest", Required: true, Usage: "private signed manifest file for the installed binary"},
			&cli.StringFlag{Name: "trust", Required: true, Usage: "private Ed25519 trust file obtained out of band"},
			&cli.StringFlag{Name: "artifact", Required: true, Usage: "installed binary artifact ID in the signed manifest"},
			&cli.StringFlag{Name: "node-id", Usage: "existing node ID, required for manual installations"},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 || actions.enableUpdates == nil {
				return errors.New("invalid enable-updates invocation")
			}
			return actions.enableUpdates(ctx, agentinstall.UpdateOptions{ConfigPath: command.String("config"), ManifestPath: command.String("manifest"), TrustPath: command.String("trust"),
				ArtifactID: command.String("artifact"), NodeID: command.String("node-id")})
		}}
}

func agentUpdateHelperCommand(actions agentActions) *cli.Command {
	return &cli.Command{Name: "update-helper", Hidden: true, Flags: []cli.Flag{&cli.StringFlag{Name: "config", Required: true}},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 || actions.helper == nil {
				return errors.New("invalid update helper invocation")
			}
			return actions.helper(ctx, command.String("config"))
		}}
}
