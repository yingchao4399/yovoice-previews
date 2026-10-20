package workbench

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"yovoice/internal/diag"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// OrcaRouter 一键授权（PKCE）：本机临时监听回环端口，浏览器登录同意后回调带回 code，
// 由本地服务用 verifier 换取 API 密钥并添加为当前 AI 服务，默认使用免费模型，用户无需复制密钥。
const (
	orcaPreset    = "orcarouter"
	orcaFreeModel = "orcarouter/free"
	orcaRef       = "ref_6e57c0a95de534ec77da"
	orcaAppName   = "yovoice"
	// orcaCallbackPath 是在 OrcaRouter 登记的回环回调路径，只有端口可以变化。
	orcaCallbackPath = "/cb"
	orcaWait         = 10 * time.Minute
)

// 授权页与换取密钥的地址；测试替换为本地服务。
var (
	orcaAuthURL = "https://www.orcarouter.ai/auth"
	orcaKeysURL = "https://api.orcarouter.ai/api/v1/auth/keys"
)

// connectSession 是一次进行中的授权：同一时间只保留一个，新的授权会关闭旧的监听。
type connectSession struct {
	server *http.Server
	done   chan struct{}
	once   sync.Once
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ConnectOrcaRouter 开始一次授权并返回授权页地址，界面在系统浏览器中打开它；
// 回调完成后服务出现在状态里并成为当前服务。等待超过 10 分钟自动结束。
func (w *Workbench) ConnectOrcaRouter() (string, error) {
	verifier, e := randomToken()
	if e != nil {
		return "", e
	}
	state, e := randomToken()
	if e != nil {
		return "", e
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return "", msg.Err(msg.ErrAINetwork, msg.Params{"detail": e.Error()})
	}
	callback := fmt.Sprintf("http://127.0.0.1:%d%s", listener.Addr().(*net.TCPAddr).Port, orcaCallbackPath)
	sum := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"callback_url":          {callback},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"app_name":              {orcaAppName},
		"scope":                 {"api"},
		"ref":                   {orcaRef},
	}
	session := &connectSession{done: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc(orcaCallbackPath, func(rw http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// state 不一致的回调不是本次授权发起的，直接拒绝且不结束等待。
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			diag.Log(w.Store.Root, "ai.connect_rejected", "reason", "state")
			connectPage(rw, http.StatusBadRequest, false)
			return
		}
		ok := false
		if code := q.Get("code"); code != "" {
			ok = w.finishOrcaRouter(r.Context(), code, verifier) == nil
		} else {
			diag.Log(w.Store.Root, "ai.connect_denied", "error", q.Get("error"))
		}
		connectPage(rw, http.StatusOK, ok)
		// 回调只用一次：响应发出后关闭监听。
		go w.closeConnect(session)
	})
	session.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	w.ai.mu.Lock()
	previous := w.ai.connect
	w.ai.connect = session
	w.ai.mu.Unlock()
	if previous != nil {
		w.closeConnect(previous)
	}
	go func() { _ = session.server.Serve(listener) }()
	go func() {
		select {
		case <-session.done:
		case <-time.After(orcaWait):
			w.closeConnect(session)
		}
	}()
	diag.Log(w.Store.Root, "ai.connect_started", "preset", orcaPreset)
	return orcaAuthURL + "?" + query.Encode(), nil
}

func (w *Workbench) closeConnect(session *connectSession) {
	w.ai.mu.Lock()
	if w.ai.connect == session {
		w.ai.connect = nil
	}
	w.ai.mu.Unlock()
	session.once.Do(func() {
		close(session.done)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = session.server.Shutdown(ctx)
	})
}

// finishOrcaRouter 用 code 与 verifier 换取密钥，添加或更新 OrcaRouter 服务并设为当前服务。
func (w *Workbench) finishOrcaRouter(ctx context.Context, code, verifier string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	key, e := w.exchangeOrcaCode(ctx, code, verifier)
	if e != nil {
		diag.Log(w.Store.Root, "ai.connect_failed", "error", diag.Error(e))
		return e
	}
	input := AIProviderInput{Preset: orcaPreset, Model: orcaFreeModel, Key: &key}
	s := w.Store.Read()
	if i := slices.IndexFunc(s.AIProviders, func(p schema.AIProvider) bool { return p.Preset == orcaPreset }); i >= 0 {
		old := s.AIProviders[i]
		input.ID, input.Name = old.ID, old.Name
		if old.Model != "" {
			input.Model = old.Model
		}
	}
	p, e := w.SaveAIProvider(input)
	if e == nil {
		e = w.UseAIProvider(p.ID, "")
	}
	if e != nil {
		diag.Log(w.Store.Root, "ai.connect_failed", "error", diag.Error(e))
		return e
	}
	diag.Log(w.Store.Root, "ai.connect_finished", "provider_id", p.ID)
	return nil
}

func (w *Workbench) exchangeOrcaCode(ctx context.Context, code, verifier string) (string, error) {
	body, _ := json.Marshal(map[string]string{"code": code, "code_verifier": verifier})
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, orcaKeysURL, bytes.NewReader(body))
	if e != nil {
		return "", e
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := w.client.Do(req)
	if e != nil {
		return "", e
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if e != nil {
		return "", e
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return "", fmt.Errorf("auth/keys HTTP %d", res.StatusCode)
	}
	var out struct {
		Key   string `json:"key"`
		Scope string `json:"scope"`
	}
	if e = json.Unmarshal(raw, &out); e != nil {
		return "", e
	}
	key := strings.TrimSpace(out.Key)
	if key == "" || len(key) > 4096 {
		return "", errors.New("auth/keys returned no key")
	}
	// 只接受普通 api 权限的密钥，返回的权限与请求不符时拒绝使用。
	if out.Scope != "" && out.Scope != "api" {
		return "", fmt.Errorf("auth/keys returned scope %q", out.Scope)
	}
	return key, nil
}

// connectPage 是浏览器回调后看到的结果页，提示回到 yovoice。
func connectPage(rw http.ResponseWriter, status int, ok bool) {
	title, text := "连接失败 · Connection failed", "授权没有完成，请回到 yovoice 重新点击“免费连接 OrcaRouter”。<br>Authorization did not complete. Go back to yovoice and try again."
	if ok {
		title, text = "已连接 OrcaRouter · Connected", "可以关闭此页面，回到 yovoice 直接使用 AI 功能。<br>You can close this page and go back to yovoice."
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	rw.WriteHeader(status)
	_, _ = fmt.Fprintf(rw, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>%s</title><body style="font-family:system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1rem;line-height:1.6"><h1 style="font-size:1.4rem">%s</h1><p>%s</p></body>`, title, title, text)
}
