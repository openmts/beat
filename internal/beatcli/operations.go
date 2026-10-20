package beatcli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	cli "github.com/urfave/cli/v3"
)

// operationsTasksPath 是远端操作任务的 router.admin 入口：会话鉴权，服务令牌无法覆盖。
const operationsTasksPath = "/api/v1/admin/operations/tasks"

// operationsCommand 用管理面会话做远端任务清理：列出现有任务并取消其中一条。
func (a *application) operationsCommand() *cli.Command {
	return &cli.Command{
		Name:        "operations",
		Usage:       "inspect and clean up remote operation tasks through an administrator session",
		Description: "These routes accept administrator sessions only; service tokens have no remote-operation scope.",
		Commands:    []*cli.Command{a.operationsTasksCommand()},
	}
}

func (a *application) operationsTasksCommand() *cli.Command {
	return &cli.Command{
		Name: "tasks", Usage: "list remote operation tasks or cancel one",
		Commands: []*cli.Command{
			{Name: "list", Usage: "list remote operation tasks (limit, cursor)",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: limitFlag, Usage: "maximum number of tasks to return"},
					&cli.StringFlag{Name: "cursor", Usage: "opaque cursor from the previous page"},
				},
				Action: func(ctx context.Context, command *cli.Command) error {
					if command.Args().Len() != 0 {
						return errors.New("operations tasks list does not accept positional arguments")
					}
					query, err := taskPageQuery(command)
					if err != nil {
						return err
					}
					session, err := a.erasureSession(ctx, command)
					if err != nil {
						return err
					}
					return session.get(ctx, operationsTasksPath, query)
				}},
			{Name: "cancel", ArgsUsage: "<task-id>", Usage: "cancel one remote operation task",
				Action: func(ctx context.Context, command *cli.Command) error {
					id, err := singleArgument(command, "operations tasks cancel")
					if err != nil {
						return err
					}
					session, err := a.erasureSession(ctx, command)
					if err != nil {
						return err
					}
					return session.post(ctx, operationsTasksPath+"/"+url.PathEscape(id)+"/cancel")
				}},
		},
	}
}

// taskPageQuery 只发送显式给出的分页参数，负 limit 直接拒绝。
func taskPageQuery(command *cli.Command) (url.Values, error) {
	query := url.Values{}
	if command.IsSet(limitFlag) {
		limit := command.Int(limitFlag)
		if limit < 0 {
			return nil, fmt.Errorf("--%s must not be negative", limitFlag)
		}
		query.Set(limitFlag, strconv.Itoa(limit))
	}
	if cursor := strings.TrimSpace(command.String("cursor")); cursor != "" {
		query.Set("cursor", cursor)
	}
	return query, nil
}
