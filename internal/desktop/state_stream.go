package desktop

import (
	"bytes"
	"encoding/json"
)

// stateStream 记录一条状态事件连接上次发出的各顶层字段，之后只发出变化的字段。
// 生成进度、下载进度和 Ocho 流式回复每秒会推送多次，若每次都带上全部作品、版本记录和会话，
// 界面要反复解析几 MB 的 JSON 并整体重算；只发变化的字段时通常只有几百字节。
type stateStream struct {
	sent map[string]json.RawMessage
}

// next 接收完整状态的 JSON，返回本次要发出的事件：第一次为完整状态（event=state），
// 之后为变化的字段（event=patch，被省略的字段列在 removed 里）；没有变化时返回 nil。
func (s *stateStream) next(b []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(b, &fields); e != nil {
		return nil, e
	}
	if s.sent == nil {
		s.sent = fields
		return json.Marshal(map[string]json.RawMessage{"event": json.RawMessage(`"state"`), "state": b})
	}
	patch, removed := map[string]json.RawMessage{}, []string{}
	for key, value := range fields {
		if !bytes.Equal(s.sent[key], value) {
			patch[key] = value
		}
	}
	for key := range s.sent {
		if _, ok := fields[key]; !ok {
			removed = append(removed, key)
		}
	}
	s.sent = fields
	if len(patch) == 0 && len(removed) == 0 {
		return nil, nil
	}
	return json.Marshal(map[string]any{"event": "patch", "patch": patch, "removed": removed})
}
