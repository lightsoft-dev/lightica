package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveStartDir(t *testing.T) {
	home := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	linked := mk("lightsoft-projects/b1-boardgame")
	described := mk("lightsoft-projects/bookhive_app")
	outside := t.TempDir()

	localDir := func(daemon, path string) apiResource {
		ref, _ := json.Marshal(map[string]string{"daemon_id": daemon, "local_path": path})
		return apiResource{ResourceType: "local_directory", ResourceRef: ref}
	}
	repo := apiResource{ResourceType: "github_repo", ResourceRef: json.RawMessage(`{"url":"https://github.com/x/y"}`)}

	cases := []struct {
		name       string
		daemonID   string
		resources  []apiResource
		desc       string
		wantDir    string
		wantSource string
	}{
		{
			name:       "local_directory on this machine wins over the description",
			daemonID:   "d1",
			resources:  []apiResource{repo, localDir("d1", linked)},
			desc:       "로컬: ~/lightsoft-projects/bookhive_app",
			wantDir:    linked,
			wantSource: sourceLocalDirectory,
		},
		{
			name:       "local_directory of another machine is skipped",
			daemonID:   "d1",
			resources:  []apiResource{localDir("d2", linked)},
			desc:       "로컬(사용자 작업 폴더, 참고용): ~/lightsoft-projects/bookhive_app — 프로젝트에 local_directory로 연결되기 전에는 ...",
			wantDir:    described,
			wantSource: sourceDescription,
		},
		{
			name:       "description line among others, markdown escaped",
			desc:       "개요: 앱\n운영: 설정은 ~/.multica/server/.env\n- **로컬**: `~/lightsoft-projects/bookhive\\_app` — 참고",
			wantDir:    described,
			wantSource: sourceDescription,
		},
		{
			name:       "absolute description path",
			desc:       "로컬: " + linked + ".",
			wantDir:    linked,
			wantSource: sourceDescription,
		},
		{
			name:       "missing folder falls back to home",
			desc:       "로컬: ~/lightsoft-projects/does-not-exist",
			wantDir:    home,
			wantSource: sourceHome,
		},
		{
			name:       "folder outside home is refused",
			resources:  []apiResource{localDir("", outside)},
			desc:       "로컬: " + outside,
			wantDir:    home,
			wantSource: sourceHome,
		},
		{
			name:       "paths on non-로컬 lines are ignored",
			desc:       "운영: ~/lightsoft-projects/b1-boardgame",
			wantDir:    home,
			wantSource: sourceHome,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, source := resolveStartDir(home, tc.daemonID, tc.resources, tc.desc)
			if dir != tc.wantDir || source != tc.wantSource {
				t.Fatalf("got (%q, %q), want (%q, %q)", dir, source, tc.wantDir, tc.wantSource)
			}
		})
	}
}

func TestShellEnvDropsMulticaCredentials(t *testing.T) {
	env := shellEnv([]string{
		"PATH=/opt/bin",
		"MULTICA_TOKEN=secret",
		"MULTICA_SERVER_URL=http://x",
		"LIGHTSOFT_TERM_API_URL=http://y",
		"TERM=dumb",
		"PWD=/somewhere/else",
		"TERM_PROGRAM=Apple_Terminal",
	}, "/home/u", "/bin/zsh", "/home/u/proj", "a@b.c")
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if _, dup := got[k]; dup {
			t.Fatalf("duplicate %s", k)
		}
		got[k] = v
	}
	for _, k := range []string{"MULTICA_TOKEN", "MULTICA_SERVER_URL", "LIGHTSOFT_TERM_API_URL", "TERM_PROGRAM"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s leaked into the shell environment", k)
		}
	}
	want := map[string]string{
		"PATH":                    "/opt/bin",
		"HOME":                    "/home/u",
		"PWD":                     "/home/u/proj",
		"TERM":                    "xterm-256color",
		"LIGHTSOFT_TERMINAL_USER": "a@b.c",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}
