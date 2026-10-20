// Package remote 提供远程 HTTP API、异步生成任务与 MCP 工具，只开放推理相关能力。
package remote

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"yovoice/internal/catalog"
	"yovoice/internal/diag"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/workbench"
)

// API 为远程客户端提供有限的推理接口，不开放桌面管理与任意路径访问。
type API struct {
	Workbench         *workbench.Workbench
	Token             string
	Context           context.Context
	GenerationTimeout time.Duration
	// Local 表示本机 stdio 模式：结果给出本机 WAV 路径，参考音频可按路径导入。
	Local      bool
	jobsMu     sync.Mutex
	jobs       map[string]*generationJob
	busy       sync.Mutex
	mcpOnce    sync.Once
	mcpHandler http.Handler
}

// statusRecorder 记下响应状态码供请求日志使用；Flush 透传给 MCP 的流式响应。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// ServeHTTP 记录写操作和失败的请求；轮询状态的成功 GET 不记，避免刷屏。只记路径，不记查询参数与请求体。
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	recorder := &statusRecorder{ResponseWriter: w}
	a.serve(recorder, r)
	status := recorder.status
	if status == 0 {
		status = http.StatusOK
	}
	if a.Workbench != nil && (r.Method != http.MethodGet || status >= 400) {
		event := "api.request"
		if status >= 500 {
			event = "api.request_failed"
		} else if status >= 400 {
			event = "api.request_rejected"
		}
		diag.Log(a.Workbench.Store.Root, event, "method", r.Method, "path", r.URL.Path, "status", status, "elapsed_ms", time.Since(started).Milliseconds(), "local", a.Local)
	}
}

func (a *API) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if serveDocs(w, r) {
		return
	}
	if len(a.Token) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.Token)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Origin") != "" && !sameOrigin(r) {
		http.Error(w, "浏览器跨域调用不受支持", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/mcp" {
		a.mcpOnce.Do(func() { a.mcpHandler = a.newMCPHandler() })
		a.mcpHandler.ServeHTTP(w, r)
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/v1/jobs/"):
		a.jobHTTP(w, r)
	case r.Method == "GET" && r.URL.Path == "/v1/status":
		state := a.Workbench.Store.Read()
		apiJSON(w, map[string]any{"engineVersion": catalog.EngineVersion, "engineMinimum": catalog.EngineMinimum, "activity": state.Activity, "modelActivity": state.ModelActivity, "runtimeVersion": state.RuntimeVersion, "ready": workbench.RuntimeReady(state)})
	case r.Method == "GET" && r.URL.Path == "/v1/models":
		apiJSON(w, map[string]any{"catalog": catalog.Models, "installed": a.Workbench.Store.Read().Models, "generationOptions": catalog.GenerationOptions})
	case r.Method == "GET" && r.URL.Path == "/v1/voices":
		apiJSON(w, a.Workbench.Store.Read().Voices)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/audio/"):
		a.audio(w, r, strings.TrimPrefix(r.URL.Path, "/v1/audio/"))
	case r.Method == "POST" && (r.URL.Path == "/v1/voices" || r.URL.Path == "/v1/generate"):
		// 单个引擎只运行一个请求，繁忙时直接拒绝，不积压无限队列。
		if !a.busy.TryLock() {
			http.Error(w, errBusy.Error(), http.StatusConflict)
			return
		}
		defer a.busy.Unlock()
		if r.URL.Path == "/v1/voices" {
			a.upload(w, r)
		} else {
			a.generate(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

func apiJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	voice, err := a.Workbench.ImportVoiceFrom(r.Context(), r.Body, r.URL.Query().Get("name"))
	if err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		var callErr *msg.CallError
		if errors.As(err, &tooLarge) || (errors.As(err, &callErr) && callErr.Code == msg.ErrAudioTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}
	apiJSON(w, voice)
}

func (a *API) generate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d, err := decodeGeneration(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	g, err := a.generateAudio(r.Context(), d)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		} else if errors.Is(err, errBusy) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	a.audio(w, r, g.ID)
}

// decodeGeneration 解析 HTTP 请求体中的生成参数，规则与 MCP 工具共用（见 workbench.DecodeRemoteGeneration）。
func decodeGeneration(body io.Reader) (schema.Draft, error) {
	return workbench.DecodeRemoteGeneration(body)
}

var errBusy = errors.New("推理服务繁忙，请稍后重试")

func (a *API) generateAudio(ctx context.Context, d schema.Draft) (schema.Generation, error) {
	ctx, cancel := context.WithTimeout(ctx, a.generationTimeout())
	defer cancel()
	g, err := a.Workbench.Synthesize(ctx, d)
	// 桌面应用内置接口与界面共用引擎，界面正在生成或下载时同样按繁忙处理。
	var callErr *msg.CallError
	if errors.As(err, &callErr) && callErr.Code == msg.ErrBusy {
		return g, errBusy
	}
	return g, err
}

func (a *API) audio(w http.ResponseWriter, r *http.Request, id string) {
	path, err := a.Workbench.MediaFile("outputs", id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.wav"`)
	w.Header().Set("X-Yovoice-Generation-ID", id)
	// 隐藏 io.ReaderFrom，避免 Windows sendfile 与连接后台读在 Go 1.26 中的数据竞争。
	http.ServeFile(struct{ http.ResponseWriter }{w}, r, path)
}
