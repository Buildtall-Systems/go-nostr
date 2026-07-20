package blossom

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newUploadTestServer(t *testing.T, gotBody *[]byte, descriptorSize int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		*gotBody = data
		if err := json.NewEncoder(w).Encode(BlobDescriptor{
			URL:    "https://blossom.example/blob",
			SHA256: strings.Repeat("d", 64),
			Size:   descriptorSize,
		}); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
}

func writeUploadInput(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "blob.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	return path
}

func TestUploadFileWithProgress(t *testing.T) {
	content := bytes.Repeat([]byte("x"), 4096)
	path := writeUploadInput(t, content)

	var gotBody []byte
	srv := newUploadTestServer(t, &gotBody, len(content))
	defer srv.Close()

	c := NewClient(srv.URL, stubSigner{})

	var progress bytes.Buffer
	bd, err := c.UploadFileWithProgress(context.Background(), path, &progress)
	if err != nil {
		t.Fatalf("UploadFileWithProgress: %v", err)
	}
	if bd.Size != len(content) {
		t.Errorf("descriptor size: want %d, got %d", len(content), bd.Size)
	}
	if !bytes.Equal(gotBody, content) {
		t.Errorf("uploaded body: want %d bytes matching input, got %d bytes", len(content), len(gotBody))
	}
	if !bytes.Equal(progress.Bytes(), content) {
		t.Errorf("progress writer: want %d bytes matching input, got %d bytes", len(content), progress.Len())
	}
}

func TestUploadFileWithHeaders(t *testing.T) {
	content := []byte("gated-upload")
	path := writeUploadInput(t, content)

	var gotPolicy []string
	var gotGrants []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPolicy = r.Header.Values("X-Access-Policy")
		gotGrants = r.Header.Values("X-Access-Grant")
		if err := json.NewEncoder(w).Encode(BlobDescriptor{
			URL:    "https://blossom.example/blob",
			SHA256: strings.Repeat("d", 64),
			Size:   len(content),
		}); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, stubSigner{})

	_, err := c.UploadFile(context.Background(), path,
		WithUploadHeader("X-Access-Policy", "private"),
		WithUploadHeader("X-Access-Grant", "npub1alice"),
		WithUploadHeader("X-Access-Grant", "npub1bob"),
	)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if len(gotPolicy) != 1 || gotPolicy[0] != "private" {
		t.Errorf("X-Access-Policy: want [private], got %v", gotPolicy)
	}
	if len(gotGrants) != 2 || gotGrants[0] != "npub1alice" || gotGrants[1] != "npub1bob" {
		t.Errorf("X-Access-Grant: want [npub1alice npub1bob], got %v", gotGrants)
	}
}

func TestUploadFileNoOptionsSendsNoExtraHeaders(t *testing.T) {
	content := []byte("plain-upload-headers")
	path := writeUploadInput(t, content)

	var gotPolicy []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPolicy = r.Header.Values("X-Access-Policy")
		if err := json.NewEncoder(w).Encode(BlobDescriptor{
			URL:    "https://blossom.example/blob",
			SHA256: strings.Repeat("d", 64),
			Size:   len(content),
		}); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, stubSigner{})

	if _, err := c.UploadFile(context.Background(), path); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if len(gotPolicy) != 0 {
		t.Errorf("X-Access-Policy: want none, got %v", gotPolicy)
	}
}

func TestUploadFileNilProgress(t *testing.T) {
	content := []byte("plain-upload")
	path := writeUploadInput(t, content)

	var gotBody []byte
	srv := newUploadTestServer(t, &gotBody, len(content))
	defer srv.Close()

	c := NewClient(srv.URL, stubSigner{})

	bd, err := c.UploadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if bd.Size != len(content) {
		t.Errorf("descriptor size: want %d, got %d", len(content), bd.Size)
	}
	if !bytes.Equal(gotBody, content) {
		t.Errorf("uploaded body: want %q, got %q", content, gotBody)
	}
}
