package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

const (
	// GraphScope and TeamsScope are the scopes of the two access tokens
	// Connect-MicrosoftTeams -AccessTokens needs, in the public cloud (also
	// used by GCC): Microsoft Graph, and the Skype and Teams Tenant Admin API.
	GraphScope = "https://graph.microsoft.com/.default"
	TeamsScope = "48ac35b8-9aa8-4d74-927d-1f4a14a0b239/.default"
	// GitHubAudience is the audience Entra ID expects in an OIDC token it
	// exchanges, in the public cloud.
	GitHubAudience = "api://AzureADTokenExchange"
)

// AccessTokens are the Microsoft Graph and Teams access tokens, and the time
// the first of them expires.
type AccessTokens struct {
	Graph     string
	Teams     string
	ExpiresOn time.Time
}

// TokenSource returns the access tokens. The client asks for them each time it
// starts a PowerShell process; the source caches the tokens and renews them
// before they expire.
//
// An interface is a set of methods: any type that has a Token method with this
// signature is a TokenSource. The tests use a fake one, OIDC uses
// credentialTokenSource and the managed identity managedIdentityTokenSource.
type TokenSource interface {
	Token(ctx context.Context) (AccessTokens, error)
}

// AssertionFunc returns the OIDC token that Entra ID exchanges for an access
// token. It is called each time a new access token is needed, so a source that
// renews its token (GitHub, a Kubernetes token file) is read again.
type AssertionFunc func(ctx context.Context) (string, error)

// StaticAssertion returns a token passed as a fixed value, without the blanks
// around it (HCL file() keeps the final newline). It cannot be renewed: once
// it expires, no new access token can be obtained.
func StaticAssertion(token string) AssertionFunc {
	token = strings.TrimSpace(token)
	// The returned function "captures" token: it keeps a reference to it after
	// StaticAssertion has returned (a closure).
	return func(context.Context) (string, error) { return token, nil }
}

// FileAssertion returns the token stored in a file, read again on every call.
func FileAssertion(path string) AssertionFunc {
	return func(context.Context) (string, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading the OIDC token file %s: %w", path, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
}

// githubHTTPClient bounds the request for the job's OIDC token.
var githubHTTPClient = &http.Client{Timeout: 30 * time.Second}

// GitHubAssertion requests the job's OIDC token from GitHub Actions. requestURL
// and requestToken are the ACTIONS_ID_TOKEN_REQUEST_URL and
// ACTIONS_ID_TOKEN_REQUEST_TOKEN variables GitHub sets in a job that has
// "permissions: id-token: write".
func GitHubAssertion(requestURL, requestToken string) AssertionFunc {
	return func(ctx context.Context) (string, error) {
		// The request URL already carries a query string (api-version).
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL+"&audience="+url.QueryEscape(GitHubAudience), nil)
		if err != nil {
			return "", fmt.Errorf("requesting the OIDC token from GitHub Actions: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+requestToken)
		req.Header.Set("Accept", "application/json; api-version=2.0")

		resp, err := githubHTTPClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("requesting the OIDC token from GitHub Actions: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		// The body is never quoted in an error: it could echo a credential.
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf(`requesting the OIDC token from GitHub Actions failed (HTTP %d): check that the job has "permissions: id-token: write"`, resp.StatusCode)
		}
		var body struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Value == "" {
			return "", errors.New("GitHub Actions returned no OIDC token")
		}
		return body.Value, nil
	}
}

// oidcCredentialOptions configures the azidentity credential. nil keeps the
// library's defaults; tests replace it to answer without the network.
var oidcCredentialOptions *azidentity.ClientAssertionCredentialOptions

// credentialTokenSource gets both access tokens from one azidentity
// credential.
type credentialTokenSource struct {
	credential azcore.TokenCredential
	method     string // "OIDC", for the error message
	hint       string
}

// NewOIDCTokenSource returns a TokenSource for the app registration clientID
// in tenantID (a GUID or a domain of the tenant). hint, when not empty, is
// added to the error when no token can be obtained.
//
// azidentity, Microsoft's library, does the exchange: it caches the access
// tokens and calls assertion again when it needs a new one.
func NewOIDCTokenSource(tenantID, clientID string, assertion AssertionFunc, hint string) (TokenSource, error) {
	var options azidentity.ClientAssertionCredentialOptions
	if oidcCredentialOptions != nil {
		options = *oidcCredentialOptions
	}
	// An explicit cloud makes azidentity ignore AZURE_AUTHORITY_HOST: a
	// variable left in a CI runner cannot send the request to another cloud.
	options.Cloud = cloud.AzurePublic
	credential, err := azidentity.NewClientAssertionCredential(tenantID, clientID, assertion, &options)
	if err != nil {
		return nil, err
	}
	return &credentialTokenSource{credential: credential, method: "OIDC", hint: hint}, nil
}

func (s *credentialTokenSource) Token(ctx context.Context) (AccessTokens, error) {
	// Teams needs a token for each API; the credential caches both.
	graph, err := s.credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{GraphScope}})
	if err != nil {
		return AccessTokens{}, s.failure(err)
	}
	teams, err := s.credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{TeamsScope}})
	if err != nil {
		return AccessTokens{}, s.failure(err)
	}
	expiresOn := graph.ExpiresOn
	if teams.ExpiresOn.Before(expiresOn) {
		expiresOn = teams.ExpiresOn
	}
	return AccessTokens{Graph: graph.Token, Teams: teams.Token, ExpiresOn: expiresOn}, nil
}

// failure turns an azidentity error into the message shown in Terraform.
func (s *credentialTokenSource) failure(err error) error {
	msg := "obtaining the Microsoft Teams access tokens with " + s.method + " failed: " + strings.TrimSuffix(err.Error(), ".")
	if s.hint != "" {
		msg += ". " + s.hint
	}
	return errors.New(msg)
}

// Retry bounds of the managed identity credential. IMDS, the identity endpoint
// of an Azure VM, answers in milliseconds; azidentity's defaults (6 retries of
// up to a minute each) are meant for a VM that is still starting, and keep a
// run outside Azure waiting for minutes before it reports the problem.
const (
	managedIdentityMaxRetries = 3
	managedIdentityTryTimeout = 5 * time.Second
)

// managedIdentityFailureMemory is how long a failure to get the tokens is
// returned again without asking the endpoint. Off Azure each attempt waits for
// the timeouts above, and Terraform starts many PowerShell workers: without
// it, a plan would wait for one attempt per worker.
const managedIdentityFailureMemory = time.Minute

// managedIdentityCredentialOptions configures the managed identity credential.
// nil keeps the defaults below; tests replace it to answer without the network.
var managedIdentityCredentialOptions *azidentity.ManagedIdentityCredentialOptions

// managedIdentityHint lists what to check when no token can be obtained: the
// SDK's own message names the endpoint it tried, not what the user can fix.
const managedIdentityHint = "Check that Terraform runs on an Azure resource with a managed identity, or on an Azure Arc-enabled server where the account running Terraform is in the himds group (Linux) or the Hybrid agent extension applications group (Windows)"

// managedIdentityUserAssignedHint is added when client_id selects a
// user-assigned identity.
const managedIdentityUserAssignedHint = ". client_id (or TEAMS_CLIENT_ID) selects a user-assigned identity: check that it is attached to the resource, or remove it to use the system-assigned identity (Azure Arc may offer only that one)"

// managedIdentityOptions returns the credential options: the bounded retries
// and, when clientID is not empty, the user-assigned identity to use.
func managedIdentityOptions(clientID string) azidentity.ManagedIdentityCredentialOptions {
	var options azidentity.ManagedIdentityCredentialOptions
	if managedIdentityCredentialOptions != nil {
		options = *managedIdentityCredentialOptions
	}
	// Zero means "not set": a test can still disable retries with -1.
	if options.Retry.MaxRetries == 0 {
		options.Retry.MaxRetries = managedIdentityMaxRetries
	}
	if options.Retry.TryTimeout == 0 {
		options.Retry.TryTimeout = managedIdentityTryTimeout
	}
	if clientID != "" {
		options.ID = azidentity.ClientID(clientID)
	}
	return options
}

// managedIdentityTokenSource gets the access tokens for the managed identity
// of the Azure resource or Azure Arc-enabled server running Terraform.
type managedIdentityTokenSource struct {
	tokens credentialTokenSource
	now    func() time.Time // time.Now; tests replace it

	// mu is a mutex: one goroutine at a time runs Token, so workers that start
	// together wait for one request instead of each sending its own.
	mu       sync.Mutex
	failure  error // the last failure, returned again until failedAt + managedIdentityFailureMemory
	failedAt time.Time
}

// NewManagedIdentityTokenSource returns a TokenSource for the system-assigned
// managed identity, or for the user-assigned one whose client ID is clientID.
//
// azidentity finds the identity endpoint of the host (IMDS on a VM, the local
// agent on Azure Arc, App Service...) and caches the tokens. It does not read
// AZURE_CLIENT_ID: only clientID selects a user-assigned identity, so a
// variable left in a runner for another tool cannot change the identity.
func NewManagedIdentityTokenSource(clientID string) (TokenSource, error) {
	options := managedIdentityOptions(clientID)
	credential, err := azidentity.NewManagedIdentityCredential(&options)
	if err != nil {
		return nil, err
	}
	hint := managedIdentityHint
	if clientID != "" {
		hint += managedIdentityUserAssignedHint
	}
	return &managedIdentityTokenSource{
		tokens: credentialTokenSource{credential: credential, method: "the managed identity", hint: hint},
		now:    time.Now,
	}, nil
}

func (s *managedIdentityTokenSource) Token(ctx context.Context) (AccessTokens, error) {
	s.mu.Lock()
	// defer runs when Token returns, whichever return it takes.
	defer s.mu.Unlock()

	if s.failure != nil && s.now().Sub(s.failedAt) < managedIdentityFailureMemory {
		return AccessTokens{}, s.failure
	}
	tokens, err := s.tokens.Token(ctx)
	if err != nil {
		s.failure, s.failedAt = err, s.now()
		return AccessTokens{}, err
	}
	s.failure = nil
	return tokens, nil
}
