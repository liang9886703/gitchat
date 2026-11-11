package cards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gitchat/src/modelServer"
	"strings"
	"time"
	"unicode/utf8"
)

func normalizeInput(in ChatInput) (ChatInput, error) {
	in.Question = strings.TrimSpace(in.Question)
	in.Quote = strings.TrimSpace(in.Quote)
	in.RequestID = strings.TrimSpace(in.RequestID)
	if in.Operation == "" {
		in.Operation = "deepen"
	}
	if in.Operation != "deepen" && in.Operation != "branch" && in.Operation != "merge" {
		return in, fmt.Errorf("%w: unknown operation", ErrInvalid)
	}
	if in.Question == "" || utf8.RuneCountInString(in.Question) > 12000 || utf8.RuneCountInString(in.Quote) > 12000 || len(in.RequestID) > 128 {
		return in, fmt.Errorf("%w: question, quote or request ID length", ErrInvalid)
	}
	if len(in.ParentIDs) == 0 || len(in.ParentIDs) > 16 {
		return in, fmt.Errorf("%w: select between 1 and 16 cards", ErrInvalid)
	}
	ids := map[string]bool{}
	for _, id := range in.ParentIDs {
		if ids[id] {
			return in, fmt.Errorf("%w: duplicate parent", ErrInvalid)
		}
		ids[id] = true
	}
	if in.Operation == "merge" && len(in.ParentIDs) < 2 {
		return in, fmt.Errorf("%w: merge requires at least two cards", ErrInvalid)
	}
	if in.Operation != "merge" && len(in.ParentIDs) != 1 {
		return in, fmt.Errorf("%w: operation requires one card", ErrInvalid)
	}
	return in, nil
}
func (cs *CardServer) Chat(ctx context.Context, owner, sessionID string, input ChatInput) (<-chan Event, error) {
	input, err := normalizeInput(input)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrConflict
	}
	v, err := s.session(owner, sessionID)
	if err != nil {
		return nil, err
	}
	if previous, exists := v.Requests[input.RequestID]; input.RequestID != "" && exists {
		a, _ := json.Marshal(previous.Input)
		b, _ := json.Marshal(input)
		if string(a) != string(b) {
			return nil, fmt.Errorf("%w: request ID was used for a different question", ErrConflict)
		}
		card, err := findCard(v, previous.CardID)
		if err != nil {
			return nil, err
		}
		if isActive(card.Status) {
			return nil, fmt.Errorf("%w: request is already generating", ErrConflict)
		}
		ch := make(chan Event, 1)
		copy := clone(*card)
		eventType := "completed"
		if copy.Status != "completed" {
			eventType = "error"
		}
		ch <- Event{Type: eventType, Card: &copy, Error: copy.Error}
		close(ch)
		return ch, nil
	}
	parents := append([]string{}, input.ParentIDs...)
	for _, id := range parents {
		card, err := findCard(v, id)
		if err != nil {
			return nil, err
		}
		if isActive(card.Status) {
			return nil, fmt.Errorf("%w: parent is still generating", ErrConflict)
		}
	}
	if input.Operation == "branch" {
		card, _ := findCard(v, parents[0])
		parents = append([]string{}, card.ParentIDs...)
		if len(parents) == 0 {
			parents = []string{v.RootID}
		}
	}
	depth := 1
	for _, id := range parents {
		p, _ := findCard(v, id)
		if p.Depth+1 > depth {
			depth = p.Depth + 1
		}
	}
	messages, memory := contextFor(v.SessionView, parents, input.Operation)
	if memory.Goal == "" {
		memory.Goal = input.Question
	}
	selectedProvider, err := s.providerForOwner(owner)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	card := Card{ID: newID(), ParentIDs: parents, Question: input.Question, Quote: input.Quote, Title: shortTitle(input.Question), Status: "pending", Operation: input.Operation, Depth: depth, CreatedAt: now, UpdatedAt: now, Suggestions: []modelServer.Suggestion{}, Memory: memory}
	reqQuestion := input.Question
	if input.Quote != "" {
		reqQuestion += "\n用户引用：\n" + input.Quote
	}
	req := modelServer.Request{Question: reqQuestion, Messages: messages, Depth: depth, Operation: input.Operation, Preferences: prefText(s.db.Preferences[owner]), Memory: memory, SystemPrompt: s.prompts[v.AppID]}
	if parent, parentErr := findCard(v, parents[0]); parentErr == nil {
		req.PreviousModel = parent.Model
	}
	if err = s.mutate(func() error {
		v.Cards = append(v.Cards, card)
		v.FocusID = card.ID
		v.UpdatedAt = now
		if len(v.Cards) == 2 && v.Title == "新的思考" {
			v.Title = shortTitle(input.Question)
		}
		if input.RequestID != "" {
			v.Requests[input.RequestID] = requestRecord{CardID: card.ID, Input: input}
		}
		s.count(owner, input.Operation)
		return nil
	}); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s.active[card.ID] = cancel
	ch := make(chan Event, 32)
	cardCopy := clone(card)
	ch <- Event{Type: "created", Card: &cardCopy}
	s.wg.Add(1)
	go s.generate(runCtx, cancel, owner, sessionID, card.ID, req, selectedProvider, ch)
	return ch, nil
}
func (s *serverState) generate(ctx context.Context, cancel context.CancelFunc, owner, sessionID, cardID string, req modelServer.Request, selectedProvider modelServer.Provider, ch chan Event) {
	defer s.wg.Done()
	defer close(ch)
	defer cancel()
	result, err := selectedProvider.Generate(ctx, req, func(delta string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		v, lookupErr := s.session(owner, sessionID)
		if lookupErr != nil {
			s.mu.Unlock()
			return lookupErr
		}
		c, lookupErr := findCard(v, cardID)
		if lookupErr != nil {
			s.mu.Unlock()
			return lookupErr
		}
		c.Status = "streaming"
		c.Answer += delta
		c.UpdatedAt = time.Now().UTC()
		s.mu.Unlock()
		select {
		case ch <- Event{Type: "delta", Delta: delta}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err == nil {
		err = ctx.Err()
	}
	s.mu.Lock()
	delete(s.active, cardID)
	v, lookupErr := s.session(owner, sessionID)
	if lookupErr != nil {
		s.mu.Unlock()
		return
	}
	card, lookupErr := findCard(v, cardID)
	if lookupErr != nil {
		s.mu.Unlock()
		return
	}
	card.UpdatedAt = time.Now().UTC()
	v.UpdatedAt = card.UpdatedAt
	eventType := "completed"
	if err != nil {
		card.Status = "error"
		card.Error = err.Error()
		eventType = "error"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			card.Status = "cancelled"
			card.Error = "生成已停止，可从此卡片继续或重新提问"
		}
	} else {
		card.Status = "completed"
		card.Answer = result.Answer
		card.Model = result.Model
		card.Suggestions = clone(result.Suggestions)
		card.Memory = clone(result.Memory)
		if card.Memory.Goal == "" {
			card.Memory.Goal = req.Memory.Goal
		}
		if strings.TrimSpace(result.Title) != "" {
			card.Title = result.Title
		}
		p := s.db.Preferences[owner]
		p.Learned = uniqueStrings(append(p.Learned, result.Memory.Preferences...), 24)
		s.db.Preferences[owner] = p
		s.count(owner, "completed")
	}
	if saveErr := s.save(); saveErr != nil {
		card.Status = "error"
		card.Error = "保存失败：" + saveErr.Error()
		eventType = "error"
	}
	copy := clone(*card)
	s.mu.Unlock()
	event := Event{Type: eventType, Card: &copy, Error: copy.Error}
	// Prefer a terminal event even when a user explicitly stopped generation.
	select {
	case ch <- event:
		return
	default:
	}
	// Cancellation must never leave a goroutine blocked on a disconnected client.
	select {
	case ch <- event:
	case <-ctx.Done():
	}
}
func (cs *CardServer) Stop(owner, sessionID, cardID string) error {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.session(owner, sessionID)
	if err != nil {
		return err
	}
	if _, err = findCard(v, cardID); err != nil {
		return err
	}
	if cancel := s.active[cardID]; cancel != nil {
		cancel()
	}
	return nil
}
