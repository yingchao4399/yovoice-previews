package desktop

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/msg"
	"yovoice/internal/workbench"
)

// freePort 找一个当前空闲的本机端口。
func freePort(t *testing.T) int {
	t.Helper()
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	must(t, e)
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func localStatus(t *testing.T, s *Server, method string, data any) LocalAPIStatus {
	t.Helper()
	raw, e := json.Marshal(data)
	must(t, e)
	result, e := s.call(context.Background(), method, raw)
	must(t, e)
	return result.(LocalAPIStatus)
}

func apiStatus(t *testing.T, url, token string) int {
	t.Helper()
	r, e := http.NewRequest("GET", url+"/v1/status", nil)
	must(t, e)
	r.Header.Set("Authorization", "Bearer "+token)
	res, e := http.DefaultClient.Do(r)
	must(t, e)
	res.Body.Close()
	return res.StatusCode
}

func TestLocalAPILifecycle(t *testing.T) {
	root := t.TempDir()
	w, e := workbench.New(root)
	must(t, e)
	defer w.Close()
	local := &LocalAPI{Workbench: w}
	defer local.Close()
	s := &Server{Workbench: w, LocalAPI: local}

	status := localStatus(t, s, "localApi.get", nil)
	if status.Enabled || status.Running || status.Port != workbench.LocalAPIDefaultPort || len(status.Token) < 32 {
		t.Fatalf("默认状态异常：%+v", status)
	}
	// 令牌保存在状态库的私有设置中，不随状态广播给界面。
	if b, _ := json.Marshal(w.Store.Read()); strings.Contains(string(b), status.Token) {
		t.Fatal("令牌不应出现在状态中")
	}
	info, e := os.Stat(filepath.Join(root, "yovoice.db"))
	must(t, e)
	if info.Mode().Perm()&0077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("状态库权限过宽：%v", info.Mode())
	}

	port := freePort(t)
	status = localStatus(t, s, "localApi.save", map[string]any{"enabled": true, "port": port})
	if !status.Running || status.Error != nil || status.URL == "" {
		t.Fatalf("开启后未运行：%+v", status)
	}
	if code := apiStatus(t, status.URL, status.Token); code != 200 {
		t.Fatalf("接口状态码 %d", code)
	}
	if code := apiStatus(t, status.URL, "wrong-token-wrong-token-wrong-token"); code != 401 {
		t.Fatalf("错误令牌状态码 %d", code)
	}

	// 重置令牌后旧令牌失效，新令牌立即可用。
	old := status.Token
	status = localStatus(t, s, "localApi.resetToken", nil)
	if status.Token == old || !status.Running {
		t.Fatalf("重置令牌异常：%+v", status)
	}
	if code := apiStatus(t, status.URL, old); code != 401 {
		t.Fatalf("旧令牌仍可用：%d", code)
	}
	if code := apiStatus(t, status.URL, status.Token); code != 200 {
		t.Fatalf("新令牌不可用：%d", code)
	}

	// 重启服务后按已保存的设置自动开启。
	local.Close()
	again := &LocalAPI{Workbench: w}
	defer again.Close()
	must(t, again.Apply())
	restored, e := again.Status()
	must(t, e)
	if !restored.Running || restored.Token != status.Token || restored.Port != port {
		t.Fatalf("重启后未恢复：%+v", restored)
	}
	s.LocalAPI = again

	// 端口被占用时设置仍保存，状态带出可翻译的错误。
	busy, e := net.Listen("tcp4", "127.0.0.1:0")
	must(t, e)
	defer busy.Close()
	taken := busy.Addr().(*net.TCPAddr).Port
	status = localStatus(t, s, "localApi.save", map[string]any{"enabled": true, "port": taken})
	if status.Running || status.Port != taken || status.Error == nil || status.Error["code"] != msg.ErrLocalAPIPortInUse {
		t.Fatalf("端口占用未提示：%+v", status)
	}

	status = localStatus(t, s, "localApi.save", map[string]any{"enabled": false, "port": port})
	if status.Running || status.Enabled || status.Error != nil {
		t.Fatalf("关闭后仍在运行：%+v", status)
	}
	if _, e = s.call(context.Background(), "localApi.save", json.RawMessage(`{"enabled":true,"port":80}`)); e == nil {
		t.Fatal("系统端口未拒绝")
	}
}
