package cards

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gitchat/src/config"
	"gitchat/src/modelServer"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type storedSession struct {
	SessionView
	Owner    string                   `json:"owner"`
	Requests map[string]requestRecord `json:"requests"`
}
type requestRecord struct {
	CardID string    `json:"card_id"`
	Input  ChatInput `json:"input"`
}
type database struct {
	Models      map[string]ModelSettings  `json:"model_settings,omitempty"`
	Version     int                       `json:"version"`
	Sessions    map[string]*storedSession `json:"sessions"`
	Shares      map[string]SessionView    `json:"shares"`
	Preferences map[string]Preferences    `json:"preferences"`
	Stats       map[string]map[string]int `json:"stats"`
}
type serverState struct {
	mu         sync.Mutex
	db         database
	provider   modelServer.Provider
	baseConfig config.Config
	secretKey  []byte
	apps       []AppView
	prompts    map[string]string
	file       string
	lockFile   *os.File
	active     map[string]context.CancelFunc
	closed     bool
	wg         sync.WaitGroup
}

// The state pointer keeps the legacy value-returning constructor safe to copy.
type CardServer struct{ state *serverState }

func CreateCardServer(cfg *config.Config) (CardServer, error) {
	if cfg == nil {
		return CardServer{}, fmt.Errorf("%w: config is required", ErrInvalid)
	}
	p, err := modelServer.NewProvider(cfg)
	if err != nil {
		return CardServer{}, err
	}
	return CreateCardServerWithProvider(cfg, p)
}
func CreateCardServerWithProvider(cfg *config.Config, p modelServer.Provider) (CardServer, error) {
	if cfg == nil || p == nil {
		return CardServer{}, fmt.Errorf("%w: config and provider are required", ErrInvalid)
	}
	s := &serverState{baseConfig: *cfg, provider: p, active: map[string]context.CancelFunc{}, prompts: map[string]string{}, db: database{Models: map[string]ModelSettings{}, Version: 1, Sessions: map[string]*storedSession{}, Shares: map[string]SessionView{}, Preferences: map[string]Preferences{}, Stats: map[string]map[string]int{}}}
	for _, t := range cfg.Chattemplate {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		if _, exists := s.prompts[name]; exists {
			return CardServer{}, fmt.Errorf("%w: duplicate application", ErrInvalid)
		}
		s.apps = append(s.apps, AppView{ID: name, Name: name})
		s.prompts[name] = t.Template.Sysprompt
	}
	if len(s.apps) == 0 {
		s.apps = []AppView{{ID: "default", Name: "求知"}}
		s.prompts["default"] = ""
	}
	if cfg.DataDir != "" {
		if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
			return CardServer{}, err
		}
		lock, err := os.OpenFile(filepath.Join(cfg.DataDir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return CardServer{}, err
		}
		if err = lockDataFile(lock); err != nil {
			lock.Close()
			return CardServer{}, fmt.Errorf("data directory is in use: %w", err)
		}
		s.lockFile = lock
		s.file = filepath.Join(cfg.DataDir, "gitchat.json")
		if err = s.load(); err != nil {
			_ = unlockDataFile(lock)
			lock.Close()
			return CardServer{}, err
		}
	}
	return CardServer{state: s}, nil
}
func newID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func (s *serverState) load() error {
	b, err := os.ReadFile(s.file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var db database
	if err = json.Unmarshal(b, &db); err != nil {
		return fmt.Errorf("read saved conversations: %w", err)
	}
	if db.Version != 1 || db.Sessions == nil || db.Shares == nil || db.Preferences == nil || db.Stats == nil {
		return errors.New("unsupported or incomplete saved data")
	}
	if db.Models == nil {
		db.Models = map[string]ModelSettings{}
	}
	s.db = db
	for id, session := range db.Sessions {
		if session == nil || session.ID != id || strings.TrimSpace(session.Owner) == "" {
			return errors.New("invalid saved session identity")
		}
		if session.Requests == nil {
			session.Requests = map[string]requestRecord{}
		}
		if err := validateGraph(session.SessionView); err != nil {
			return fmt.Errorf("invalid saved conversation: %w", err)
		}
		for i := range session.Cards {
			c := &session.Cards[i]
			if isActive(c.Status) {
				c.Status = "cancelled"
				c.Error = "服务重启，生成已中断，请重新提问"
				c.UpdatedAt = time.Now().UTC()
			}
		}
	}
	for _, shared := range db.Shares {
		if err := validateGraph(shared); err != nil {
			return fmt.Errorf("invalid shared conversation: %w", err)
		}
	}
	return s.save()
}
func validateGraph(v SessionView) error {
	seen := map[string]bool{}
	for _, c := range v.Cards {
		if c.ID == "" || seen[c.ID] {
			return ErrInvalid
		}
		for _, p := range c.ParentIDs {
			if !seen[p] {
				return ErrInvalid
			}
		}
		seen[c.ID] = true
	}
	if !seen[v.RootID] || !seen[v.FocusID] {
		return ErrInvalid
	}
	return nil
}
func (s *serverState) save() error {
	if s.file == "" {
		return nil
	}
	b, err := json.Marshal(s.db)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.file), ".gitchat-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, s.file); err != nil {
		return err
	}
	// Sync the directory so the atomic rename survives a system crash.
	if d, openErr := os.Open(filepath.Dir(s.file)); openErr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// mutate runs under mu and rolls back all in-memory changes on persistence failure.
func (s *serverState) mutate(fn func() error) error {
	if s.closed {
		return fmt.Errorf("%w: server is closed", ErrConflict)
	}
	previous := clone(s.db)
	if err := fn(); err != nil {
		s.db = previous
		return err
	}
	if err := s.save(); err != nil {
		s.db = previous
		return fmt.Errorf("save conversations: %w", err)
	}
	return nil
}
func (s *serverState) session(owner, id string) (*storedSession, error) {
	v, ok := s.db.Sessions[id]
	if !ok || owner == "" || v.Owner != owner {
		return nil, ErrNotFound
	}
	return v, nil
}
func findCard(v *storedSession, id string) (*Card, error) {
	for i := range v.Cards {
		if v.Cards[i].ID == id {
			return &v.Cards[i], nil
		}
	}
	return nil, ErrNotFound
}
func (s *serverState) count(owner, action string) {
	if s.db.Stats[owner] == nil {
		s.db.Stats[owner] = map[string]int{}
	}
	s.db.Stats[owner][action]++
}
func isActive(status string) bool { return status == "pending" || status == "streaming" }
func shortTitle(v string) string {
	r := []rune(strings.TrimSpace(v))
	if len(r) > 32 {
		return string(r[:32]) + "…"
	}
	return string(r)
}
func (cs *CardServer) Apps() []AppView {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.apps)
}
func (cs *CardServer) ListSessions(owner string) []SessionView {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []SessionView{}
	for _, v := range s.db.Sessions {
		if v.Owner == owner {
			view := v.SessionView
			view.Cards = nil
			out = append(out, view)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}
func (cs *CardServer) NewSession(owner, appID, title string) (SessionView, error) {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(owner) == "" {
		return SessionView{}, ErrInvalid
	}
	if appID == "" {
		appID = s.apps[0].ID
	}
	if _, ok := s.prompts[appID]; !ok {
		return SessionView{}, fmt.Errorf("%w: application", ErrNotFound)
	}
	if utf8.RuneCountInString(title) > 200 {
		return SessionView{}, fmt.Errorf("%w: title is too long", ErrInvalid)
	}
	now := time.Now().UTC()
	root := Card{ID: newID(), ParentIDs: []string{}, Title: "开始思考", Status: "completed", Operation: "root", CreatedAt: now, UpdatedAt: now, Suggestions: []modelServer.Suggestion{}}
	if strings.TrimSpace(title) == "" {
		title = "新的思考"
	}
	v := SessionView{ID: newID(), AppID: appID, Title: title, RootID: root.ID, FocusID: root.ID, Cards: []Card{root}, CreatedAt: now, UpdatedAt: now}
	err := s.mutate(func() error {
		s.db.Sessions[v.ID] = &storedSession{SessionView: v, Owner: owner, Requests: map[string]requestRecord{}}
		s.count(owner, "new_session")
		return nil
	})
	return clone(v), err
}
func (cs *CardServer) GetSession(owner, id string) (SessionView, error) {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.session(owner, id)
	if err != nil {
		return SessionView{}, err
	}
	return clone(v.SessionView), nil
}
func (cs *CardServer) DeleteSession(owner, id string) error {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.session(owner, id)
	if err != nil {
		return err
	}
	ids := []string{}
	for _, c := range v.Cards {
		ids = append(ids, c.ID)
	}
	err = s.mutate(func() error { delete(s.db.Sessions, id); s.count(owner, "delete_session"); return nil })
	if err == nil {
		for _, id := range ids {
			if cancel := s.active[id]; cancel != nil {
				cancel()
			}
		}
	}
	return err
}
func (cs *CardServer) Close() error {
	s := cs.state
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	for _, cancel := range s.active {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.save()
	if s.lockFile != nil {
		_ = unlockDataFile(s.lockFile)
		closeErr := s.lockFile.Close()
		s.lockFile = nil
		if err == nil {
			err = closeErr
		}
	}
	return err
}
