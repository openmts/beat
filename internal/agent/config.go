package agent

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Options contains the runtime settings accepted by the Agent.
type Options struct {
	ServerURL          string
	Token              string
	Interval           time.Duration
	Name               string
	LabelsRaw          string
	StateFile          string
	DiskPath           string
	InsecureSkipVerify bool
	ShowVersion        bool
}

type fileConfig struct {
	ServerURL          string            `yaml:"server"`
	Token              string            `yaml:"token"`
	Interval           string            `yaml:"interval"`
	Name               string            `yaml:"name"`
	Labels             map[string]string `yaml:"labels"`
	StateFile          string            `yaml:"state_file"`
	DiskPath           string            `yaml:"disk_path"`
	InsecureSkipVerify *bool             `yaml:"insecure_skip_verify"`
}

// ParseOptions applies defaults and compatible environment variables, then an
// optional YAML file, and finally command-line flags. Command-line values win.
func ParseOptions(args []string) (Options, error) {
	opts := optionsFromEnvironment()
	configPath, err := configPathFromArgs(args)
	if err != nil {
		return Options{}, err
	}
	if configPath == "" {
		configPath = strings.TrimSpace(os.Getenv("BEAT_AGENT_CONFIG"))
	}
	if configPath != "" {
		if err := applyConfigFile(&opts, configPath); err != nil {
			return Options{}, err
		}
	}

	fs := flag.NewFlagSet("beat-agent", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&configPath, "config", configPath, "YAML configuration file")
	fs.StringVar(&opts.ServerURL, "server", opts.ServerURL, "Beat server URL")
	fs.StringVar(&opts.Token, "token", opts.Token, "short-lived enrollment token")
	fs.DurationVar(&opts.Interval, "interval", opts.Interval, "collection interval")
	fs.StringVar(&opts.Name, "name", opts.Name, "node display name")
	fs.StringVar(&opts.LabelsRaw, "labels", opts.LabelsRaw, "labels in key=value,key=value form")
	fs.StringVar(&opts.StateFile, "state-file", opts.StateFile, "agent identity file")
	fs.StringVar(&opts.DiskPath, "disk-path", opts.DiskPath, "filesystem path used for disk metrics")
	fs.BoolVar(&opts.InsecureSkipVerify, "insecure-skip-verify", opts.InsecureSkipVerify, "skip HTTPS certificate verification (not recommended)")
	fs.BoolVar(&opts.ShowVersion, "version", false, "print version")
	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}
	if fs.NArg() > 0 {
		return Options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func optionsFromEnvironment() Options {
	return Options{
		ServerURL: env("BEAT_SERVER_URL", "http://127.0.0.1:9099"),
		Token:     env("BEAT_AGENT_TOKEN", ""),
		Interval:  envDuration("BEAT_AGENT_INTERVAL", 15*time.Second),
		Name:      env("BEAT_NODE_NAME", ""),
		LabelsRaw: env("BEAT_AGENT_LABELS", "env=production"),
		StateFile: env("BEAT_AGENT_STATE_FILE", "/etc/beat/agent-id"),
		DiskPath:  env("BEAT_DISK_PATH", "/"),
	}
}

func applyConfigFile(opts *Options, path string) error {
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

	if value := strings.TrimSpace(raw.ServerURL); value != "" {
		opts.ServerURL = value
	}
	if value := strings.TrimSpace(raw.Token); value != "" {
		opts.Token = value
	}
	if value := strings.TrimSpace(raw.Interval); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("parse interval in %s: %w", path, err)
		}
		if parsed <= 0 {
			return fmt.Errorf("parse interval in %s: duration must be positive", path)
		}
		opts.Interval = parsed
	}
	if value := strings.TrimSpace(raw.Name); value != "" {
		opts.Name = value
	}
	if raw.Labels != nil {
		opts.LabelsRaw = formatLabels(raw.Labels)
	}
	if value := strings.TrimSpace(raw.StateFile); value != "" {
		opts.StateFile = value
	}
	if value := strings.TrimSpace(raw.DiskPath); value != "" {
		opts.DiskPath = value
	}
	if raw.InsecureSkipVerify != nil {
		opts.InsecureSkipVerify = *raw.InsecureSkipVerify
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

func formatLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, strings.TrimSpace(key)+"="+strings.TrimSpace(labels[key]))
	}
	return strings.Join(pairs, ",")
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
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
