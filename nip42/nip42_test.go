package nip42

import (
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/stretchr/testify/require"
)

const (
	testRelayURL  = "wss://relay.example.com"
	testChallenge = "challenge-value"
)

func signedAuthEvent(t *testing.T) (*nostr.Event, string) {
	t.Helper()
	sk := nostr.GeneratePrivateKey()
	pk, err := nostr.GetPublicKey(sk)
	require.NoError(t, err)

	evt := CreateUnsignedAuthEvent(testChallenge, pk, testRelayURL)
	require.NoError(t, evt.Sign(sk))
	return &evt, pk
}

func TestValidateAuthEventAccepts(t *testing.T) {
	evt, pk := signedAuthEvent(t)

	got, ok := ValidateAuthEvent(evt, testChallenge, testRelayURL)
	require.True(t, ok)
	require.Equal(t, pk, got)
}

func TestValidateAuthEventRejectsTamperedID(t *testing.T) {
	evt, _ := signedAuthEvent(t)
	other, _ := signedAuthEvent(t)

	// the signature check hashes the body and never reads the id field, so a
	// false id on a correctly signed body passes it
	evt.ID = other.ID

	ok, err := evt.CheckSignature()
	require.NoError(t, err)
	require.True(t, ok, "the signature alone does not detect the false id")
	require.False(t, evt.CheckID())

	_, ok = ValidateAuthEvent(evt, testChallenge, testRelayURL)
	require.False(t, ok)
}

func TestValidateAuthEventRejectsWrongChallenge(t *testing.T) {
	evt, _ := signedAuthEvent(t)

	_, ok := ValidateAuthEvent(evt, "another-challenge", testRelayURL)
	require.False(t, ok)
}
