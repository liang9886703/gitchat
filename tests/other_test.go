package main

import (
	"strings"
	"testing"
)

// 测试：多个 goroutine 同时计数
func testBadCounter() {
	builder1 := strings.Builder{}
	builder1.WriteString("Hello")

	builder2 := builder1 // 值拷贝
	builder2.WriteString("World")
}
func TestBadCounter(t *testing.T) {
	testBadCounter()
}
