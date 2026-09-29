// Package config loads the small, installation-time configuration of tempo-agent.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains settings that belong to the local installation. Policies and
// other remotely managed values are deliberately not part of this file.
type Config struct {
	DatabasePath       string
	ControlledUser     string
	TickInterval       time.Duration
	CheckpointInterval time.Duration
	LoginctlPath       string
	ServerURL          string
	DeviceID           string
	DeviceToken        string
	// HeartbeatInterval remains readable for upgrades from existing local files.
	// The installed agent passes syncclient its embedded safe fallback instead.
	HeartbeatInterval time.Duration
	HTTPTimeout       time.Duration
}

// Load reads the flat TOML subset used by the agent configuration. Keeping the
// parser deliberately small avoids adding a runtime dependency for five scalar
// installation settings.
func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()

	values := make(map[string]string)
	allowedKeys := map[string]bool{
		"database_path": true, "controlled_user": true, "tick_interval": true,
		"checkpoint_interval": true, "loginctl_path": true, "server_url": true,
		"device_id": true, "device_token": true, "heartbeat_interval": true,
		"http_timeout": true,
	}
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(stripComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return Config{}, fmt.Errorf("configuration line %d: sections are not supported", lineNumber)
		}
		key, rawValue, found := strings.Cut(line, "=")
		if !found {
			return Config{}, fmt.Errorf("configuration line %d: expected key = value", lineNumber)
		}
		key = strings.TrimSpace(key)
		if !allowedKeys[key] {
			return Config{}, fmt.Errorf("configuration line %d: unknown key %q", lineNumber, key)
		}
		if _, duplicate := values[key]; duplicate {
			return Config{}, fmt.Errorf("configuration line %d: duplicate key %q", lineNumber, key)
		}
		value, err := strconv.Unquote(strings.TrimSpace(rawValue))
		if err != nil {
			return Config{}, fmt.Errorf("configuration line %d: value must be a quoted string: %w", lineNumber, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return Config{}, fmt.Errorf("read configuration: %w", err)
	}

	config := Config{
		DatabasePath:       values["database_path"],
		ControlledUser:     values["controlled_user"],
		TickInterval:       time.Second,
		CheckpointInterval: 5 * time.Second,
		LoginctlPath:       "/usr/bin/loginctl",
		ServerURL:          strings.TrimRight(values["server_url"], "/"),
		DeviceID:           values["device_id"],
		DeviceToken:        values["device_token"],
		HeartbeatInterval:  3 * time.Second,
		HTTPTimeout:        8 * time.Second,
	}
	if value := values["tick_interval"]; value != "" {
		config.TickInterval, err = time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse tick_interval: %w", err)
		}
	}
	if value := values["checkpoint_interval"]; value != "" {
		config.CheckpointInterval, err = time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse checkpoint_interval: %w", err)
		}
	}
	if value := values["loginctl_path"]; value != "" {
		config.LoginctlPath = value
	}
	if value := values["heartbeat_interval"]; value != "" {
		config.HeartbeatInterval, err = time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse heartbeat_interval: %w", err)
		}
	}
	if value := values["http_timeout"]; value != "" {
		config.HTTPTimeout, err = time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse http_timeout: %w", err)
		}
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Validate rejects unsafe or incomplete local settings.
func (c Config) Validate() error {
	if c.DatabasePath == "" {
		return errors.New("database_path cannot be empty")
	}
	if c.ControlledUser == "" || strings.ContainsAny(c.ControlledUser, " \t\r\n/") {
		return errors.New("controlled_user must be a single Linux account name")
	}
	if c.TickInterval <= 0 {
		return errors.New("tick_interval must be positive")
	}
	if c.CheckpointInterval <= 0 {
		return errors.New("checkpoint_interval must be positive")
	}
	if c.LoginctlPath == "" {
		return errors.New("loginctl_path cannot be empty")
	}
	configured := 0
	for _, value := range []string{c.ServerURL, c.DeviceID, c.DeviceToken} {
		if value != "" {
			configured++
		}
	}
	if configured != 0 && configured != 3 {
		return errors.New("server_url, device_id and device_token must be configured together")
	}
	if configured == 3 {
		parsed, err := url.Parse(c.ServerURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("server_url must be an http(s) origin without credentials, query or fragment")
		}
		if parsed.Scheme == "http" && !isLoopbackDevelopmentHost(parsed.Hostname()) {
			return errors.New("server_url must use HTTPS unless it points to the local machine")
		}
	}
	if c.HeartbeatInterval < time.Second || c.HeartbeatInterval > 10*time.Minute {
		return errors.New("heartbeat_interval must be between 1 second and 10 minutes")
	}
	if c.HTTPTimeout < time.Second || c.HTTPTimeout > time.Minute {
		return errors.New("http_timeout must be between 1 second and 1 minute")
	}
	return nil
}

func (c Config) SyncEnabled() bool {
	return c.ServerURL != "" && c.DeviceID != "" && c.DeviceToken != ""
}

func isLoopbackDevelopmentHost(hostname string) bool {
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	address := net.ParseIP(hostname)
	return address != nil && address.IsLoopback()
}

func stripComment(line string) string {
	inString := false
	escaped := false
	for index, character := range line {
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' && inString {
			escaped = true
			continue
		}
		if character == '"' {
			inString = !inString
			continue
		}
		if character == '#' && !inString {
			return line[:index]
		}
	}
	return line
}
