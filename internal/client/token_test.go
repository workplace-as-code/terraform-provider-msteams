package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

func TestStaticAssertion(t *testing.T) {
	got, err := StaticAssertion("jwt-static")(context.Background())
	if err != nil || got != "jwt-static" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// HCL file() and pasted CI secrets keep the trailing newline: Entra ID would
// reject the token with an error that does not point at the cause.
func TestStaticAssertionTrims(t *testing.T) {
	got, err := StaticAssertion("  jwt-static\n")(context.Background())
	if err != nil || got != "jwt-static" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestFileAssertionTrimsAndRereads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.jwt")
	assertion := FileAssertion(path)

	if err := os.WriteFile(path, []byte("jwt-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := assertion(context.Background()); err != nil || got != "jwt-1" {
		t.Fatalf("first read: got %q, %v", got, err)
	}

	// Kubernetes rewrites the projected token file: every exchange reads it again.
	if err := os.WriteFile(path, []byte("jwt-2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := assertion(context.Background()); err != nil || got != "jwt-2" {
		t.Fatalf("second read: got %q, %v", got, err)
	}

	missing := filepath.Join(t.TempDir(), "missing.jwt")
	if _, err := FileAssertion(missing)(context.Background()); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("missing file: err = %v, want the path in the message", err)
	}
}

func TestGitHubAssertionRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("audience"); got != GitHubAudience {
			t.Errorf("audience = %q, want %q", got, GitHubAudience)
		}
		if got := r.URL.Query().Get("api-version"); got != "2.0" {
			t.Errorf("the query string of the request URL was lost: api-version = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer req-token" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"value":"gh-jwt"}`))
	}))
	defer server.Close()

	got, err := GitHubAssertion(server.URL+"/token?api-version=2.0", "req-token")(context.Background())
	if err != nil || got != "gh-jwt" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestGitHubAssertionErrorNamesPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"bad token req-token"}`))
	}))
	defer server.Close()

	_, err := GitHubAssertion(server.URL+"/token?api-version=2.0", "req-token")(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "id-token: write") || !strings.Contains(msg, "403") {
		t.Errorf("error should name the permission and the status: %s", msg)
	}
	if strings.Contains(msg, "req-token") {
		t.Errorf("error leaks the request token: %s", msg)
	}
}

func TestNewOIDCTokenSourceAcceptsDomain(t *testing.T) {
	if _, err := NewOIDCTokenSource("contoso.onmicrosoft.com", "00000000-0000-0000-0000-000000000001", StaticAssertion("x"), ""); err != nil {
		t.Fatal(err)
	}
}

// offlineTransport fails every request at once, so a test of a token source
// never waits for Entra ID.
type offlineTransport struct{}

func (offlineTransport) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}

func TestOIDCTokenSourceErrorCarriesHint(t *testing.T) {
	previous := oidcCredentialOptions
	oidcCredentialOptions = &azidentity.ClientAssertionCredentialOptions{ClientOptions: azcore.ClientOptions{
		Transport: offlineTransport{},
		Retry:     policy.RetryOptions{MaxRetries: -1},
	}}
	t.Cleanup(func() { oidcCredentialOptions = previous })

	failing := func(context.Context) (string, error) { return "", errors.New("no assertion") }
	source, err := NewOIDCTokenSource("contoso.onmicrosoft.com", "00000000-0000-0000-0000-000000000001", failing, "use the file")
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Token(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if msg := err.Error(); !strings.Contains(msg, "with OIDC failed") || !strings.Contains(msg, "use the file") {
		t.Errorf("error = %s", msg)
	}
}

// recordingTransport keeps the URL of every request, then fails it.
type recordingTransport struct{ urls []string }

func (r *recordingTransport) Do(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL.String())
	return nil, errors.New("offline")
}

// A stray AZURE_AUTHORITY_HOST in a CI runner must not send the token request
// to another cloud: the provider only supports the public cloud with OIDC.
func TestOIDCTokenSourceIgnoresAuthorityHostVariable(t *testing.T) {
	t.Setenv("AZURE_AUTHORITY_HOST", "https://login.example.invalid/")
	transport := &recordingTransport{}
	previous := oidcCredentialOptions
	oidcCredentialOptions = &azidentity.ClientAssertionCredentialOptions{ClientOptions: azcore.ClientOptions{
		Transport: transport,
		Retry:     policy.RetryOptions{MaxRetries: -1},
	}}
	t.Cleanup(func() { oidcCredentialOptions = previous })

	source, err := NewOIDCTokenSource("contoso.onmicrosoft.com", "00000000-0000-0000-0000-000000000001", StaticAssertion("x"), "")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.Token(context.Background())
	if len(transport.urls) == 0 {
		t.Fatal("no request was sent")
	}
	// MSAL first asks login.microsoftonline.com about an unknown authority,
	// with that authority in the query string: check the whole URL.
	for _, u := range transport.urls {
		if !strings.HasPrefix(u, "https://login.microsoftonline.com/") || strings.Contains(u, "example.invalid") {
			t.Errorf("request for another authority: %s", u)
		}
	}
}

// fakeCredential returns a token per scope, so a test can check what each
// API receives.
type fakeCredential struct {
	tokens map[string]azcore.AccessToken
	scopes []string
}

func (f *fakeCredential) GetToken(_ context.Context, options policy.TokenRequestOptions) (azcore.AccessToken, error) {
	scope := strings.Join(options.Scopes, " ")
	f.scopes = append(f.scopes, scope)
	token, ok := f.tokens[scope]
	if !ok {
		return azcore.AccessToken{}, errors.New("AADSTS65001: no consent for " + scope)
	}
	return token, nil
}

// Connect-MicrosoftTeams -AccessTokens needs a Graph token and a Teams token;
// the worker must be recycled before the first of them expires.
func TestCredentialTokenSourceGetsBothTokens(t *testing.T) {
	soon, later := time.Now().Add(30*time.Minute), time.Now().Add(time.Hour)
	credential := &fakeCredential{tokens: map[string]azcore.AccessToken{
		GraphScope: {Token: "graph", ExpiresOn: later},
		TeamsScope: {Token: "teams", ExpiresOn: soon},
	}}
	tokens, err := (&credentialTokenSource{credential: credential, method: "OIDC"}).Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tokens.Graph != "graph" || tokens.Teams != "teams" || !tokens.ExpiresOn.Equal(soon) {
		t.Errorf("got %+v, want both tokens and the earliest expiry", tokens)
	}
	if strings.Join(credential.scopes, ",") != GraphScope+","+TeamsScope {
		t.Errorf("scopes requested: %v", credential.scopes)
	}
}

func TestCredentialTokenSourceErrorNamesTheMethod(t *testing.T) {
	credential := &fakeCredential{tokens: map[string]azcore.AccessToken{GraphScope: {Token: "graph", ExpiresOn: time.Now().Add(time.Hour)}}}
	_, err := (&credentialTokenSource{credential: credential, method: "OIDC", hint: "check the app"}).Token(context.Background())
	if err == nil {
		t.Fatal("expected an error when the Teams token is refused")
	}
	if msg := err.Error(); !strings.Contains(msg, "with OIDC failed") || !strings.Contains(msg, TeamsScope) || !strings.Contains(msg, "check the app") {
		t.Errorf("error = %s", msg)
	}
}
