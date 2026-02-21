package provider

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	OpenAICodexOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	OpenAICodexOAuthIssuer   = "https://auth.openai.com"
	OpenAICodexRedirectURI   = "http://localhost:1455/auth/callback"
)

type OpenAICodexOAuthTokens struct {
	APIKey               string
	IDToken              string
	AccessToken          string
	RefreshToken         string
	UsedAccessTokenAsKey bool
}

func GenerateOpenAICodexPKCE() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	v := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(v))
	return v, base64.RawURLEncoding.EncodeToString(h[:]), nil
}

func GenerateOpenAICodexState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func BuildOpenAICodexAuthorizeURL(redirectURI, codeChallenge, state string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", OpenAICodexOAuthClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "openid profile email offline_access")
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("state", state)
	q.Set("originator", "codex_cli_rs")
	return OpenAICodexOAuthIssuer + "/oauth/authorize?" + q.Encode()
}

func ParseOpenAICodexRedirectURL(input, expectedState string) (string, error) {
	raw := strings.TrimSpace(input)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("paste the full redirect URL")
	}
	q := u.Query()
	if q.Get("error") != "" {
		msg := q.Get("error")
		if d := q.Get("error_description"); d != "" {
			msg += ": " + d
		}
		return "", fmt.Errorf("oauth error: %s", msg)
	}
	code := strings.TrimSpace(q.Get("code"))
	state := strings.TrimSpace(q.Get("state"))
	if code == "" || state == "" {
		return "", fmt.Errorf("paste the full redirect URL (must include code + state)")
	}
	if expectedState != "" && state != expectedState {
		return "", fmt.Errorf("oauth state mismatch")
	}
	return code, nil
}

func ExchangeOpenAICodexOAuthCode(
	ctx context.Context,
	code,
	codeVerifier,
	redirectURI string,
) (*OpenAICodexOAuthTokens, error) {
	return exchangeOpenAICodexOAuth(
		ctx,
		http.DefaultClient,
		OpenAICodexOAuthIssuer,
		OpenAICodexOAuthClientID,
		code,
		codeVerifier,
		redirectURI,
	)
}

func exchangeOpenAICodexOAuth(
	ctx context.Context,
	client *http.Client,
	issuer, clientID, code, codeVerifier, redirectURI string,
) (*OpenAICodexOAuthTokens, error) {
	id, access, refresh, err := exchangeOpenAICodexAuthCode(ctx, client, issuer, clientID, code, codeVerifier, redirectURI)
	if err != nil {
		return nil, err
	}
	usedAccessFallback := false
	apiKey, err := exchangeOpenAICodexIDToken(ctx, client, issuer, clientID, id)
	if err != nil {
		if !shouldUseAccessTokenFallback(err) {
			return nil, err
		}
		apiKey = access
		usedAccessFallback = true
	}
	return &OpenAICodexOAuthTokens{
		APIKey:               apiKey,
		IDToken:              id,
		AccessToken:          access,
		RefreshToken:         refresh,
		UsedAccessTokenAsKey: usedAccessFallback,
	}, nil
}

func exchangeOpenAICodexAuthCode(
	ctx context.Context,
	client *http.Client,
	issuer, clientID, code, codeVerifier, redirectURI string,
) (string, string, string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", codeVerifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, issuer+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", readOAuthStatus(resp, "oauth code exchange failed")
	}
	var token struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", "", "", err
	}
	return token.IDToken, token.AccessToken, token.RefreshToken, nil
}

func exchangeOpenAICodexIDToken(
	ctx context.Context,
	client *http.Client,
	issuer, clientID, idToken string,
) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	form.Set("client_id", clientID)
	form.Set("requested_token", "openai-api-key")
	form.Set("subject_token", idToken)
	form.Set("subject_token_type", "urn:ietf:params:oauth:token-type:id_token")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, issuer+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", readOAuthStatus(resp, "oauth api key exchange failed")
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("oauth api key exchange returned empty access_token")
	}
	return out.AccessToken, nil
}

func readOAuthStatus(resp *http.Response, prefix string) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		return fmt.Errorf("%s: status %d", prefix, resp.StatusCode)
	}
	return fmt.Errorf("%s: status %d: %s", prefix, resp.StatusCode, msg)
}

func shouldUseAccessTokenFallback(err error) bool {
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "invalid_subject_token") || strings.Contains(m, "missing organization_id")
}
