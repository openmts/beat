package agentinstall

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/buildinfo"
)

const (
	DefaultConfigPath = "/etc/beat-agent/agent.json"
	ServiceName       = "beat-agent.service"
	privateDirectory  = 0o700
	privateFile       = 0o600
	executableFile    = 0o700
	stateVersion      = 1
	serviceTimeout    = 30 * time.Second
	maxPrivateFile    = 64 * 1024
)

type JoinOptions struct {
	ServerURL string
	Token     string
	Name      string
	Host      string
	SSHPort   int
	AllowHTTP bool
}

type Summary struct {
	NodeID     string
	ServerURL  string
	ConfigPath string
}

type installPaths struct {
	helperUnit string
	config     string
	stateDir   string
	binary     string
	unit       string
	systemd    string
}

type commandRunner func(context.Context, string, []string) (string, error)

type atomicWriter func(string, []byte, os.FileMode) error

type Installer struct {
	version     func() string
	container   func() bool
	paths       installPaths
	client      *http.Client
	goos        string
	euid        func() int
	hostname    func() (string, error)
	sshPort     func() (int, error)
	executable  func() (string, error)
	lookPath    func(string) (string, error)
	run         commandRunner
	write       atomicWriter
	credential  func() (agentcredential.Token, error)
	random      io.Reader
	dropInRoots []string
}

func NewInstaller() *Installer {
	return &Installer{
		paths: installPaths{
			config: DefaultConfigPath, stateDir: filepath.Dir(DefaultConfigPath),
			binary: "/opt/beat-agent/beat-agent", unit: "/etc/systemd/system/" + ServiceName,
			systemd: "/run/systemd/system",
		},
		version: func() string { return buildinfo.Version }, container: detectUpdateContainer, client: newHTTPClient(), goos: runtime.GOOS, euid: os.Geteuid,
		hostname: os.Hostname, sshPort: agent.DetectSSHPort, executable: func() (string, error) { return "/proc/self/exe", nil },
		lookPath: exec.LookPath, run: runSystemctl, write: atomicWrite,
		credential: agentcredential.Generate, random: rand.Reader, dropInRoots: vendorDropInRoots,
	}
}

func runSystemctl(ctx context.Context, executable string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, serviceTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, executable, args...).CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("systemctl %s failed: %w", args[1], errors.Join(ctx.Err(), err))
	}
	return string(output), nil
}
