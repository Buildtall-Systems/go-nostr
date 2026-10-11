package nip11

import (
	"encoding/json"
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/stretchr/testify/require"
)

func TestSelfRoundTrip(t *testing.T) {
	self, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	require.NoError(t, err)

	data, err := json.Marshal(RelayInformationDocument{Name: "groups", Self: self})
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	require.Equal(t, self, raw["self"])

	var info RelayInformationDocument
	require.NoError(t, json.Unmarshal(data, &info))
	require.Equal(t, self, info.Self)
}

func TestSelfOmittedWhenEmpty(t *testing.T) {
	data, err := json.Marshal(RelayInformationDocument{Name: "groups"})
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	require.NotContains(t, raw, "self")
}
