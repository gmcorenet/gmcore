package manifest

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	Version  string           `yaml:"version"`
	Name     string           `yaml:"name"`
	Framework Component       `yaml:"framework"`
	SDKs     []SDKComponent   `yaml:"sdks"`
	Skeleton Component       `yaml:"skeleton"`
}

type Component struct {
	Repo    string `yaml:"repo"`
	Release string `yaml:"release"`
	Path    string `yaml:"path"`
}

type SDKComponent struct {
	Name    string `yaml:"name"`
	Release string `yaml:"release"`
}

func Fetch(owner, repo, version string) (*Manifest, error) {
	if version == "" || version == "latest" {
		version = "main"
	}

	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/app/v1.0.yaml", owner, repo, version)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("manifest not found at %s", url)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch manifest: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	return Parse(data)
}

func Parse(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	if manifest.Version == "" {
		manifest.Version = "1.0"
	}

	return &manifest, nil
}

func (m *Manifest) GetFramework() Component {
	return m.Framework
}

func (m *Manifest) GetSDKs() []SDKComponent {
	return m.SDKs
}

func (m *Manifest) GetSkeleton() Component {
	return m.Skeleton
}

func (m *Manifest) GetAllComponents() []Component {
	components := make([]Component, 0, len(m.SDKs)+2)
	components = append(components, m.Framework)
	components = append(components, m.Skeleton)
	for _, sdk := range m.SDKs {
		components = append(components, Component{
			Repo:    "gmcorenet/" + sdk.Name,
			Release: sdk.Release,
			Path:    "vendor/sdks/" + sdk.Name,
		})
	}
	return components
}

func ResolveRelease(version string) (string, error) {
	if version == "" || version == "latest" {
		return "main", nil
	}

	if strings.HasPrefix(version, "v") {
		return version, nil
	}

	return "v" + version, nil
}