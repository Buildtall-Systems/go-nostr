//go:build !js

package nostr

import (
	"bytes"
	"context"
	stdjson "encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
)

func TestPublish(t *testing.T) {
	// test note to be sent over websocket
	priv, pub := makeKeyPair(t)
	textNote := Event{
		Kind:      KindTextNote,
		Content:   "hello",
		CreatedAt: Timestamp(1672068534), // random fixed timestamp
		Tags:      Tags{[]string{"foo", "bar"}},
		PubKey:    pub,
	}
	err := textNote.Sign(priv)
	assert.NoError(t, err)

	// fake relay server
	var mu sync.Mutex // guards published to satisfy go test -race
	var published bool
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		mu.Lock()
		published = true
		mu.Unlock()
		// verify the client sent exactly the textNote
		var raw []stdjson.RawMessage
		err := websocket.JSON.Receive(conn, &raw)
		assert.NoError(t, err)

		event := parseEventMessage(t, raw)
		assert.True(t, bytes.Equal(event.Serialize(), textNote.Serialize()))

		// send back an ok nip-20 command result
		res := []any{"OK", textNote.ID, true, ""}
		err = websocket.JSON.Send(conn, res)
		assert.NoError(t, err)
	})
	defer ws.Close()

	// connect a client and send the text note
	rl := mustRelayConnect(t, ws.URL)
	err = rl.Publish(context.Background(), textNote)
	assert.NoError(t, err)

	assert.True(t, published, "fake relay server saw no event")
}

func TestPublishBlocked(t *testing.T) {
	// test note to be sent over websocket
	textNote := Event{Kind: KindTextNote, Content: "hello"}
	textNote.ID = textNote.GetID()

	// fake relay server
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		// discard received message; not interested
		var raw []stdjson.RawMessage
		err := websocket.JSON.Receive(conn, &raw)
		assert.NoError(t, err)

		// send back a not ok nip-20 command result
		res := []any{"OK", textNote.ID, false, "blocked"}
		websocket.JSON.Send(conn, res)
	})
	defer ws.Close()

	// connect a client and send a text note
	rl := mustRelayConnect(t, ws.URL)
	err := rl.Publish(context.Background(), textNote)
	assert.Error(t, err)
}

func TestPublishWriteFailed(t *testing.T) {
	// test note to be sent over websocket
	textNote := Event{Kind: KindTextNote, Content: "hello"}
	textNote.ID = textNote.GetID()

	// fake relay server
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		// reject receive - force send error
		conn.Close()
	})
	defer ws.Close()

	// connect a client and send a text note
	rl := mustRelayConnect(t, ws.URL)
	// Force brief period of time so that publish always fails on closed socket.
	time.Sleep(1 * time.Millisecond)
	err := rl.Publish(context.Background(), textNote)
	assert.Error(t, err)
}

func TestConnectContext(t *testing.T) {
	// fake relay server
	var mu sync.Mutex // guards connected to satisfy go test -race
	var connected bool
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		mu.Lock()
		connected = true
		mu.Unlock()
		io.ReadAll(conn) // discard all input
	})
	defer ws.Close()

	// relay client
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := RelayConnect(ctx, ws.URL)
	assert.NoError(t, err)

	defer r.Close()

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, connected, "fake relay server saw no client connect")
}

func TestConnectContextCanceled(t *testing.T) {
	// fake relay server
	ws := newWebsocketServer(discardingHandler)
	defer ws.Close()

	// relay client
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // make ctx expired
	_, err := RelayConnect(ctx, ws.URL)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestConnectWithOrigin(t *testing.T) {
	// fake relay server
	// default handler requires origin golang.org/x/net/websocket
	ws := httptest.NewServer(websocket.Handler(discardingHandler))
	defer ws.Close()

	// relay client
	r := NewRelay(context.Background(), NormalizeURL(ws.URL),
		WithRequestHeader(http.Header{"origin": {"https://example.com"}}))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := r.Connect(ctx)
	assert.NoError(t, err)
}

func discardingHandler(conn *websocket.Conn) {
	io.ReadAll(conn) // discard all input
}

func newWebsocketServer(handler func(*websocket.Conn)) *httptest.Server {
	return httptest.NewServer(&websocket.Server{
		Handshake: anyOriginHandshake,
		Handler:   handler,
	})
}

// anyOriginHandshake is an alternative to default in golang.org/x/net/websocket
// which checks for origin. nostr client sends no origin and it makes no difference
// for the tests here anyway.
var anyOriginHandshake = func(conf *websocket.Config, r *http.Request) error {
	return nil
}

func makeKeyPair(t *testing.T) (priv, pub string) {
	t.Helper()

	privkey := GeneratePrivateKey()
	pubkey, err := GetPublicKey(privkey)
	assert.NoError(t, err)

	return privkey, pubkey
}

func mustRelayConnect(t *testing.T, url string) *Relay {
	t.Helper()

	rl, err := RelayConnect(context.Background(), url)
	require.NoError(t, err)

	return rl
}

func parseEventMessage(t *testing.T, raw []stdjson.RawMessage) Event {
	t.Helper()

	assert.Condition(t, func() (success bool) {
		return len(raw) >= 2
	})

	var typ string
	err := json.Unmarshal(raw[0], &typ)
	assert.NoError(t, err)
	assert.Equal(t, "EVENT", typ)

	var event Event
	err = json.Unmarshal(raw[1], &event)
	require.NoError(t, err)

	return event
}

func parseSubscriptionMessage(t *testing.T, raw []stdjson.RawMessage) (subid string, filters []Filter) {
	t.Helper()

	assert.Greater(t, len(raw), 3)

	var typ string
	err := json.Unmarshal(raw[0], &typ)

	assert.NoError(t, err)
	assert.Equal(t, "REQ", typ)

	var id string
	err = json.Unmarshal(raw[1], &id)
	assert.NoError(t, err)

	var ff []Filter
	for _, b := range raw[2:] {
		var f Filter
		err := json.Unmarshal(b, &f)
		assert.NoError(t, err)
		ff = append(ff, f)
	}
	return id, ff
}

// mockAuthHandler creates a websocket handler that simulates NIP-42 AUTH.
// It sends an AUTH challenge, waits for the client's AUTH response, validates
// the kind 22242 event, and sends an OK with the given acceptance result.
func mockAuthHandler(challenge string, acceptAuth bool) func(*websocket.Conn) {
	return func(conn *websocket.Conn) {
		// send AUTH challenge
		authMsg := []any{"AUTH", challenge}
		if err := websocket.JSON.Send(conn, authMsg); err != nil {
			return
		}

		// read client messages until we get an AUTH response
		for {
			var raw []stdjson.RawMessage
			if err := websocket.JSON.Receive(conn, &raw); err != nil {
				return
			}
			if len(raw) < 2 {
				continue
			}
			var typ string
			if err := stdjson.Unmarshal(raw[0], &typ); err != nil {
				continue
			}
			if typ != "AUTH" {
				continue
			}

			// parse the AUTH event to extract its ID
			var event Event
			if err := stdjson.Unmarshal(raw[1], &event); err != nil {
				return
			}

			reason := ""
			if !acceptAuth {
				reason = "auth-required: rejected"
			}
			okMsg := []any{"OK", event.ID, acceptAuth, reason}
			websocket.JSON.Send(conn, okMsg)
			break
		}

		// keep connection alive
		io.ReadAll(conn)
	}
}

func TestAuthDoneInitiallyOpen(t *testing.T) {
	r := NewRelay(context.Background(), "wss://example.com")
	select {
	case <-r.AuthDone():
		t.Fatal("AuthDone channel should not be closed on a new relay")
	default:
	}
}

func TestChallengeChanReceivesChallenge(t *testing.T) {
	challenge := "test-challenge-abc123"
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		authMsg := []any{"AUTH", challenge}
		websocket.JSON.Send(conn, authMsg)
		io.ReadAll(conn)
	})
	defer ws.Close()

	rl := mustRelayConnect(t, ws.URL)
	defer rl.Close()

	select {
	case got := <-rl.challengeCh:
		assert.Equal(t, challenge, got)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for challenge on challengeCh")
	}
	assert.Equal(t, challenge, rl.challenge)
}

func TestAuthDoneClosesOnSuccess(t *testing.T) {
	challenge := "test-challenge-success"
	ws := newWebsocketServer(mockAuthHandler(challenge, true))
	defer ws.Close()

	rl := mustRelayConnect(t, ws.URL)
	defer rl.Close()

	// wait for challenge to arrive
	select {
	case <-rl.challengeCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for challenge")
	}

	err := rl.Auth(context.Background(), func(ev *Event) error {
		return ev.Sign(GeneratePrivateKey())
	})
	assert.NoError(t, err)

	select {
	case <-rl.AuthDone():
	default:
		t.Fatal("AuthDone channel should be closed after successful Auth")
	}
}

func TestAuthDoneNotClosedOnFailure(t *testing.T) {
	challenge := "test-challenge-failure"
	ws := newWebsocketServer(mockAuthHandler(challenge, false))
	defer ws.Close()

	rl := mustRelayConnect(t, ws.URL)
	defer rl.Close()

	select {
	case <-rl.challengeCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for challenge")
	}

	err := rl.Auth(context.Background(), func(ev *Event) error {
		return ev.Sign(GeneratePrivateKey())
	})
	assert.Error(t, err)

	select {
	case <-rl.AuthDone():
		t.Fatal("AuthDone channel should NOT be closed after failed Auth")
	default:
	}
}

func TestAuthDoneDoubleSuccess(t *testing.T) {
	// mock relay that accepts AUTH twice
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		challenge := "double-success-challenge"
		websocket.JSON.Send(conn, []any{"AUTH", challenge})

		for i := 0; i < 2; i++ {
			for {
				var raw []stdjson.RawMessage
				if err := websocket.JSON.Receive(conn, &raw); err != nil {
					return
				}
				if len(raw) < 2 {
					continue
				}
				var typ string
				if err := stdjson.Unmarshal(raw[0], &typ); err != nil {
					continue
				}
				if typ != "AUTH" {
					continue
				}
				var event Event
				if err := stdjson.Unmarshal(raw[1], &event); err != nil {
					return
				}
				websocket.JSON.Send(conn, []any{"OK", event.ID, true, ""})
				break
			}
		}
		io.ReadAll(conn)
	})
	defer ws.Close()

	rl := mustRelayConnect(t, ws.URL)
	defer rl.Close()

	select {
	case <-rl.challengeCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for challenge")
	}

	sign := func(ev *Event) error { return ev.Sign(GeneratePrivateKey()) }

	err := rl.Auth(context.Background(), sign)
	assert.NoError(t, err)

	// second Auth call should not panic (sync.Once guards the close)
	err = rl.Auth(context.Background(), sign)
	assert.NoError(t, err)

	select {
	case <-rl.AuthDone():
	default:
		t.Fatal("AuthDone should be closed")
	}
}

func TestChallengeChanReplacesOld(t *testing.T) {
	first := "challenge-first"
	second := "challenge-second"
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		websocket.JSON.Send(conn, []any{"AUTH", first})
		time.Sleep(100 * time.Millisecond)
		websocket.JSON.Send(conn, []any{"AUTH", second})
		io.ReadAll(conn)
	})
	defer ws.Close()

	rl := mustRelayConnect(t, ws.URL)
	defer rl.Close()

	// wait long enough for both challenges to arrive
	time.Sleep(500 * time.Millisecond)

	// only the latest challenge should be available
	select {
	case got := <-rl.challengeCh:
		assert.Equal(t, second, got)
	default:
		t.Fatal("expected a challenge on challengeCh")
	}

	// channel should be empty now
	select {
	case extra := <-rl.challengeCh:
		t.Fatalf("expected empty channel, got %q", extra)
	default:
	}
}

func TestPerformAuthHappyPath(t *testing.T) {
	challenge := "perform-auth-happy"
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		time.Sleep(100 * time.Millisecond)
		websocket.JSON.Send(conn, []any{"AUTH", challenge})

		for {
			var raw []stdjson.RawMessage
			if err := websocket.JSON.Receive(conn, &raw); err != nil {
				return
			}
			if len(raw) < 2 {
				continue
			}
			var typ string
			if err := stdjson.Unmarshal(raw[0], &typ); err != nil {
				continue
			}
			if typ != "AUTH" {
				continue
			}
			var event Event
			if err := stdjson.Unmarshal(raw[1], &event); err != nil {
				return
			}
			websocket.JSON.Send(conn, []any{"OK", event.ID, true, ""})
			break
		}
		io.ReadAll(conn)
	})
	defer ws.Close()

	rl := mustRelayConnect(t, ws.URL)
	defer rl.Close()

	err := rl.PerformAuth(context.Background(), func(ev *Event) error {
		return ev.Sign(GeneratePrivateKey())
	})
	assert.NoError(t, err)

	select {
	case <-rl.AuthDone():
	default:
		t.Fatal("AuthDone should be closed after PerformAuth success")
	}
}

func TestPerformAuthChallengeAlreadyAvailable(t *testing.T) {
	challenge := "pre-existing-challenge"
	ws := newWebsocketServer(mockAuthHandler(challenge, true))
	defer ws.Close()

	rl := mustRelayConnect(t, ws.URL)
	defer rl.Close()

	// wait for challenge to arrive on the channel so r.challenge is set
	select {
	case <-rl.challengeCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for challenge")
	}

	// r.challenge is now set; PerformAuth should skip waiting
	err := rl.PerformAuth(context.Background(), func(ev *Event) error {
		return ev.Sign(GeneratePrivateKey())
	})
	assert.NoError(t, err)

	select {
	case <-rl.AuthDone():
	default:
		t.Fatal("AuthDone should be closed")
	}
}

func TestPerformAuthContextTimeout(t *testing.T) {
	// relay that never sends AUTH challenge
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		io.ReadAll(conn)
	})
	defer ws.Close()

	rl := mustRelayConnect(t, ws.URL)
	defer rl.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := rl.PerformAuth(ctx, func(ev *Event) error {
		return ev.Sign(GeneratePrivateKey())
	})
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-rl.AuthDone():
		t.Fatal("AuthDone should NOT be closed after timeout")
	default:
	}
}

func TestPerformAuthConnectionDeath(t *testing.T) {
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		time.Sleep(100 * time.Millisecond)
		conn.Close()
	})
	defer ws.Close()

	rl := mustRelayConnect(t, ws.URL)
	defer rl.Close()

	err := rl.PerformAuth(context.Background(), func(ev *Event) error {
		return ev.Sign(GeneratePrivateKey())
	})
	assert.Error(t, err)
}

func TestPerformAuthRelayRejectsAuth(t *testing.T) {
	challenge := "perform-auth-rejected"
	ws := newWebsocketServer(mockAuthHandler(challenge, false))
	defer ws.Close()

	rl := mustRelayConnect(t, ws.URL)
	defer rl.Close()

	err := rl.PerformAuth(context.Background(), func(ev *Event) error {
		return ev.Sign(GeneratePrivateKey())
	})
	assert.Error(t, err)

	select {
	case <-rl.AuthDone():
		t.Fatal("AuthDone should NOT be closed after rejected auth")
	default:
	}
}
