package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"yovoice/internal/workbench"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServeCommand(t *testing.T) {
	token := strings.Repeat("b", 32)
	t.Setenv("YOVOICE_API_TOKEN", token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	result := make(chan error, 1)
	go func() {
		defer writer.Close()
		result <- run(ctx, []string{"serve", "--listen", "127.0.0.1:0", "--data-dir", t.TempDir()}, writer, io.Discard)
	}()
	var ready struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(reader).Decode(&ready); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", ready.URL+"/v1/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("API 未退出")
	}
	if err := run(context.Background(), []string{"serve", "--generation-timeout", "0s", "--data-dir", t.TempDir()}, io.Discard, io.Discard); err == nil {
		t.Fatal("无效生成超时未拒绝")
	}

	t.Setenv("YOVOICE_API_TOKEN", "")
	if err := run(context.Background(), []string{"serve", "--data-dir", t.TempDir()}, io.Discard, io.Discard); err == nil {
		t.Fatal("缺少凭证仍启动服务")
	}
}

func TestMCPCommand(t *testing.T) {
	wb, err := workbench.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer wb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	result := make(chan error, 1)
	go func() { result <- serveMCP(ctx, wb, time.Minute, serverSide) }()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	status, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "status", Arguments: map[string]any{}})
	if err != nil || status.IsError {
		t.Fatal(err, status)
	}
	// 客户端断开后命令结束，不残留进程。
	session.Close()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("MCP 未随客户端断开退出")
	}
	if err := run(context.Background(), []string{"mcp", "--generation-timeout", "0s", "--data-dir", t.TempDir()}, io.Discard, io.Discard); err == nil {
		t.Fatal("无效生成超时未拒绝")
	}
}
