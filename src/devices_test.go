package router

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitchat/src/cards"
	"github.com/gin-gonic/gin"
)

func TestDevicePairingRetainsOriginalWorkspaceAndDisconnectIsLocal(t *testing.T) {
	server, source := newHTTPTest(t)
	base := server.URL
	jar, _ := cookiejar.New(nil)
	target := &http.Client{Jar: jar}
	var origin, previous cards.SessionView
	apiRequest(t, source, "POST", base+"/api/sessions", map[string]string{"title": "手机上的探索"}, 201, &origin)
	apiRequest(t, target, "POST", base+"/api/sessions", map[string]string{"title": "电脑原有空间"}, 201, &previous)
	apiRequest(t, target, "GET", base+"/api/sessions/"+origin.ID, nil, 404, nil)
	var code struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
		ExpiresIn int       `json:"expires_in"`
	}
	apiRequest(t, source, "POST", base+"/api/devices/pairing", map[string]string{}, 201, &code)
	if len(code.Code) != 43 || code.ExpiresIn != 600 || time.Until(code.ExpiresAt) < 9*time.Minute {
		t.Fatalf("unexpected pairing capability metadata: %+v", code)
	}
	u, _ := url.Parse(base)
	sourceOwner := source.Jar.Cookies(u)[0].Value
	previousOwner := target.Jar.Cookies(u)[0].Value
	if strings.Contains(code.Code, sourceOwner) {
		t.Fatal("pairing leaked owner credential")
	}
	apiRequest(t, target, "POST", base+"/api/devices/pair", map[string]string{"code": code.Code}, 200, nil)
	apiRequest(t, target, "GET", base+"/api/sessions/"+origin.ID, nil, 200, nil)
	apiRequest(t, target, "GET", base+"/api/sessions/"+previous.ID, nil, 404, nil)
	apiRequest(t, target, "PUT", base+"/api/preferences", cards.Preferences{Tone: "从电脑修改"}, 200, nil)
	var preferences cards.Preferences
	apiRequest(t, source, "GET", base+"/api/preferences", nil, 200, &preferences)
	if preferences.Tone != "从电脑修改" {
		t.Fatal("paired workspace preferences did not update on source")
	}
	var deviceState struct {
		CanRestore bool `json:"can_restore"`
	}
	apiRequest(t, target, "GET", base+"/api/devices", nil, 200, &deviceState)
	if !deviceState.CanRestore {
		t.Fatal("paired device lost recovery for its previous workspace")
	}
	// No API except the device endpoints receives the backup credential.
	for _, cookie := range target.Jar.Cookies(u) {
		if cookie.Name == "gitchat_previous_owner" {
			t.Fatal("backup cookie escaped the device endpoint path")
		}
	}
	apiRequest(t, target, "POST", base+"/api/devices/pair", map[string]string{"code": code.Code}, 409, nil)
	apiRequest(t, target, "POST", base+"/api/devices/disconnect", map[string]string{}, 409, nil)
	apiRequest(t, target, "POST", base+"/api/devices/restore", map[string]string{}, 200, nil)
	if target.Jar.Cookies(u)[0].Value != previousOwner {
		t.Fatal("restore did not return the original workspace credential")
	}
	apiRequest(t, target, "GET", base+"/api/sessions/"+origin.ID, nil, 404, nil)
	apiRequest(t, source, "GET", base+"/api/sessions/"+origin.ID, nil, 200, nil)
	apiRequest(t, target, "GET", base+"/api/sessions/"+previous.ID, nil, 200, nil)
	apiRequest(t, target, "GET", base+"/api/devices", nil, 200, &deviceState)
	if deviceState.CanRestore {
		t.Fatal("restored backup cookie was not cleared")
	}
	apiRequest(t, target, "POST", base+"/api/devices/restore", map[string]string{}, 409, nil)
	apiRequest(t, target, "POST", base+"/api/devices/disconnect", map[string]string{}, 200, nil)
	apiRequest(t, target, "GET", base+"/api/sessions/"+previous.ID, nil, 404, nil)
	apiRequest(t, target, "POST", base+"/api/devices/restore", map[string]string{}, 200, nil)
	apiRequest(t, target, "GET", base+"/api/sessions/"+previous.ID, nil, 200, nil)
}

func TestPairingRejectsThirdSwitchWithoutConsumingCapability(t *testing.T) {
	server, first := newHTTPTest(t)
	base := server.URL
	newClient := func() *http.Client { jar, _ := cookiejar.New(nil); return &http.Client{Jar: jar} }
	second, target := newClient(), newClient()
	var original cards.SessionView
	apiRequest(t, target, "POST", base+"/api/sessions", map[string]string{}, 201, &original)
	var firstCode, secondCode struct {
		Code string `json:"code"`
	}
	apiRequest(t, first, "POST", base+"/api/devices/pairing", map[string]string{}, 201, &firstCode)
	apiRequest(t, second, "POST", base+"/api/devices/pairing", map[string]string{}, 201, &secondCode)
	apiRequest(t, target, "POST", base+"/api/devices/pair", map[string]string{"code": firstCode.Code}, 200, nil)
	apiRequest(t, target, "POST", base+"/api/devices/pair", map[string]string{"code": secondCode.Code}, 409, nil)
	apiRequest(t, target, "POST", base+"/api/devices/disconnect", map[string]string{}, 409, nil)
	apiRequest(t, target, "POST", base+"/api/devices/restore", map[string]string{}, 200, nil)
	apiRequest(t, target, "GET", base+"/api/sessions/"+original.ID, nil, 200, nil)
	apiRequest(t, target, "POST", base+"/api/devices/pair", map[string]string{"code": secondCode.Code}, 200, nil)
	apiRequest(t, target, "POST", base+"/api/devices/restore", map[string]string{}, 200, nil)
	// A same-space capability is a no-op and does not occupy the recovery slot.
	apiRequest(t, target, "POST", base+"/api/devices/pairing", map[string]string{}, 201, &firstCode)
	apiRequest(t, target, "POST", base+"/api/devices/pair", map[string]string{"code": firstCode.Code}, 200, nil)
	var deviceState struct {
		CanRestore bool `json:"can_restore"`
	}
	apiRequest(t, target, "GET", base+"/api/devices", nil, 200, &deviceState)
	if deviceState.CanRestore {
		t.Fatal("same-workspace pairing unnecessarily created a backup")
	}
}

func TestStaleWorkspaceHeaderCannotWriteIntoPairedSpace(t *testing.T) {
	server, source := newHTTPTest(t)
	base := server.URL
	jar, _ := cookiejar.New(nil)
	target := &http.Client{Jar: jar}
	var before, after struct {
		WorkspaceID string `json:"workspace_id"`
	}
	apiRequest(t, target, "GET", base+"/api/config", nil, 200, &before)
	u, _ := url.Parse(base)
	if len(before.WorkspaceID) != 64 || before.WorkspaceID == target.Jar.Cookies(u)[0].Value {
		t.Fatal("workspace fingerprint is missing or exposes owner credential")
	}
	apiRequest(t, source, "PUT", base+"/api/preferences", cards.Preferences{Tone: "保留这个偏好"}, 200, nil)
	var code struct {
		Code string `json:"code"`
	}
	apiRequest(t, source, "POST", base+"/api/devices/pairing", map[string]string{}, 201, &code)
	apiRequest(t, target, "POST", base+"/api/devices/pair", map[string]string{"code": code.Code}, 200, nil)
	apiRequest(t, target, "GET", base+"/api/config", nil, 200, &after)
	if after.WorkspaceID == before.WorkspaceID {
		t.Fatal("workspace fingerprint did not follow switched identity")
	}
	for _, attempt := range []struct {
		fingerprint string
		status      int
	}{{before.WorkspaceID, 409}, {after.WorkspaceID, 200}} {
		req, _ := http.NewRequest("PUT", base+"/api/preferences", strings.NewReader(`{"tone":"在新空间确认的修改"}`))
		req.Header.Set("X-GitChat-Workspace", attempt.fingerprint)
		response, err := target.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != attempt.status {
			t.Fatalf("workspace header write got %d want %d", response.StatusCode, attempt.status)
		}
		var preferences cards.Preferences
		apiRequest(t, source, "GET", base+"/api/preferences", nil, 200, &preferences)
		want := "保留这个偏好"
		if attempt.status == 200 {
			want = "在新空间确认的修改"
		}
		if preferences.Tone != want {
			t.Fatalf("unexpected preference after guarded write: %s", preferences.Tone)
		}
	}
}

func TestPairingCapabilityExpiryReissueAndAtomicConsumption(t *testing.T) {
	store := newPairingStore()
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	old, _, err := store.issue("owner", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := store.issue("owner", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.consume(old, "127.0.0.1"); !errors.Is(err, errPairingInvalid) {
		t.Fatal("reissuing did not invalidate old capability")
	}
	if len(store.codes) != 1 || store.codes[sha256.Sum256([]byte(code))].owner != "owner" {
		t.Fatal("store does not retain one hashed capability per owner")
	}
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if owner, err := store.consume(code, "127.0.0.1"); err == nil && owner == "owner" {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("one capability was consumed %d times", successes.Load())
	}
	code, _, err = store.issue("owner", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(pairingLifetime)
	if _, err := store.consume(code, "127.0.0.1"); !errors.Is(err, errPairingInvalid) {
		t.Fatal("expired capability was accepted")
	}
}

func TestPairingRateLimitsIgnoreForwardedAddresses(t *testing.T) {
	server, client := newHTTPTest(t)
	for i := 0; i <= pairingAttempts; i++ {
		body := bytes.NewBufferString(`{"code":"wrong"}`)
		req, _ := http.NewRequest("POST", server.URL+"/api/devices/pair", body)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i))
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := http.StatusBadRequest
		if i == pairingAttempts {
			want = http.StatusTooManyRequests
			if response.Header.Get("Retry-After") == "" {
				t.Fatal("rate-limit response omitted retry time")
			}
		}
		if response.StatusCode != want {
			t.Fatalf("attempt %d got %d want %d", i, response.StatusCode, want)
		}
	}
}

func TestPairingStoreRemainsBounded(t *testing.T) {
	store := newPairingStore()
	for i := 0; i < pairingMaxCodes; i++ {
		_, _, err := store.issue(fmt.Sprint(i), fmt.Sprint(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := store.issue("overflow", "overflow"); !errors.Is(err, errPairingLimited) {
		t.Fatal("code map exceeded its bound")
	}
	for i := len(store.attempts); i < pairingMaxIPs; i++ {
		store.attempts[fmt.Sprint(i)] = pairingLimit{expires: time.Now().Add(time.Hour)}
	}
	if _, err := store.consume("wrong", "overflow"); !errors.Is(err, errPairingLimited) {
		t.Fatal("attempt map exceeded its bound")
	}
}

func TestDeviceCookiesAndCrossSiteProtection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, secure := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest("POST", "/api/devices/pair", nil)
		if secure {
			context.Request.TLS = &tls.ConnectionState{}
		}
		setOwnerCookie(context, "new-owner")
		context.Set("owner", strings.Repeat("a", 64))
		backUpOwner(context)
		cookies := recorder.Result().Cookies()
		if len(cookies) != 2 || !cookies[0].HttpOnly || cookies[0].Secure != secure || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
			t.Fatalf("incorrect owner cookie policy: %+v", cookies)
		}
		if !cookies[1].HttpOnly || cookies[1].Secure != secure || cookies[1].SameSite != http.SameSiteLaxMode || cookies[1].Path != "/api/devices" || cookies[1].Name != "gitchat_previous_owner" || cookies[1].MaxAge != cookies[0].MaxAge {
			t.Fatalf("incorrect recovery cookie policy: %+v", cookies[1])
		}
	}
	t.Setenv("GITCHAT_SECURE_COOKIES", "1")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "/", nil)
	setOwnerCookie(context, "owner")
	if !recorder.Result().Cookies()[0].Secure {
		t.Fatal("explicit secure-cookie policy ignored")
	}
	t.Setenv("GITCHAT_SECURE_COOKIES", "")
	server, client := newHTTPTest(t)
	for _, path := range []string{"pairing", "pair", "disconnect", "restore"} {
		req, _ := http.NewRequest("POST", server.URL+"/api/devices/"+path, strings.NewReader(`{"code":"anything"}`))
		req.Header.Set("Origin", "https://unrelated.example")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-site %s was accepted", path)
		}
	}
	// A public share is not a workspace credential.
	var session cards.SessionView
	apiRequest(t, client, "POST", server.URL+"/api/sessions", map[string]string{}, 201, &session)
	var share map[string]string
	apiRequest(t, client, "POST", server.URL+"/api/sessions/"+session.ID+"/share", map[string]string{"card_id": session.RootID}, 201, &share)
	data, _ := json.Marshal(map[string]string{"code": share["token"]})
	response, err := client.Post(server.URL+"/api/devices/pair", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatal("a sharing token was accepted for workspace pairing")
	}
}
