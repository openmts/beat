package agentoperations

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

var ErrPolicy = errors.New("local operation policy rejected the request")

var ErrStorage = errors.New("private operation state is unavailable")

var ErrBoundary = errors.New("required operation process boundary is unavailable")

const (
	defaultTimeout      = 300
	defaultOutput       = 262144
	defaultMemory       = 256 << 20
	defaultProcesses    = 64
	maximumJournalBytes = 2 << 20
	maximumJournals     = 128
)

type Config struct {
	Enabled        bool          `json:"enabled"`
	StateDir       string        `json:"state_dir"`
	MaxConcurrency int           `json:"max_concurrency"`
	Commands       CommandConfig `json:"commands"`
	FileRoots      []FileRoot    `json:"file_roots"`
}

type CommandConfig struct {
	Enabled            bool   `json:"enabled"`
	AllowAdHoc         bool   `json:"allow_ad_hoc"`
	RunAsUser          string `json:"run_as_user"`
	AllowRoot          bool   `json:"allow_root"`
	MaximumTimeout     string `json:"maximum_timeout"`
	MaximumOutputBytes int    `json:"maximum_output_bytes"`
	WorkingRoot        string `json:"working_root"`
	ShellPath          string `json:"shell_path"`
	CgroupRoot         string `json:"cgroup_root"`
	RequireCgroup      *bool  `json:"-"`
	MemoryBytes        uint64 `json:"memory_bytes"`
	MaximumProcesses   uint64 `json:"maximum_processes"`
}

type FileRoot struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Read         bool   `json:"read"`
	Write        bool   `json:"write"`
	MaximumBytes int64  `json:"maximum_file_bytes"`
	Overwrite    bool   `json:"overwrite"`
	Executable   bool   `json:"allow_executable"`
}

type settings struct {
	config   Config
	policy   protocol.Policy
	working  string
	identity identity
}

func normalizeConfig(config Config) Config {
	if config.MaxConcurrency == 0 {
		config.MaxConcurrency = 1
	}
	command := &config.Commands
	if command.MaximumTimeout == "" {
		command.MaximumTimeout = "5m"
	}
	if command.MaximumOutputBytes == 0 {
		command.MaximumOutputBytes = defaultOutput
	}
	if command.RunAsUser == "" {
		command.RunAsUser = "beat-ops"
	}
	if command.MemoryBytes == 0 {
		command.MemoryBytes = defaultMemory
	}
	if command.MaximumProcesses == 0 {
		command.MaximumProcesses = defaultProcesses
	}
	if command.ShellPath == "" {
		command.ShellPath = defaultShell()
	}
	return config
}

func capabilities(config Config) (protocol.Policy, error) {
	if !operationPlatformSupported() {
		if config.Enabled {
			return protocol.Policy{}, ErrBoundary
		}
		config.Commands.Enabled = false
		config.FileRoots = nil
	}
	return configuredCapabilities(config)
}

func configuredCapabilities(config Config) (protocol.Policy, error) {
	timeout, err := time.ParseDuration(config.Commands.MaximumTimeout)
	if err != nil || timeout%time.Second != 0 {
		return protocol.Policy{}, ErrPolicy
	}
	policy := protocol.Policy{Enabled: config.Enabled, Kinds: []string{}, MaximumTargets: 50, MaximumActiveTasks: 5, Concurrency: config.MaxConcurrency, TimeoutSeconds: int(timeout / time.Second), OutputBytes: config.Commands.MaximumOutputBytes, AllowRoot: config.Commands.AllowRoot, Roots: []protocol.Root{}}
	if config.Commands.Enabled {
		policy.Kinds = append(policy.Kinds, "snippet")
		policy.SupportedShells = supportedShells()
	}
	if config.Commands.Enabled && config.Commands.AllowAdHoc {
		policy.Kinds = append(policy.Kinds, "shell")
	}
	for _, root := range config.FileRoots {
		policy.Roots = append(policy.Roots, protocol.Root{ID: root.ID, Name: root.Name, Read: root.Read, Write: root.Write, MaximumBytes: root.MaximumBytes, Overwrite: root.Overwrite, Executable: root.Executable})
	}
	if slices.ContainsFunc(config.FileRoots, func(root FileRoot) bool { return root.Write }) {
		policy.Kinds = append(policy.Kinds, "deploy")
	}
	if slices.ContainsFunc(config.FileRoots, func(root FileRoot) bool { return root.Read }) {
		policy.Kinds = append(policy.Kinds, "collect")
	}
	if err := policy.Validate(); err != nil {
		return policy, ErrPolicy
	}
	return policy, nil
}

func ValidateConfig(config Config, configPath string) error {
	_, err := prepareSettings(config, configPath)
	return err
}

func prepareSettings(config Config, configPath string) (settings, error) {
	config = normalizeConfig(config)
	policy, err := capabilities(config)
	if err != nil {
		return settings{}, err
	}
	result := settings{config: config, policy: policy}
	configPath, err = operationConfigPath(configPath)
	if err != nil {
		return result, err
	}
	if !config.Enabled {
		return result, nil
	}
	if !validExecutionSettings(config) {
		return result, ErrPolicy
	}
	if config.Commands.Enabled {
		result.identity, err = resolveIdentity(config.Commands)
		if err != nil {
			return result, err
		}
	}

	result.working, err = validateLocalRoots(config, configPath)
	if err != nil {
		return result, err
	}
	if config.Commands.Enabled && result.working == "" {
		return result, ErrPolicy
	}
	return result, nil
}

func validateLocalRoots(config Config, configPath string) (string, error) {
	working := ""
	protected, err := protectedOperationPaths(config, configPath)
	if err != nil {
		return "", err
	}
	for index, root := range config.FileRoots {
		if !filepath.IsAbs(root.Path) || dangerousRoot(root.Path, protected) {
			return "", ErrPolicy
		}
		if err := validateOperationDirectory(root.Path); err != nil {
			return "", ErrPolicy
		}
		for _, previous := range config.FileRoots[:index] {
			if overlaps(root.Path, previous.Path) {
				return "", ErrPolicy
			}
		}
		if root.ID == config.Commands.WorkingRoot {
			working = root.Path
		}
	}
	return working, nil
}

func dangerousRoot(path string, extra []string) bool {
	if filepath.Clean(path) == filepath.VolumeName(path)+string(filepath.Separator) {
		return true
	}
	denied := append(platformDeniedRoots(), extra...)
	for _, other := range denied {
		if other == "" {
			continue
		}
		if other == string(filepath.Separator) {
			if filepath.Clean(path) == other {
				return true
			}
			continue
		}
		if overlaps(path, other) {
			return true
		}
	}
	return false
}

func overlaps(left, right string) bool {
	relative, err := filepath.Rel(filepath.Clean(left), filepath.Clean(right))
	if err != nil {
		return true
	}
	if relative == "." || (!filepath.IsAbs(relative) && !slices.Contains(splitPath(relative), "..")) {
		return true
	}
	relative, err = filepath.Rel(filepath.Clean(right), filepath.Clean(left))
	return err != nil || relative == "." || (!filepath.IsAbs(relative) && !slices.Contains(splitPath(relative), ".."))
}

func validExecutionSettings(config Config) bool {
	return filepath.IsAbs(config.StateDir) && config.Commands.MemoryBytes >= 16<<20 && config.Commands.MemoryBytes <= 4<<30 && config.Commands.MaximumProcesses >= 1 && config.Commands.MaximumProcesses <= 1024
}

func protectedOperationPaths(config Config, configPath string) ([]string, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, ErrPolicy
	}
	paths := []string{config.StateDir, configPath, executable}
	if config.Commands.Enabled {
		paths = append(paths, config.Commands.ShellPath)
	}
	for _, candidate := range []string{executable, config.Commands.ShellPath} {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			paths = append(paths, resolved)
		}
	}
	return paths, nil
}

func operationConfigPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", ErrPolicy
	}
	return absolute, nil
}
