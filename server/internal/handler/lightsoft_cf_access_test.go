package handler

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/testutil"
)

const (
	cfAccessTestTeam = "lightsoft-test.cloudflareaccess.com"
	cfAccessTestAud  = "handler-aud"
)

func setLightsoftCFAccessVerifierForTest(t *testing.T, v *auth.CloudflareAccessVerifier) {
	t.Helper()
	cfAccessVerifierOnce.Do(func() {})
	prev := cfAccessVerifier
	cfAccessVerifier = v
	t.Cleanup(func() { cfAccessVerifier = prev })
}

// useCFAccessTestKey serves one signing key and returns a token minter.
func useCFAccessTestKey(t *testing.T) func(email string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "k1", "kty": "RSA", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	setLightsoftCFAccessVerifierForTest(t, auth.NewCloudflareAccessVerifier(auth.CloudflareAccessConfig{
		TeamDomain: cfAccessTestTeam,
		Audiences:  []string{cfAccessTestAud},
		CertsURL:   srv.URL,
	}))
	return func(email string) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"aud":   []string{cfAccessTestAud},
			"email": email,
			"iss":   "https://" + cfAccessTestTeam,
			"exp":   time.Now().Add(time.Hour).Unix(),
			"iat":   time.Now().Unix(),
		})
		tok.Header["kid"] = "k1"
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
}

func cfAccessLoginRequest(assertion string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/lightsoft/auth/cloudflare-access", nil)
	if assertion != "" {
		req.Header.Set(auth.CloudflareAccessHeader, assertion)
	}
	return req
}

func hasAuthCookie(resp *testutil.Response) bool {
	for _, c := range resp.Result().Cookies() {
		if c.Name == auth.AuthCookieName && c.Value != "" {
			return true
		}
	}
	return false
}

func TestLightsoftCloudflareAccessLoginNotConfigured(t *testing.T) {
	setLightsoftCFAccessVerifierForTest(t, nil)
	testutil.Call(t, testHandler.LightsoftCloudflareAccessLogin, cfAccessLoginRequest("x")).Want(http.StatusNotFound)
}

func TestLightsoftCloudflareAccessLoginRejectsMissingOrForgedAssertion(t *testing.T) {
	useCFAccessTestKey(t)
	for name, assertion := range map[string]string{
		"missing": "",
		"forged":  "eyJhbGciOiJub25lIn0.eyJlbWFpbCI6ImhhbmRsZXItdGVzdEBtdWx0aWNhLmFpIn0.",
	} {
		t.Run(name, func(t *testing.T) {
			var body map[string]string
			resp := testutil.Call(t, testHandler.LightsoftCloudflareAccessLogin, cfAccessLoginRequest(assertion)).Want(http.StatusForbidden).JSON(&body)
			want := cfAccessCodeInvalidAssertion
			if assertion == "" {
				want = cfAccessCodeNoAssertion
			}
			if body["code"] != want || hasAuthCookie(resp) {
				t.Fatalf("code = %q, auth cookie = %t", body["code"], hasAuthCookie(resp))
			}
		})
	}
}

func TestLightsoftCloudflareAccessLoginExistingUser(t *testing.T) {
	mint := useCFAccessTestKey(t)
	h := restrictSignupHandler(t)
	var out LoginResponse
	resp := testutil.Call(t, h.LightsoftCloudflareAccessLogin, cfAccessLoginRequest(mint(handlerTestEmail))).Want(http.StatusOK).JSON(&out)
	if out.User.ID != testUserID || out.Token == "" || !hasAuthCookie(resp) {
		t.Fatalf("user = %q, token set = %t, auth cookie = %t", out.User.ID, out.Token != "", hasAuthCookie(resp))
	}
}

func TestLightsoftCloudflareAccessLoginNewEmailNeedsInvitation(t *testing.T) {
	mint := useCFAccessTestKey(t)
	for _, invited := range []bool{false, true} {
		email := "cf-access-uninvited@example.com"
		if invited {
			email = "cf-access-invited@example.com"
			signupInvitation(t, email)
		}
		dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, email)
		h := restrictSignupHandler(t)
		if !invited {
			var body map[string]string
			resp := testutil.Call(t, h.LightsoftCloudflareAccessLogin, cfAccessLoginRequest(mint(email))).Want(http.StatusForbidden).JSON(&body)
			if body["code"] != cfAccessCodeNotInvited || body["email"] != email || hasAuthCookie(resp) {
				t.Fatalf("uninvited: body = %v, auth cookie = %t", body, hasAuthCookie(resp))
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM "user" WHERE email = $1`, email); n != 0 {
				t.Fatal("uninvited email must not get an account")
			}
			continue
		}
		resp := testutil.Call(t, h.LightsoftCloudflareAccessLogin, cfAccessLoginRequest(mint(email))).Want(http.StatusOK)
		if !hasAuthCookie(resp) {
			t.Fatal("invited email must get a session")
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM "user" WHERE email = $1`, email); n != 1 {
			t.Fatalf("invited email accounts = %d, want 1", n)
		}
	}
}
