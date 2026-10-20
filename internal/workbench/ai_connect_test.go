package workbench

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// 一键授权：state 不一致的回调被拒绝；正确回调用 verifier 换取密钥，添加 OrcaRouter 免费模型并设为当前服务。
func TestConnectOrcaRouter(t *testing.T) {
	var challenge string
	keys := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var in struct {
			Code     string `json:"code"`
			Verifier string `json:"code_verifier"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		sum := sha256.Sum256([]byte(in.Verifier))
		if in.Code != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			rw.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = rw.Write([]byte(`{"key":"sk-orca-connected-0001","user_id":"1","scope":"api"}`))
	}))
	defer keys.Close()
	oldKeys := orcaKeysURL
	orcaKeysURL = keys.URL
	defer func() { orcaKeysURL = oldKeys }()

	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	other := "sk-other-provider-key"
	first, err := w.SaveAIProvider(AIProviderInput{Preset: "deepseek", Key: &other, Model: "deepseek-chat"})
	must(t, err)

	raw, err := w.ConnectOrcaRouter()
	must(t, err)
	auth, err := url.Parse(raw)
	must(t, err)
	q := auth.Query()
	challenge = q.Get("code_challenge")
	if q.Get("ref") != orcaRef || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" || challenge == "" {
		t.Fatalf("授权地址缺少参数：%s", raw)
	}
	callback, err := url.Parse(q.Get("callback_url"))
	must(t, err)
	if callback.Hostname() != "127.0.0.1" || callback.Path != orcaCallbackPath {
		t.Fatalf("回调应为本机回环地址：%s", callback)
	}

	res, err := http.Get(callback.String() + "?code=good-code&state=forged")
	must(t, err)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || len(w.Store.Read().AIProviders) != 1 {
		t.Fatal("state 不一致的回调应被拒绝")
	}

	res, err = http.Get(callback.String() + "?code=good-code&state=" + url.QueryEscape(q.Get("state")))
	must(t, err)
	res.Body.Close()
	s := w.Store.Read()
	if len(s.AIProviders) != 2 {
		t.Fatalf("应添加 OrcaRouter 服务：%+v", s.AIProviders)
	}
	orca := s.AIProviders[1]
	if orca.Preset != orcaPreset || orca.Model != orcaFreeModel || s.AIProviderID != orca.ID || s.AIProviderID == first.ID {
		t.Fatalf("OrcaRouter 应成为当前服务并使用免费模型：%+v", orca)
	}
	if _, c, _ := w.aiConfig(""); c.Key != "sk-orca-connected-0001" {
		t.Fatal("换取的密钥应保存到 secrets.json")
	}
	// 回调只用一次，之后监听关闭。
	for i := 0; ; i++ {
		w.ai.mu.Lock()
		closed := w.ai.connect == nil
		w.ai.mu.Unlock()
		if closed {
			break
		}
		if i > 200 {
			t.Fatal("授权完成后应关闭回环监听")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
