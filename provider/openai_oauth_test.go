package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBuildOpenAICodexAuthorizeURLIncludesRequiredParams(t *testing.T) {
	u := BuildOpenAICodexAuthorizeURL("http://localhost:1455/auth/callback", "challenge", "state123")
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if parsed.Host != "auth.openai.com" || parsed.Path != "/oauth/authorize" {
		t.Fatalf("unexpected auth url: %s", u)
	}
	q := parsed.Query()
	if q.Get("response_type") != "code" {
		t.Fatalf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("client_id") != OpenAICodexOAuthClientID {
		t.Fatalf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://localhost:1455/auth/callback" {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("scope") != "openid profile email offline_access" {
		t.Fatalf("scope = %q", q.Get("scope"))
	}
	if q.Get("code_challenge") != "challenge" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("pkce query mismatch: %v", q)
	}
	if q.Get("state") != "state123" {
		t.Fatalf("state = %q", q.Get("state"))
	}
	if q.Get("originator") != "codex_cli_rs" {
		t.Fatalf("originator = %q", q.Get("originator"))
	}
}

func TestParseOpenAICodexRedirectURLRejectsCodeOnly(t *testing.T) {
	_, err := ParseOpenAICodexRedirectURL("abc123", "s1")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "full redirect URL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseOpenAICodexRedirectURLValidatesState(t *testing.T) {
	_, err := ParseOpenAICodexRedirectURL("http://localhost:1455/auth/callback?code=abc&state=bad", "good")
	if err == nil {
		t.Fatal("expected state mismatch error")
	}
	if !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseOpenAICodexRedirectURLReturnsCode(t *testing.T) {
	code, err := ParseOpenAICodexRedirectURL("http://localhost:1455/auth/callback?code=abc123&state=s1", "s1")
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if code != "abc123" {
		t.Fatalf("code = %q", code)
	}
}

func TestExchangeOpenAICodexOAuthCode(t *testing.T) {
	reqBodies := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		reqBodies = append(reqBodies, string(b))
		switch len(reqBodies) {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id_token":"id-1","access_token":"acc-1","refresh_token":"ref-1"}`))
		case 2:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"sk-oauth-1"}`))
		default:
			t.Fatalf("unexpected request count: %d", len(reqBodies))
		}
	}))
	defer srv.Close()

	tokens, err := exchangeOpenAICodexOAuth(
		context.Background(),
		srv.Client(),
		srv.URL,
		OpenAICodexOAuthClientID,
		"code-1",
		"verifier-1",
		"http://localhost:1455/auth/callback",
	)
	if err != nil {
		t.Fatalf("exchange oauth: %v", err)
	}
	if tokens.APIKey != "sk-oauth-1" {
		t.Fatalf("api key = %q", tokens.APIKey)
	}
	if tokens.UsedAccessTokenAsKey {
		t.Fatal("expected token-exchange API key, got access-token fallback")
	}
	if tokens.IDToken != "id-1" || tokens.AccessToken != "acc-1" || tokens.RefreshToken != "ref-1" {
		t.Fatalf("unexpected tokens: %#v", tokens)
	}
	if len(reqBodies) != 2 {
		t.Fatalf("request count = %d", len(reqBodies))
	}
	if !strings.Contains(reqBodies[0], "grant_type=authorization_code") {
		t.Fatalf("first body = %q", reqBodies[0])
	}
	if !strings.Contains(reqBodies[0], "code=code-1") {
		t.Fatalf("first body missing code: %q", reqBodies[0])
	}
	if !strings.Contains(reqBodies[1], "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Atoken-exchange") {
		t.Fatalf("second body = %q", reqBodies[1])
	}
	if !strings.Contains(reqBodies[1], "requested_token=openai-api-key") {
		t.Fatalf("second body missing requested_token: %q", reqBodies[1])
	}
	if !strings.Contains(reqBodies[1], "subject_token=id-1") {
		t.Fatalf("second body missing subject_token: %q", reqBodies[1])
	}
}

func TestExchangeOpenAICodexOAuthCodeFallsBackWhenAPIKeyExchangeRejected(t *testing.T) {
	reqBodies := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		reqBodies = append(reqBodies, string(b))
		switch len(reqBodies) {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id_token":"id-1","access_token":"acc-1","refresh_token":"ref-1"}`))
		case 2:
			w.WriteHeader(http.StatusUnauthorized)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"error":{
					"message":"Invalid ID token: missing organization_id",
					"code":"invalid_subject_token"
				}
			}`))
		default:
			t.Fatalf("unexpected request count: %d", len(reqBodies))
		}
	}))
	defer srv.Close()

	tokens, err := exchangeOpenAICodexOAuth(
		context.Background(),
		srv.Client(),
		srv.URL,
		OpenAICodexOAuthClientID,
		"code-1",
		"verifier-1",
		"http://localhost:1455/auth/callback",
	)
	if err != nil {
		t.Fatalf("exchange oauth: %v", err)
	}
	if tokens.APIKey != "acc-1" {
		t.Fatalf("api key = %q", tokens.APIKey)
	}
	if !tokens.UsedAccessTokenAsKey {
		t.Fatal("expected access-token fallback")
	}
	if tokens.IDToken != "id-1" || tokens.AccessToken != "acc-1" || tokens.RefreshToken != "ref-1" {
		t.Fatalf("unexpected tokens: %#v", tokens)
	}
}
