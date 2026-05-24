package media

import (
	"bytes"
	"testing"

	"maunium.net/go/mautrix/crypto/attachment"
)

func TestEncryptAttachment_RoundTrip(t *testing.T) {
	plaintext := []byte("hello, Matrix attachment!")
	ciphertext, ef := EncryptAttachment(plaintext)

	if bytes.Equal(ciphertext, plaintext) {
		t.Error("ciphertext must differ from plaintext")
	}
	if len(ciphertext) != len(plaintext) {
		t.Errorf("ciphertext length %d != plaintext length %d (AES-CTR must preserve length)",
			len(ciphertext), len(plaintext))
	}

	// Copy only the exported fields so Decrypt re-derives key material via
	// decodeKeys (decoded == nil), matching the real serialisation path.
	efCopy := attachment.EncryptedFile{
		Key:        ef.Key,
		InitVector: ef.InitVector,
		Hashes:     ef.Hashes,
		Version:    ef.Version,
	}
	recovered, err := efCopy.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(recovered, plaintext) {
		t.Errorf("round-trip failed: got %q, want %q", recovered, plaintext)
	}
}

func TestEncryptAttachment_PlaintextUnmodified(t *testing.T) {
	original := []byte("original data that must not change")
	snapshot := append([]byte(nil), original...)

	EncryptAttachment(original)

	if !bytes.Equal(original, snapshot) {
		t.Error("EncryptAttachment must not modify the plaintext argument")
	}
}

func TestEncryptAttachment_KeyMaterialPopulated(t *testing.T) {
	_, ef := EncryptAttachment([]byte("test data"))

	if ef.Key.Key == "" {
		t.Error("EncryptedFile.Key.Key must be non-empty")
	}
	if ef.InitVector == "" {
		t.Error("EncryptedFile.InitVector must be non-empty")
	}
	if ef.Hashes.SHA256 == "" {
		t.Error("EncryptedFile.Hashes.SHA256 must be non-empty")
	}
	if ef.Version == "" {
		t.Error("EncryptedFile.Version must be non-empty")
	}
}
