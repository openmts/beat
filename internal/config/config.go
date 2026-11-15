package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultListen      = "[::]:9099"
	DefaultDataDir     = "./data"
	DefaultTokenTTL    = 10 * time.Minute
	DefaultRetention   = 7 * 24 * time.Hour
	minFixedAgentToken = 32
)

type Config struct {
	Listen             string
	DataDir            string
	DatabasePath       string
	MTSDataPath        string
	MTSSync            bool
	AgentToken         string
	EnrollmentTokenTTL time.Duration
	AgentBinaryDir     string
	DemoMode           bool
	Retention          time.Duration
	TLSCertPath        string
	TLSKeyPath         string
}

// Load is retained for callers that rely on the environment-based settings used
// by service managers and container deployments.
func Load() (Config, error) {
	return finalize(environmentConfig())
}

// LoadServer applies defaults, optional YAML configuration, then command-line
// flags. YAML is loaded before flags so command-line values always win.
func LoadServer(args []string) (Config, error) {
	cfg := environmentConfig()
	configPath, err := configPathFromArgs(args)
	if err != nil {
		return Config{}, err
	}
	if configPath == "" {
		configPath = strings.TrimSpace(os.Getenv("BEAT_CONFIG"))
	}
	if configPath != "" {
		if err := applyConfigFile(&cfg, configPath); err != nil {
			return Config{}, err
		}
	}
	fs := flag.NewFlagSet("beat-server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&configPath, "config", configPath, "YAML configuration file")
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, "HTTP and gRPC listen address")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "data directory")
	fs.StringVar(&cfg.AgentBinaryDir, "agent-binary-dir", cfg.AgentBinaryDir, "directory containing architecture-specific Agent binaries")
	fs.StringVar(&cfg.AgentToken, "agent-token", cfg.AgentToken, "optional fixed Agent token (minimum 32 characters)")
	fs.DurationVar(&cfg.EnrollmentTokenTTL, "enrollment-token-ttl", cfg.EnrollmentTokenTTL, "Agent enrollment token lifetime")
	fs.BoolVar(&cfg.DemoMode, "demo", cfg.DemoMode, "seed and continuously refresh demonstration data")
	fs.StringVar(&cfg.TLSCertPath, "tls-cert", cfg.TLSCertPath, "TLS certificate path (enables HTTPS and secure gRPC)")
	fs.StringVar(&cfg.TLSKeyPath, "tls-key", cfg.TLSKeyPath, "TLS private key path")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return finalize(cfg)
}

// LoadResetAdmin parses only the settings needed by the offline credential
// reset command.
func LoadResetAdmin(args []string) (Config, error) {
	cfg := environmentConfig()
	configPath, err := configPathFromArgs(args)
	if err != nil {
		return Config{}, err
	}
	if configPath == "" {
		configPath = strings.TrimSpace(os.Getenv("BEAT_CONFIG"))
	}
	if configPath != "" {
		if err := applyConfigFile(&cfg, configPath); err != nil {
			return Config{}, err
		}
	}
	// Credential recovery must not depend on Agent-only environment settings.
	cfg.AgentToken = ""
	fs := flag.NewFlagSet("beat-server reset-admin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&configPath, "config", configPath, "YAML configuration file")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "data directory")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return finalize(cfg)
}

type fileConfig struct {
	Listen             string `yaml:"listen"`
	DataDir            string `yaml:"data_dir"`
	AgentBinaryDir     string `yaml:"agent_binary_dir"`
	AgentToken         string `yaml:"agent_token"`
	EnrollmentTokenTTL string `yaml:"enrollment_token_ttl"`
	DemoMode           *bool  `yaml:"demo_mode"`
	TLSCertPath        string `yaml:"tls_cert"`
	TLSKeyPath         string `yaml:"tls_key"`
}

func applyConfigFile(cfg *Config, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open config file %s: %w", path, err)
	}
	defer file.Close()
	var raw fileConfig
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse config file %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse config file %s: %w", path, err)
	} else if err == nil {
		return fmt.Errorf("parse config file %s: multiple YAML documents are not supported", path)
	}
	if value := strings.TrimSpace(raw.Listen); value != "" {
		cfg.Listen = value
	}
	if value := strings.TrimSpace(raw.DataDir); value != "" {
		cfg.DataDir = value
	}
	if value := strings.TrimSpace(raw.AgentBinaryDir); value != "" {
		cfg.AgentBinaryDir = value
	}
	if value := strings.TrimSpace(raw.AgentToken); value != "" {
		cfg.AgentToken = value
	}
	if value := strings.TrimSpace(raw.EnrollmentTokenTTL); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("parse enrollment_token_ttl in %s: %w", path, err)
		}
		cfg.EnrollmentTokenTTL = parsed
	}
	if raw.DemoMode != nil {
		cfg.DemoMode = *raw.DemoMode
	}
	if value := strings.TrimSpace(raw.TLSCertPath); value != "" {
		cfg.TLSCertPath = value
	}
	if value := strings.TrimSpace(raw.TLSKeyPath); value != "" {
		cfg.TLSKeyPath = value
	}
	return nil
}

func configPathFromArgs(args []string) (string, error) {
	var path string
	for index := 0; index < len(args); index++ {
		switch {
		case args[index] == "--config":
			if index+1 >= len(args) {
				return "", errors.New("--config requires a file path")
			}
			index++
			path = args[index]
		case strings.HasPrefix(args[index], "--config="):
			path = strings.TrimPrefix(args[index], "--config=")
		}
	}
	return strings.TrimSpace(path), nil
}

func environmentConfig() Config {
	dataDir := env("BEAT_DATA_DIR", DefaultDataDir)
	return Config{
		Listen:             env("BEAT_LISTEN", DefaultListen),
		DataDir:            dataDir,
		AgentToken:         strings.TrimSpace(os.Getenv("BEAT_AGENT_TOKEN")),
		AgentBinaryDir:     strings.TrimSpace(os.Getenv("BEAT_AGENT_BINARY_DIR")),
		DemoMode:           envBool("BEAT_DEMO_MODE", false),
		TLSCertPath:        strings.TrimSpace(os.Getenv("BEAT_TLS_CERT")),
		TLSKeyPath:         strings.TrimSpace(os.Getenv("BEAT_TLS_KEY")),
		Retention:          DefaultRetention,
		EnrollmentTokenTTL: envDuration("BEAT_ENROLLMENT_TOKEN_TTL", DefaultTokenTTL),
	}
}

func finalize(cfg Config) (Config, error) {
	cfg.Listen = strings.TrimSpace(cfg.Listen)
	cfg.DataDir = strings.TrimSpace(cfg.DataDir)
	cfg.AgentToken = strings.TrimSpace(cfg.AgentToken)
	cfg.AgentBinaryDir = strings.TrimSpace(cfg.AgentBinaryDir)
	cfg.TLSCertPath = strings.TrimSpace(cfg.TLSCertPath)
	cfg.TLSKeyPath = strings.TrimSpace(cfg.TLSKeyPath)
	if cfg.Listen == "" {
		return Config{}, fmt.Errorf("listen address must not be empty")
	}
	if cfg.DataDir == "" {
		return Config{}, fmt.Errorf("data directory must not be empty")
	}
	absoluteDataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data directory: %w", err)
	}
	cfg.DataDir = absoluteDataDir
	cfg.DatabasePath = filepath.Join(cfg.DataDir, "beat.db")
	cfg.MTSDataPath = filepath.Join(cfg.DataDir, "mts")
	if cfg.EnrollmentTokenTTL < time.Minute || cfg.EnrollmentTokenTTL > 24*time.Hour {
		return Config{}, fmt.Errorf("enrollment token TTL must be between 1m and 24h")
	}
	if cfg.Retention <= 0 {
		cfg.Retention = DefaultRetention
	}
	if cfg.AgentToken != "" && len(cfg.AgentToken) < minFixedAgentToken {
		return Config{}, fmt.Errorf("fixed Agent token must contain at least %d characters", minFixedAgentToken)
	}
	if (cfg.TLSCertPath == "") != (cfg.TLSKeyPath == "") {
		return Config{}, fmt.Errorf("--tls-cert and --tls-key must be provided together")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
