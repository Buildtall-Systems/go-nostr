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
