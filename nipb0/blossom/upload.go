package blossom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"

	"github.com/nbd-wtf/go-nostr"
)

// UploadOption adjusts a single upload request.
type UploadOption func(*uploadOptions)

type uploadOptions struct {
	headers [][2]string
}

// WithUploadHeader adds an extra header to the upload request. Repeatable;
// repeated names are sent as repeated headers.
func WithUploadHeader(name, value string) UploadOption {
	return func(o *uploadOptions) {
		o.headers = append(o.headers, [2]string{name, value})
	}
}

// UploadFile uploads a file to the media server
func (c *Client) UploadFile(ctx context.Context, filePath string, opts ...UploadOption) (*BlobDescriptor, error) {
	return c.UploadFileWithProgress(ctx, filePath, nil, opts...)
}

// UploadFileWithProgress uploads a file to the media server, teeing the bytes
// sent over the network through progress. The preliminary hashing pass does
// not touch progress, so a byte-counting writer observes only the transfer
// itself. A nil progress behaves exactly like UploadFile.
func (c *Client) UploadFileWithProgress(ctx context.Context, filePath string, progress io.Writer, opts ...UploadOption) (*BlobDescriptor, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", filePath, err)
	}

	bd, err := c.uploadOpenFile(ctx, file, filePath, progress, opts)
	if closeErr := file.Close(); closeErr != nil {
		err = errors.Join(err, fmt.Errorf("failed to close %s: %w", filePath, closeErr))
	}
	if err != nil {
		return nil, err
	}
	return bd, nil
}

// uploadOpenFile hashes the open file, rewinds it, and streams it to the server.
func (c *Client) uploadOpenFile(ctx context.Context, file *os.File, filePath string, progress io.Writer, opts []UploadOption) (*BlobDescriptor, error) {
	options := uploadOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	sha := sha256.New()
	size, err := io.Copy(sha, file)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filePath, err)
	}
	hash := sha.Sum(nil)

	_, err = file.Seek(0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to reset file position: %w", err)
	}

	contentType := mime.TypeByExtension(filepath.Ext(filePath))

	// fasthttp closes a body stream that implements io.Closer; NopCloser keeps
	// ownership of the file handle here (the tee path hides Close by nature).
	var body io.Reader = io.NopCloser(file)
	if progress != nil {
		body = io.TeeReader(file, progress)
	}

	bd := BlobDescriptor{}
	err = c.httpCall(ctx, "PUT", "upload", contentType, func() string {
		return c.authorizationHeader(ctx, func(evt *nostr.Event) {
			evt.Tags = append(evt.Tags, nostr.Tag{"t", "upload"})
			evt.Tags = append(evt.Tags, nostr.Tag{"x", hex.EncodeToString(hash[:])})
		})
	}, options.headers, body, size, &bd)
	if err != nil {
		return nil, fmt.Errorf("failed to upload %s: %w", filePath, err)
	}

	return &bd, nil
}
