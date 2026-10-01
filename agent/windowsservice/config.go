package windowsservice

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// Configuration contains the machine-wide settings required by the Windows
// agent. DeviceToken exists only in memory; the persisted form is protected by
// Windows DPAPI.
type Configuration struct {
	ServerURL         string `json:"server_url"`
	DeviceID          string `json:"device_id"`
	DeviceToken       string `json:"device_token,omitempty"`
	ControlledUserSID string `json:"controlled_user_sid"`
}

// PublicConfiguration is safe to return to diagnostics and user interfaces.
type PublicConfiguration struct {
	ServerURL         string `json:"server_url"`
	DeviceID          string `json:"device_id"`
	ControlledUserSID string `json:"controlled_user_sid"`
	HasDeviceToken    bool   `json:"has_device_token"`
}

func (c Configuration) Validate() error {
	if strings.TrimSpace(c.DeviceID) == "" || strings.TrimSpace(c.DeviceToken) == "" {
		return errors.New("device_id and device_token are required")
	}
	if !strings.HasPrefix(c.ControlledUserSID, "S-1-") || strings.ContainsAny(c.ControlledUserSID, " \t\r\n/") {
		return errors.New("controlled_user_sid must be a Windows SID")
	}
	parsed, err := url.Parse(c.ServerURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return errors.New("server_url must be an http(s) origin without path, credentials, query or fragment")
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return errors.New("server_url must use HTTPS unless it points to the local machine")
	}
	return nil
}

func (c Configuration) Public() PublicConfiguration {
	return PublicConfiguration{
		ServerURL: strings.TrimRight(c.ServerURL, "/"), DeviceID: c.DeviceID,
		ControlledUserSID: c.ControlledUserSID, HasDeviceToken: c.DeviceToken != "",
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
