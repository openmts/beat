package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const minReportInterval = time.Second

const defaultSSHPort = 22

const systemSSHConfigPath = "/etc/ssh/sshd_config"

type Config struct {
	ServerURL      string        `json:"server_url"`
	AgentToken     string        `json:"agent_token"`
	NodeName       string        `json:"node_name"`
	AdvertisedHost string        `json:"advertised_host"`
	SSHPort        int           `json:"ssh_port"`
	ReportInterval time.Duration `json:"-"`
}

type configFile struct {
	ServerURL      string `json:"server_url"`
	AgentToken     string `json:"agent_token"`
	NodeName       string `json:"node_name"`
	AdvertisedHost string `json:"advertised_host"`
	SSHPort        *int   `json:"ssh_port"`
	ReportInterval string `json:"report_interval"`
}

var readSSHPort = func() (int, error) {
	return readSSHPortFromFile(systemSSHConfigPath)
}

func LoadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return Config{}, fmt.Errorf("stat config: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Config{}, errors.New("config permissions must not allow group or other access")
	}

	var raw configFile
	decoder := json.NewDecoder(io.LimitReader(file, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := ensureConfigEOF(decoder); err != nil {
		return Config{}, err
	}
	return raw.parse()
}

func ensureConfigEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("decode config: multiple JSON values")
		}
		return fmt.Errorf("decode config: %w", err)
	}
	return nil
}

func (raw configFile) parse() (Config, error) {
	interval, err := time.ParseDuration(raw.ReportInterval)
	if err != nil || interval < minReportInterval {
		return Config{}, errors.New("report_interval must be a duration of at least 1s")
	}
	sshPort := raw.SSHPort
	if sshPort == nil {
		resolved, err := readSSHPort()
		if err != nil {
			return Config{}, fmt.Errorf("resolve ssh port: %w", err)
		}
		sshPort = &resolved
	}
	config := Config{
		ServerURL: raw.ServerURL, AgentToken: raw.AgentToken, NodeName: raw.NodeName,
		AdvertisedHost: raw.AdvertisedHost, SSHPort: *sshPort, ReportInterval: interval,
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) validate() error {
	serverURL, err := url.ParseRequestURI(config.ServerURL)
	if err != nil || (serverURL.Scheme != "http" && serverURL.Scheme != "https") || serverURL.Host == "" {
		return errors.New("server_url must be an absolute http or https URL")
	}
	if config.AgentToken == "" {
		return errors.New("agent_token is required")
	}
	if config.NodeName == "" {
		return errors.New("node_name is required")
	}
	if config.AdvertisedHost == "" {
		return errors.New("advertised_host is required")
	}
	if config.SSHPort < 1 || config.SSHPort > 65535 {
		return errors.New("ssh_port must be between 1 and 65535")
	}
	return nil
}

func readSSHPortFromFile(path string) (int, error) {
	port, err := resolveSSHPort(path)
	if err != nil {
		return 0, err
	}
	if port == nil {
		return defaultSSHPort, nil
	}
	return *port, nil
}

// resolveSSHPort returns the first explicit Port directive found in the config
// tree (including Include files), or nil when no Port is specified anywhere.
func resolveSSHPort(path string) (*int, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open sshd config: %w", err)
	}
	defer func() { _ = file.Close() }()

	return scanSSHConfigPort(filepath.Dir(path), bufio.NewScanner(file))
}

func scanSSHConfigPort(baseDir string, scanner *bufio.Scanner) (*int, error) {
	for scanner.Scan() {
		line := stripSSHConfigComment(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		key := strings.ToLower(fields[0])
		if key == "include" && len(fields) > 1 {
			pattern := fields[1]
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(baseDir, pattern)
			}
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return nil, fmt.Errorf("expand sshd include: %w", err)
			}
			for _, includePath := range matches {
				port, err := resolveSSHPort(includePath)
				if err != nil {
					return nil, err
				}
				if port != nil {
					return port, nil
				}
			}
			continue
		}
		if key == "port" && len(fields) > 1 {
			value, err := strconv.Atoi(fields[1])
			if err != nil || value < 1 || value > 65535 {
				return nil, fmt.Errorf("invalid sshd Port %q", fields[1])
			}
			return &value, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read sshd config: %w", err)
	}
	return nil, nil
}

func stripSSHConfigComment(line string) string {
	if index := strings.IndexByte(line, '#'); index >= 0 {
		return line[:index]
	}
	return line
}
