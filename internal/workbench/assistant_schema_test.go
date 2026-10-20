package workbench

import "testing"

// 每个工具的输入必须能由MCP推导成有效JSON Schema，不能等助手启动时才发现。
func TestAssistantToolSchemasRegisterWithoutPanic(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	w.newAssistantServer()
}
