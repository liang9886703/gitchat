package cards

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

func (cs *CardServer) PatchCard(owner, sessionID, cardID string, patch CardPatch) (Card, error) {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.session(owner, sessionID)
	if err != nil {
		return Card{}, err
	}
	c, err := findCard(v, cardID)
	if err != nil {
		return Card{}, err
	}
	if c.ID == v.RootID {
		return Card{}, fmt.Errorf("%w: root cannot be marked", ErrInvalid)
	}
	if patch.Archived != nil && *patch.Archived && isActive(c.Status) {
		return Card{}, fmt.Errorf("%w: card is generating", ErrConflict)
	}
	err = s.mutate(func() error {
		if patch.Favorite != nil {
			c.Favorite = *patch.Favorite
			if *patch.Favorite {
				s.count(owner, "favorite")
			}
		}
		if patch.Success != nil {
			c.Success = *patch.Success
			if *patch.Success {
				s.count(owner, "success")
			}
		}
		if patch.Archived != nil {
			c.Archived = *patch.Archived
			if *patch.Archived {
				s.count(owner, "archive")
			} else {
				s.count(owner, "restore")
			}
		}
		c.UpdatedAt = time.Now().UTC()
		v.UpdatedAt = c.UpdatedAt
		return nil
	})
	if err != nil {
		return Card{}, err
	}
	return clone(*c), nil
}
func (cs *CardServer) Focus(owner, sessionID, cardID string) error {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.session(owner, sessionID)
	if err != nil {
		return err
	}
	focus, err := findCard(v, cardID)
	if err != nil {
		return err
	}
	return s.mutate(func() error {
		v.FocusID = cardID
		focus.Archived = false
		focus.UpdatedAt = time.Now().UTC()
		v.UpdatedAt = focus.UpdatedAt
		s.count(owner, "focus")
		// Preserve the current path, valuable conclusions, and recent work. Distance,
		// elapsed time, and weight jointly determine which cards leave the focus view.
		keep := map[string]bool{}
		for _, c := range ancestors(v.SessionView, []string{cardID}) {
			keep[c.ID] = true
		}
		distances := graphDistances(v.SessionView, cardID)
		for i := range v.Cards {
			c := &v.Cards[i]
			distance := distances[c.ID]
			if !keep[c.ID] && distance > 3 && time.Since(c.UpdatedAt) > 24*time.Hour && !c.Favorite && !c.Success && !isActive(c.Status) {
				c.Archived = true
			}
		}
		return nil
	})
}
func nodeSnapshot(v SessionView, cardID string) (SessionView, error) {
	exists := false
	for _, c := range v.Cards {
		if c.ID == cardID {
			exists = true
		}
	}
	if !exists {
		return SessionView{}, ErrNotFound
	}
	v = clone(v)
	v.Cards = ancestors(v, []string{cardID})
	v.FocusID = cardID
	for _, c := range v.Cards {
		if isActive(c.Status) {
			return SessionView{}, fmt.Errorf("%w: wait for generation before copying or sharing", ErrConflict)
		}
	}
	return v, nil
}
func (s *serverState) copySnapshot(owner string, v SessionView) SessionView {
	ids := map[string]string{}
	for _, c := range v.Cards {
		ids[c.ID] = newID()
	}
	for i := range v.Cards {
		c := &v.Cards[i]
		c.ID = ids[c.ID]
		for j, p := range c.ParentIDs {
			c.ParentIDs[j] = ids[p]
		}
	}
	v.ID = newID()
	v.RootID = ids[v.RootID]
	v.FocusID = ids[v.FocusID]
	v.Title += " · 副本"
	v.CreatedAt = time.Now().UTC()
	v.UpdatedAt = v.CreatedAt
	if _, ok := s.prompts[v.AppID]; !ok {
		v.AppID = s.apps[0].ID
	}
	s.db.Sessions[v.ID] = &storedSession{SessionView: v, Owner: owner, Requests: map[string]requestRecord{}}
	return v
}
func (cs *CardServer) CopySession(owner, sessionID, cardID string) (SessionView, error) {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.session(owner, sessionID)
	if err != nil {
		return SessionView{}, err
	}
	snap, err := nodeSnapshot(v.SessionView, cardID)
	if err != nil {
		return SessionView{}, err
	}
	var copy SessionView
	err = s.mutate(func() error { copy = s.copySnapshot(owner, snap); s.count(owner, "copy"); return nil })
	return clone(copy), err
}
func (cs *CardServer) Share(owner, sessionID, cardID string) (string, error) {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.session(owner, sessionID)
	if err != nil {
		return "", err
	}
	snap, err := nodeSnapshot(v.SessionView, cardID)
	if err != nil {
		return "", err
	}
	token := newID()
	err = s.mutate(func() error { s.db.Shares[token] = snap; s.count(owner, "share"); return nil })
	if err != nil {
		return "", err
	}
	return token, nil
}
func (cs *CardServer) GetShare(token string) (SessionView, error) {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.db.Shares[token]
	if !ok {
		return SessionView{}, ErrNotFound
	}
	return clone(v), nil
}
func (cs *CardServer) ImportShare(owner, token string) (SessionView, error) {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(owner) == "" {
		return SessionView{}, ErrInvalid
	}
	v, ok := s.db.Shares[token]
	if !ok {
		return SessionView{}, ErrNotFound
	}
	var copy SessionView
	err := s.mutate(func() error { copy = s.copySnapshot(owner, clone(v)); s.count(owner, "import"); return nil })
	return clone(copy), err
}
func (cs *CardServer) GetPreferences(owner string) Preferences {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.db.Preferences[owner])
}
func (cs *CardServer) SetPreferences(owner string, p Preferences) error {
	if strings.TrimSpace(owner) == "" || utf8.RuneCountInString(p.Tone) > 500 || len(p.Interests) > 32 || len(p.CommonTasks) > 32 || len(p.Learned) > 32 {
		return ErrInvalid
	}
	for _, values := range [][]string{p.Interests, p.CommonTasks, p.Learned} {
		for _, v := range values {
			if utf8.RuneCountInString(v) > 500 {
				return ErrInvalid
			}
		}
	}
	p.Tone = strings.TrimSpace(p.Tone)
	p.Interests = uniqueStrings(p.Interests, 32)
	p.CommonTasks = uniqueStrings(p.CommonTasks, 32)
	p.Learned = uniqueStrings(p.Learned, 32)
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(func() error { s.db.Preferences[owner] = clone(p); return nil })
}
func (cs *CardServer) Stats(owner string) map[string]int {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db.Stats[owner] == nil {
		return map[string]int{}
	}
	return clone(s.db.Stats[owner])
}

// graphDistances measures hops, including lateral travel through shared parents.
func graphDistances(v SessionView, start string) map[string]int {
	adjacent := map[string][]string{}
	for _, c := range v.Cards {
		for _, p := range c.ParentIDs {
			adjacent[c.ID] = append(adjacent[c.ID], p)
			adjacent[p] = append(adjacent[p], c.ID)
		}
	}
	distance := map[string]int{start: 0}
	queue := []string{start}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[id] {
			if _, seen := distance[next]; !seen {
				distance[next] = distance[id] + 1
				queue = append(queue, next)
			}
		}
	}
	return distance
}
