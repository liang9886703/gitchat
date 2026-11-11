package modelServer

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Suggestion struct {
	Title    string  `json:"title"`
	Question string  `json:"question"`
	Score    float64 `json:"score"`
}
type Memory struct {
	Goal        string   `json:"goal"`
	Progress    []string `json:"progress"`
	BranchIdeas []string `json:"branch_ideas"`
	Preferences []string `json:"preferences"`
}
type Request struct {
	Question      string
	Messages      []Message
	Depth         int
	Operation     string
	Preferences   string
	SystemPrompt  string
	PreviousModel string
	Memory        Memory
}
type Result struct {
	Title       string       `json:"title"`
	Answer      string       `json:"answer"`
	Model       string       `json:"model"`
	Suggestions []Suggestion `json:"suggestions"`
	Memory      Memory       `json:"memory"`
}
type Provider interface {
	Generate(context.Context, Request, func(string) error) (Result, error)
}
