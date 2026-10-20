package beatcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	cli "github.com/urfave/cli/v3"
)

// erasurePath 是 owner 专属的指标擦除入口；它只接受范围 + 目标 + 确认短语，从不接受 SQL、路径或时间范围。
const (
	erasurePath = "/api/v1/admin/metric-erasure/jobs"

	scopeFlag     = "scope"
	targetFlag    = "target"
	requestIDFlag = "request-id"
	confirmFlag   = "confirm"
	limitFlag     = "limit"
)

// erasureCommand 通过管理面会话驱动指标擦除任务；这些路由要求 owner 角色与最近认证。
func (a *application) erasureCommand() *cli.Command {
	return &cli.Command{
		Name:        "erasure",
		Usage:       "drive owner-only metric erasure jobs through an administrator session",
		Description: "These routes accept administrator sessions only; service tokens have no erasure scope.",
		Commands: []*cli.Command{
			a.erasureListCommand(), a.erasureGetCommand(), a.erasureCreateCommand(),
			a.erasureActionCommand("retry"), a.erasureActionCommand("cancel"),
		},
	}
}

func (a *application) erasureListCommand() *cli.Command {
	return &cli.Command{
		Name: "list", Usage: "list recent metric erasure jobs",
		Flags: []cli.Flag{&cli.IntFlag{Name: limitFlag, Usage: "maximum number of jobs to return"}},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return errors.New("erasure list does not accept positional arguments")
			}
			if limit := command.Int(limitFlag); limit < 0 {
				return fmt.Errorf("--%s must not be negative", limitFlag)
			}
			session, err := a.erasureSession(ctx, command)
			if err != nil {
				return err
			}
			query := url.Values{}
			if command.Int(limitFlag) > 0 {
				query.Set(limitFlag, strconv.Itoa(command.Int(limitFlag)))
			}
			return session.get(ctx, erasurePath, query)
		},
	}
}

func (a *application) erasureGetCommand() *cli.Command {
	return &cli.Command{
		Name: "get", ArgsUsage: "<job-id>", Usage: "read one metric erasure job",
		Action: func(ctx context.Context, command *cli.Command) error {
			id, err := singleArgument(command, "erasure get")
			if err != nil {
				return err
			}
			session, err := a.erasureSession(ctx, command)
			if err != nil {
				return err
			}
			return session.get(ctx, erasurePath+"/"+url.PathEscape(id), nil)
		},
	}
}

func (a *application) erasureCreateCommand() *cli.Command {
	return &cli.Command{
		Name: "create", Usage: "request a bounded erasure job",
		Description: "The confirmation phrase must match the scope exactly; node, device and task scopes also need --target.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: scopeFlag, Required: true, Usage: "erasure scope: node, device, task, fleet or all-history"},
			&cli.StringFlag{Name: targetFlag, Usage: "target id for node, device and task scopes"},
			&cli.StringFlag{Name: requestIDFlag, Required: true, Usage: "caller-generated UUID stamped on the job"},
			&cli.StringFlag{Name: confirmFlag, Required: true, Usage: "exact confirmation phrase for the scope"},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return errors.New("erasure create does not accept positional arguments")
			}
			payload, err := json.Marshal(map[string]string{
				"scope": command.String(scopeFlag), "target_id": command.String(targetFlag),
				"request_id": command.String(requestIDFlag), "confirmation": command.String(confirmFlag)})
			if err != nil {
				return fmt.Errorf("encode erasure request: %w", err)
			}
			session, err := a.erasureSession(ctx, command)
			if err != nil {
				return err
			}
			return session.postJSON(ctx, erasurePath, payload)
		},
	}
}

// erasureActionCommand 生成 retry/cancel 两条动作命令；两者形状完全一致。
func (a *application) erasureActionCommand(action string) *cli.Command {
	return &cli.Command{
		Name: action, ArgsUsage: "<job-id>", Usage: action + " a metric erasure job",
		Action: func(ctx context.Context, command *cli.Command) error {
			id, err := singleArgument(command, "erasure "+action)
			if err != nil {
				return err
			}
			session, err := a.erasureSession(ctx, command)
			if err != nil {
				return err
			}
			return session.post(ctx, erasurePath+"/"+url.PathEscape(id)+"/"+action)
		},
	}
}

// erasureSession 建立管理面会话并完成最近认证：擦除路由的五条入口都要求两者。
func (a *application) erasureSession(ctx context.Context, command *cli.Command) (*session, error) {
	session, err := a.adminSession(ctx, command, true)
	if err != nil {
		return nil, fmt.Errorf("administrator session: %w", err)
	}
	return session, nil
}

// singleArgument 校验只给一个位置参数，并把它当作路径段返回。
func singleArgument(command *cli.Command, name string) (string, error) {
	if command.Args().Len() != 1 || strings.TrimSpace(command.Args().First()) == "" {
		return "", fmt.Errorf("%s requires exactly one identifier", name)
	}
	return command.Args().First(), nil
}
