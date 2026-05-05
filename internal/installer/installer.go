package installer

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Component struct {
	Repo    string
	Release string
	Path    string
}

type MergeResult struct {
	Skipped  []string
	Merged   []string
	NewFiles []string
}

type Installer struct {
	destPath          string
	verbose           bool
	protectedPatterns []string
}

func New(destPath string, verbose bool) *Installer {
	return &Installer{
		destPath: destPath,
		verbose:  verbose,
	}
}

func NewWithProtection(destPath string, verbose bool, protectedPatterns []string) *Installer {
	return &Installer{
		destPath:          destPath,
		verbose:           verbose,
		protectedPatterns: protectedPatterns,
	}
}

func NewWithVars(destPath string, verbose bool) *Installer {
	return &Installer{
		destPath: destPath,
		verbose:  verbose,
	}
}

var DefaultProtectedPatterns = []string{
	".env",
	".env.local",
	".env.*",
	"config/",
	"var/",
	"data/",
	"storage/",
	"*.yaml",
	"*.yml",
	"*.json",
	"composer.json",
	"go.mod",
	"go.sum",
}

func (i *Installer) InstallComponent(comp Component) error {
	release, err := i.resolveRelease(comp.Release, comp.Repo)
	if err != nil {
		return err
	}

	if i.verbose {
		fmt.Printf("Installing %s @ %s...\n", comp.Repo, release)
	}

	owner, name := parseRepo(comp.Repo)
	tarballURL := fmt.Sprintf(
		"https://github.com/%s/%s/archive/refs/tags/%s.tar.gz",
		owner, name, release,
	)

	if i.verbose {
		fmt.Printf("  Downloading from %s\n", tarballURL)
	}

	tmpDir, err := os.MkdirTemp("", "gmcore-install-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, "component.tar.gz")

	if err := download.File(tarballURL, tarballPath); err != nil {
		return fmt.Errorf("failed to download %s: %w", comp.Repo, err)
	}

	extractPath := filepath.Join(tmpDir, "extracted")
	if err := extractTarGz(tarballPath, extractPath); err != nil {
		return fmt.Errorf("failed to extract %s: %w", comp.Repo, err)
	}

	entries, err := os.ReadDir(extractPath)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("failed to read extracted content for %s", comp.Repo)
	}

	sourceDir := filepath.Join(extractPath, entries[0].Name())
	destDir := filepath.Join(i.destPath, comp.Path)

	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", comp.Path, err)
	}

	if err := copyDir(sourceDir, destDir); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", comp.Repo, comp.Path, err)
	}

	if i.verbose {
		fmt.Printf("  Installed %s to %s\n", comp.Repo, comp.Path)
	}

	return nil
}

func (i *Installer) InstallComponentWithVars(comp Component, vars map[string]string) error {
	release, err := i.resolveRelease(comp.Release, comp.Repo)
	if err != nil {
		return err
	}

	if i.verbose {
		fmt.Printf("Installing %s @ %s with variable substitution...\n", comp.Repo, release)
	}

	owner, name := parseRepo(comp.Repo)
	tarballURL := fmt.Sprintf(
		"https://github.com/%s/%s/archive/refs/tags/%s.tar.gz",
		owner, name, release,
	)

	if i.verbose {
		fmt.Printf("  Downloading from %s\n", tarballURL)
	}

	tmpDir, err := os.MkdirTemp("", "gmcore-install-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, "component.tar.gz")

	if err := download.File(tarballURL, tarballPath); err != nil {
		return fmt.Errorf("failed to download %s: %w", comp.Repo, err)
	}

	extractPath := filepath.Join(tmpDir, "extracted")
	if err := extractTarGz(tarballPath, extractPath); err != nil {
		return fmt.Errorf("failed to extract %s: %w", comp.Repo, err)
	}

	entries, err := os.ReadDir(extractPath)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("failed to read extracted content for %s", comp.Repo)
	}

	sourceDir := filepath.Join(extractPath, entries[0].Name())
	destDir := filepath.Join(i.destPath, comp.Path)

	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", comp.Path, err)
	}

	if err := copyDirWithVars(sourceDir, destDir, vars); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", comp.Repo, comp.Path, err)
	}

	if i.verbose {
		fmt.Printf("  Installed %s to %s with vars\n", comp.Repo, comp.Path)
	}

	return nil
}

func copyDirWithVars(src, dst string, vars map[string]string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return copyFileWithVars(path, destPath, vars)
	})
}

func copyFileWithVars(src, dst string, vars map[string]string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	processed := substituteVars(string(content), vars)

	ext := strings.ToLower(filepath.Ext(src))
	if ext == ".yaml" || ext == ".yml" || ext == ".json" {
		processed, err = processConfigWithVars(string(content), ext, vars)
		if err != nil {
			processed = substituteVars(string(content), vars)
		}
	}

	return os.WriteFile(dst, []byte(processed), 0644)
}

func substituteVars(content string, vars map[string]string) string {
	result := content
	for key, value := range vars {
		placeholder := fmt.Sprintf("{{%s}}", key)
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

func processConfigWithVars(content, ext string, vars map[string]string) (string, error) {
	switch ext {
	case ".yaml", ".yml":
		return processYamlWithVars(content, vars)
	case ".json":
		return processJsonWithVars(content, vars)
	default:
		return substituteVars(content, vars), nil
	}
}

func processYamlWithVars(content string, vars map[string]string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return "", err
	}

	substituteYamlNode(&doc, vars)

	output, err := yaml.Marshal(&doc)
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func substituteYamlNode(node *yaml.Node, vars map[string]string) {
	if node == nil {
		return
	}

	switch node.Kind {
	case yaml.ScalarNode:
		node.Value = substituteVars(node.Value, vars)
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]
			keyNode.Value = substituteVars(keyNode.Value, vars)
			substituteYamlNode(valNode, vars)
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			substituteYamlNode(child, vars)
		}
	case yaml.DocumentNode:
		for _, child := range node.Content {
			substituteYamlNode(child, vars)
		}
	}
}

func processJsonWithVars(content string, vars map[string]string) (string, error) {
	var doc interface{}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return "", err
	}

	doc = substituteJsonNode(doc, vars)

	output, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func substituteJsonNode(node interface{}, vars map[string]string) interface{} {
	switch n := node.(type) {
	case map[string]interface{}:
		for key, value := range n {
			newKey := substituteVars(key, vars)
			if newKey != key {
				delete(n, key)
				n[newKey] = value
			}
			n[newKey] = substituteJsonNode(value, vars)
		}
		return n
	case []interface{}:
		for i, item := range n {
			n[i] = substituteJsonNode(item, vars)
		}
		return n
	case string:
		return substituteVars(n, vars)
	default:
		return n
	}
}

func (i *Installer) InstallComponentProtected(comp Component, verbose bool) error {
	release, err := i.resolveRelease(comp.Release, comp.Repo)
	if err != nil {
		return err
	}

	if verbose {
		fmt.Printf("Installing protected %s @ %s...\n", comp.Repo, release)
	}

	owner, name := parseRepo(comp.Repo)
	tarballURL := fmt.Sprintf(
		"https://github.com/%s/%s/archive/refs/tags/%s.tar.gz",
		owner, name, release,
	)

	if verbose {
		fmt.Printf("  Downloading from %s\n", tarballURL)
	}

	tmpDir, err := os.MkdirTemp("", "gmcore-install-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, "component.tar.gz")

	if err := download.File(tarballURL, tarballPath); err != nil {
		return fmt.Errorf("failed to download %s: %w", comp.Repo, err)
	}

	extractPath := filepath.Join(tmpDir, "extracted")
	if err := extractTarGz(tarballPath, extractPath); err != nil {
		return fmt.Errorf("failed to extract %s: %w", comp.Repo, err)
	}

	entries, err := os.ReadDir(extractPath)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("failed to read extracted content for %s", comp.Repo)
	}

	sourceDir := filepath.Join(extractPath, entries[0].Name())
	destDir := filepath.Join(i.destPath, comp.Path)

	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", comp.Path, err)
	}

	skippedFiles := []string{}
	for _, pattern := range i.protectedPatterns {
		if verbose {
			fmt.Printf("  Protected pattern: %s\n", pattern)
		}
	}

	if err := copyDirProtected(sourceDir, destDir, i.protectedPatterns, &skippedFiles); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", comp.Repo, comp.Path, err)
	}

	if verbose && len(skippedFiles) > 0 {
		fmt.Printf("  Skipped %d protected files:\n", len(skippedFiles))
		for _, f := range skippedFiles {
			if len(f) < 50 {
				fmt.Printf("    - %s\n", f)
			}
		}
	}

	if verbose {
		fmt.Printf("  Installed %s to %s (with protection)\n", comp.Repo, comp.Path)
	}

	return nil
}

func (i *Installer) InstallComponentMerge(comp Component, verbose, force bool) (*MergeResult, error) {
	release, err := i.resolveRelease(comp.Release, comp.Repo)
	if err != nil {
		return nil, err
	}

	if verbose {
		fmt.Printf("Installing with merge %s @ %s...\n", comp.Repo, release)
	}

	owner, name := parseRepo(comp.Repo)
	tarballURL := fmt.Sprintf(
		"https://github.com/%s/%s/archive/refs/tags/%s.tar.gz",
		owner, name, release,
	)

	if verbose {
		fmt.Printf("  Downloading from %s\n", tarballURL)
	}

	tmpDir, err := os.MkdirTemp("", "gmcore-install-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, "component.tar.gz")

	if err := download.File(tarballURL, tarballPath); err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", comp.Repo, err)
	}

	extractPath := filepath.Join(tmpDir, "extracted")
	if err := extractTarGz(tarballPath, extractPath); err != nil {
		return nil, fmt.Errorf("failed to extract %s: %w", comp.Repo, err)
	}

	entries, err := os.ReadDir(extractPath)
	if err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("failed to read extracted content for %s", comp.Repo)
	}

	sourceDir := filepath.Join(extractPath, entries[0].Name())
	destDir := filepath.Join(i.destPath, comp.Path)

	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for %s: %w", comp.Path, err)
	}

	result := &MergeResult{
		Skipped:  []string{},
		Merged:   []string{},
		NewFiles: []string{},
	}

	protectedFiles := i.getProtectedFiles(sourceDir)
	filesToMerge := i.getFilesToMerge(sourceDir, destDir, protectedFiles)

	if len(filesToMerge) > 0 && !force {
		fmt.Printf("\n  Merge required for %d protected file(s):\n", len(filesToMerge))
		for _, f := range filesToMerge {
			fmt.Printf("    - %s\n", f)
		}
		fmt.Print("\n  Do you want to merge these files? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			fmt.Println("  Merge skipped. Use --force to merge without asking.")
			for _, f := range filesToMerge {
				result.Skipped = append(result.Skipped, f)
			}
			copyDirProtected(sourceDir, destDir, i.protectedPatterns, &result.Skipped)
			return result, nil
		}
	}

	for _, file := range filesToMerge {
		srcFile := filepath.Join(sourceDir, file)
		dstFile := filepath.Join(destDir, file)

		if err := i.mergeFile(srcFile, dstFile); err != nil {
			if verbose {
				fmt.Printf("    Warning: failed to merge %s: %v\n", file, err)
			}
			result.Skipped = append(result.Skipped, file)
			continue
		}
		result.Merged = append(result.Merged, file)
		if verbose {
			fmt.Printf("    Merged: %s\n", file)
		}
	}

	if err := copyDirProtected(sourceDir, destDir, i.protectedPatterns, &result.Skipped); err != nil {
		return nil, fmt.Errorf("failed to copy %s to %s: %w", comp.Repo, comp.Path, err)
	}

	for _, f := range result.Skipped {
		found := false
		for _, m := range result.Merged {
			if m == f {
				found = true
				break
			}
		}
		if !found {
			result.NewFiles = append(result.NewFiles, f)
		}
	}

	if verbose {
		fmt.Printf("  Installed %s to %s (merged %d, skipped %d)\n",
			comp.Repo, comp.Path, len(result.Merged), len(result.Skipped))
	}

	return result, nil
}

func (i *Installer) getProtectedFiles(sourceDir string) map[string]bool {
	protected := make(map[string]bool)
	filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return nil
		}
		if isProtected(relPath, i.protectedPatterns) {
			protected[relPath] = true
		}
		return nil
	})
	return protected
}

func (i *Installer) getFilesToMerge(sourceDir, destDir string, protected map[string]bool) []string {
	var toMerge []string
	for file := range protected {
		dstFile := filepath.Join(destDir, file)
		if _, err := os.Stat(dstFile); err == nil {
			if i.isMergeable(file) {
				toMerge = append(toMerge, file)
			}
		}
	}
	return toMerge
}

func (i *Installer) isMergeable(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".env" || ext == ".yaml" || ext == ".yml" || ext == ".json"
}

func (i *Installer) mergeFile(src, dst string) error {
	srcExt := strings.ToLower(filepath.Ext(src))

	switch srcExt {
	case ".env":
		return i.mergeEnvFile(src, dst)
	case ".yaml", ".yml":
		return i.mergeYamlFile(src, dst)
	case ".json":
		return i.mergeJsonFile(src, dst)
	default:
		return fmt.Errorf("unsupported merge type: %s", srcExt)
	}
}

func (i *Installer) mergeEnvFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Open(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	srcVars := make(map[string]string)
	scanner := bufio.NewScanner(srcFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			srcVars[key] = value
		}
	}

	dstVars := make(map[string]string)
	scanner = bufio.NewScanner(dstFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			dstVars[key] = value
		}
	}

	for key, value := range srcVars {
		if _, exists := dstVars[key]; !exists {
			dstVars[key] = value
		}
	}

	var newLines []string
	newLines = append(newLines, "# Generated by GMCore - DO NOT EDIT MANUALLY")
	newLines = append(newLines, "# Existing local values preserved")
	for key, value := range dstVars {
		newLines = append(newLines, fmt.Sprintf("%s=%s", key, value))
	}

	output := strings.Join(newLines, "\n")
	return os.WriteFile(dst, []byte(output+"\n"), 0644)
}

func (i *Installer) mergeYamlFile(src, dst string) error {
	srcData, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	dstData, err := os.ReadFile(dst)
	if err != nil {
		return err
	}

	srcMap, err := parseYamlSimple(string(srcData))
	if err != nil {
		return err
	}
	dstMap, err := parseYamlSimple(string(dstData))
	if err != nil {
		return err
	}

	mergeYamlMaps(srcMap, dstMap)

	output, err := formatYaml(dstMap)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, []byte(output), 0644)
}

func mergeYamlMaps(src, dst map[string]interface{}) {
	for key, srcVal := range src {
		if dstVal, exists := dst[key]; exists {
			if srcMap, srcIsMap := srcVal.(map[string]interface{}); srcIsMap {
				if dstMap, dstIsMap := dstVal.(map[string]interface{}); dstIsMap {
					mergeYamlMaps(srcMap, dstMap)
					dst[key] = dstMap
				} else {
					dst[key] = srcVal
				}
			} else {
				// Key exists in dst, preserve dst value (don't overwrite)
			}
		} else {
			dst[key] = srcVal
		}
	}
}

func (i *Installer) mergeJsonFile(src, dst string) error {
	srcData, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	dstData, err := os.ReadFile(dst)
	if err != nil {
		return err
	}

	srcMap, err := parseJsonSimple(string(srcData))
	if err != nil {
		return err
	}
	dstMap, err := parseJsonSimple(string(dstData))
	if err != nil {
		return err
	}

	mergeJsonMaps(srcMap, dstMap)

	output, err := formatJson(dstMap)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, []byte(output), 0644)
}

func mergeJsonMaps(src, dst map[string]interface{}) {
	for key, srcVal := range src {
		if dstVal, exists := dst[key]; exists {
			if srcMap, srcIsMap := srcVal.(map[string]interface{}); srcIsMap {
				if dstMap, dstIsMap := dstVal.(map[string]interface{}); dstIsMap {
					mergeJsonMaps(srcMap, dstMap)
					dst[key] = dstMap
				} else {
					// Key exists in dst with different type, preserve dst
				}
			} else {
				// Key exists in dst, preserve dst value (don't overwrite)
			}
		} else {
			dst[key] = srcVal
		}
	}
}

func parseYamlSimple(content string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	lines := strings.Split(content, "\n")
	var currentIndent int
	var currentKey string

	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := len(line) - len(trimmed)
		parts := strings.SplitN(trimmed, ":", 2)

		if len(parts) < 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if indent == 0 {
			currentKey = key
			if value != "" {
				result[key] = value
			} else {
				if result[key] == nil {
					result[key] = make(map[string]interface{})
				}
			}
			currentIndent = indent
		} else if indent > currentIndent && currentKey != "" {
		}
	}
	return result, nil
}

func parseJsonSimple(content string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	re := regexp.MustCompile(`"([^"]+)":\s*"?([^",\}]+)"?`)
	matches := re.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		key := match[1]
		value := strings.Trim(match[2], `" \t`)
		result[key] = value
	}
	return result, nil
}

func formatYaml(data map[string]interface{}) (string, error) {
	var lines []string
	for key, value := range data {
		if m, ok := value.(map[string]interface{}); ok {
			lines = append(lines, fmt.Sprintf("%s:", key))
			for k, v := range m {
				lines = append(lines, fmt.Sprintf("  %s: %v", k, v))
			}
		} else {
			lines = append(lines, fmt.Sprintf("%s: %v", key, value))
		}
	}
	return strings.Join(lines, "\n"), nil
}

func formatJson(data map[string]interface{}) (string, error) {
	var lines []string
	lines = append(lines, "{")
	for key, value := range data {
		if m, ok := value.(map[string]interface{}); ok {
			subJson, _ := formatJson(m)
			lines = append(lines, fmt.Sprintf(`  "%s": %s,`, key, subJson))
		} else {
			lines = append(lines, fmt.Sprintf(`  "%s": "%v",`, key, value))
		}
	}
	lines = append(lines, "}")
	return strings.Join(lines, "\n"), nil
}

func (i *Installer) resolveRelease(release, repo string) (string, error) {
	if release == "" || release == "latest" {
		tag, err := i.getLatestTag(repo)
		if err != nil {
			return "main", nil
		}
		return tag, nil
	}

	if strings.HasPrefix(release, "v") || strings.HasPrefix(release, "1.") {
		return release, nil
	}

	return "v" + release, nil
}

func (i *Installer) getLatestTag(repo string) (string, error) {
	owner, name := parseRepo(repo)
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, name)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to get latest release: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"tag_name"`) {
			parts := strings.Split(line, `"`)
			if len(parts) >= 4 {
				return parts[3], nil
			}
		}
	}

	return "", fmt.Errorf("tag_name not found in response")
}

func parseRepo(repo string) (owner, name string) {
	parts := strings.Split(repo, "/")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", repo
}

func extractTarGz(tarballPath, destPath string) error {
	file, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destPath, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, 0644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return copyFile(path, destPath)
	})
}

func copyDirProtected(src, dst string, patterns []string, skippedFiles *[]string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			if _, err := os.Stat(destPath); os.IsNotExist(err) {
				return os.MkdirAll(destPath, info.Mode())
			}
			return nil
		}

		if isProtected(relPath, patterns) {
			if _, err := os.Stat(destPath); err == nil {
				*skippedFiles = append(*skippedFiles, relPath)
				return nil
			}
		}

		return copyFile(path, destPath)
	})
}

func isProtected(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchPattern(relPath, pattern) {
			return true
		}
	}
	return false
}

func matchPattern(path, pattern string) bool {
	if pattern == "" {
		return false
	}

	if strings.HasSuffix(pattern, "/") {
		dirPrefix := strings.TrimSuffix(pattern, "/")
		if strings.HasPrefix(path, dirPrefix) {
			return true
		}
		return false
	}

	if strings.HasPrefix(pattern, "*.") {
		ext := strings.TrimPrefix(pattern, "*.")
		return strings.HasSuffix(path, "."+ext)
	}

	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			return strings.HasPrefix(path, parts[0]) && strings.HasSuffix(path, parts[1])
		}
		return false
	}

	return path == pattern || path == strings.TrimPrefix(pattern, "./")
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func BuildAppVars(appName string, goVersion string) map[string]string {
	vars := map[string]string{
		"APP_NAME":       appName,
		"APP_NAME_CAMEL": toCamelCase(appName),
		"APP_NAME_LOWER": strings.ToLower(appName),
		"APP_NAME_UPPER": strings.ToUpper(appName),
		"GO_VERSION":     goVersion,
	}

	return vars
}

func toCamelCase(s string) string {
	words := strings.Fields(s)
	for i, word := range words {
		if i == 0 {
			words[i] = strings.ToLower(word)
		} else {
			words[i] = strings.Title(strings.ToLower(word))
		}
	}
	result := strings.Join(words, "")
	return strings.Title(strings.ToLower(result))
}