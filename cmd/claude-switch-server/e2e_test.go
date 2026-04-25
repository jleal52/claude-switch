//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jleal52/claude-switch/internal/api"
	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/oauth"
	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

// stubProvider is a fake OAuth provider so the test doesn't need real
// GitHub/Google. Login redirects to /__stub_oauth and Exchange returns a
// fixed profile.
type stubProvider struct{ name, email string }

func (s *stubProvider) Name() string                    { return s.name }
func (s *stubProvider) AuthCodeURL(state string) string { return "/__stub_oauth?state=" + state }
func (s *stubProvider) Exchange(_ context.Context, _ string) (*store.OAuthProfile, error) {
	return &store.OAuthProfile{Provider: s.name, Subject: "stub-1", Email: s.email, Name: "Stub User"}, nil
}

func TestEndToEndPairOpenStream(t *testing.T) {
	st := store.NewTestStore(t, "e2e_full")
	h := hub.New()
	router := api.NewRouter(api.RouterConfig{
		Store: st, Hub: h,
		Providers:      []oauth.Provider{&stubProvider{name: "github", email: "u@x"}},
		BaseURL:        "http://localhost",
		Secure:         false,
		ServerEndpoint: "", // we use srv.URL when wrapping wss; here we just don't care because the test reaches /ws/wrapper directly
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	hc := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. /auth/github/login → captures state cookie + redirect to /__stub_oauth.
	loginURL := srv.URL + "/auth/github/login"
	resp, err := hc.Get(loginURL)
	require.NoError(t, err)
	resp.Body.Close()
	state := stateCookieFromJar(jar, srv.URL)
	require.NotEmpty(t, state)

	// 2. /auth/github/callback?state=...&code=ok → sets cs_session + cs_csrf.
	cbURL := srv.URL + "/auth/github/callback?state=" + state + "&code=ok"
	resp2, err := hc.Get(cbURL)
	require.NoError(t, err)
	resp2.Body.Close()
	require.Equal(t, http.StatusFound, resp2.StatusCode)

	csrf := cookieValue(jar, srv.URL, "cs_csrf")
	require.NotEmpty(t, csrf)

	// 3. Wrapper pairs (anonymous).
	startBody, _ := json.Marshal(map[string]string{"name": "ireland", "os": "linux", "arch": "amd64", "version": "0.1"})
	resp3, err := http.Post(srv.URL+"/device/pair/start", "application/json", strings.NewReader(string(startBody)))
	require.NoError(t, err)
	var startResp struct {
		Code    string `json:"code"`
		PollURL string `json:"poll_url"`
	}
	_ = json.NewDecoder(resp3.Body).Decode(&startResp)
	resp3.Body.Close()
	require.NotEmpty(t, startResp.Code)

	// 4. Browser redeems.
	redeemBody, _ := json.Marshal(map[string]string{"code": startResp.Code})
	req, _ := http.NewRequest("POST", srv.URL+"/api/pair/redeem", strings.NewReader(string(redeemBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp4, err := hc.Do(req)
	require.NoError(t, err)
	resp4.Body.Close()
	require.Equal(t, http.StatusOK, resp4.StatusCode)

	// 5. Wrapper polls and gets credentials.
	resp5, err := http.Get(srv.URL + "/device/pair/poll?c=" + startResp.Code)
	require.NoError(t, err)
	var creds struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(resp5.Body).Decode(&creds)
	resp5.Body.Close()
	require.NotEmpty(t, creds.AccessToken)

	// 6. Wrapper connects WS, says hello.
	wsURL := "ws" + srv.URL[len("http"):] + "/ws/wrapper"
	wHeaders := http.Header{}
	wHeaders.Set("Authorization", "Bearer "+creds.AccessToken)
	wConn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: wHeaders})
	require.NoError(t, err)
	defer wConn.CloseNow()
	hello := proto.Hello{
		WrapperID: "ireland", OS: "linux", Arch: "amd64", Version: "0.1",
		Accounts: []string{"default"}, Capabilities: []string{"pty"},
	}
	helloRaw, _ := proto.Encode(proto.TypeHello, "", hello)
	_ = wConn.Write(context.Background(), websocket.MessageText, helloRaw)

	// Wait for wrapper to be registered with the hub (UpdateLastSeen happens in handleText).
	time.Sleep(100 * time.Millisecond)

	// 7. Browser creates session.
	wrappersResp, _ := hc.Get(srv.URL + "/api/wrappers")
	var wrappers []struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(wrappersResp.Body).Decode(&wrappers)
	wrappersResp.Body.Close()
	require.Len(t, wrappers, 1)

	createBody, _ := json.Marshal(map[string]string{"wrapper_id": wrappers[0].ID, "cwd": "/tmp", "account": "default"})
	createReq, _ := http.NewRequest("POST", srv.URL+"/api/sessions", strings.NewReader(string(createBody)))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-CSRF-Token", csrf)
	createResp, err := hc.Do(createReq)
	require.NoError(t, err)
	var sessJSON struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(createResp.Body).Decode(&sessJSON)
	createResp.Body.Close()
	require.NotEmpty(t, sessJSON.ID)

	// Drain the open_session frame the wrapper just received.
	_, _, _ = wConn.Read(context.Background())

	// 8. Wrapper acks the open with session.started.
	ssRaw, _ := proto.Encode(proto.TypeSessionStarted, sessJSON.ID, proto.SessionStarted{
		PID: 1, JSONLUUID: "uuid-x", Cwd: "/tmp", Account: "default",
	})
	_ = wConn.Write(context.Background(), websocket.MessageText, ssRaw)

	// 9. Browser subscribes; expects replay.start, replay.end.
	browserWS := "ws" + srv.URL[len("http"):] + "/ws/sessions/" + sessJSON.ID + "?ct=" + csrf
	bHeaders := http.Header{}
	bHeaders.Set("Cookie", sessionCookieFromJar(jar, srv.URL))
	bConn, _, err := websocket.Dial(context.Background(), browserWS, &websocket.DialOptions{HTTPHeader: bHeaders})
	require.NoError(t, err)
	defer bConn.CloseNow()

	mtyp, raw, _ := bConn.Read(context.Background())
	require.Equal(t, websocket.MessageText, mtyp)
	tt, _, _, _ := proto.Decode(raw)
	require.Equal(t, "replay.start", tt)
	mtyp, raw, _ = bConn.Read(context.Background())
	require.Equal(t, websocket.MessageText, mtyp)
	tt, _, _, _ = proto.Decode(raw)
	require.Equal(t, "replay.end", tt)

	// 10. Wrapper sends pty.data; browser receives.
	id, _ := ulid.ParseStrict(sessJSON.ID)
	_ = wConn.Write(context.Background(), websocket.MessageBinary, proto.EncodePTYData(id, []byte("hello!")))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mtyp, raw, _ := bConn.Read(context.Background())
		if mtyp == websocket.MessageBinary {
			_, payload, _ := proto.DecodePTYData(raw)
			if string(payload) == "hello!" {
				return // success
			}
		}
	}
	t.Fatal("did not receive expected pty.data on browser ws")
}

// stateCookieFromJar returns the cs_oauth_state cookie value for the given URL.
func stateCookieFromJar(jar http.CookieJar, rawURL string) string {
	return cookieValue(jar, rawURL, "cs_oauth_state")
}

// sessionCookieFromJar returns the cs_session cookie line ("cs_session=value")
// suitable for the Cookie header on a manual ws.Dial.
func sessionCookieFromJar(jar http.CookieJar, rawURL string) string {
	v := cookieValue(jar, rawURL, "cs_session")
	if v == "" {
		return ""
	}
	return "cs_session=" + v
}

func cookieValue(jar http.CookieJar, rawURL, name string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	for _, c := range jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}
