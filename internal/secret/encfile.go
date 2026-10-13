package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
)

// pbkdf2Iter is the PBKDF2-SHA256 iteration count for deriving the file key from a
// passphrase. 600k is in line with current OWASP guidance for PBKDF2-HMAC-SHA256.
const pbkdf2Iter = 600000

// encEnvelope is the on-disk format of an encrypted secrets file.
type encEnvelope struct {
	KDF        string `json:"kdf"`  // "pbkdf2-sha256"
	Iter       int    `json:"iter"` // PBKDF2 iterations
	Salt       string `json:"salt"` // base64
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func deriveKey(passphrase string, salt []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, iter, 32)
}

// SealEncryptedFile writes secrets to path, encrypted with AES-256-GCM under a key
// derived from passphrase (PBKDF2). The file is written mode 0600.
func SealEncryptedFile(path, passphrase string, secrets map[string]string) error {
	plaintext, err := json.Marshal(secrets)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	key, err := deriveKey(passphrase, salt, pbkdf2Iter)
	if err != nil {
		return err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	env := encEnvelope{
		KDF:        "pbkdf2-sha256",
		Iter:       pbkdf2Iter,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ct),
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// OpenEncryptedFile reads and decrypts an encrypted secrets file.
func OpenEncryptedFile(path, passphrase string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read secrets file: %w", err)
	}
	var env encEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse secrets file: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(env.Salt)
	if err != nil {
		return nil, fmt.Errorf("bad salt: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("bad nonce: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("bad ciphertext: %w", err)
	}
	iter := env.Iter
	if iter <= 0 {
		iter = pbkdf2Iter
	}
	key, err := deriveKey(passphrase, salt, iter)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed (wrong passphrase or corrupt file)")
	}
	var m map[string]string
	if err := json.Unmarshal(pt, &m); err != nil {
		return nil, fmt.Errorf("parse decrypted secrets: %w", err)
	}
	return m, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
