package blossom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/nbd-wtf/go-nostr"
)

// OpenDownload requests a blob from the media server and returns its body
// stream along with the size reported by Content-Length (-1 when the server
// does not declare one). The caller must close the returned stream.
func (c *Client) OpenDownload(ctx context.Context, hash string) (io.ReadCloser, int64, error) {
	if !nostr.IsValid32ByteHex(hash) {
		return nil, 0, fmt.Errorf("%s is not a valid 32-byte hex string", hash)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.mediaserver+hash, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	authHeader := c.authorizationHeader(ctx, func(evt *nostr.Event) {
		evt.Tags = append(evt.Tags, nostr.Tag{"t", "get"})
		evt.Tags = append(evt.Tags, nostr.Tag{"x", hash})
	})
	req.Header.Add("Authorization", authHeader)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to call %s for %s: %w", c.mediaserver, hash, err)
	}

	if resp.StatusCode >= 300 {
		statusErr := fmt.Errorf("%s is not present in %s: %d", hash, c.mediaserver, resp.StatusCode)
		if closeErr := resp.Body.Close(); closeErr != nil {
			statusErr = errors.Join(statusErr, closeErr)
		}
		return nil, 0, statusErr
	}

	return resp.Body, resp.ContentLength, nil
}

// Download downloads a file from the media server by its hash
func (c *Client) Download(ctx context.Context, hash string) ([]byte, error) {
	body, _, err := c.OpenDownload(ctx, hash)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(body)
	if closeErr := body.Close(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// DownloadToFile downloads a file from the media server and saves it to the specified path
func (c *Client) DownloadToFile(ctx context.Context, hash string, filePath string) error {
	body, _, err := c.OpenDownload(ctx, hash)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		err = fmt.Errorf("failed to create file %s for %s: %w", filePath, hash, err)
		if closeErr := body.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return err
	}

	if _, copyErr := io.Copy(file, body); copyErr != nil {
		err = fmt.Errorf("failed to write to file %s for %s: %w", filePath, hash, copyErr)
	}
	if closeErr := body.Close(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		err = errors.Join(err, fmt.Errorf("failed to close file %s: %w", filePath, closeErr))
	}
	return err
}
