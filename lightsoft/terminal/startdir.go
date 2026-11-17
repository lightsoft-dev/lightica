package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Start directory sources, in resolution order.
const (
	sourceLocalDirectory = "local_directory"
	sourceDescription    = "description"
	sourceHome           = "home"
)

// localLinePattern matches the project-description line that records the
// project's working folder on this machine, e.g.
// "로컬(사용자 작업 폴더): ~/lightsoft-projects/lawbis — ..." or
// "- **로컬**: ~/foo". Every project in the workspace follows this
// convention; only a few also carry a local_directory resource.
var localLinePattern = regexp.MustCompile(`^\s*(?:[-*]\s+)?(?:\*\*)?로컬`)

// pathPattern picks the first home-relative or absolute path on a line.
var pathPattern = regexp.MustCompile("(?:^|[\\s(`:])(~/[^\\s`),]+|/[^\\s`),]+)")

var markdownEscape = regexp.MustCompile(`\\([!-/:-@\[-` + "`" + `{-~])`)

// resolveStartDir picks the directory a project's terminal opens in. A path
// that is missing, not a directory, or outside home falls through to the next
// source, so a stale description never strands the user.
func resolveStartDir(home, daemonID string, resources []apiResource, description string) (dir, source string) {
	for _, res := range resources {
		if res.ResourceType != "local_directory" {
			continue
		}
		var ref struct {
			DaemonID  string `json:"daemon_id"`
			LocalPath string `json:"local_path"`
		}
		if json.Unmarshal(res.ResourceRef, &ref) != nil {
			continue
		}
		if daemonID != "" && ref.DaemonID != daemonID {
			continue
		}
		if d, ok := usableDir(home, ref.LocalPath); ok {
			return d, sourceLocalDirectory
		}
	}
	for _, line := range strings.Split(description, "\n") {
		if !localLinePattern.MatchString(line) {
			continue
		}
		// Descriptions saved from the rich-text editor are Markdown, which
		// escapes characters such as "_" in folder names.
		m := pathPattern.FindStringSubmatch(markdownEscape.ReplaceAllString(line, "$1"))
		if m == nil {
			continue
		}
		if d, ok := usableDir(home, m[1]); ok {
			return d, sourceDescription
		}
	}
	return home, sourceHome
}

func usableDir(home, p string) (string, bool) {
	p = strings.TrimRight(strings.TrimSpace(p), ".;")
	if p == "~" {
		p = home
	} else if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		return "", false
	}
	p = filepath.Clean(p)
	if p != home && !strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "", false
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return p, true
}
