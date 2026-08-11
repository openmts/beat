package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"
)

const minReportInterval = time.Second

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
	SSHPort        int    `json:"ssh_port"`
	ReportInterval string `json:"report_interval"`
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
	config := Config{
		ServerURL: raw.ServerURL, AgentToken: raw.AgentToken, NodeName: raw.NodeName,
		AdvertisedHost: raw.AdvertisedHost, SSHPort: raw.SSHPort, ReportInterval: interval,
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
