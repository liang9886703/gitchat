package modelServer

import (
	"context"
	"fmt"
	"strings"
)

func demo(ctx context.Context, r Request, emit func(string) error) (Result, error) {
	subject := limitRunes(strings.TrimSpace(r.Question), 40)
	answer := fmt.Sprintf("先把「%s」拆成一个可以判断的小问题。\n\n你希望最后得到什么结果？先选定目标，再列出已经知道的条件，以及最需要确认的一点。你可以沿一个方向继续追问，也可以保留多个分支作比较。", subject)
	suggestions := []Suggestion{{Title: "明确目标", Question: "关于“" + subject + "”，怎样明确目标和判断标准？", Score: .95}, {Title: "梳理约束", Question: "关于“" + subject + "”，有哪些条件和限制需要先确认？", Score: .85}, {Title: "比较方案", Question: "关于“" + subject + "”，有哪些不同的可行方案？", Score: .75}}
	text := strings.ToLower(r.Question)
	switch {
	case strings.Contains(text, "日本") || strings.Contains(text, "旅行") || strings.Contains(text, "旅游"):
		answer = "先确定这次旅行最想获得的体验。可以把计划拆成美食、文化与交通预算三个方向，各自探索后再合并。\n\n从你最在意的一项开始，并补充天数、出发地和大致预算。这样后续路线才有可比较的依据。"
		suggestions = []Suggestion{{Title: "美食路线", Question: "这次旅行想以美食为主，应该怎样筛选城市和用餐体验？", Score: .96}, {Title: "文化景点", Question: "这次旅行想了解当地文化，如何选择景点和安排节奏？", Score: .9}, {Title: "交通预算", Question: "如何在交通便利、停留时间和预算之间做权衡？", Score: .8}}
	case strings.Contains(text, "架构") || strings.Contains(text, "技术"):
		answer = "先明确系统必须满足的一项约束，例如延迟、可靠性或交付周期。然后把候选架构放在独立分支讨论，用相同的标准比较。\n\n可以先写下当前规模、未来增长和团队维护能力。已有方案能否满足核心约束，是第一步需要验证的问题。"
		suggestions = []Suggestion{{Title: "沿用现有方案", Question: "沿用现有架构时，怎样验证容量与可靠性边界？", Score: .95}, {Title: "引入新组件", Question: "引入新组件需要验证哪些能力和维护成本？", Score: .86}, {Title: "对比实施成本", Question: "不同架构如何比较交付周期、迁移风险和维护成本？", Score: .8}}
	}
	if r.Operation == "merge" {
		var points []string
		for _, m := range r.Messages {
			if m.Role == "assistant" {
				points = append(points, limitRunes(m.Content, 100))
			}
		}
		if len(points) > 4 {
			points = points[len(points)-4:]
		}
		answer = "把已选择的分支放到同一组标准下比较。先确认它们共同满足的目标，再保留各分支独有的优势、限制和待验证事项。"
		if len(points) > 0 {
			answer += "\n\n已有分支要点\n• " + strings.Join(points, "\n• ")
		}
		answer += "\n\n下一步可以按成本、风险和时间排序，并说明哪项约束最重要。"
	}
	goal := r.Memory.Goal
	if goal == "" {
		goal = r.Question
	}
	result := Result{Title: subject, Answer: answer, Model: "demo", Suggestions: suggestions, Memory: Memory{Goal: goal, Progress: []string{subject}, BranchIdeas: []string{suggestions[0].Title, suggestions[1].Title}}}
	if err := emitAnswer(ctx, result.Answer, emit); err != nil {
		return Result{}, err
	}
	return result, nil
}
