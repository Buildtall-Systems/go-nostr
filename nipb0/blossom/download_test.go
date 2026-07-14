package blossom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadUsesSingleSlashPath(t *testing.T) {
	hash := strings.Repeat("a", 64)
	body := []byte("blob-bytes")

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if _, err := w.Write(body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, stubSigner{})
	wantPath := "/" + hash

	t.Run("Download", func(t *testing.T) {
		gotPath = ""
		got, err := c.Download(context.Background(), hash)
		if err != nil {
			t.Fatalf("Download: %v", err)
		}
		if gotPath != wantPath {
			t.Errorf("request path: want %q, got %q", wantPath, gotPath)
		}
		if string(got) != string(body) {
			t.Errorf("body: want %q, got %q", body, got)
		}
	})

	t.Run("DownloadToFile", func(t *testing.T) {
		gotPath = ""
		out := filepath.Join(t.TempDir(), "blob.bin")
		if err := c.DownloadToFile(context.Background(), hash, out); err != nil {
			t.Fatalf("DownloadToFile: %v", err)
		}
		if gotPath != wantPath {
			t.Errorf("request path: want %q, got %q", wantPath, gotPath)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		if string(data) != string(body) {
			t.Errorf("file body: want %q, got %q", body, data)
		}
	})
}
