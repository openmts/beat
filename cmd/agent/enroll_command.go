package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	cli "github.com/urfave/cli/v3"

	"github.com/beat/backend/internal/agentinstall"
)

// maxGrantSecretInput 限制授权秘密的读取长度，避免 stdin 被灌满。
const maxGrantSecretInput = 1024

// defaultEnrollmentWait 是等待管理员审批的默认时长；超时后可重跑同一命令继续等待。
const defaultEnrollmentWait = 10 * time.Minute

func agentEnrollCommand(action agentActions) *cli.Command {
	return &cli.Command{
		Name: "enroll", Usage: "join Beat with a one-command enrollment grant and start the systemd service (requires root)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "server", Usage: "Beat server origin, for example https://beat.example.com", Required: true},
			&cli.BoolFlag{Name: "grant-stdin", Usage: "read the enrollment grant secret from standard input"},
			&cli.StringFlag{Name: "grant-file", Usage: "read the enrollment grant secret from a 0600 file"},
			&cli.BoolFlag{Name: "allow-http", Usage: "explicitly allow sending credentials over unencrypted HTTP"},
			&cli.StringFlag{Name: "name", Usage: "node name (defaults to this machine's hostname)"},
			&cli.StringFlag{Name: "host", Usage: "reachable node address (defaults to the server-observed source IP)"},
			&cli.IntFlag{Name: "ssh-port", Usage: "SSH port (defaults to sshd configuration, then 22)"},
			&cli.DurationFlag{Name: "wait", Value: defaultEnrollmentWait,
				Usage: "how long to wait for administrator approval before exiting"},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			return runEnrollCommand(ctx, command, action)
		},
	}
}

func runEnrollCommand(ctx context.Context, command *cli.Command, action agentActions) error {
	if command.Args().Len() != 0 {
		return errors.New("enroll does not accept positional arguments")
	}
	if command.IsSet("config") {
		return errors.New("enroll generates /etc/beat-agent/agent.json; --config is for foreground mode or service install")
	}
	if command.IsSet("ssh-port") && (command.Int("ssh-port") < 1 || command.Int("ssh-port") > maximumSSHPort) {
		return errors.New("ssh-port must be between 1 and 65535")
	}
	secret, err := readGrantSecret(command)
	if err != nil {
		return err
	}
	result, err := action.enroll(ctx, agentinstall.GrantOptions{
		ServerURL: command.String("server"), GrantSecret: secret, AllowHTTP: command.Bool("allow-http"),
		Name: command.String("name"), Host: command.String("host"), SSHPort: command.Int("ssh-port"),
		PollTimeout: command.Duration("wait"),
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(command.Root().Writer,
		"Node enrolled: %s\n%s is enabled and active.\nServer: %s\nConfig: %s\n",
		result.NodeID, agentinstall.ServiceName, result.ServerURL, result.ConfigPath)
	return err
}

// readGrantSecret 只接受 stdin 或 0600 文件，避免秘密出现在进程参数与 shell 历史里。
func readGrantSecret(command *cli.Command) (string, error) {
	path := strings.TrimSpace(command.String("grant-file"))
	if command.Bool("grant-stdin") {
		if path != "" {
			return "", errors.New("use either --grant-stdin or --grant-file")
		}
		return readGrantReader(command.Root().Reader, "enrollment grant input")
	}
	if path == "" {
		return "", errors.New("--grant-stdin or --grant-file is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read enrollment grant file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return "", errors.New("enrollment grant file must be a regular file with 0600 permissions")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read enrollment grant file: %w", err)
	}
	defer func() { _ = file.Close() }()
	return readGrantReader(file, "enrollment grant file")
}

func readGrantReader(reader io.Reader, label string) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxGrantSecretInput+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	if len(data) > maxGrantSecretInput {
		return "", fmt.Errorf("%s is too long", label)
	}
	return strings.TrimSpace(string(data)), nil
}
