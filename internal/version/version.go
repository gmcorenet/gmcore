package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	owner     = "gmcorenet"
	repo      = "sdk"
	baseURL   = "https://api.github.com"
	tagPrefix = "v"
)

type Version struct {
	Tag      string
	Name     string
	URL      string
	TarURL   string
	IsLatest bool
}

type releaseInfo struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	HTMLURL    string `json:"html_url"`
	TarballURL string `json:"tarball_url"`
	Prerelease bool   `json:"prerelease"`
}

func Resolve(v string) (*Version, error) {
	versions, err := List()
	if err != nil {
		return nil, err
	}

	if v == "latest" {
		for _, ver := range versions {
			if ver.IsLatest {
				return &ver, nil
			}
		}
	}

	v = normalizeVersion(v)

	for _, ver := range versions {
		if ver.Tag == v || ver.Tag == tagPrefix+v {
			return &ver, nil
		}
	}

	for _, ver := range versions {
		if strings.HasPrefix(ver.Tag, tagPrefix+v) {
			return &ver, nil
		}
	}

	return nil, fmt.Errorf("version %s not found", v)
}

func List() ([]Version, error) {
	resp, err := http.Get(fmt.Sprintf("%s/repos/%s/%s/releases", baseURL, owner, repo))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	versions := make([]Version, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, Version{
			Tag:      r.TagName,
			Name:     r.Name,
			URL:      r.HTMLURL,
			TarURL:   r.TarballURL,
			IsLatest: r.TagName == "v1.0.0",
		})
	}

	return versions, nil
}

func normalizeVersion(v string) string {
	if !strings.HasPrefix(v, tagPrefix) {
		return tagPrefix + v
	}
	return v
}

