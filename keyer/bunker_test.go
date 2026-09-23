//go:build !js

package keyer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"github.com/nbd-wtf/go-nostr/nip46"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
)

const (
	testTimeout       = 10 * time.Second
	testBunkerTimeout = 500 * time.Millisecond
)

// fakeBunker is an in-process relay that also plays the remote signer: it
// decrypts every request the client publishes, records it, and answers with
// whatever answer returns (nil leaves the request unanswered).
type fakeBunker struct {
	t               *testing.T
	signerSecret    string
	signerPubkey    string
	clientPubkey    string
	conversationKey [32]byte
	requests        chan nip46.Request
	answer          func(nip46.Request) *nip46.Response
}

func newFakeBunker(t *testing.T, clientPubkey string, answer func(nip46.Request) *nip46.Response) (*fakeBunker, *httptest.Server) {
	t.Helper()
	signerSecret := nostr.GeneratePrivateKey()
	signerPubkey, err := nostr.GetPublicKey(signerSecret)
	require.NoError(t, err)
	conversationKey, err := nip44.GenerateConversationKey(clientPubkey, signerSecret)
	require.NoError(t, err)

	fb := &fakeBunker{
		t:               t,
		signerSecret:    signerSecret,
		signerPubkey:    signerPubkey,
		clientPubkey:    clientPubkey,
		conversationKey: conversationKey,
		requests:        make(chan nip46.Request, 8),
		answer:          answer,
	}
	srv := httptest.NewServer(&websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler:   fb.serve,
	})
	t.Cleanup(srv.Close)
	return fb, srv
}

func (fb *fakeBunker) send(conn *websocket.Conn, subID string, resp nip46.Response) bool {
	body, err := json.Marshal(resp)
	if !assert.NoError(fb.t, err) {
		return false
	}
	content, err := nip44.Encrypt(string(body), fb.conversationKey)
	if !assert.NoError(fb.t, err) {
		return false
	}
	evt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindNostrConnect,
		Tags:      nostr.Tags{{"p", fb.clientPubkey}},
		Content:   content,
	}
	if !assert.NoError(fb.t, evt.Sign(fb.signerSecret)) {
		return false
	}
	return assert.NoError(fb.t, websocket.JSON.Send(conn, []any{"EVENT", subID, evt}))
}

func (fb *fakeBunker) serve(conn *websocket.Conn) {
	var subID string
	var pending []nip46.Request
	flush := func() bool {
		if subID == "" {
			return true
		}
		for _, req := range pending {
			if resp := fb.answer(req); resp != nil {
				if !fb.send(conn, subID, *resp) {
					return false
				}
			}
		}
		pending = nil
		return true
	}

	for {
		var raw []json.RawMessage
		if err := websocket.JSON.Receive(conn, &raw); err != nil {
			return
		}
		if len(raw) < 2 {
			continue
		}
		var typ string
		if err := json.Unmarshal(raw[0], &typ); err != nil {
			continue
		}
		switch typ {
		case "REQ":
			if err := json.Unmarshal(raw[1], &subID); err != nil {
				return
			}
			if err := websocket.JSON.Send(conn, []any{"EOSE", subID}); err != nil {
				return
			}
			if !flush() {
				return
			}
		case "EVENT":
			var evt nostr.Event
			if err := json.Unmarshal(raw[1], &evt); err != nil {
				return
			}
			if err := websocket.JSON.Send(conn, []any{"OK", evt.ID, true, ""}); err != nil {
				return
			}
			plain, err := nip44.Decrypt(evt.Content, fb.conversationKey)
			if err != nil {
				return
			}
			var req nip46.Request
			if err := json.Unmarshal([]byte(plain), &req); err != nil {
				return
			}
			fb.requests <- req
			pending = append(pending, req)
			if !flush() {
				return
			}
		}
	}
}

func newClientKey(t *testing.T) (string, string) {
	t.Helper()
	sec := nostr.GeneratePrivateKey()
	pub, err := nostr.GetPublicKey(sec)
	require.NoError(t, err)
	return sec, pub
}

func TestBunkerSignerDecryptCallsNIP44Decrypt(t *testing.T) {
	clientSecret, clientPubkey := newClientKey(t)
	fb, srv := newFakeBunker(t, clientPubkey, func(req nip46.Request) *nip46.Response {
		return &nip46.Response{ID: req.ID, Result: "the plaintext"}
	})
	_, senderPubkey := newClientKey(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	bs := NewBunkerSignerFromBunkerClient(nip46.NewBunker(ctx, clientSecret, fb.signerPubkey, []string{srv.URL}, nil, nil))

	plain, err := bs.Decrypt(ctx, "the ciphertext", senderPubkey)
	require.NoError(t, err)
	assert.Equal(t, "the plaintext", plain)

	select {
	case req := <-fb.requests:
		assert.Equal(t, "nip44_decrypt", req.Method)
		assert.Equal(t, []string{senderPubkey, "the ciphertext"}, req.Params)
	case <-ctx.Done():
		t.Fatal("the bunker never received the decrypt request")
	}
}

func TestBunkerSignerHonorsSignTimeout(t *testing.T) {
	clientSecret, clientPubkey := newClientKey(t)
	fb, srv := newFakeBunker(t, clientPubkey, func(nip46.Request) *nip46.Response { return nil })
	_, peerPubkey := newClientKey(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	bs := BunkerSigner{
		bunker:  nip46.NewBunker(ctx, clientSecret, fb.signerPubkey, []string{srv.URL}, nil, nil),
		timeout: testBunkerTimeout,
	}

	ops := map[string]func(context.Context) error{
		"GetPublicKey": func(ctx context.Context) error { _, err := bs.GetPublicKey(ctx); return err },
		"SignEvent":    func(ctx context.Context) error { return bs.SignEvent(ctx, &nostr.Event{Kind: nostr.KindTextNote}) },
		"Encrypt":      func(ctx context.Context) error { _, err := bs.Encrypt(ctx, "plain", peerPubkey); return err },
		"Decrypt":      func(ctx context.Context) error { _, err := bs.Decrypt(ctx, "cipher", peerPubkey); return err },
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- op(context.Background()) }()
			select {
			case err := <-done:
				require.Error(t, err, "an unanswered %s must fail once the bunker timeout elapses", name)
			case <-time.After(testTimeout):
				t.Fatalf("%s hung past the test deadline; the configured bunker timeout was not honored", name)
			}
		})
	}
}

func TestWithTimeoutBoundsAClientBuiltSigner(t *testing.T) {
	clientSecret, clientPubkey := newClientKey(t)
	fb, srv := newFakeBunker(t, clientPubkey, func(nip46.Request) *nip46.Response { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	base := NewBunkerSignerFromBunkerClient(nip46.NewBunker(ctx, clientSecret, fb.signerPubkey, []string{srv.URL}, nil, nil))
	bs := base.WithTimeout(testBunkerTimeout)
	assert.Equal(t, testBunkerTimeout, bs.timeout)
	assert.Zero(t, base.timeout, "WithTimeout must not change the signer it was called on")

	done := make(chan error, 1)
	go func() { done <- bs.SignEvent(context.Background(), &nostr.Event{Kind: nostr.KindTextNote}) }()
	select {
	case err := <-done:
		require.Error(t, err, "an unanswered sign_event must fail once the bunker timeout elapses")
	case <-time.After(testTimeout):
		t.Fatal("SignEvent hung past the test deadline; WithTimeout was not honored")
	}
}

func TestNewPassesBunkerSignTimeoutThrough(t *testing.T) {
	clientSecret, clientPubkey := newClientKey(t)
	fb, srv := newFakeBunker(t, clientPubkey, func(req nip46.Request) *nip46.Response {
		return &nip46.Response{ID: req.ID, Result: "ack"}
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	uri := "bunker://" + fb.signerPubkey + "?relay=" + srv.URL + "&secret=s"
	signer, err := New(ctx, nil, uri, &SignerOptions{
		BunkerClientSecretKey: clientSecret,
		BunkerSignTimeout:     testBunkerTimeout,
	})
	require.NoError(t, err)

	bs, ok := signer.(BunkerSigner)
	require.True(t, ok, "a bunker:// input must yield a BunkerSigner, got %T", signer)
	assert.Equal(t, testBunkerTimeout, bs.timeout)
}
