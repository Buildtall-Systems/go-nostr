package blossom

import (
	"context"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

type stubSigner struct{}

func (stubSigner) GetPublicKey(_ context.Context) (string, error) {
	return "stub", nil
}

func (stubSigner) SignEvent(_ context.Context, _ *nostr.Event) error {
	return nil
}

var _ nostr.Signer = stubSigner{}

func TestNewClientDefaultNoTimeout(t *testing.T) {
	c := NewClient("https://blossom.example", stubSigner{})

	if c.httpClient.ReadTimeout != 0 {
		t.Errorf("default ReadTimeout: want 0, got %s", c.httpClient.ReadTimeout)
	}
	if c.httpClient.WriteTimeout != 0 {
		t.Errorf("default WriteTimeout: want 0, got %s", c.httpClient.WriteTimeout)
	}
}

func TestNewClientWithReadTimeout(t *testing.T) {
	want := 5 * time.Second
	c := NewClient("https://blossom.example", stubSigner{}, WithReadTimeout(want))

	if c.httpClient.ReadTimeout != want {
		t.Errorf("ReadTimeout: want %s, got %s", want, c.httpClient.ReadTimeout)
	}
	if c.httpClient.WriteTimeout != 0 {
		t.Errorf("WriteTimeout should remain zero when only WithReadTimeout is set, got %s", c.httpClient.WriteTimeout)
	}
}

func TestNewClientWithWriteTimeout(t *testing.T) {
	want := 7 * time.Second
	c := NewClient("https://blossom.example", stubSigner{}, WithWriteTimeout(want))

	if c.httpClient.WriteTimeout != want {
		t.Errorf("WriteTimeout: want %s, got %s", want, c.httpClient.WriteTimeout)
	}
	if c.httpClient.ReadTimeout != 0 {
		t.Errorf("ReadTimeout should remain zero when only WithWriteTimeout is set, got %s", c.httpClient.ReadTimeout)
	}
}

func TestNewClientWithBothTimeouts(t *testing.T) {
	wantRead := 3 * time.Second
	wantWrite := 11 * time.Second
	c := NewClient("https://blossom.example", stubSigner{},
		WithReadTimeout(wantRead),
		WithWriteTimeout(wantWrite),
	)

	if c.httpClient.ReadTimeout != wantRead {
		t.Errorf("ReadTimeout: want %s, got %s", wantRead, c.httpClient.ReadTimeout)
	}
	if c.httpClient.WriteTimeout != wantWrite {
		t.Errorf("WriteTimeout: want %s, got %s", wantWrite, c.httpClient.WriteTimeout)
	}
}

func TestNewClientNormalizesMediaServerURL(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bare host gets https prefix and trailing slash",
			input: "blossom.example",
			want:  "https://blossom.example/",
		},
		{
			name:  "https prefix preserved, trailing slash added",
			input: "https://blossom.example",
			want:  "https://blossom.example/",
		},
		{
			name:  "http prefix preserved",
			input: "http://blossom.example",
			want:  "http://blossom.example/",
		},
		{
			name:  "trailing slash not doubled",
			input: "https://blossom.example/",
			want:  "https://blossom.example/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(tc.input, stubSigner{})
			if c.mediaserver != tc.want {
				t.Errorf("mediaserver: want %q, got %q", tc.want, c.mediaserver)
			}
		})
	}
}
