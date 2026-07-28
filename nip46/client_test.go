//go:build !js

package nip46

import (
	"context"
	stdjson "encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
)

const testTimeout = 10 * time.Second

func newFakeRelay(handler func(*websocket.Conn)) *httptest.Server {
	return httptest.NewServer(&websocket.Server{
		Handshake: func(conf *websocket.Config, r *http.Request) error { return nil },
		Handler:   handler,
	})
}

func sendBunkerResponse(
	t *testing.T,
	conn *websocket.Conn,
	subID string,
	signerSecret string,
	clientPubkey string,
	conversationKey [32]byte,
	resp Response,
) {
	t.Helper()

	content, err := nip44.Encrypt(resp.String(), conversationKey)
	require.NoError(t, err)

	evt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindNostrConnect,
		Tags:      nostr.Tags{{"p", clientPubkey}},
		Content:   content,
	}
	require.NoError(t, evt.Sign(signerSecret))
	require.NoError(t, websocket.JSON.Send(conn, []any{"EVENT", subID, evt}))
}

// signerHandler acts as the remote signer behind the fake relay: it answers
// every RPC request with an auth_url challenge followed by a terminal response
// carrying the same request id. It tolerates REQ and EVENT arriving in either
// order on the shared connection.
func signerHandler(
	t *testing.T,
	signerSecret string,
	clientPubkey string,
	conversationKey [32]byte,
	authURL string,
	terminalResult string,
) func(*websocket.Conn) {
	return func(conn *websocket.Conn) {
		var subID string
		var pending *Request

		maybeRespond := func() {
			if subID == "" || pending == nil {
				return
			}
			sendBunkerResponse(t, conn, subID, signerSecret, clientPubkey, conversationKey,
				Response{ID: pending.ID, Result: "auth_url", Error: authURL})
			sendBunkerResponse(t, conn, subID, signerSecret, clientPubkey, conversationKey,
				Response{ID: pending.ID, Result: terminalResult})
			pending = nil
		}

		for {
			var raw []stdjson.RawMessage
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
				maybeRespond()
			case "EVENT":
				var evt nostr.Event
				if err := json.Unmarshal(raw[1], &evt); err != nil {
					return
				}
				if err := websocket.JSON.Send(conn, []any{"OK", evt.ID, true, ""}); err != nil {
					return
				}
				plain, err := nip44.Decrypt(evt.Content, conversationKey)
				if err != nil {
					return
				}
				var req Request
				if err := json.Unmarshal([]byte(plain), &req); err != nil {
					return
				}
				pending = &req
				maybeRespond()
			}
		}
	}
}

func TestAuthURLIsNonTerminalAndInvokesCallback(t *testing.T) {
	clientSecret := nostr.GeneratePrivateKey()
	clientPubkey, err := nostr.GetPublicKey(clientSecret)
	require.NoError(t, err)
	signerSecret := nostr.GeneratePrivateKey()
	signerPubkey, err := nostr.GetPublicKey(signerSecret)
	require.NoError(t, err)

	conversationKey, err := nip44.GenerateConversationKey(clientPubkey, signerSecret)
	require.NoError(t, err)

	authURL := "https://signer.example/auth/1"
	ws := newFakeRelay(signerHandler(t, signerSecret, clientPubkey, conversationKey, authURL, "ack"))
	defer ws.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	authReceived := make(chan string, 1)
	bunker := NewBunker(ctx, clientSecret, signerPubkey, []string{ws.URL}, nil, func(url string) {
		authReceived <- url
	})

	result, err := bunker.RPC(ctx, "ping", []string{})
	require.NoError(t, err)
	assert.Equal(t, "ack", result, "RPC must resolve with the follow-up response, not the auth_url challenge")

	select {
	case url := <-authReceived:
		assert.Equal(t, authURL, url)
	default:
		t.Fatal("onAuth callback was not invoked for the auth_url challenge")
	}
}

func TestAuthURLWithNilCallbackDoesNotPanic(t *testing.T) {
	clientSecret := nostr.GeneratePrivateKey()
	clientPubkey, err := nostr.GetPublicKey(clientSecret)
	require.NoError(t, err)
	signerSecret := nostr.GeneratePrivateKey()
	signerPubkey, err := nostr.GetPublicKey(signerSecret)
	require.NoError(t, err)

	conversationKey, err := nip44.GenerateConversationKey(clientPubkey, signerSecret)
	require.NoError(t, err)

	ws := newFakeRelay(signerHandler(t, signerSecret, clientPubkey, conversationKey, "https://signer.example/auth/2", "ack"))
	defer ws.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	bunker := NewBunker(ctx, clientSecret, signerPubkey, []string{ws.URL}, nil, nil)

	result, err := bunker.RPC(ctx, "ping", []string{})
	require.NoError(t, err)
	assert.Equal(t, "ack", result)
}
