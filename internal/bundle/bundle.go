package bundle

import (
	"fmt"
	"io"
	"net/http"

	"gopkg.in/yaml.v3"
)

type Bundle struct {
	Version    string            `yaml:"version"`
	Released   string            `yaml:"released"`
	Repo       string            `yaml:"repo"`
	Components map[string]Component `yaml:"components"`
}

type Component struct {
	Version string `yaml:"version"`
	Verify  bool   `yaml:"verify"`
}

type BundleManifest struct {
	Version  string    `yaml:"version"`
	Released string    `yaml:"released"`
	Repo     string    `yaml:"repo"`
	Components []BundleComponent `yaml:"components"`
}

type BundleComponent struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Verify  bool   `yaml:"verify"`
}

const (
	TierOfficial = "official"
	TierApproved = "approved"
	TierWild     = "wild"
)

func Fetch(tier, name, version string) (*BundleManifest, error) {
	if version == "" || version == "latest" {
		version = "main"
	}

	url := fmt.Sprintf("https://raw.githubusercontent.com/gmcorenet/bundles/%s/%s/%s.yaml", tier, name, version)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch bundle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("bundle not found: %s/%s@%s", tier, name, version)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch bundle: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read bundle: %w", err)
	}

	return Parse(data)
}

func Parse(data []byte) (*BundleManifest, error) {
	var bundle BundleManifest
	if err := yaml.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("failed to parse bundle: %w", err)
	}

	return &bundle, nil
}

func (b *BundleManifest) GetRepo() string {
	return b.Repo
}

func (b *BundleManifest) GetVersion() string {
	return b.Version
}

func (b *BundleManifest) GetReleased() string {
	return b.Released
}

func (b *BundleManifest) GetComponents() []BundleComponent {
	return b.Components
}

func (b *BundleManifest) GetAllSDKs() []string {
	sdks := make([]string, 0, len(b.Components))
	for _, comp := range b.Components {
		sdks = append(sdks, comp.Name)
	}
	return sdks
}