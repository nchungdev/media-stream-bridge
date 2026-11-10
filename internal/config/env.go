package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultListenHost = "127.0.0.1"
	defaultPort       = 6890
	defaultMaxJobs    = 200
	defaultPublicURL  = "http://127.0.0.1:6890"
	defaultTimeout    = 3 * time.Hour

	minPasswordLen = 12
	minAPIKeyLen   = 24

	// publishedAPIKey is the key that used to be shown in the README; never accept it.
	publishedAPIKey = "fshare-bridge-secret"
)

// ApplyEnv overrides the file configuration with MSB_* environment variables.
func (c *Config) ApplyEnv() error {
	// The listen address comes from MSB_LISTEN_HOST only: a stale "0.0.0.0"
	// in config.json must not silently expose the service.
	c.Server.Host = defaultListenHost
	if c.Server.Port == 0 {
		c.Server.Port = defaultPort
	}
	c.MaxJobs = defaultMaxJobs
	c.PublicURL = defaultPublicURL
	c.EpisodeTimeout = defaultTimeout
	c.APIKey = c.Server.APIKey

	if v := os.Getenv("MSB_LISTEN_HOST"); v != "" {
		c.Server.Host = v
	}
	if v := os.Getenv("MSB_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("MSB_PORT: invalid port %q", v)
		}
		c.Server.Port = port
	}
	c.QBitUsername = os.Getenv("MSB_QBIT_USERNAME")
	c.QBitPassword = os.Getenv("MSB_QBIT_PASSWORD")
	if v := os.Getenv("MSB_API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("MSB_MAX_JOBS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return fmt.Errorf("MSB_MAX_JOBS: invalid value %q", v)
		}
		c.MaxJobs = n
	}
	if v := os.Getenv("MSB_PUBLIC_URL"); v != "" {
		c.PublicURL = v
	}
	if v := os.Getenv("MSB_EPISODE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return fmt.Errorf("MSB_EPISODE_TIMEOUT: invalid duration %q", v)
		}
		c.EpisodeTimeout = d
	}
	return c.Validate()
}

// Validate rejects missing or weak credentials so the service fails closed.
func (c *Config) Validate() error {
	var errs []error
	if c.QBitUsername == "" {
		errs = append(errs, errors.New("MSB_QBIT_USERNAME is required"))
	}
	if len(c.QBitPassword) < minPasswordLen || c.QBitPassword == "admin" {
		errs = append(errs, fmt.Errorf("MSB_QBIT_PASSWORD is required and must be at least %d characters", minPasswordLen))
	}
	if len(c.APIKey) < minAPIKeyLen || c.APIKey == publishedAPIKey {
		errs = append(errs, fmt.Errorf("MSB_API_KEY is required and must be at least %d characters (not the published default)", minAPIKeyLen))
	}
	return errors.Join(errs...)
}
