package sdk

import (
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/stretchr/testify/require"
)

func TestPrepareNoteEvent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantTags nostr.Tags
		want     string
	}{
		{
			name:     "plain text",
			content:  "hello world",
			wantTags: nostr.Tags{},
			want:     "hello world",
		},
		{
			name:    "with nostr: prefix, url and hashtag",
			content: "hello nostr:npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6 please visit https://banana.com/ and get your free #banana",
			wantTags: nostr.Tags{
				{"p", "3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"},
			},
			want: "hello nostr:npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6 please visit https://banana.com/ and get your free #banana",
		},
		{
			// bare references are not matched by nip27 and the
			// scheme-fixing pass is not implemented, so the event
			// passes through untouched.
			name:     "with bare npub and bare url",
			content:  "hello npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6 please visit banana.com",
			wantTags: nostr.Tags{},
			want:     "hello npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6 please visit banana.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := &nostr.Event{
				Content: tt.content,
				Tags:    nostr.Tags{},
			}

			PrepareNoteEvent(evt)
			require.Equal(t, tt.want, evt.Content)
			require.Equal(t, tt.wantTags, evt.Tags)
		})
	}
}

func TestPrepareNoteEventDedup(t *testing.T) {
	mention := "nostr:npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6"
	pubkey := "3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"

	evt := &nostr.Event{
		Content: "hello " + mention + " and again " + mention,
		Tags:    nostr.Tags{},
	}
	PrepareNoteEvent(evt)
	require.Equal(t, nostr.Tags{{"p", pubkey}}, evt.Tags, "the same profile mentioned twice yields a single p tag")

	evt = &nostr.Event{
		Content: "hello " + mention,
		Tags:    nostr.Tags{{"p", pubkey}},
	}
	PrepareNoteEvent(evt)
	require.Equal(t, nostr.Tags{{"p", pubkey}}, evt.Tags, "a pre-existing p tag is not duplicated")
}
