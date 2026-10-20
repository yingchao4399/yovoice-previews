package desktop

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
	"yovoice/internal/diag"
	"yovoice/internal/msg"
	"yovoice/internal/remote"
	"yovoice/internal/workbench"
)

// LocalAPI 按设置在 127.0.0.1 上运行远程 HTTP / MCP 接口，供本机其他软件调用推理。
// 与桌面界面共用同一个 Workbench，推理仍由 Workbench 串行调度。
type LocalAPI struct {
	Workbench *workbench.Workbench
	// Context 在服务退出时取消，同时中止接口发起的生成任务。
	Context context.Context
	mu      sync.Mutex
	server  *http.Server
	stop    context.CancelFunc
	port    int
	token   string
	// failure 记录最近一次启动失败，界面据此提示端口被占用。
	failure error
	closed  bool
}

// LocalAPIStatus 是设置页展示的接口状态；URL 只在运行时给出。
type LocalAPIStatus struct {
	Enabled bool           `json:"enabled"`
	Port    int            `json:"port"`
	Token   string         `json:"token"`
	Running bool           `json:"running"`
	URL     string         `json:"url,omitempty"`
	Error   map[string]any `json:"error,omitempty"`
}

// Apply 读取已保存的设置并启动、重启或停止接口；端口被占用时返回错误并保持停止。
func (l *LocalAPI) Apply() error {
	settings, e := l.Workbench.LocalAPI()
	if e != nil {
		return e
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	if l.server != nil && settings.Enabled && l.port == settings.Port && l.token == settings.Token {
		return nil
	}
	l.shutdown()
	l.failure = nil
	if !settings.Enabled {
		return nil
	}
	listener, e := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(settings.Port)))
	if e != nil {
		diag.Log(l.Workbench.Store.Root, "local_api.listen_failed", "port", settings.Port, "error", diag.Error(e))
		l.failure = msg.Err(msg.ErrLocalAPIPortInUse, msg.Params{"port": settings.Port})
		return l.failure
	}
	parent := l.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	handler := &remote.API{Workbench: l.Workbench, Token: settings.Token, Context: ctx}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	l.server, l.stop, l.port, l.token = server, cancel, settings.Port, settings.Token
	go func() {
		if e := server.Serve(listener); e != nil && e != http.ErrServerClosed {
			diag.Log(l.Workbench.Store.Root, "local_api.serve_failed", "error", diag.Error(e))
		}
	}()
	diag.Log(l.Workbench.Store.Root, "local_api.started", "port", settings.Port)
	return nil
}

// Status 返回当前设置与运行状态。
func (l *LocalAPI) Status() (LocalAPIStatus, error) {
	settings, e := l.Workbench.LocalAPI()
	if e != nil {
		return LocalAPIStatus{}, e
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	status := LocalAPIStatus{Enabled: settings.Enabled, Port: settings.Port, Token: settings.Token, Running: l.server != nil}
	if status.Running {
		status.URL = "http://127.0.0.1:" + strconv.Itoa(l.port)
	}
	if l.failure != nil {
		status.Error = msg.Encode(l.failure)
	}
	return status, nil
}

// Close 停止接口并取消进行中的接口任务，之后不再启动。
func (l *LocalAPI) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	l.shutdown()
}

// shutdown 先取消接口任务，再给连接短暂收尾时间，避免同步生成拖住设置保存或退出。
func (l *LocalAPI) shutdown() {
	if l.server == nil {
		return
	}
	l.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if e := l.server.Shutdown(ctx); e != nil {
		_ = l.server.Close()
	}
	l.server, l.stop, l.port, l.token = nil, nil, 0, ""
}

// call 处理设置页的本机接口调用；保存失败（如端口占用）时设置仍会保存，状态中带出错误。
func (l *LocalAPI) call(method string, enabled bool, port int) (any, error) {
	var e error
	switch method {
	case "localApi.get":
	case "localApi.save":
		_, e = l.Workbench.SaveLocalAPI(enabled, port)
	case "localApi.resetToken":
		_, e = l.Workbench.ResetLocalAPIToken()
	default:
		return nil, msg.Err(msg.ErrMethodUnsupported, nil)
	}
	if e != nil {
		return nil, e
	}
	if method != "localApi.get" {
		if e = l.Apply(); e != nil {
			if _, ok := e.(*msg.CallError); !ok {
				return nil, e
			}
		}
	}
	return l.Status()
}
