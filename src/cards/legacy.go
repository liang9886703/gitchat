package cards

import (
	"context"
	"fmt"
	"gitchat/src/modelServer"
)

// The original Go API uses one local workspace. HTTP handlers use explicit owners.
func (cs *CardServer) CreateNewSessionByApp(appID any) (SessionId, Version, error) {
	id := ""
	if v, ok := appID.(AppId); ok {
		id = v.MainId
	}
	found := false
	for _, app := range cs.Apps() {
		if app.ID == id {
			found = true
		}
	}
	if !found {
		id = ""
	}
	v, err := cs.NewSession("local", id, "")
	return SessionId{v.ID}, Version{v.RootID}, err
}
func (cs *CardServer) FirstStreamChatOnce(id SessionId, version Version, message string) (modelServer.RecvStreamContext, Version, error) {
	return cs.legacyStream(id, version, message)
}
func (cs *CardServer) NextStreamChatOnce(id SessionId, version Version, message string) (modelServer.RecvStreamContext, Version, error) {
	return cs.legacyStream(id, version, message)
}
func (cs *CardServer) legacyStream(id SessionId, version Version, message string) (modelServer.RecvStreamContext, Version, error) {
	ctx, cancel := context.WithCancel(context.Background())
	events, err := cs.Chat(ctx, "local", id.MainId, ChatInput{Operation: "deepen", ParentIDs: []string{version.MainId}, Question: message})
	if err != nil {
		cancel()
		return modelServer.RecvStreamContext{}, version, err
	}
	first, ok := <-events
	if !ok || first.Card == nil {
		cancel()
		return modelServer.RecvStreamContext{}, version, fmt.Errorf("missing initial card")
	}
	nextVersion := Version{first.Card.ID}
	ch := make(chan modelServer.StreamMessage, 32)
	go func() {
		defer close(ch)
		defer cancel()
		for event := range events {
			var msg modelServer.StreamMessage
			switch event.Type {
			case "delta":
				msg = modelServer.StreamMessage{Type: modelServer.SendStreamContextTypeStream, Message: event.Delta, Question: message}
			case "completed":
				msg = modelServer.StreamMessage{Type: modelServer.SendStreamContextTypeEnd, Message: "[STREAM_END]", FullMsg: event.Card.Answer, Question: message}
			case "error":
				msg = modelServer.StreamMessage{Type: modelServer.SendStreamContextTypeError, Message: event.Error, Question: message}
			default:
				continue
			}
			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	return modelServer.RecvStreamContext{Ctx: &ctx, Cancel: &cancel, Ch: &ch}, nextVersion, nil
}
