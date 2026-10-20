package workbench

import (
	"crypto/rand"
	"encoding/hex"
	"yovoice/internal/msg"
)

// LocalAPIDefaultPort 是本机接口的默认端口，与命令行 serve 的默认地址一致。
const LocalAPIDefaultPort = 8080

// LocalAPISettings 是桌面应用内置本机接口的设置；令牌与设置一起保存在状态库的私有设置中。
type LocalAPISettings struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
}

// LocalAPI 读取本机接口设置；首次读取时生成令牌并保存，之后令牌保持不变。
func (w *Workbench) LocalAPI() (LocalAPISettings, error) {
	w.edit.Lock()
	defer w.edit.Unlock()
	return w.localAPI()
}

// SaveLocalAPI 保存开关与端口，令牌保持不变。
func (w *Workbench) SaveLocalAPI(enabled bool, port int) (LocalAPISettings, error) {
	if port < 1024 || port > 65535 {
		return LocalAPISettings{}, msg.Err(msg.ErrLocalAPIPort, nil)
	}
	w.edit.Lock()
	defer w.edit.Unlock()
	s, e := w.localAPI()
	if e != nil {
		return s, e
	}
	s.Enabled, s.Port = enabled, port
	return s, w.writeLocalAPI(s)
}

// ResetLocalAPIToken 生成新令牌，旧令牌随即失效。
func (w *Workbench) ResetLocalAPIToken() (LocalAPISettings, error) {
	w.edit.Lock()
	defer w.edit.Unlock()
	s, e := w.localAPI()
	if e != nil {
		return s, e
	}
	if s.Token, e = newAPIToken(); e != nil {
		return s, e
	}
	return s, w.writeLocalAPI(s)
}

func (w *Workbench) localAPI() (LocalAPISettings, error) {
	s := LocalAPISettings{Port: LocalAPIDefaultPort}
	if _, e := w.Store.Private(localAPIPrivate, &s); e != nil {
		return s, e
	}
	var e error
	if len(s.Token) >= 32 {
		return s, nil
	}
	if s.Token, e = newAPIToken(); e != nil {
		return s, e
	}
	return s, w.writeLocalAPI(s)
}

// localAPIPrivate 是状态库私有设置中保存本机接口设置的键；令牌不随状态广播给界面。
const localAPIPrivate = "localApi"

func (w *Workbench) writeLocalAPI(s LocalAPISettings) error {
	return w.Store.SetPrivate(localAPIPrivate, s)
}

func newAPIToken() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
