package keyer

import (
	"context"
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"github.com/nbd-wtf/go-nostr/nip49"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptedKeySignerEncryptDecrypt(t *testing.T) {
	ctx := context.Background()
	const password = "correct horse battery staple"

	signerSecret := nostr.GeneratePrivateKey()
	ncryptsec, err := nip49.Encrypt(signerSecret, password, 1, nip49.KnownToHaveBeenHandledInsecurely)
	require.NoError(t, err)
	signer := &EncryptedKeySigner{ncryptsec, "", func(context.Context) string { return password }}

	signerPubkey, err := nostr.GetPublicKey(signerSecret)
	require.NoError(t, err)
	got, err := signer.GetPublicKey(ctx)
	require.NoError(t, err)
	assert.Equal(t, signerPubkey, got)

	peerSecret := nostr.GeneratePrivateKey()
	peerPubkey, err := nostr.GetPublicKey(peerSecret)
	require.NoError(t, err)
	peerKey, err := nip44.GenerateConversationKey(signerPubkey, peerSecret)
	require.NoError(t, err)

	const message = "a message for the signer"
	ciphertext, err := nip44.Encrypt(message, peerKey)
	require.NoError(t, err)
	plaintext, err := signer.Decrypt(ctx, ciphertext, peerPubkey)
	require.NoError(t, err)
	assert.Equal(t, message, plaintext)

	reply, err := signer.Encrypt(ctx, message, peerPubkey)
	require.NoError(t, err)
	plaintext, err = nip44.Decrypt(reply, peerKey)
	require.NoError(t, err)
	assert.Equal(t, message, plaintext)
}

func TestEncryptedKeySignerWrongPassword(t *testing.T) {
	ncryptsec, err := nip49.Encrypt(nostr.GeneratePrivateKey(), "right", 1, nip49.KnownToHaveBeenHandledInsecurely)
	require.NoError(t, err)
	signer := &EncryptedKeySigner{ncryptsec, "", func(context.Context) string { return "wrong" }}

	peerPubkey, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	require.NoError(t, err)
	_, err = signer.Decrypt(context.Background(), "irrelevant", peerPubkey)
	assert.ErrorContains(t, err, "invalid password")
}
