package beatcli

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	cli "github.com/urfave/cli/v3"

	"github.com/beat/backend/internal/buildinfo"
)

// endpoint 常量对齐 internal/api 注册的数据面路由。
const (
	runtimePath   = "/api/v1/admin/runtime"
	nodesPath     = "/api/v1/nodes/manage"
	backupsPath   = "/api/v1/admin/backups"
	auditPath     = "/api/v1/admin/audit-events"
	metricsSuffix = "/metrics"
	downloadPath  = "/download"
)

// NewCommand 构造 beat-cli 命令树。
func NewCommand(options Options) *cli.Command {
	application := &application{options: normalizeOptions(options)}
	return &cli.Command{
		Name:      "beat-cli",
		Usage:     "drive the Beat data plane with a scoped service token",
		Version:   buildinfo.Version,
		Writer:    application.options.Output,
		ErrWriter: application.options.Errors,
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: serverFlag, Usage: "Beat server base URL, for example https://[::1]:8080"},
			&cli.StringFlag{Name: tokenFlag, Usage: "path to a 0600 service token file, or - to read standard input"},
			&cli.StringFlag{Name: caFlag, Usage: "extra CA bundle for self-signed deployments"},
			&cli.DurationFlag{Name: timeoutFlag, Value: DefaultTimeout, Usage: "per-request timeout"},
			&cli.BoolFlag{Name: rawFlag, Usage: "print the raw response envelope instead of its data field"},
		}, adminFlags()...),
		Commands: []*cli.Command{
			application.healthCommand(),
			application.nodesCommand(),
			application.backupsCommand(),
			application.auditCommand(),
			application.erasureCommand(),
			application.operationsCommand(),
			application.releaseCommand(),
		},
		Action: func(_ context.Context, command *cli.Command) error {
			if command.Args().Len() == 0 {
				return errors.New("a command is required: health, nodes, backups, audit, erasure, operations or release")
			}
			return fmt.Errorf("unknown command: %s", command.Args().First())
		},
	}
}

func (a *application) healthCommand() *cli.Command {
	return &cli.Command{
		Name: "health", Usage: "read the authenticated runtime status (health:read)",
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return errors.New("health does not accept positional arguments")
			}
			session, err := a.connect(command)
			if err != nil {
				return err
			}
			return session.get(ctx, runtimePath, nil)
		},
	}
}

func (a *application) nodesCommand() *cli.Command {
	return &cli.Command{
		Name: "nodes", Usage: "read node inventory and metric history",
		Commands: []*cli.Command{
			{Name: "list", Usage: "list managed nodes (nodes:read)", Action: func(ctx context.Context, command *cli.Command) error {
				if command.Args().Len() != 0 {
					return errors.New("nodes list does not accept positional arguments")
				}
				session, err := a.connect(command)
				if err != nil {
					return err
				}
				return session.get(ctx, nodesPath, nil)
			}},
			{Name: "metrics", ArgsUsage: "<node-id>", Usage: "read node metric history (metrics:read)", Flags: []cli.Flag{
				&cli.StringFlag{Name: "metric", Usage: "metric name; the server default applies when empty"},
				&cli.StringFlag{Name: "from", Usage: "RFC3339 start time"},
				&cli.StringFlag{Name: "to", Usage: "RFC3339 end time"},
			}, Action: func(ctx context.Context, command *cli.Command) error {
				if command.Args().Len() != 1 {
					return errors.New("nodes metrics requires exactly one node id")
				}
				session, err := a.connect(command)
				if err != nil {
					return err
				}
				query := url.Values{}
				addQuery(query, command, "metric", "from", "to")
				nodeID := url.PathEscape(command.Args().First())
				return session.get(ctx, "/api/v1/nodes/"+nodeID+metricsSuffix, query)
			}},
		},
	}
}

func (a *application) backupsCommand() *cli.Command {
	return &cli.Command{
		Name: "backups", Usage: "list, create and download backups",
		Commands: []*cli.Command{
			{Name: "list", Usage: "list backup records (backups:read-metadata)", Action: func(ctx context.Context, command *cli.Command) error {
				if command.Args().Len() != 0 {
					return errors.New("backups list does not accept positional arguments")
				}
				session, err := a.connect(command)
				if err != nil {
					return err
				}
				return session.get(ctx, backupsPath, nil)
			}},
			{Name: "create", Usage: "start an asynchronous backup (backups:create)", Action: func(ctx context.Context, command *cli.Command) error {
				if command.Args().Len() != 0 {
					return errors.New("backups create does not accept positional arguments")
				}
				session, err := a.connect(command)
				if err != nil {
					return err
				}
				return session.post(ctx, backupsPath)
			}},
			{Name: "download", ArgsUsage: "<backup-id>", Usage: "download a backup archive (backups:download)", Flags: []cli.Flag{
				&cli.StringFlag{Name: outputFlag, Usage: "output file path, or - for standard output", Required: true},
				&cli.BoolFlag{Name: forceFlag, Usage: "overwrite an existing output file"},
			}, Action: func(ctx context.Context, command *cli.Command) error {
				if command.Args().Len() != 1 {
					return errors.New("backups download requires exactly one backup id")
				}
				session, err := a.connect(command)
				if err != nil {
					return err
				}
				return session.downloadBackup(ctx, command.Args().First(), command.String(outputFlag), command.Bool(forceFlag))
			}},
		},
	}
}

func (a *application) auditCommand() *cli.Command {
	return &cli.Command{
		Name: "audit", Usage: "read the administrative audit feed",
		Commands: []*cli.Command{
			{Name: "list", Usage: "list audit events (audit:read)", Flags: []cli.Flag{
				&cli.StringFlag{Name: "action", Usage: "filter by action"},
				&cli.StringFlag{Name: "actor-id", Usage: "filter by actor id"},
				&cli.IntFlag{Name: "limit", Usage: "page size"},
				&cli.IntFlag{Name: "offset", Usage: "page offset"},
			}, Action: func(ctx context.Context, command *cli.Command) error {
				if command.Args().Len() != 0 {
					return errors.New("audit list does not accept positional arguments")
				}
				query := url.Values{}
				addQuery(query, command, "action")
				if actor := command.String("actor-id"); actor != "" {
					query.Set("actor_id", actor)
				}
				if err := pagingQuery(query, command, "limit", "offset"); err != nil {
					return err
				}
				session, err := a.connect(command)
				if err != nil {
					return err
				}
				return session.get(ctx, auditPath, query)
			}},
		},
	}
}
