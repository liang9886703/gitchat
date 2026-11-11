package cards

import (
	"errors"
	"gitchat/src/modelServer"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("resource not found")
	ErrInvalid  = errors.New("invalid request")
	ErrConflict = errors.New("operation conflicts with current state")
)

type AppId struct{ MainId string }
type SessionId struct{ MainId string }
type Version struct{ MainId string }

func (v AppId) IsEmpty() bool                { return strings.TrimSpace(v.MainId) == "" }
func (v SessionId) IsEmpty() bool            { return strings.TrimSpace(v.MainId) == "" }
func (v Version) IsEmpty() bool              { return strings.TrimSpace(v.MainId) == "" }
func (v AppId) String() string               { return v.MainId }
func (v SessionId) String() string           { return v.MainId }
func (v Version) String() string             { return v.MainId }
func AppIdFromString(s string) AppId         { return AppId{strings.TrimSpace(s)} }
func SessionIdFromString(s string) SessionId { return SessionId{strings.TrimSpace(s)} }
func VersionFromString(s string) Version     { return Version{strings.TrimSpace(s)} }

type ChatCompletionMessage = modelServer.ChatCompletionMessage

type Card struct {
	ID          string                   `json:"id"`
	ParentIDs   []string                 `json:"parent_ids"`
	Question    string                   `json:"question"`
	Quote       string                   `json:"quote,omitempty"`
	Answer      string                   `json:"answer"`
	Title       string                   `json:"title"`
	Status      string                   `json:"status"`
	Operation   string                   `json:"operation"`
	Model       string                   `json:"model"`
	Depth       int                      `json:"depth"`
	Suggestions []modelServer.Suggestion `json:"suggestions"`
	Favorite    bool                     `json:"favorite"`
	Archived    bool                     `json:"archived"`
	Success     bool                     `json:"success"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
	Memory      modelServer.Memory       `json:"memory"`
	Error       string                   `json:"error,omitempty"`
}
type SessionView struct {
	ID        string    `json:"id"`
	AppID     string    `json:"app_id"`
	Title     string    `json:"title"`
	RootID    string    `json:"root_id"`
	FocusID   string    `json:"focus_id"`
	Cards     []Card    `json:"cards"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type AppView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Event struct {
	Type  string `json:"type"`
	Card  *Card  `json:"card,omitempty"`
	Delta string `json:"delta,omitempty"`
	Error string `json:"error,omitempty"`
}
type ChatInput struct {
	Operation string   `json:"operation"`
	ParentIDs []string `json:"parent_ids"`
	Question  string   `json:"question"`
	Quote     string   `json:"quote"`
	RequestID string   `json:"request_id"`
}
type CardPatch struct {
	Favorite *bool `json:"favorite"`
	Archived *bool `json:"archived"`
	Success  *bool `json:"success"`
}
type Preferences struct {
	Tone        string   `json:"tone"`
	Interests   []string `json:"interests"`
	CommonTasks []string `json:"common_tasks"`
	Learned     []string `json:"learned,omitempty"`
}
