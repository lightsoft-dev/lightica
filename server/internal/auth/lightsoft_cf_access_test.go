package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testCFTeam = "lightsoft-test.cloudflareaccess.com"
	testCFAud  = "aud-tag-1234"
)

type cfAccessTestKeys struct {
	mu      sync.Mutex
	keys    map[string]*rsa.PrivateKey
	fetches atomic.Int32
	server  *httptest.Server
}

func newCFAccessTestKeys(t *testing.T, kids ...string) *cfAccessTestKeys {
	t.Helper()
	k := &cfAccessTestKeys{keys: map[string]*rsa.PrivateKey{}}
	for _, kid := range kids {
		k.add(t, kid)
	}
	k.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k.fetches.Add(1)
		type jwk struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		}
		var body struct {
			Keys []jwk `json:"keys"`
		}
		k.mu.Lock()
		defer k.mu.Unlock()
		for kid, key := range k.keys {
			body.Keys = append(body.Keys, jwk{
				Kid: kid, Kty: "RSA", Alg: "RS256",
				N: base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			})
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(k.server.Close)
	return k
}

func (k *cfAccessTestKeys) add(t *testing.T, kid string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	k.mu.Lock()
	k.keys[kid] = key
	k.mu.Unlock()
}

func (k *cfAccessTestKeys) verifier() *CloudflareAccessVerifier {
	return NewCloudflareAccessVerifier(CloudflareAccessConfig{
		TeamDomain: testCFTeam,
		Audiences:  []string{testCFAud},
		CertsURL:   k.server.URL,
	})
}

func cfAccessTestClaims() jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"aud":   []string{testCFAud},
		"email": "Member@Lightsoft.dev",
		"sub":   "cf-user-1",
		"iss":   "https://" + testCFTeam,
		"type":  "app",
		"iat":   now.Unix(),
		"nbf":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
}

func signCFAccess(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCloudflareAccessVerifierAcceptsValidToken(t *testing.T) {
	keys := newCFAccessTestKeys(t, "k1")
	id, err := keys.verifier().Verify(context.Background(), signCFAccess(t, keys.keys["k1"], "k1", cfAccessTestClaims()))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.Email != "member@lightsoft.dev" || id.Subject != "cf-user-1" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestCloudflareAccessVerifierRejects(t *testing.T) {
	keys := newCFAccessTestKeys(t, "k1")
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	with := func(edit func(jwt.MapClaims)) jwt.MapClaims {
		c := cfAccessTestClaims()
		edit(c)
		return c
	}
	cases := map[string]string{
		"wrong audience": signCFAccess(t, keys.keys["k1"], "k1", with(func(c jwt.MapClaims) { c["aud"] = []string{"another-app"} })),
		"wrong issuer":   signCFAccess(t, keys.keys["k1"], "k1", with(func(c jwt.MapClaims) { c["iss"] = "https://evil.cloudflareaccess.com" })),
		"expired":        signCFAccess(t, keys.keys["k1"], "k1", with(func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() })),
		"no expiry":      signCFAccess(t, keys.keys["k1"], "k1", with(func(c jwt.MapClaims) { delete(c, "exp") })),
		"foreign key":    signCFAccess(t, other, "k1", cfAccessTestClaims()),
		"unknown kid":    signCFAccess(t, other, "k9", cfAccessTestClaims()),
		"no email":       signCFAccess(t, keys.keys["k1"], "k1", with(func(c jwt.MapClaims) { delete(c, "email"); c["common_name"] = "svc.access" })),
		"garbage":        "not-a-jwt",
	}
	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, cfAccessTestClaims())
	hs.Header["kid"] = "k1"
	hsToken, _ := hs.SignedString([]byte("secret"))
	cases["hs256"] = hsToken

	v := keys.verifier()
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Verify(context.Background(), token); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	if _, err := v.Verify(context.Background(), cases["no email"]); !errors.Is(err, ErrCloudflareAccessNoEmail) {
		t.Fatalf("no email: err = %v", err)
	}
}

func TestCloudflareAccessVerifierRefetchesRotatedKeys(t *testing.T) {
	keys := newCFAccessTestKeys(t, "k1")
	v := keys.verifier()
	if _, err := v.Verify(context.Background(), signCFAccess(t, keys.keys["k1"], "k1", cfAccessTestClaims())); err != nil {
		t.Fatal(err)
	}
	keys.add(t, "k2")
	// Within the backoff window an unknown kid does not refetch.
	if _, err := v.Verify(context.Background(), signCFAccess(t, keys.keys["k2"], "k2", cfAccessTestClaims())); err == nil {
		t.Fatal("expected unknown kid inside the backoff window to fail")
	}
	if got := keys.fetches.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1", got)
	}
	v.mu.Lock()
	v.fetchedAt = time.Now().Add(-cfAccessKeysRefetchBackoff)
	v.mu.Unlock()
	if _, err := v.Verify(context.Background(), signCFAccess(t, keys.keys["k2"], "k2", cfAccessTestClaims())); err != nil {
		t.Fatalf("rotated key after backoff: %v", err)
	}
	if got := keys.fetches.Load(); got != 2 {
		t.Fatalf("fetches = %d, want 2", got)
	}
}

func TestNewCloudflareAccessVerifierConfig(t *testing.T) {
	if NewCloudflareAccessVerifier(CloudflareAccessConfig{TeamDomain: testCFTeam}) != nil {
		t.Fatal("missing audience must disable the verifier")
	}
	if NewCloudflareAccessVerifier(CloudflareAccessConfig{Audiences: []string{" ", ""}, TeamDomain: testCFTeam}) != nil {
		t.Fatal("blank audiences must disable the verifier")
	}
	if NewCloudflareAccessVerifier(CloudflareAccessConfig{Audiences: []string{testCFAud}}) != nil {
		t.Fatal("missing team domain must disable the verifier")
	}
	v := NewCloudflareAccessVerifier(CloudflareAccessConfig{TeamDomain: " https://" + testCFTeam + "/ ", Audiences: []string{testCFAud}})
	if v == nil || v.issuer != "https://"+testCFTeam || v.certsURL != "https://"+testCFTeam+"/cdn-cgi/access/certs" {
		t.Fatalf("verifier = %+v", v)
	}
}
