package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func serve(t *testing.T, dir string, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	newHandler(dir).ServeHTTP(rec, req)
	return rec
}

func feedDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"index.html":                              "<h1>Lightica</h1>",
		"latest-mac.yml":                          "version: 0.11.0-20261119\n",
		"lightica-0.11.0-20261119-mac-arm64.dmg":  "0123456789",
		".lightica-0.11.0-20261120-mac-arm64.dmg": "staged",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestServesPublishedFilesUnderThePrefix(t *testing.T) {
	dir := feedDir(t)

	rec := serve(t, dir, httptest.NewRequest("GET", "/lightica/latest-mac.yml", nil))
	if rec.Code != 200 || rec.Body.String() != "version: 0.11.0-20261119\n" {
		t.Fatalf("latest-mac.yml: %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("metadata Cache-Control = %q", got)
	}

	rec = serve(t, dir, httptest.NewRequest("GET", "/lightica/", nil))
	if rec.Code != 200 || rec.Body.String() != "<h1>Lightica</h1>" {
		t.Fatalf("release page: %d %q", rec.Code, rec.Body.String())
	}

	rec = serve(t, dir, httptest.NewRequest("GET", "/lightica", nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/lightica/" {
		t.Fatalf("bare prefix: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestArtifactsSupportRangeRequestsAndCaching(t *testing.T) {
	// electron-updater downloads differentially with Range requests.
	req := httptest.NewRequest("GET", "/lightica/lightica-0.11.0-20261119-mac-arm64.dmg", nil)
	req.Header.Set("Range", "bytes=2-4")
	rec := serve(t, feedDir(t), req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "234" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("artifact Cache-Control = %q", got)
	}
}

func TestHidesStagedFilesAndAnythingOutsideTheFeed(t *testing.T) {
	dir := feedDir(t)
	for _, path := range []string{
		"/lightica/.lightica-0.11.0-20261120-mac-arm64.dmg",
		"/lightica/../secret",
		"/other",
	} {
		if rec := serve(t, dir, httptest.NewRequest("GET", path, nil)); rec.Code == 200 {
			t.Fatalf("%s served: %q", path, rec.Body.String())
		}
	}
	if rec := serve(t, dir, httptest.NewRequest("POST", "/lightica/latest-mac.yml", nil)); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	if rec := serve(t, t.TempDir(), httptest.NewRequest("GET", "/lightica/health", nil)); rec.Code != 200 {
		t.Fatalf("health: %d", rec.Code)
	}
}
