package nip49

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil/bech32"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecryptKeyFromNIPText(t *testing.T) {
	ncrypt := "ncryptsec1qgg9947rlpvqu76pj5ecreduf9jxhselq2nae2kghhvd5g7dgjtcxfqtd67p9m0w57lspw8gsq6yphnm8623nsl8xn9j4jdzz84zm3frztj3z7s35vpzmqf6ksu8r89qk5z2zxfmu5gv8th8wclt0h4p"
	secretKey, err := Decrypt(ncrypt, "nostr")
	assert.NoError(t, err)
	assert.Equal(t, "3501454135014541350145413501453fefb02227e449e57cf4d3a3ce05378683", secretKey)
}

func TestEncryptAndDecrypt(t *testing.T) {
	for i, f := range []struct {
		password  string
		secretkey string
		logn      uint8
		ksb       KeySecurityByte
	}{
		{".ksjabdk.aselqwe", "14c226dbdd865d5e1645e72c7470fd0a17feb42cc87b750bab6538171b3a3f8a", 1, 0x00},
		{"skjdaklrnçurbç l", "f7f2f77f98890885462764afb15b68eb5f69979c8046ecb08cad7c4ae6b221ab", 2, 0x01},
		{"777z7z7z7z7z7z7z", "11b25a101667dd9208db93c0827c6bdad66729a5b521156a7e9d3b22b3ae8944", 3, 0x02},
		{".ksjabdk.aselqwe", "14c226dbdd865d5e1645e72c7470fd0a17feb42cc87b750bab6538171b3a3f8a", 7, 0x00},
		{"skjdaklrnçurbç l", "f7f2f77f98890885462764afb15b68eb5f69979c8046ecb08cad7c4ae6b221ab", 8, 0x01},
		{"777z7z7z7z7z7z7z", "11b25a101667dd9208db93c0827c6bdad66729a5b521156a7e9d3b22b3ae8944", 9, 0x02},
		{"", "f7f2f77f98890885462764afb15b68eb5f69979c8046ecb08cad7c4ae6b221ab", 4, 0x00},
		{"", "11b25a101667dd9208db93c0827c6bdad66729a5b521156a7e9d3b22b3ae8944", 5, 0x01},
		{"", "f7f2f77f98890885462764afb15b68eb5f69979c8046ecb08cad7c4ae6b221ab", 1, 0x00},
		{"ÅΩẛ̣", "11b25a101667dd9208db93c0827c6bdad66729a5b521156a7e9d3b22b3ae8944", 9, 0x01},
		{"ÅΩṩ", "11b25a101667dd9208db93c0827c6bdad66729a5b521156a7e9d3b22b3ae8944", 9, 0x01},
	} {
		bech32code, err := Encrypt(f.secretkey, f.password, f.logn, f.ksb)
		assert.NoError(t, err)
		assert.True(t, strings.HasPrefix(bech32code, "ncryptsec1"), "bech32 code is wrong %d: %s", i, bech32code)
		assert.Equal(t, 162, len(bech32code), "bech32 code is wrong %d: %s", i, bech32code)

		secretKey, err := Decrypt(bech32code, f.password)
		assert.NoError(t, err)
		assert.Equal(t, f.secretkey, secretKey)
	}
}

func TestNormalization(t *testing.T) {
	nonce := []byte{1, 2, 3, 4}
	n := 8
	key1, err1 := getKey(string([]byte{0xE2, 0x84, 0xAB, 0xE2, 0x84, 0xA6, 0xE1, 0xBA, 0x9B, 0xCC, 0xA3}), nonce, n)
	key2, err2 := getKey(string([]byte{0xC3, 0x85, 0xCE, 0xA9, 0xE1, 0xB9, 0xA9}), nonce, n)
	key3, err3 := getKey("ÅΩẛ̣", nonce, n)
	key4, err4 := getKey("ÅΩẛ̣", nonce, n)
	err := errors.Join(err1, err2, err3, err4)
	assert.NoError(t, err)
	assert.True(t, slices.Equal(key1, key2), "normalization failed")
	assert.True(t, slices.Equal(key2, key3), "normalization failed")
	assert.True(t, slices.Equal(key3, key4), "normalization failed")
}

// reencode decodes an ncryptsec, lets edit change its payload bytes, and encodes it again.
func reencode(t *testing.T, ncrypt string, edit func([]byte) []byte) string {
	t.Helper()
	_, bits5, err := bech32.DecodeNoLimit(ncrypt)
	require.NoError(t, err)
	data, err := bech32.ConvertBits(bits5, 5, 8, false)
	require.NoError(t, err)
	bits5, err = bech32.ConvertBits(edit(data), 8, 5, true)
	require.NoError(t, err)
	out, err := bech32.Encode("ncryptsec", bits5)
	require.NoError(t, err)
	return out
}

func TestDecryptRefusesWrongPayloadLength(t *testing.T) {
	ncrypt, err := Encrypt("14c226dbdd865d5e1645e72c7470fd0a17feb42cc87b750bab6538171b3a3f8a", "pw", 1, 0x00)
	require.NoError(t, err)

	for name, edit := range map[string]func([]byte) []byte{
		"one byte short": func(d []byte) []byte { return d[:len(d)-1] },
		"one byte long":  func(d []byte) []byte { return append(d, 0) },
		"version only":   func(d []byte) []byte { return d[:1] },
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decrypt(reencode(t, ncrypt, edit), "pw")
			assert.ErrorIs(t, err, ErrInvalidPayload)
		})
	}
}

func TestDecryptRefusesLogNAboveDefaultMaximum(t *testing.T) {
	ncrypt, err := Encrypt("14c226dbdd865d5e1645e72c7470fd0a17feb42cc87b750bab6538171b3a3f8a", "pw", 1, 0x00)
	require.NoError(t, err)

	// logn 40 asks scrypt for 1 TiB, so only a refusal before key derivation returns.
	for _, logn := range []uint8{DefaultMaxLogN + 1, 40, 255} {
		crafted := reencode(t, ncrypt, func(d []byte) []byte { d[1] = logn; return d })
		_, err := Decrypt(crafted, "pw")
		assert.ErrorIs(t, err, ErrLogNTooLarge, "logn %d", logn)
		_, err = DecryptToBytes(crafted, "pw")
		assert.ErrorIs(t, err, ErrLogNTooLarge, "logn %d", logn)
	}
}

func TestDecryptBoundedRefusesLogNAboveCallerMaximum(t *testing.T) {
	const secretKey = "f7f2f77f98890885462764afb15b68eb5f69979c8046ecb08cad7c4ae6b221ab"
	ncrypt, err := Encrypt(secretKey, "pw", 10, 0x00)
	require.NoError(t, err)

	_, err = DecryptBounded(ncrypt, "pw", 9)
	assert.ErrorIs(t, err, ErrLogNTooLarge)
	_, err = DecryptToBytesBounded(ncrypt, "pw", 9)
	assert.ErrorIs(t, err, ErrLogNTooLarge)

	got, err := DecryptBounded(ncrypt, "pw", 10)
	require.NoError(t, err)
	assert.Equal(t, secretKey, got)
}

func TestDecryptBoundedRoundTripAtLogN16(t *testing.T) {
	const secretKey = "11b25a101667dd9208db93c0827c6bdad66729a5b521156a7e9d3b22b3ae8944"
	ncrypt, err := Encrypt(secretKey, "correct horse battery staple", 16, KnownToHaveBeenHandledInsecurely)
	require.NoError(t, err)

	got, err := DecryptBounded(ncrypt, "correct horse battery staple", 16)
	require.NoError(t, err)
	assert.Equal(t, secretKey, got)

	_, err = DecryptBounded(ncrypt, "wrong password", 16)
	assert.Error(t, err)
}
