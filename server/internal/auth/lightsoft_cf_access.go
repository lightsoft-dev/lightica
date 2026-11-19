package auth

// Lightsoft fork (LIGH-253): verify the identity Cloudflare Access puts in
// front of the team client. Access signs a JWT for every request it lets
// through and forwards it to the origin as the Cf-Access-Jwt-Assertion
// header. The signature, issuer and audience pin the token to our team and
// our Access application, so a forged or foreign header verifies as nothing
// even when the request reaches the backend another way (tailnet, localhost).

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// CloudflareAccessHeader is the request header Cloudflare Access sets on
// every request it forwards to the origin.
const CloudflareAccessHeader = "Cf-Access-Jwt-Assertion"

const (
	cfAccessKeysMaxAge         = time.Hour
	cfAccessKeysRefetchBackoff = 30 * time.Second
	cfAccessClockLeeway        = 30 * time.Second
)

// ErrCloudflareAccessNoEmail rejects Access tokens that carry no user email,
// such as service-token tokens: they identify a machine, not a Multica user.
var ErrCloudflareAccessNoEmail = errors.New("cloudflare access token has no email")

// CloudflareAccessConfig pins tokens to one Access team and application.
type CloudflareAccessConfig struct {
	// TeamDomain is the Zero Trust team domain, e.g. "lightsoft.cloudflareaccess.com".
	TeamDomain string
	// Audiences are the Access application AUD tags; a token must carry one.
	Audiences []string
	// CertsURL overrides the team's signing-key endpoint (tests).
	CertsURL   string
	HTTPClient *http.Client
}

// CloudflareAccessIdentity is the verified user behind an Access token.
type CloudflareAccessIdentity struct {
	Email   string
	Subject string
}

type CloudflareAccessVerifier struct {
	issuer    string
	audiences []string
	certsURL  string
	client    *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// NewCloudflareAccessVerifier returns nil when the team domain or audience
// is missing, which leaves Access login switched off.
func NewCloudflareAccessVerifier(cfg CloudflareAccessConfig) *CloudflareAccessVerifier {
	team := strings.TrimRight(strings.TrimSpace(cfg.TeamDomain), "/")
	team = strings.TrimPrefix(strings.TrimPrefix(team, "https://"), "http://")
	var audiences []string
	for _, aud := range cfg.Audiences {
		if aud = strings.TrimSpace(aud); aud != "" {
			audiences = append(audiences, aud)
		}
	}
	if team == "" || len(audiences) == 0 {
		return nil
	}
	issuer := "https://" + team
	certsURL := cfg.CertsURL
	if certsURL == "" {
		certsURL = issuer + "/cdn-cgi/access/certs"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &CloudflareAccessVerifier{
		issuer:    issuer,
		audiences: audiences,
		certsURL:  certsURL,
		client:    client,
	}
}

// NewCloudflareAccessVerifierFromEnv reads LIGHTSOFT_CF_ACCESS_TEAM_DOMAIN and
// LIGHTSOFT_CF_ACCESS_AUD (comma-separated AUD tags).
func NewCloudflareAccessVerifierFromEnv() *CloudflareAccessVerifier {
	return NewCloudflareAccessVerifier(CloudflareAccessConfig{
		TeamDomain: os.Getenv("LIGHTSOFT_CF_ACCESS_TEAM_DOMAIN"),
		Audiences:  strings.Split(os.Getenv("LIGHTSOFT_CF_ACCESS_AUD"), ","),
	})
}

type cfAccessClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// Verify checks an Access token and returns the user it names.
func (v *CloudflareAccessVerifier) Verify(ctx context.Context, token string) (CloudflareAccessIdentity, error) {
	claims := &cfAccessClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(cfAccessClockLeeway),
	)
	_, err := parser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return v.key(ctx, kid)
	})
	if err != nil {
		return CloudflareAccessIdentity{}, err
	}
	if !v.audienceMatches(claims.Audience) {
		return CloudflareAccessIdentity{}, jwt.ErrTokenInvalidAudience
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		return CloudflareAccessIdentity{}, ErrCloudflareAccessNoEmail
	}
	return CloudflareAccessIdentity{Email: email, Subject: claims.Subject}, nil
}

func (v *CloudflareAccessVerifier) audienceMatches(got jwt.ClaimStrings) bool {
	for _, want := range v.audiences {
		for _, aud := range got {
			if aud == want {
				return true
			}
		}
	}
	return false
}

// key returns the signing key for kid. Keys are cached for an hour; an
// unknown kid (Access rotated its keys) triggers a refetch, at most once per
// backoff window so a stream of bogus kids cannot hammer the endpoint.
func (v *CloudflareAccessVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if kid == "" {
		return nil, errors.New("cloudflare access token has no kid")
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	if k, ok := v.keys[kid]; ok && time.Since(v.fetchedAt) < cfAccessKeysMaxAge {
		return k, nil
	}
	if v.keys == nil || time.Since(v.fetchedAt) >= cfAccessKeysRefetchBackoff {
		keys, err := v.fetchKeys(ctx)
		if err != nil {
			// Keep serving the cached keys through a transient outage.
			if k, ok := v.keys[kid]; ok {
				return k, nil
			}
			return nil, err
		}
		v.keys, v.fetchedAt = keys, time.Now()
	}
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("cloudflare access signing key %q not found", kid)
}

func (v *CloudflareAccessVerifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch cloudflare access keys: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch cloudflare access keys: status %d", resp.StatusCode)
	}
	var body struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode cloudflare access keys: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(body.Keys))
	for _, k := range body.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(k.N)
		e, errE := base64.RawURLEncoding.DecodeString(k.E)
		if errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(n),
			E: int(new(big.Int).SetBytes(e).Int64()),
		}
	}
	if len(keys) == 0 {
		return nil, errors.New("cloudflare access keys: no usable RSA keys")
	}
	return keys, nil
}
