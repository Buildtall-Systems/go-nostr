//go:build !js

package nostr

import (
	"context"
	stdjson "encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
)

func mockAuthAndPublishHandler(challenge string) func(*websocket.Conn) {
	return func(conn *websocket.Conn) {
		websocket.JSON.Send(conn, []any{"AUTH", challenge})

		authed := false
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
			switch typ {
			case "AUTH":
				var event Event
				if err := stdjson.Unmarshal(raw[1], &event); err != nil {
					return
				}
				websocket.JSON.Send(conn, []any{"OK", event.ID, true, ""})
				authed = true
			case "EVENT":
				var event Event
				if err := stdjson.Unmarshal(raw[1], &event); err != nil {
					return
				}
				if authed {
					websocket.JSON.Send(conn, []any{"OK", event.ID, true, ""})
				} else {
					websocket.JSON.Send(conn, []any{"OK", event.ID, false, "auth-required: please authenticate"})
				}
			}
		}
	}
}

func TestWithProactiveAuthPublish(t *testing.T) {
	ws := newWebsocketServer(mockAuthAndPublishHandler("proactive-challenge"))
	defer ws.Close()

	url := strings.Replace(ws.URL, "http://", "ws://", 1)

	pool := NewSimplePool(context.Background(),
		WithProactiveAuth(func(ctx context.Context, ae RelayEvent) error {
			return ae.Event.Sign(GeneratePrivateKey())
		}),
	)

	relay, err := pool.EnsureRelay(url)
	require.NoError(t, err)
	defer relay.Close()

	priv := GeneratePrivateKey()
	evt := Event{
		Kind:      KindTextNote,
		Content:   "proactive auth test",
		CreatedAt: Now(),
	}
	err = evt.Sign(priv)
	require.NoError(t, err)

	err = relay.Publish(context.Background(), evt)
	assert.NoError(t, err)
}

func TestWithProactiveAuthNonAuthRelay(t *testing.T) {
	ws := newWebsocketServer(func(conn *websocket.Conn) {
		io.ReadAll(conn)
	})
	defer ws.Close()

	url := strings.Replace(ws.URL, "http://", "ws://", 1)

	pool := NewSimplePool(context.Background(),
		WithProactiveAuth(func(ctx context.Context, ae RelayEvent) error {
			return ae.Event.Sign(GeneratePrivateKey())
		}),
	)

	start := time.Now()
	relay, err := pool.EnsureRelay(url)
	elapsed := time.Since(start)

	require.NoError(t, err)
	defer relay.Close()

	assert.Less(t, elapsed, 10*time.Second, "EnsureRelay should not block for full 15s connect timeout")
}

func TestWithProactiveAuthReactiveFallback(t *testing.T) {
	ws := newWebsocketServer(mockAuthAndPublishHandler("reactive-challenge"))
	defer ws.Close()

	url := strings.Replace(ws.URL, "http://", "ws://", 1)

	pool := NewSimplePool(context.Background(),
		WithAuthHandler(func(ctx context.Context, ae RelayEvent) error {
			return ae.Event.Sign(GeneratePrivateKey())
		}),
	)

	relay, err := pool.EnsureRelay(url)
	require.NoError(t, err)
	defer relay.Close()

	// drain the challenge so it's available for reactive auth
	select {
	case <-relay.challengeCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for challenge")
	}

	priv := GeneratePrivateKey()
	evt := Event{
		Kind:      KindTextNote,
		Content:   "reactive auth test",
		CreatedAt: Now(),
	}
	err = evt.Sign(priv)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := pool.PublishMany(ctx, []string{url}, evt)
	result := <-results
	assert.NoError(t, result.Error)
}

func TestWithProactiveAuthAuthDoneClosed(t *testing.T) {
	ws := newWebsocketServer(mockAuthAndPublishHandler("done-challenge"))
	defer ws.Close()

	url := strings.Replace(ws.URL, "http://", "ws://", 1)

	pool := NewSimplePool(context.Background(),
		WithProactiveAuth(func(ctx context.Context, ae RelayEvent) error {
			return ae.Event.Sign(GeneratePrivateKey())
		}),
	)

	relay, err := pool.EnsureRelay(url)
	require.NoError(t, err)
	defer relay.Close()

	select {
	case <-relay.AuthDone():
	default:
		t.Fatal("AuthDone should be closed after proactive auth in EnsureRelay")
	}
}
