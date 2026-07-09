package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	issuer       = "https://balsamiq.cloud"
	authorizeURL = issuer + "/oauth2/authorize"
	tokenURL     = issuer + "/oauth2/token"
	registerURL  = issuer + "/oauth2/register"
	scopes       = "mcp:read mcp:write"
	callbackPort = 8976
)

type credentials struct {
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func credsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	return filepath.Join(dir, "bais", "credentials.json")
}

func loadCreds() (*credentials, error) {
	b, err := os.ReadFile(credsPath())
	if err != nil {
		return nil, err
	}
	var c credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveCreds(c *credentials) error {
	p := credsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

func cmdLogout() error {
	err := os.Remove(credsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// accessToken returns a valid token, refreshing it if expired.
func accessToken() (string, error) {
	c, err := loadCreds()
	if err != nil {
		return "", errors.New("not logged in, run: bmc login")
	}
	if time.Until(c.ExpiresAt) > time.Minute {
		return c.AccessToken, nil
	}
	if c.RefreshToken == "" {
		return "", errors.New("token expired, run: bmc login")
	}
	tok, err := exchangeToken(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.RefreshToken},
		"client_id":     {c.ClientID},
		"resource":      {mcpURL()},
	})
	if err != nil {
		return "", fmt.Errorf("token refresh failed (%w), run: bmc login", err)
	}
	tok.ClientID = c.ClientID
	if tok.RefreshToken == "" {
		tok.RefreshToken = c.RefreshToken
	}
	if err := saveCreds(tok); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

func cmdLogin() error {
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", callbackPort)

	clientID, err := registerClient(redirectURI)
	if err != nil {
		return fmt.Errorf("client registration: %w", err)
	}

	verifier := randomToken(32)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomToken(16)

	authURL := authorizeURL + "?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {scopes},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"resource":              {mcpURL()},
	}.Encode()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", callbackPort))
	if err != nil {
		return fmt.Errorf("cannot listen on callback port %d: %w", callbackPort, err)
	}
	defer ln.Close()

	fmt.Println("Opening browser for Balsamiq login...")
	fmt.Println("If it does not open, visit:\n" + authURL)
	_ = exec.Command("open", authURL).Start()

	code, err := waitForCode(ln, state)
	if err != nil {
		return err
	}

	tok, err := exchangeToken(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
		"resource":      {mcpURL()},
	})
	if err != nil {
		return fmt.Errorf("token exchange: %w", err)
	}
	tok.ClientID = clientID
	if err := saveCreds(tok); err != nil {
		return err
	}
	fmt.Println("Logged in.")
	return nil
}

func registerClient(redirectURI string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"client_name":                "bais-cli",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      scopes,
	})
	resp, err := http.Post(registerURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("registration returned %s", resp.Status)
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return "", err
	}
	return reg.ClientID, nil
}

func waitForCode(ln net.Listener, state string) (string, error) {
	type result struct {
		code string
		err  error
	}
	ch := make(chan result, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		if e := q.Get("error"); e != "" {
			fmt.Fprintf(w, "Login failed: %s", e)
			ch <- result{err: fmt.Errorf("authorization error: %s (%s)", e, q.Get("error_description"))}
			return
		}
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			ch <- result{err: errors.New("state mismatch")}
			return
		}
		fmt.Fprint(w, "bmc: login complete, you can close this tab.")
		ch <- result{code: q.Get("code")}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	select {
	case r := <-ch:
		return r.code, r.err
	case <-time.After(5 * time.Minute):
		return "", errors.New("timed out waiting for browser login")
	}
}

func exchangeToken(form url.Values) (*credentials, error) {
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	if tr.Error != "" || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s (%s)", resp.Status, tr.Error, tr.ErrorDesc)
	}
	if tr.ExpiresIn == 0 {
		tr.ExpiresIn = 3600
	}
	return &credentials{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
