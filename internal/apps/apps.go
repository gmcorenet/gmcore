package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type Entry struct {
	Name string
	Dir  string
	Path string
}

func BasePath() string {
	switch runtime.GOOS {
	case "windows":
		return "C:\\ProgramData\\gmcore"
	case "darwin":
		return "/usr/local/gmcore"
	default:
		return "/opt/gmcore"
	}
}

func NormalizeName(dirName string) string {
	if strings.HasPrefix(dirName, "gmcore-") {
		trimmed := strings.TrimPrefix(dirName, "gmcore-")
		if trimmed != "" {
			return trimmed
		}
	}
	return dirName
}

func CandidateDirs(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	candidates := []string{name}
	if !strings.HasPrefix(name, "gmcore-") {
		candidates = append(candidates, "gmcore-"+name)
	} else {
		trimmed := strings.TrimPrefix(name, "gmcore-")
		if trimmed != "" {
			candidates = append(candidates, trimmed)
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	uniq := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		uniq = append(uniq, c)
	}

	return uniq
}

func ResolveByName(basePath, name string) (Entry, error) {
	if basePath == "" {
		basePath = BasePath()
	}

	for _, dir := range CandidateDirs(name) {
		path := filepath.Join(basePath, dir)
		st, err := os.Stat(path)
		if err != nil || !st.IsDir() {
			continue
		}
		return Entry{
			Name: NormalizeName(dir),
			Dir:  dir,
			Path: path,
		}, nil
	}

	return Entry{}, fmt.Errorf("application %s not found under %s", name, basePath)
}

func LooksLikeAppRoot(path string) bool {
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return false
	}

	markers := []string{
		filepath.Join(path, "app.yaml"),
		filepath.Join(path, "current", "app.yaml"),
		filepath.Join(path, "config"),
		filepath.Join(path, "bin"),
	}

	for _, marker := range markers {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}

	return false
}

func DetectFromCWD(basePath, cwd string) string {
	if basePath == "" {
		basePath = BasePath()
	}

	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return ""
		}
	}

	basePathNormalized := filepath.ToSlash(basePath)
	cwdNormalized := filepath.ToSlash(cwd)

	if !strings.HasPrefix(cwdNormalized, basePathNormalized) {
		return ""
	}

	relative := strings.TrimPrefix(cwdNormalized, basePathNormalized)
	relative = strings.TrimPrefix(relative, "/")
	if relative == "" || relative == "." || strings.Contains(relative, "..") {
		return ""
	}

	parts := strings.SplitN(relative, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}

	appRoot := filepath.Join(basePath, parts[0])
	if LooksLikeAppRoot(appRoot) {
		return appRoot
	}

	return ""
}

func List(basePath string) ([]Entry, error) {
	if basePath == "" {
		basePath = BasePath()
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, err
	}

	byName := make(map[string]Entry)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		dir := e.Name()
		path := filepath.Join(basePath, dir)
		if !LooksLikeAppRoot(path) {
			if !strings.HasPrefix(dir, "gmcore-") {
				continue
			}
		}

		name := NormalizeName(dir)
		candidate := Entry{Name: name, Dir: dir, Path: path}

		existing, ok := byName[name]
		if !ok {
			byName[name] = candidate
			continue
		}

		if existing.Dir != name && candidate.Dir == name {
			byName[name] = candidate
		}
	}

	result := make([]Entry, 0, len(byName))
	for _, v := range byName {
		result = append(result, v)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}
