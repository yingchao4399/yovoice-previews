package remote

import (
	"context"
	"net/http"
	"yovoice/internal/schema"
	"yovoice/internal/workbench"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newMCPHandler 由官方 SDK 处理协议协商与传输，工具与 HTTP 共用实例和并发限制。
func (a *API) newMCPHandler() http.Handler {
	server := a.newMCPServer()
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 28 << 20, PropagateRequestCancellation: true})
}

// RunMCP 在给定传输上运行 MCP，直到客户端断开或 ctx 取消；命令行以标准输入输出启动。
func (a *API) RunMCP(ctx context.Context, transport mcp.Transport) error {
	return a.newMCPServer().Run(ctx, transport)
}

// audioResult 返回生成结果：远程客户端经 downloadPath 下载，本机模式直接给出 WAV 路径。
func (a *API) audioResult(g schema.Generation) map[string]any {
	if a.Local {
		path, err := a.Workbench.MediaFile("outputs", g.ID)
		if err == nil {
			return map[string]any{"id": g.ID, "duration": g.Duration, "path": path}
		}
	}
	return map[string]any{"id": g.ID, "duration": g.Duration, "downloadPath": "/v1/audio/" + g.ID}
}

// newMCPServer 注册对外 MCP 工具；定义统一放在 workbench，传输相关的互斥、任务与路径在这里注入。
func (a *API) newMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "yovoice", Version: "1.0.0"}, nil)
	a.Workbench.RegisterRemoteTools(server, workbench.RemoteTools{
		Local:    a.Local,
		TryLock:  a.busy.TryLock,
		Unlock:   a.busy.Unlock,
		Generate: a.generateAudio,
		Submit:   func(id string, d schema.Draft) (any, error) { return a.submitJob(id, d) },
		Get:      func(id string, cancel bool) (any, error) { return a.getJob(id, cancel) },
		Audio:    a.audioResult,
	})
	return server
}
