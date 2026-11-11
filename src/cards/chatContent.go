package cards

import (
	"fmt"
	"gitchat/src/modelServer"
	"strings"
)

// ancestors returns the induced ancestor subgraph in stable topological order.
// Shared ancestors of a merge appear once, and unrelated siblings never enter context.
func ancestors(v SessionView, ids []string) []Card {
	byID := map[string]Card{}
	for _, c := range v.Cards {
		byID[c.ID] = c
	}
	selected := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if selected[id] {
			return
		}
		c, ok := byID[id]
		if !ok {
			return
		}
		selected[id] = true
		for _, p := range c.ParentIDs {
			visit(p)
		}
	}
	for _, id := range ids {
		visit(id)
	}
	out := []Card{}
	for _, c := range v.Cards {
		if selected[c.ID] {
			out = append(out, c)
		}
	}
	return out
}
func contextFor(v SessionView, parents []string, operation string) ([]modelServer.Message, modelServer.Memory) {
	messages := []modelServer.Message{}
	if operation == "merge" {
		messages = append(messages, modelServer.Message{Role: "system", Content: "用户正在合并独立思考分支。以下节点标注了父节点，请对比不同分支的共同点、差异、冲突与待确认事项，保留各分支来源，不将互斥方案当成同时成立的事实。"})
	}
	var memory modelServer.Memory
	for _, c := range ancestors(v, parents) {
		if c.Operation == "root" {
			continue
		}
		question := c.Question
		if c.Quote != "" {
			question += "\n引用内容：\n" + c.Quote
		}
		if operation == "merge" {
			question = fmt.Sprintf("[节点 %s，父节点 %s]\n%s", c.ID, strings.Join(c.ParentIDs, ","), question)
		}
		messages = append(messages, modelServer.Message{Role: "user", Content: question}, modelServer.Message{Role: "assistant", Content: c.Answer})
		if memory.Goal == "" {
			memory.Goal = c.Memory.Goal
		}
		memory.Progress = uniqueStrings(append(memory.Progress, c.Memory.Progress...), 32)
		memory.BranchIdeas = uniqueStrings(append(memory.BranchIdeas, c.Memory.BranchIdeas...), 24)
	}
	return messages, memory
}
func uniqueStrings(values []string, max int) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}
func prefText(p Preferences) string {
	return fmt.Sprintf("表达风格：%s\n兴趣：%s\n常见任务：%s\n已记住的偏好：%s", p.Tone, strings.Join(p.Interests, "、"), strings.Join(p.CommonTasks, "、"), strings.Join(p.Learned, "、"))
}
