package engine

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"yovoice/internal/catalog"
	"yovoice/internal/schema"
)

// 只将本次编曲请求的服务端原因附回候选，不能把上一轮日志误报给新任务。
func arrangementEngineFailure(root string, started time.Time, err error) error {
	path := filepath.Join(root, "logs", "last-inference-error.txt")
	info, e := os.Stat(path)
	if e != nil || info.ModTime().Before(started) {
		return err
	}
	var response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	b, e := os.ReadFile(path)
	if e != nil || json.Unmarshal(b, &response) != nil {
		return err
	}
	text := []rune(strings.TrimSpace(response.Error.Message))
	if len(text) == 0 {
		return err
	}
	if len(text) > 400 {
		text = text[:400]
	}
	return fmt.Errorf("编曲音频引擎失败：%s", string(text))
}

// BuildArrangementRequest 的长度白名单与普通歌曲分离，避免旧歌曲改变生成规则。
func BuildArrangementRequest(d schema.Draft, c schema.ArrangementGenerationContract) (map[string]any, error) {
	if c.Version != schema.ArrangementGenerationVersion || c.ModelID != d.ModelID || math.IsNaN(c.RequestSeconds) || math.IsInf(c.RequestSeconds, 0) || c.RequestSeconds <= 0 || c.RequestSeconds > 300 {
		return nil, fmt.Errorf("无效的编曲生成合同或长度预算")
	}
	m, err := catalog.Lookup(d.ModelID)
	if err != nil {
		return nil, err
	}
	p, err := BuildRequest(d, "", "")
	if err != nil {
		return nil, err
	}
	o := p["request"].(map[string]any)["options"].(map[string]any)
	switch m.Family {
	case "yue2":
		if c.EngineVersion != "v0.9.1" {
			return nil, fmt.Errorf("当前引擎 %s 尚未验证 YuE2 语义长度合同", c.EngineVersion)
		}
		if c.SemanticMin < c.PrefixFrames || c.SemanticMax <= c.PrefixFrames || c.SemanticMin > c.SemanticMax || c.SemanticMax > 7500 || math.Abs(c.RequestSeconds*25-math.Round(c.RequestSeconds*25)) > 1e-6 || c.SemanticMax != c.PrefixFrames+int(math.Round(c.RequestSeconds*25)) {
			return nil, fmt.Errorf("语义帧预算无效：前缀 %d，下限 %d，上限 %d", c.PrefixFrames, c.SemanticMin, c.SemanticMax)
		}
		// 本轮没有开放续写前缀上传；拒绝伪造前缀长度，不能仅增加总预算。
		if c.PrefixFrames != 0 {
			return nil, fmt.Errorf("当前编曲适配器尚未验证续写前缀，不能忽略前文")
		}
		if c.Settings.Mode == "score" && strings.TrimSpace(d.Abc) == "" {
			return nil, fmt.Errorf("按谱生成缺少经过校验的 ABC")
		}
		if c.Settings.Mode == "free" {
			delete(o, "abc")
			o["cot"] = "off"
			if c.Settings.SoundType != "wordless" {
				o["lyrics"] = "[Instrumental]"
				p["request"].(map[string]any)["text"] = "[Instrumental]"
			}
		}
		o["semantic_min_tokens"], o["semantic_max_tokens"], o["export_semantic"] = c.SemanticMin, c.SemanticMax, true
	case "ace_step":
		if c.RequestSeconds < 10 {
			return nil, fmt.Errorf("当前 ACE-Step 编曲请求至少 10 秒，需明确设置原片预算")
		}
		o["duration_seconds"] = c.RequestSeconds
	default:
		return nil, fmt.Errorf("模型不支持编曲音频生成")
	}
	return p, nil
}

// semanticResult 保留引擎的截断声明；没有产物时不能编造“完整结束”。
func (r taskResult) semanticResult() (frames int, truncated, known bool) {
	for _, a := range r.Artifacts {
		if !strings.Contains(strings.ToLower(a.ID+" "+a.Meta["format"]+" "+a.Meta["kind"]), "semantic") {
			continue
		}
		if value, ok := a.Meta["truncated"]; ok {
			truncated, _ = strconv.ParseBool(value)
			known = value == "true" || value == "false"
		}
		b, err := base64.StdEncoding.DecodeString(a.Payload)
		if err != nil {
			return 0, truncated, known
		}
		var tokens []int
		if json.Unmarshal(b, &tokens) == nil {
			return len(tokens), truncated, known
		}
		if n, err := strconv.Atoi(a.Meta["frames"]); err == nil {
			frames = n
		}
		return frames, truncated, known
	}
	return
}
