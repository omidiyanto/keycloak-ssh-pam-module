package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the Keycloak SSH PAM module and monitor daemon.
type Config struct {
	Keycloak KeycloakConfig `yaml:"keycloak"`
	Auth     AuthConfig     `yaml:"auth"`
	Session  SessionConfig  `yaml:"session"`
	Monitor  MonitorConfig  `yaml:"monitor"`
	Logging  LoggingConfig  `yaml:"logging"`
}

// KeycloakConfig holds Keycloak server connection details.
type KeycloakConfig struct {
	ServerURL    string `yaml:"server_url"`
	Realm        string `yaml:"realm"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

// AuthConfig holds authentication flow parameters.
type AuthConfig struct {
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
	PollTimeoutSeconds  int    `yaml:"poll_timeout_seconds"`
	Scopes              string `yaml:"scopes"`
}

// SessionConfig holds session storage settings.
type SessionConfig struct {
	StorageDir string `yaml:"storage_dir"`
}

// MonitorConfig holds backchannel logout monitor settings.
type MonitorConfig struct {
	ListenAddress string `yaml:"listen_address"`
	TLSCert       string `yaml:"tls_cert"`
	TLSKey        string `yaml:"tls_key"`
}

// LoggingConfig holds logging preferences.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Syslog bool   `yaml:"syslog"`
}

// DefaultConfigPath is the default location for the configuration file.
const DefaultConfigPath = "/etc/keycloak-ssh/config.yaml"

// Load reads and parses the configuration file from the given path.
// If path is empty, DefaultConfigPath is used.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Set sensible defaults before parsing
	cfg := &Config{
		Auth: AuthConfig{
			PollIntervalSeconds: 5,
			PollTimeoutSeconds:  300,
			Scopes:              "openid",
		},
		Session: SessionConfig{
			StorageDir: "/var/run/keycloak-ssh/sessions",
		},
		Monitor: MonitorConfig{
			ListenAddress: "0.0.0.0:7291",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Syslog: true,
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// validate checks that all required configuration fields are present.
func (c *Config) validate() error {
	if c.Keycloak.ServerURL == "" {
		return fmt.Errorf("keycloak.server_url is required")
	}
	if c.Keycloak.Realm == "" {
		return fmt.Errorf("keycloak.realm is required")
	}
	if c.Keycloak.ClientID == "" {
		return fmt.Errorf("keycloak.client_id is required")
	}
	return nil
}

// DeviceAuthEndpoint returns the full URL for the Device Authorization endpoint.
func (c *Config) DeviceAuthEndpoint() string {
	return c.Keycloak.ServerURL + "/realms/" + c.Keycloak.Realm + "/protocol/openid-connect/auth/device"
}

// TokenEndpoint returns the full URL for the Token endpoint.
func (c *Config) TokenEndpoint() string {
	return c.Keycloak.ServerURL + "/realms/" + c.Keycloak.Realm + "/protocol/openid-connect/token"
}

// IntrospectEndpoint returns the full URL for the Token Introspection endpoint.
func (c *Config) IntrospectEndpoint() string {
	return c.Keycloak.ServerURL + "/realms/" + c.Keycloak.Realm + "/protocol/openid-connect/token/introspect"
}
