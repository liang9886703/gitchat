package cards

import (
	"context"
	"encoding/json"
	"errors"
	"gitchat/src/config"
	"gitchat/src/modelServer"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingProvider struct {
	mu       sync.Mutex
	requests []modelServer.Request
}

func (p *recordingProvider) Generate(ctx context.Context, r modelServer.Request, emit func(string) error) (modelServer.Result, error) {
	p.mu.Lock()
	p.requests = append(p.requests, r)
	p.mu.Unlock()
	if err := emit("answer: " + r.Question); err != nil {
		return modelServer.Result{}, err
	}
	return modelServer.Result{Answer: "answer: " + r.Question, Title: r.Question, Model: "test", Memory: modelServer.Memory{Goal: r.Memory.Goal, Progress: []string{r.Question}, Preferences: []string{"简洁"}}}, nil
}
func newTestServer(t *testing.T, p modelServer.Provider, dir string) CardServer {
	t.Helper()
	s, err := CreateCardServerWithProvider(&config.Config{DataDir: dir}, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func newSession(t *testing.T, s *CardServer) SessionView {
	t.Helper()
	v, err := s.NewSession("alice", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func generate(t *testing.T, s *CardServer, v SessionView, in ChatInput) Card {
	t.Helper()
	ch, err := s.Chat(context.Background(), "alice", v.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	var last Card
	for e := range ch {
		if e.Type == "error" {
			t.Fatal(e.Error)
		}
		if e.Card != nil {
			last = *e.Card
		}
	}
	if last.Status != "completed" {
		t.Fatalf("status %s", last.Status)
	}
	return last
}
func input(op, q string, parents ...string) ChatInput {
	return ChatInput{Operation: op, Question: q, ParentIDs: parents, RequestID: q}
}
func TestBranchesMergeAndImmutableViews(t *testing.T) {
	p := &recordingProvider{}
	s := newTestServer(t, p, "")
	v := newSession(t, &s)
	base := generate(t, &s, v, input("deepen", "base", v.RootID))
	left := generate(t, &s, v, input("deepen", "left", base.ID))
	right := generate(t, &s, v, input("branch", "right", left.ID))
	if !reflect.DeepEqual(right.ParentIDs, []string{base.ID}) {
		t.Fatalf("parallel parent %v", right.ParentIDs)
	}
	p.mu.Lock()
	rightReq := p.requests[2]
	p.mu.Unlock()
	for _, m := range rightReq.Messages {
		if strings.Contains(m.Content, "left") {
			t.Fatalf("sibling leaked: %+v", rightReq.Messages)
		}
	}
	merged := generate(t, &s, v, input("merge", "compare", left.ID, right.ID))
	if len(merged.ParentIDs) != 2 || merged.Depth != 3 {
		t.Fatalf("merge %+v", merged)
	}
	p.mu.Lock()
	mergeReq := p.requests[3]
	p.mu.Unlock()
	count := 0
	for _, m := range mergeReq.Messages {
		if m.Role == "assistant" && m.Content == "answer: base" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared ancestor repeated %d", count)
	}
	view, err := s.GetSession("alice", v.ID)
	if err != nil {
		t.Fatal(err)
	}
	view.Cards[1].ParentIDs[0] = "tamper"
	view.Cards[1].Memory.Progress[0] = "tamper"
	fresh, _ := s.GetSession("alice", v.ID)
	if fresh.Cards[1].ParentIDs[0] != v.RootID || fresh.Cards[1].Memory.Progress[0] == "tamper" {
		t.Fatal("mutable view leaked")
	}
	if _, err = s.GetSession("bob", v.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("owner bypass")
	}
}
func TestPersistenceCopyShareAndRequestReplay(t *testing.T) {
	dir := t.TempDir()
	p := &recordingProvider{}
	s := newTestServer(t, p, dir)
	v := newSession(t, &s)
	first := generate(t, &s, v, input("deepen", "remember", v.RootID))
	b := true
	if _, err := s.PatchCard("alice", v.ID, first.ID, CardPatch{Favorite: &b, Success: &b}); err != nil {
		t.Fatal(err)
	}
	token, err := s.Share("alice", v.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := s.CopySession("alice", v.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if copy.ID == v.ID || copy.RootID == v.RootID || copy.Cards[1].ParentIDs[0] != copy.RootID {
		t.Fatal("copy IDs not independent")
	}
	// A published snapshot is independent of later mutations.
	b = false
	if _, err = s.PatchCard("alice", v.ID, first.ID, CardPatch{Favorite: &b}); err != nil {
		t.Fatal(err)
	}
	shared, err := s.GetShare(token)
	if err != nil || !shared.Cards[1].Favorite {
		t.Fatalf("share changed: %+v %v", shared, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := newTestServer(t, p, dir)
	loaded, err := reopened.GetSession("alice", v.ID)
	if err != nil || len(loaded.Cards) != 2 || !loaded.Cards[1].Success {
		t.Fatalf("reload failed %+v %v", loaded, err)
	}
	generate(t, &reopened, v, input("deepen", "remember", v.RootID))
	p.mu.Lock()
	calls := len(p.requests)
	p.mu.Unlock()
	if calls != 1 {
		t.Fatalf("replay generated %d times", calls)
	}
	changed := input("deepen", "changed", v.RootID)
	changed.RequestID = "remember"
	if _, err = reopened.Chat(context.Background(), "alice", v.ID, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("idempotency body mismatch accepted")
	}
	imported, err := reopened.ImportShare("bob", token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.GetSession("bob", imported.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.GetSession("alice", imported.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("import ownership")
	}
}

type blockedProvider struct {
	started chan struct{}
	once    sync.Once
}

func (p *blockedProvider) Generate(ctx context.Context, r modelServer.Request, emit func(string) error) (modelServer.Result, error) {
	if err := emit("partial"); err != nil {
		return modelServer.Result{}, err
	}
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return modelServer.Result{}, ctx.Err()
}
func TestCancellationAndBusyIdempotency(t *testing.T) {
	p := &blockedProvider{started: make(chan struct{})}
	s := newTestServer(t, p, "")
	v := newSession(t, &s)
	in := input("deepen", "wait", v.RootID)
	ch, err := s.Chat(context.Background(), "alice", v.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	first := <-ch
	<-p.started
	if _, err = s.Chat(context.Background(), "alice", v.ID, in); !errors.Is(err, ErrConflict) {
		t.Fatal("busy request duplicated")
	}
	if err = s.Stop("bob", v.ID, first.Card.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("unauthorized stop")
	}
	if err = s.Stop("alice", v.ID, first.Card.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not close stream")
	}
	saved, _ := s.GetSession("alice", v.ID)
	if saved.Cards[1].Status != "cancelled" || saved.Cards[1].Answer != "partial" {
		t.Fatalf("cancelled state %+v", saved.Cards[1])
	}
}
func TestConcurrentBranches(t *testing.T) {
	s := newTestServer(t, &recordingProvider{}, "")
	v := newSession(t, &s)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, err := s.Chat(context.Background(), "alice", v.ID, ChatInput{Question: "independent", ParentIDs: []string{v.RootID}})
			if err != nil {
				t.Error(err)
				return
			}
			for range ch {
			}
		}()
	}
	wg.Wait()
	got, _ := s.GetSession("alice", v.ID)
	if len(got.Cards) != 17 {
		t.Fatalf("lost nodes: %d", len(got.Cards))
	}
	if err := validateGraph(got); err != nil {
		t.Fatal(err)
	}
}
func TestPersistenceFailureRollsBack(t *testing.T) {
	s := newTestServer(t, &recordingProvider{}, "")
	v := newSession(t, &s)
	s.state.mu.Lock()
	s.state.file = filepath.Join(t.TempDir(), "missing", "state.json")
	s.state.mu.Unlock()
	if _, err := s.NewSession("alice", "", ""); err == nil {
		t.Fatal("expected save failure")
	}
	if len(s.ListSessions("alice")) != 1 {
		t.Fatal("failed transaction remained visible")
	}
	if _, err := s.Chat(context.Background(), "alice", v.ID, input("deepen", "broken", v.RootID)); err == nil {
		t.Fatal("expected chat save failure")
	}
	got, _ := s.GetSession("alice", v.ID)
	if len(got.Cards) != 1 {
		t.Fatal("failed card creation remained visible")
	}
	s.state.mu.Lock()
	s.state.file = ""
	s.state.mu.Unlock()
}
func TestRestartCancelsPendingAndRejectsCorruptGraph(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(t, &recordingProvider{}, dir)
	v := newSession(t, &s)
	s.state.mu.Lock()
	s.state.db.Sessions[v.ID].Cards[0].Status = "pending"
	s.state.mu.Unlock()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	next := newTestServer(t, &recordingProvider{}, dir)
	got, _ := next.GetSession("alice", v.ID)
	if got.Cards[0].Status != "cancelled" {
		t.Fatal("pending persisted after reload")
	}
	next.Close()
	path := filepath.Join(dir, "gitchat.json")
	b, _ := os.ReadFile(path)
	var db database
	json.Unmarshal(b, &db)
	db.Sessions[v.ID].Cards[0].ParentIDs = []string{"missing"}
	b, _ = json.Marshal(db)
	os.WriteFile(path, b, 0600)
	if _, err := CreateCardServerWithProvider(&config.Config{DataDir: dir}, &recordingProvider{}); err == nil {
		t.Fatal("corrupt graph accepted")
	}
}

func TestArchiveWeightsAndRestore(t *testing.T) {
	s := newTestServer(t, &recordingProvider{}, "")
	v := newSession(t, &s)
	old := generate(t, &s, v, input("deepen", "old", v.RootID))
	valuable := generate(t, &s, v, input("deepen", "valuable", v.RootID))
	yes := true
	s.PatchCard("alice", v.ID, valuable.ID, CardPatch{Favorite: &yes})
	current := generate(t, &s, v, input("deepen", "new", v.RootID))
	for _, q := range []string{"step2", "step3", "step4"} {
		current = generate(t, &s, v, input("deepen", q, current.ID))
	}
	s.state.mu.Lock()
	for i := range s.state.db.Sessions[v.ID].Cards {
		s.state.db.Sessions[v.ID].Cards[i].UpdatedAt = time.Now().Add(-48 * time.Hour)
	}
	s.state.mu.Unlock()
	if err := s.Focus("alice", v.ID, current.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := s.GetSession("alice", v.ID)
	for _, c := range snapshot.Cards {
		if c.ID == old.ID && !c.Archived {
			t.Fatal("distant old card not archived")
		}
		if c.ID == valuable.ID && c.Archived {
			t.Fatal("favorite autoarchived")
		}
	}
	if err := s.Focus("alice", v.ID, old.ID); err != nil {
		t.Fatal(err)
	}
	restored, _ := s.GetSession("alice", v.ID)
	for _, c := range restored.Cards {
		if c.ID == old.ID && c.Archived {
			t.Fatal("focus did not restore")
		}
	}
}
func TestInvalidInputAndLegacy(t *testing.T) {
	s := newTestServer(t, &recordingProvider{}, "")
	v := newSession(t, &s)
	for _, in := range []ChatInput{{Question: "", ParentIDs: []string{v.RootID}}, {Question: "x", Operation: "merge", ParentIDs: []string{v.RootID}}, {Question: "x", Operation: "merge", ParentIDs: []string{v.RootID, v.RootID}}, {Question: "x", Operation: "unknown", ParentIDs: []string{v.RootID}}} {
		if _, err := s.Chat(context.Background(), "alice", v.ID, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid input accepted %+v: %v", in, err)
		}
	}
	id, version, err := s.CreateNewSessionByApp(0)
	if err != nil {
		t.Fatal(err)
	}
	stream, newVersion, err := s.FirstStreamChatOnce(id, version, "first")
	if err != nil {
		t.Fatal(err)
	}
	finished := false
	for msg := range *stream.Ch {
		if msg.Type == modelServer.SendStreamContextTypeEnd {
			finished = true
		}
	}
	if !finished || newVersion == version {
		t.Fatal("legacy stream did not complete with new version")
	}
	stream, _, err = s.NextStreamChatOnce(id, newVersion, "next")
	if err != nil {
		t.Fatal(err)
	}
	for range *stream.Ch {
	}
}
