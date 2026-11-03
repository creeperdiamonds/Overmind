package skins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The uploading account: a Microsoft account that owns Java Edition, signed in
// once with a device code (overmind skin-account), its refresh token kept in a
// file only the owner reads. Each Minecraft services token comes by the chain
// third-party launchers use (PrismarineJS prismarine-auth's "live" flow, its
// default for Java): a Microsoft Live token for the Xbox user scope, refreshed
// from the kept token, as an Xbox Live user token (RpsTicket "t=" and the
// token), an XSTS token for rp://api.minecraftservices.com/, then
// login_with_xbox with "XBL3.0 x=<user hash>;<XSTS token>". The client ID is
// the one prismarine-auth uses for that flow (Minecraft for Nintendo Switch,
// 00000000441cc96b). No token is ever logged or put in an error.

// Endpoints are the services the account talks to; the zero value is
// Microsoft's, Xbox Live's and Mojang's.
type Endpoints struct {
	LiveConnect, LiveToken, UserAuth, XSTS, LoginWithXbox string
	ClientID                                              string
}

func (e Endpoints) withDefaults() Endpoints {
	def := func(s *string, v string) {
		if *s == "" {
			*s = v
		}
	}
	def(&e.LiveConnect, "https://login.live.com/oauth20_connect.srf")
	def(&e.LiveToken, "https://login.live.com/oauth20_token.srf")
	def(&e.UserAuth, "https://user.auth.xboxlive.com/user/authenticate")
	def(&e.XSTS, "https://xsts.auth.xboxlive.com/xsts/authorize")
	def(&e.LoginWithXbox, "https://api.minecraftservices.com/authentication/login_with_xbox")
	def(&e.ClientID, "00000000441cc96b")
	return e
}

const liveScope = "service::user.auth.xboxlive.com::MBI_SSL"

// accountFile is what the account file holds.
type accountFile struct {
	RefreshToken string `json:"refresh_token"`
}

// Account is a TokenSource for the uploading account.
type Account struct {
	path string
	ep   Endpoints
	http *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// OpenAccount reads an account file written by DeviceLogin.
func OpenAccount(path string, ep Endpoints, client *http.Client) (*Account, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skins: account file: %w", err)
	}
	var f accountFile
	if json.Unmarshal(data, &f) != nil || f.RefreshToken == "" {
		return nil, errors.New("skins: the account file holds no refresh token; sign in again with overmind skin-account")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Account{path: path, ep: ep.withDefaults(), http: client}, nil
}

// Invalidate drops the Minecraft token, so the next Token makes a new one.
func (a *Account) Invalidate() {
	a.mu.Lock()
	a.token = ""
	a.mu.Unlock()
}

// Token gives a Minecraft services access token, making one when the last is
// within a minute of expiring.
func (a *Account) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && time.Until(a.expires) > time.Minute {
		return a.token, nil
	}
	data, err := os.ReadFile(a.path)
	if err != nil {
		return "", fmt.Errorf("skins: account file: %w", err)
	}
	var f accountFile
	if json.Unmarshal(data, &f) != nil || f.RefreshToken == "" {
		return "", errors.New("skins: the account file holds no refresh token")
	}
	var live struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := a.form(ctx, a.ep.LiveToken, url.Values{"client_id": {a.ep.ClientID}, "scope": {liveScope},
		"grant_type": {"refresh_token"}, "refresh_token": {f.RefreshToken}}, &live); err != nil {
		return "", fmt.Errorf("skins: Microsoft refresh: %w", err)
	}
	if live.RefreshToken != "" && live.RefreshToken != f.RefreshToken {
		if err := writeAccount(a.path, live.RefreshToken); err != nil {
			return "", err
		}
	}
	mc, expires, err := a.minecraftToken(ctx, live.AccessToken)
	if err != nil {
		return "", err
	}
	a.token, a.expires = mc, time.Now().Add(expires)
	return mc, nil
}

// minecraftToken turns a Live access token into a Minecraft services token.
func (a *Account) minecraftToken(ctx context.Context, liveToken string) (string, time.Duration, error) {
	var user struct {
		Token string `json:"Token"`
	}
	if err := a.post(ctx, a.ep.UserAuth, map[string]any{
		"RelyingParty": "http://auth.xboxlive.com", "TokenType": "JWT",
		"Properties": map[string]any{"AuthMethod": "RPS", "SiteName": "user.auth.xboxlive.com", "RpsTicket": "t=" + liveToken},
	}, &user); err != nil || user.Token == "" {
		return "", 0, fmt.Errorf("skins: Xbox Live user token: %w", orNoToken(err))
	}
	var xsts struct {
		Token         string `json:"Token"`
		DisplayClaims struct {
			XUI []struct {
				UHS string `json:"uhs"`
			} `json:"xui"`
		} `json:"DisplayClaims"`
	}
	if err := a.post(ctx, a.ep.XSTS, map[string]any{
		"RelyingParty": "rp://api.minecraftservices.com/", "TokenType": "JWT",
		"Properties": map[string]any{"SandboxId": "RETAIL", "UserTokens": []string{user.Token}},
	}, &xsts); err != nil || xsts.Token == "" || len(xsts.DisplayClaims.XUI) == 0 {
		return "", 0, fmt.Errorf("skins: XSTS token: %w", orNoToken(err))
	}
	var mc struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := a.post(ctx, a.ep.LoginWithXbox, map[string]any{
		"identityToken": "XBL3.0 x=" + xsts.DisplayClaims.XUI[0].UHS + ";" + xsts.Token,
	}, &mc); err != nil || mc.AccessToken == "" {
		return "", 0, fmt.Errorf("skins: login_with_xbox: %w", orNoToken(err))
	}
	if mc.ExpiresIn <= 0 {
		mc.ExpiresIn = 3600
	}
	return mc.AccessToken, time.Duration(mc.ExpiresIn) * time.Second, nil
}

func orNoToken(err error) error {
	if err == nil {
		return errors.New("no token in the answer")
	}
	return err
}

// post sends JSON and reads JSON. Errors carry the status only, never a body,
// which could hold a token.
func (a *Account) post(ctx context.Context, to string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, to, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-xbl-contract-version", "1")
	return a.do(req, out)
}

func (a *Account) form(ctx context.Context, to string, v url.Values, out any) error {
	return postForm(ctx, a.http, to, v, out)
}

func (a *Account) do(req *http.Request, out any) error {
	resp, err := a.http.Do(req)
	if err != nil {
		return errors.New(req.URL.Host + ": request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func postForm(ctx context.Context, client *http.Client, to string, v url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, to, strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return errors.New(req.URL.Host + ": request failed")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		return &liveError{status: resp.Status, code: e.Error}
	}
	return json.Unmarshal(data, out)
}

type liveError struct{ status, code string }

func (e *liveError) Error() string { return "login.live.com: " + e.status + " " + e.code }

// writeAccount writes the account file, readable by its owner only.
func writeAccount(path, refresh string) error {
	data, _ := json.Marshal(accountFile{RefreshToken: refresh})
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DeviceLogin signs the account in with a device code: it prints where to go
// and the code to type (never a token) and waits until the owner has signed
// in, then writes the account file.
func DeviceLogin(ctx context.Context, path string, w io.Writer, ep Endpoints, client *http.Client) error {
	ep = ep.withDefaults()
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	var d struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if err := postForm(ctx, client, ep.LiveConnect, url.Values{"client_id": {ep.ClientID}, "scope": {liveScope}, "response_type": {"device_code"}}, &d); err != nil {
		return fmt.Errorf("skins: device code: %w", err)
	}
	fmt.Fprintf(w, "Sign in to the Java account at %s with the code %s\n", d.VerificationURI, d.UserCode)
	interval := time.Duration(max(d.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(max(d.ExpiresIn, 60)) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		var tok struct {
			RefreshToken string `json:"refresh_token"`
		}
		err := postForm(ctx, client, ep.LiveToken, url.Values{"client_id": {ep.ClientID}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {d.DeviceCode}}, &tok)
		var le *liveError
		if errors.As(err, &le) && (le.code == "authorization_pending" || le.code == "slow_down") {
			continue
		}
		if err != nil {
			return fmt.Errorf("skins: device sign-in: %w", err)
		}
		if tok.RefreshToken == "" {
			return errors.New("skins: device sign-in gave no refresh token")
		}
		if err := writeAccount(path, tok.RefreshToken); err != nil {
			return err
		}
		fmt.Fprintln(w, "Signed in. The account file is", path)
		return nil
	}
	return errors.New("skins: the device code expired")
}
