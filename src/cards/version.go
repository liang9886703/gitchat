//go:build ignore
// +build ignore

package cards

import (
	"runtime"
	"time"
)

func SessionHeartbeat(app AppId, session SessionId) {
	//比较两边卡片是否一致
	//如果不一致，则增量更新，为了简单，将全量更新视为增量更新的一种特殊情况

}
func UpdateSession(app AppId, session SessionId) {

}
func DeepenSession() {
}

// 底层需要启用不同的会话id
func BranchSession() {
}

// 底层新建一个会话，提供压缩后上下文
func MergeSession() {
}

// 区分branch和deepen的意义在与merge后的节点对于客户端来说是不知道有空节点存在的，这个细节理应对前段透明
func newSession() {
}

func newApp() {
}

func newChat() {

}

// todo：定时备份工作流
func BackupWork() {
	// ✅ 更清晰的定时器使用
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// 执行定期任务
	}
}

// todo:
func optimizeMemory() {
	// 1. 清理过期的 session
	now := time.Now()
	// for sessionId, session := range s.sessions {
	//     if now.Sub(session.lastAccess) > 24*time.Hour {
	//         delete(s.sessions, sessionId)  // 删除24小时未访问的session
	//     }
	// }

	// 2. 强制 GC 回收内存，所有 goroutine 都会暂停，因此只在内存过高的时候
	runtime.GC() // 立即触发垃圾回收

	// 3. 重新分配 map（如果负载因子过低），用两个map，后续的都到新的map，旧的map每次请求移动一个，足够多次后，移除旧的map
	// if float64(len(s.sessions))/float64(cap(s.sessions)) > 0.8 {
	// 问题：Go map 没有 cap() 方法！
	// 这里应该是伪代码，实际无法获取 map 的容量

	// 4. 如果 session 数量过少，考虑重新创建 map
	// map封片，用多个map管理数据，展示没有这个需求场景
}
