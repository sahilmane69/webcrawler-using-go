package main

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchPageExtractsContentAndRelativeLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != crawlerAgent {
			t.Errorf("unexpected user agent: %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Test Page</title></head><body><header>Site header</header><nav>Shared menu</nav><main><p>Hello crawler</p><script>hidden script</script><a href="/next#part">Next page</a></main><footer>Site footer</footer></body></html>`))
	}))
	defer server.Close()

	root, _ := url.Parse(server.URL)
	result, err := fetchPage(server.Client(), server.URL, root.Hostname())
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsHTML || result.Title != "Test Page" || !strings.Contains(result.Content, "Hello crawler") {
		t.Fatalf("unexpected page result: %+v", result)
	}
	if strings.Contains(result.Content, "hidden script") {
		t.Fatalf("script text included in archive: %q", result.Content)
	}
	if strings.Contains(result.Content, "Shared menu") || strings.Contains(result.Content, "Site header") || strings.Contains(result.Content, "Site footer") {
		t.Fatalf("site navigation included in archive: %q", result.Content)
	}
	if len(result.Links) != 1 || result.Links[0] != server.URL+"/next" {
		t.Fatalf("unexpected links: %v", result.Links)
	}
}

func TestFetchPageSkipsNonHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("not HTML"))
	}))
	defer server.Close()

	root, _ := url.Parse(server.URL)
	result, err := fetchPage(server.Client(), server.URL, root.Hostname())
	if err != nil {
		t.Fatal(err)
	}
	if result.IsHTML || result.Status != http.StatusOK || len(result.Links) != 0 {
		t.Fatalf("non-HTML response was parsed: %+v", result)
	}
}

func TestRobotsRules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("User-agent: SahilCrawler\nDisallow: /private\n"))
	}))
	defer server.Close()

	root, _ := url.Parse(server.URL)
	robots, err := loadRobots(server.Client(), root)
	if err != nil {
		t.Fatal(err)
	}
	private, _ := url.Parse(server.URL + "/private/page")
	public, _ := url.Parse(server.URL + "/public/page")
	if allowedByRobots(robots, private) || !allowedByRobots(robots, public) {
		t.Fatal("robots.txt rules were not applied")
	}
}

func TestSearchArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.csv")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := csv.NewWriter(file)
	writer.WriteAll([][]string{
		{"url", "final_url", "title", "content", "status", "depth"},
		{"https://example.com/go", "https://example.com/go", "Go Basics", "Learn goroutines", "200", "0"},
		{"https://example.com/rust", "https://example.com/rust", "Rust Basics", "Ownership", "200", "1"},
	})
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := searchArchive(path, "GOROUTINES", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "https://example.com/go") || strings.Contains(output.String(), "https://example.com/rust") || !strings.Contains(output.String(), "Matches: 1") {
		t.Fatalf("unexpected search output: %s", output.String())
	}
}
