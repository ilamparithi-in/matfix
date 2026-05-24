package media

import "maunium.net/go/mautrix/crypto/attachment"

// EncryptAttachment encrypts plaintext using a freshly generated AES-256-CTR key.
// It returns the ciphertext and the populated EncryptedFile key material needed
// to populate content.File in an m.room.message event.
//
// The returned ciphertext is a copy; the plaintext is not modified.
func EncryptAttachment(plaintext []byte) ([]byte, attachment.EncryptedFile) {
	ef := attachment.NewEncryptedFile()
	ciphertext := make([]byte, len(plaintext))
	copy(ciphertext, plaintext)
	ef.EncryptInPlace(ciphertext)
	return ciphertext, *ef
}
