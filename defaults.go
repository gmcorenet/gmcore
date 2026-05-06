package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func ensureExposureAndTransportDefaults(appPath, appName string) error {
	if err := os.MkdirAll(filepath.Join(appPath, "config"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(appPath, "var", "socket"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(appPath, "var", "run"), 0755); err != nil {
		return err
	}

	transportPath := filepath.Join(appPath, "config", "transport.yaml")
	if _, err := os.Stat(transportPath); os.IsNotExist(err) {
		content := defaultTransportConfig(appName)
		if writeErr := os.WriteFile(transportPath, []byte(content), 0644); writeErr != nil {
			return fmt.Errorf("failed to write transport config: %w", writeErr)
		}
	}

	exposurePath := filepath.Join(appPath, "config", "exposure.yaml")
	if _, err := os.Stat(exposurePath); os.IsNotExist(err) {
		content := defaultExposureConfig(appName)
		if writeErr := os.WriteFile(exposurePath, []byte(content), 0644); writeErr != nil {
			return fmt.Errorf("failed to write exposure config: %w", writeErr)
		}
	}

	return nil
}

func defaultTransportConfig(appName string) string {
	return fmt.Sprintf(`server:
  mode: uds
  uds:
    path: var/socket/%s.sock
    perm: 0660
    group: gmcore
    auto_remove: false
  tcp:
    listeners:
      - host: 127.0.0.1
        ports: [8080]
      - host: "::1"
        ports: [8080]

security:
  type: hmac
  key: %%env(TRANSPORT_SECRET)%%
`, appName)
}

func defaultExposureConfig(appName string) string {
	return fmt.Sprintf(`exposure:
  mode: internal
  listeners:
    - host: 127.0.0.1
      ports: [8080]
    - host: "::1"
      ports: [8080]
  gateway:
    enabled: true
    app: gateway
    socket: var/socket/%s.sock
`, appName)
}

func readExposureMode(appPath string) string {
	path := filepath.Join(appPath, "config", "exposure.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}

	var cfg struct {
		Exposure struct {
			Mode string `yaml:"mode"`
		} `yaml:"exposure"`
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "unknown"
	}

	mode := strings.TrimSpace(cfg.Exposure.Mode)
	if mode == "" {
		return "unknown"
	}

	return mode
}
