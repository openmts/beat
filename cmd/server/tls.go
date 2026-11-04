package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	envTLSEnabled  = "BEAT_TLS_ENABLED"
	envTLSCertFile = "BEAT_TLS_CERT_FILE"
	envTLSKeyFile  = "BEAT_TLS_KEY_FILE"
)

type tlsSettings struct {
	enabled  bool
	certFile string
	keyFile  string
}

func tlsSettingsFromEnv(getenv func(string) string) (tlsSettings, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	settings := tlsSettings{
		certFile: strings.TrimSpace(getenv(envTLSCertFile)),
		keyFile:  strings.TrimSpace(getenv(envTLSKeyFile)),
	}
	rawEnabled := strings.TrimSpace(getenv(envTLSEnabled))
	if rawEnabled == "" {
		return settings, nil
	}
	enabled, err := strconv.ParseBool(rawEnabled)
	if err != nil {
		return tlsSettings{}, fmt.Errorf("%s must be true or false: %w", envTLSEnabled, err)
	}
	settings.enabled = enabled
	return settings, nil
}

func (settings tlsSettings) validate() error {
	if !settings.enabled {
		if settings.certFile != "" || settings.keyFile != "" {
			return errors.New("TLS certificate and key require TLS to be enabled")
		}
		return nil
	}
	if settings.certFile == "" || settings.keyFile == "" {
		return errors.New("TLS is enabled but certificate or key file is missing")
	}
	return nil
}

func loadServerTLSConfig(settings tlsSettings) (*tls.Config, error) {
	if err := settings.validate(); err != nil {
		return nil, err
	}
	if !settings.enabled {
		return nil, nil
	}
	if err := validateTLSFile(settings.certFile, false); err != nil {
		return nil, err
	}
	if err := validateTLSFile(settings.keyFile, true); err != nil {
		return nil, err
	}
	certificate, err := tls.LoadX509KeyPair(settings.certFile, settings.keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS certificate and key: %w", err)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
	}, nil
}

func validateTLSFile(path string, privateKey bool) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect TLS file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("TLS file %s must be a regular file", path)
	}
	permissions := info.Mode().Perm()
	if permissions&0o022 != 0 {
		return fmt.Errorf("TLS file %s must not be writable by group or others", path)
	}
	if privateKey && permissions&0o077 != 0 {
		return fmt.Errorf("TLS private key %s must not be accessible by group or others", path)
	}
	return nil
}
