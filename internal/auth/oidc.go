package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
)

// OIDCConfig holds the settings required to configure an OpenID Connect provider.
type OIDCConfig struct {
	ProviderURL  string   // Discovery URL of the OIDC provider
	ClientID     string   // OAuth2 client ID
	ClientSecret string   // OAuth2 client secret
	RedirectURL  string   // Callback URL to handle OIDC responses
	Scopes       []string // Additional scopes if needed
}

// OIDCService encapsulates the OIDC provider, OAuth2 configuration,
// ID token verifier and a session store to hold transient authentication data.
type OIDCService struct {
	Provider        *oidc.Provider
	OAuth2Config    *oauth2.Config
	IDTokenVerifier *oidc.IDTokenVerifier
	Store           *sessions.CookieStore
}

// NewOIDCService creates and configures an OIDCService instance.
// It expects the SESSION_KEY to be set in the environment for securing the session cookies.
func NewOIDCService(cfg *OIDCConfig) (*OIDCService, error) {
	provider, err := oidc.NewProvider(context.Background(), cfg.ProviderURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get OIDC provider: %w", err)
	}

	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.RedirectURL,
		Scopes:       append([]string{oidc.ScopeOpenID, "profile", "email"}, cfg.Scopes...),
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	// 반드시 환경변수 SESSION_KEY를 안전하게 관리할 것 (예, config 또는 secret management 이용)
	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
		return nil, errors.New("SESSION_KEY environment variable not set")
	}
	store := sessions.NewCookieStore([]byte(sessionKey))
	store.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		MaxAge:   3600 * 24 * 7, // 1주일
	}

	return &OIDCService{
		Provider:        provider,
		OAuth2Config:    oauth2Config,
		IDTokenVerifier: verifier,
		Store:           store,
	}, nil
}

// generateState creates a random base64-encoded string used to validate auth requests.
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// LoginHandler initiates the OIDC login process.
// It generates a state value, saves it in the session and redirects the user to the provider’s consent page.
func (s *OIDCService) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	session, err := s.Store.Get(r, "oidc-session")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	session.Values["state"] = state
	if err := session.Save(r, w); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, s.OAuth2Config.AuthCodeURL(state), http.StatusFound)
}

// CallbackHandler handles the OAuth2 callback from the OIDC provider.
// It validates the state parameter, exchanges the code for tokens, verifies the ID token,
// and finally saves the authenticated user information in the session.
func (s *OIDCService) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	session, err := s.Store.Get(r, "oidc-session")
	if err != nil {
		http.Error(w, "Failed to get session", http.StatusBadRequest)
		return
	}
	storedState, ok := session.Values["state"].(string)
	if !ok || storedState == "" {
		http.Error(w, "State not found in session", http.StatusBadRequest)
		return
	}
	queryState := r.URL.Query().Get("state")
	if queryState != storedState {
		http.Error(w, "State mismatch", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Code not found in request", http.StatusBadRequest)
		return
	}

	token, err := s.OAuth2Config.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to exchange token: %v", err), http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "No id_token field in oauth2 token", http.StatusInternalServerError)
		return
	}

	// ID Token 검증
	idToken, err := s.IDTokenVerifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to verify ID Token: %v", err), http.StatusInternalServerError)
		return
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse claims: %v", err), http.StatusInternalServerError)
		return
	}

	session.Values["id_token"] = rawIDToken
	session.Values["email"] = claims.Email
	session.Values["name"] = claims.Name
	delete(session.Values, "state")
	if err := session.Save(r, w); err != nil {
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

// AuthMiddleware checks if the user is authenticated by verifying the presence of an ID token in the session.
func (s *OIDCService) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := s.Store.Get(r, "oidc-session")
		if err != nil || session.Values["id_token"] == nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}
