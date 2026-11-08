package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Config is ~/.config/wsctl/config.json. It holds where the portal is and how
// we prove who we are. Tokens are written with mode 0600.
type Config struct {
	Portal       string    `json:"portal"`                 // https://workspaces.example.com
	Tenant       string    `json:"tenant,omitempty"`       // Entra tenant id or domain
	Issuer       string    `json:"issuer,omitempty"`       // any OIDC issuer (Keycloak realm URL …): endpoints come from discovery
	ClientID     string    `json:"clientId,omitempty"`     // public client app id (devspaces-cli)
	Scope        string    `json:"scope,omitempty"`        // api://devspaces/Workspaces.ReadWrite offline_access openid profile
	Audience     string    `json:"audience,omitempty"`     // api://devspaces (for --az)
	Auth         string    `json:"auth,omitempty"`         // device | az | token | dev
	Token        string    `json:"token,omitempty"`        // current access token
	RefreshToken string    `json:"refreshToken,omitempty"` // device flow only
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	Insecure     bool      `json:"insecure,omitempty"`
	TunnelHost   string    `json:"tunnelHost,omitempty"` // portal URL for the WebSocket tunnel when Portal is an API gateway (APIM)
	DefaultNS    string    `json:"defaultNamespace,omitempty"`
}

func defaultConfigPath() string {
	if p := os.Getenv("WSCTL_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "wsctl", "config.json")
}

func loadConfig(path string) (*Config, error) {
	c := &Config{}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, errors.New("config " + path + " is not valid JSON: " + err.Error())
	}
	return c, nil
}

func saveConfig(path string, c *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
