package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	keySize = 32
	// keyID prefixes every ciphertext so a later master-key rotation can tell
	// old and new ciphertexts apart.
	keyID byte = 1
)

var errCiphertext = errors.New("ciphertext is malformed or was not sealed for this secret")

// sealer encrypts secret values with AES-256-GCM. The additional data binds a
// ciphertext to its workspace, secret and version, so a row copied onto
// another secret (or rolled back to an old version) fails to open.
type sealer struct {
	aead cipher.AEAD
}

// loadKey reads the base64 master key. It refuses a key file that other
// users can read: the key is the only thing standing between a copy of the
// database and every secret in it.
func loadKey(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("master key: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("master key %s must not be readable by group or others (mode %v); run chmod 600", path, info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("master key: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("master key %s is not base64", path)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("master key %s must decode to %d bytes, got %d", path, keySize, len(key))
	}
	return key, nil
}

func newSealer(key []byte) (*sealer, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("master key must be %d bytes", keySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &sealer{aead: aead}, nil
}

func secretAAD(workspaceID, secretID string, version int) []byte {
	return []byte("lightsoft-secrets:v1:" + workspaceID + ":" + secretID + ":" + strconv.Itoa(version))
}

func (s *sealer) seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+s.aead.Overhead())
	out = append(out, keyID)
	out = append(out, nonce...)
	return s.aead.Seal(out, nonce, plaintext, aad), nil
}

func (s *sealer) open(blob, aad []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(blob) < 1+ns+s.aead.Overhead() || blob[0] != keyID {
		return nil, errCiphertext
	}
	plain, err := s.aead.Open(nil, blob[1:1+ns], blob[1+ns:], aad)
	if err != nil {
		return nil, errCiphertext
	}
	return plain, nil
}

// agentTokenPrefix marks tokens this service mints for enrolled agents.
const agentTokenPrefix = "lss_"

func newAgentToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return agentTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
