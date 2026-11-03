package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Page-specific scripts dereference these elements directly. Keep the HTML and
// JavaScript contract intact when removing or renaming controls during redesigns.
func TestWebPageElementReferences(t *testing.T) {
	idPattern := regexp.MustCompile(`\bid="([^"]+)"`)
	selectorPattern := regexp.MustCompile(`\$\("#([^"\s]+)"\)`)
	for _, page := range []struct{ html, script string }{
		{"index.html", "auth.js"},
		{"dashboard.html", "dashboard.js"},
		{"settings.html", "settings.js"},
	} {
		t.Run(page.html, func(t *testing.T) {
			document, err := os.ReadFile(filepath.Join("web", page.html))
			if err != nil {
				t.Fatal(err)
			}
			script, err := os.ReadFile(filepath.Join("web", "assets", page.script))
			if err != nil {
				t.Fatal(err)
			}
			ids := make(map[string]bool)
			for _, match := range idPattern.FindAllStringSubmatch(string(document), -1) {
				if ids[match[1]] {
					t.Errorf("duplicate element ID %q", match[1])
				}
				ids[match[1]] = true
			}
			for _, match := range selectorPattern.FindAllStringSubmatch(string(script), -1) {
				if !ids[match[1]] {
					t.Errorf("%s references missing element #%s", page.script, match[1])
				}
			}
		})
	}
}

func TestWebReferencedAssetsAreServedFromDisk(t *testing.T) {
	_, client := newTestServer(t)
	assetPattern := regexp.MustCompile(`(?:src|href)="(/assets/[^"#]+)(?:#[^"]*)?"`)
	assets := make(map[string]bool)
	for _, page := range []string{"index.html", "dashboard.html", "settings.html"} {
		document, err := os.ReadFile(filepath.Join("web", page))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range assetPattern.FindAllStringSubmatch(string(document), -1) {
			assets[match[1]] = true
		}
	}
	if len(assets) == 0 {
		t.Fatal("no referenced assets found")
	}
	for asset := range assets {
		t.Run(asset, func(t *testing.T) {
			expected, err := os.ReadFile(filepath.Join("web", strings.TrimPrefix(asset, "/")))
			if err != nil {
				t.Fatal(err)
			}
			response := client.request(t, http.MethodGet, asset, nil)
			assertStatus(t, response, http.StatusOK)
			if !bytes.Equal(response.Body.Bytes(), expected) {
				t.Fatal("served asset differs from the file on disk")
			}
		})
	}
}
