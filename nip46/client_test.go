//go:build !js

package nip46

import (
	"context"
	stdjson "encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
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
// carrying the same request id. A real signer sends the terminal response only
// after the user approves at the auth URL, so when approved is not nil the
// handler waits on it between the two. It tolerates REQ and EVENT arriving in
// either order on the shared connection, and answers each REQ with EOSE as a
// relay does.
func signerHandler(
	t *testing.T,
	signerSecret string,
	clientPubkey string,
	conversationKey [32]byte,
	authURL string,
	terminalResult string,
	approved <-chan struct{},
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
			if approved != nil {
				<-approved
			}
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
				if err := websocket.JSON.Send(conn, []any{"EOSE", subID}); err != nil {
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
	approved := make(chan struct{})
	ws := newFakeRelay(signerHandler(t, signerSecret, clientPubkey, conversationKey, authURL, "ack", approved))
	defer ws.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	authReceived := make(chan string, 1)
	bunker := NewBunker(ctx, clientSecret, signerPubkey, []string{ws.URL}, nil, func(url string) {
		authReceived <- url
		close(approved)
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

	ws := newFakeRelay(signerHandler(t, signerSecret, clientPubkey, conversationKey, "https://signer.example/auth/2", "ack", nil))
	defer ws.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	bunker := NewBunker(ctx, clientSecret, signerPubkey, []string{ws.URL}, nil, nil)

	result, err := bunker.RPC(ctx, "ping", []string{})
	require.NoError(t, err)
	assert.Equal(t, "ack", result)
}

// slowSubscriptionRelay registers each REQ only after registrationDelay and
// then sends EOSE. The signer behind it answers every request at once, and,
// as on a real relay, an answer that finds no registered subscription is lost.
func slowSubscriptionRelay(
	t *testing.T,
	signerSecret string,
	clientPubkey string,
	conversationKey [32]byte,
	registrationDelay time.Duration,
) func(*websocket.Conn) {
	return func(conn *websocket.Conn) {
		var mu sync.Mutex
		var subID string

		send := func(msg []any) error {
			mu.Lock()
			defer mu.Unlock()
			return websocket.JSON.Send(conn, msg)
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
				var id string
				if err := json.Unmarshal(raw[1], &id); err != nil {
					return
				}
				go func() {
					time.Sleep(registrationDelay)
					mu.Lock()
					subID = id
					mu.Unlock()
					_ = send([]any{"EOSE", id})
				}()
			case "EVENT":
				var evt nostr.Event
				if err := json.Unmarshal(raw[1], &evt); err != nil {
					return
				}
				if err := send([]any{"OK", evt.ID, true, ""}); err != nil {
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

				mu.Lock()
				registered := subID
				mu.Unlock()
				if registered == "" {
					continue
				}

				content, err := nip44.Encrypt(Response{ID: req.ID, Result: "pong"}.String(), conversationKey)
				require.NoError(t, err)
				resp := nostr.Event{
					CreatedAt: nostr.Now(),
					Kind:      nostr.KindNostrConnect,
					Tags:      nostr.Tags{{"p", clientPubkey}},
					Content:   content,
				}
				require.NoError(t, resp.Sign(signerSecret))
				if err := send([]any{"EVENT", registered, resp}); err != nil {
					return
				}
			}
		}
	}
}

func TestRPCWaitsForTheResponseSubscription(t *testing.T) {
	clientSecret := nostr.GeneratePrivateKey()
	clientPubkey, err := nostr.GetPublicKey(clientSecret)
	require.NoError(t, err)
	signerSecret := nostr.GeneratePrivateKey()
	signerPubkey, err := nostr.GetPublicKey(signerSecret)
	require.NoError(t, err)

	conversationKey, err := nip44.GenerateConversationKey(clientPubkey, signerSecret)
	require.NoError(t, err)

	ws := newFakeRelay(slowSubscriptionRelay(t, signerSecret, clientPubkey, conversationKey, 200*time.Millisecond))
	defer ws.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	bunker := NewBunker(ctx, clientSecret, signerPubkey, []string{ws.URL}, nil, nil)

	result, err := bunker.RPC(ctx, "ping", []string{})
	require.NoError(t, err, "the answer to a request published before the relay registered the subscription was lost")
	assert.Equal(t, "pong", result)
}

// needsHistoricalEvents mirrors nostr-rs-relay's Subscription rule: a REQ gets
// a stored-event query, and so an EOSE, only when some filter is not limit 0.
func needsHistoricalEvents(filters []stdjson.RawMessage) bool {
	for _, raw := range filters {
		var filter struct {
			Limit *int `json:"limit"`
		}
		if err := json.Unmarshal(raw, &filter); err != nil {
			return true
		}
		if filter.Limit == nil || *filter.Limit != 0 {
			return true
		}
	}
	return false
}

// realtimeOnlyRelay registers every REQ at once but, like nostr-rs-relay,
// sends no EOSE for a subscription whose filters are all limit 0. The signer
// behind it answers every request with "pong".
func realtimeOnlyRelay(
	t *testing.T,
	signerSecret string,
	clientPubkey string,
	conversationKey [32]byte,
) func(*websocket.Conn) {
	return func(conn *websocket.Conn) {
		var subID string
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
				if needsHistoricalEvents(raw[2:]) {
					if err := websocket.JSON.Send(conn, []any{"EOSE", subID}); err != nil {
						return
					}
				}
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
				if subID == "" {
					continue
				}
				sendBunkerResponse(t, conn, subID, signerSecret, clientPubkey, conversationKey,
					Response{ID: req.ID, Result: "pong"})
			}
		}
	}
}

func TestRPCGetsEOSEFromARelayThatSkipsItForLimitZero(t *testing.T) {
	clientSecret := nostr.GeneratePrivateKey()
	clientPubkey, err := nostr.GetPublicKey(clientSecret)
	require.NoError(t, err)
	signerSecret := nostr.GeneratePrivateKey()
	signerPubkey, err := nostr.GetPublicKey(signerSecret)
	require.NoError(t, err)

	conversationKey, err := nip44.GenerateConversationKey(clientPubkey, signerSecret)
	require.NoError(t, err)

	ws := newFakeRelay(realtimeOnlyRelay(t, signerSecret, clientPubkey, conversationKey))
	defer ws.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	bunker := NewBunker(ctx, clientSecret, signerPubkey, []string{ws.URL}, nil, nil)

	result, err := bunker.RPC(ctx, "ping", []string{})
	require.NoError(t, err, "RPC waits for an EOSE the relay never sends to a limit 0 subscription")
	assert.Equal(t, "pong", result)
}
