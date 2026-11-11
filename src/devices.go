package router

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	pairingLifetime = 10 * time.Minute
	pairingMaxCodes = 1024
	pairingMaxIPs   = 2048
	pairingAttempts = 12
)

var (
	errPairingInvalid = errors.New("配对码无效或已过期，请在原设备重新生成")
	errPairingLimited = errors.New("配对请求过于频繁，请稍后再试")
	errRestoreFirst   = errors.New("本设备保留着原空间，请先恢复原空间，再切换或断开")
)

type pairingEntry struct {
	owner   string
	expires time.Time
}

type pairingLimit struct {
	count   int
	expires time.Time
}

// The code is an expiring transfer capability, not a share token. Only its hash
// is stored. All state is bounded, process-local and deliberately lost on restart.
type pairingStore struct {
	mu       sync.Mutex
	codes    map[[32]byte]pairingEntry
	attempts map[string]pairingLimit
	now      func() time.Time
}

func newPairingStore() *pairingStore {
	return &pairingStore{codes: make(map[[32]byte]pairingEntry), attempts: make(map[string]pairingLimit), now: time.Now}
}

func (s *pairingStore) prune(now time.Time) {
	for code, entry := range s.codes {
		if !entry.expires.After(now) {
			delete(s.codes, code)
		}
	}
	for ip, limit := range s.attempts {
		if !limit.expires.After(now) {
			delete(s.attempts, ip)
		}
	}
}

func (s *pairingStore) allow(ip, operation string, now time.Time) bool {
	key := operation + ":" + ip
	limit, exists := s.attempts[key]
	if !exists {
		if len(s.attempts) >= pairingMaxIPs {
			return false
		}
		limit.expires = now.Add(pairingLifetime)
	}
	if limit.count >= pairingAttempts {
		return false
	}
	limit.count++
	s.attempts[key] = limit
	return true
}

func (s *pairingStore) issue(owner, ip string) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.prune(now)
	if !s.allow(ip, "issue", now) {
		return "", time.Time{}, errPairingLimited
	}
	for code, entry := range s.codes {
		if entry.owner == owner {
			delete(s.codes, code)
		}
	}
	if len(s.codes) >= pairingMaxCodes {
		return "", time.Time{}, errPairingLimited
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", time.Time{}, err
	}
	code := base64.RawURLEncoding.EncodeToString(bytes)
	expires := now.Add(pairingLifetime)
	s.codes[sha256.Sum256([]byte(code))] = pairingEntry{owner: owner, expires: expires}
	return code, expires, nil
}

func (s *pairingStore) consume(code, ip string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.prune(now)
	if !s.allow(ip, "redeem", now) {
		return "", errPairingLimited
	}
	code = strings.TrimSpace(code)
	decoded, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil || len(decoded) != 32 || len(code) != 43 {
		return "", errPairingInvalid
	}
	hash := sha256.Sum256([]byte(code))
	entry, exists := s.codes[hash]
	if !exists {
		return "", errPairingInvalid
	}
	delete(s.codes, hash)
	return entry.owner, nil
}

func newOwner() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func setOwnerCookie(c *gin.Context, owner string) {
	setCredentialCookie(c, "gitchat_owner", owner, "/", 365*24*60*60)
}

func setCredentialCookie(c *gin.Context, name, value, path string, maxAge int) {
	// A TLS-terminating deployment may explicitly force Secure cookies. Never
	// infer this from untrusted client-supplied forwarded headers.
	secure := c.Request.TLS != nil || os.Getenv("GITCHAT_SECURE_COOKIES") == "1"
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func previousOwner(c *gin.Context) string {
	owner, err := c.Cookie("gitchat_previous_owner")
	decoded, decodeErr := hex.DecodeString(owner)
	if err != nil || decodeErr != nil || len(decoded) != 32 {
		return ""
	}
	return owner
}

func backUpOwner(c *gin.Context) {
	setCredentialCookie(c, "gitchat_previous_owner", c.GetString("owner"), "/api/devices", 365*24*60*60)
}

// This public fingerprint detects a page acting on a cookie that has changed in
// another tab. It is not an authentication credential and cannot recover owner.
func workspaceFingerprint(owner string) string {
	fingerprint := sha256.Sum256([]byte(owner))
	return hex.EncodeToString(fingerprint[:])
}

func pairingIP(r *http.Request) string {
	// Reverse proxies intentionally share a rate limit unless an explicit trusted
	// proxy policy is implemented. X-Forwarded-For cannot bypass this boundary.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func pairingError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errPairingLimited):
		c.Header("Retry-After", "600")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
	case errors.Is(err, errPairingInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, errRestoreFirst):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "暂时无法创建设备配对"})
	}
}

func registerDevices(r *gin.Engine) {
	store := newPairingStore()
	r.GET("/api/devices", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"can_restore": previousOwner(c) != ""})
	})
	r.POST("/api/devices/pairing", func(c *gin.Context) {
		code, expires, err := store.issue(c.GetString("owner"), pairingIP(c.Request))
		if err != nil {
			pairingError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"code": code, "expires_at": expires.UTC(), "expires_in": int(pairingLifetime.Seconds())})
	})
	r.POST("/api/devices/pair", func(c *gin.Context) {
		var input struct {
			Code string `json:"code"`
		}
		if !bind(c, &input) {
			return
		}
		// Do not consume a capability until the original workspace can safely be
		// retained. Repeated switches must never evict its sole browser credential.
		if previousOwner(c) != "" {
			pairingError(c, errRestoreFirst)
			return
		}
		owner, err := store.consume(input.Code, pairingIP(c.Request))
		if err != nil {
			pairingError(c, err)
			return
		}
		if owner != c.GetString("owner") {
			backUpOwner(c)
			setOwnerCookie(c, owner)
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.POST("/api/devices/disconnect", func(c *gin.Context) {
		if previousOwner(c) != "" {
			pairingError(c, errRestoreFirst)
			return
		}
		owner, err := newOwner()
		if err != nil {
			pairingError(c, err)
			return
		}
		backUpOwner(c)
		setOwnerCookie(c, owner)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.POST("/api/devices/restore", func(c *gin.Context) {
		owner := previousOwner(c)
		if owner == "" {
			c.JSON(http.StatusConflict, gin.H{"error": "本设备没有可恢复的原空间"})
			return
		}
		setOwnerCookie(c, owner)
		setCredentialCookie(c, "gitchat_previous_owner", "", "/api/devices", -1)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
}
