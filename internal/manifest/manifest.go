package manifest

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	Version   string         `yaml:"version"`
	Name      string         `yaml:"name"`
	Framework Component      `yaml:"framework"`
	SDKs      []SDKComponent `yaml:"sdks"`
	Skeleton  Component      `yaml:"skeleton"`
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

type AppProfile string

const (
	AppProfileStandard AppProfile = "standard"
	AppProfileGateway  AppProfile = "gateway"
)

var baselineSDKs = []string{
	"gmcore-transport",
	"gmcore-lifecycle",
	"gmcore-log",
	"gmcore-security",
	"gmcore-ratelimit",
}

var gatewaySDKs = []string{
	"gmcore-router",
	"gmcore-events",
	"gmcore-validation",
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

func ProfileForAppName(appName string) AppProfile {
	name := strings.ToLower(strings.TrimSpace(appName))
	if name == "gateway" || name == "gmcore-gateway" {
		return AppProfileGateway
	}
	return AppProfileStandard
}

func (m *Manifest) EffectiveSDKs(appName string) []SDKComponent {
	result := make([]SDKComponent, 0, len(m.SDKs)+len(baselineSDKs)+len(gatewaySDKs))
	index := make(map[string]int, len(m.SDKs)+len(baselineSDKs)+len(gatewaySDKs))

	fallbackRelease := "latest"
	if len(m.SDKs) > 0 && strings.TrimSpace(m.SDKs[0].Release) != "" {
		fallbackRelease = m.SDKs[0].Release
	} else if strings.TrimSpace(m.Framework.Release) != "" {
		fallbackRelease = m.Framework.Release
	}

	for _, sdk := range m.SDKs {
		name := strings.TrimSpace(sdk.Name)
		if name == "" {
			continue
		}
		release := strings.TrimSpace(sdk.Release)
		if release == "" {
			release = fallbackRelease
		}
		if pos, ok := index[name]; ok {
			if result[pos].Release == "" {
				result[pos].Release = release
			}
			continue
		}
		index[name] = len(result)
		result = append(result, SDKComponent{Name: name, Release: release})
	}

	ensure := func(names []string) {
		for _, name := range names {
			if _, ok := index[name]; ok {
				continue
			}
			index[name] = len(result)
			result = append(result, SDKComponent{Name: name, Release: fallbackRelease})
		}
	}

	ensure(baselineSDKs)
	if ProfileForAppName(appName) == AppProfileGateway {
		ensure(gatewaySDKs)
	}

	return result
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
