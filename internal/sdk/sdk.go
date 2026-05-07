package sdk

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

type SDKEntry struct {
	Name        string `yaml:"name"`
	Repo        string `yaml:"repo"`
	Module      string `yaml:"module"`
	Version     string `yaml:"version"`
	Tier        string `yaml:"tier"`
	Category    string `yaml:"category"`
	Description string `yaml:"description"`
}

type Manifest struct {
	Version string               `yaml:"version"`
	SDKs    map[string]SDKEntry  `yaml:"sdks"`
}

const (
	TierOfficial = "official"
	TierApproved = "approved"
	TierWild     = "wild"

	CategoryFoundation   = "foundation"
	CategoryDomain       = "domain"
	CategoryExperimental = "experimental"

	ManifestURL = "https://raw.githubusercontent.com/gmcorenet/sdk-manifest/main/manifest.yaml"
)

func FetchManifest() (*Manifest, error) {
	resp, err := http.Get(ManifestURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch SDK manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("SDK manifest not found at %s", ManifestURL)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch SDK manifest: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read SDK manifest: %w", err)
	}

	return Parse(data)
}

func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse SDK manifest: %w", err)
	}
	return &m, nil
}

func (m *Manifest) GetSDK(name string) (SDKEntry, bool) {
	sdk, ok := m.SDKs[name]
	return sdk, ok
}

func (m *Manifest) ListSDKs(tier string) []SDKEntry {
	var result []SDKEntry
	for _, sdk := range m.SDKs {
		if tier == "" || sdk.Tier == tier {
			result = append(result, sdk)
		}
	}
	return result
}

func (m *Manifest) Tiers() []string {
	seen := make(map[string]bool)
	for _, sdk := range m.SDKs {
		seen[sdk.Tier] = true
	}
	tiers := make([]string, 0, len(seen))
	for t := range seen {
		tiers = append(tiers, t)
	}
	return tiers
}

func (m *Manifest) Categories() []string {
	seen := make(map[string]bool)
	for _, sdk := range m.SDKs {
		seen[sdk.Category] = true
	}
	cats := make([]string, 0, len(seen))
	for c := range seen {
		cats = append(cats, c)
	}
	return cats
}

func (m *Manifest) CountByCategory() map[string]int {
	counts := make(map[string]int)
	for _, sdk := range m.SDKs {
		counts[sdk.Category]++
	}
	return counts
}

func (s *SDKEntry) RepoURL() string {
	return s.Repo
}

func (s *SDKEntry) RepoSlug() string {
	parts := strings.Split(s.Repo, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return s.Repo
}
