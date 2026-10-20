package workbench

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"yovoice/internal/catalog"
	"yovoice/internal/schema"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RemoteTools 注入对外 MCP 工具中随传输变化的少量行为：推理互斥（HTTP 与 MCP 共用一把锁）、
// 异步任务表、生成超时与音频位置（远程 downloadPath / 本机 path）。
// remote 之外不得使用；内置助手的工具见 assistant_tools.go。
type RemoteTools struct {
	// Local 为本机 stdio 模式：结果给出本机路径，参考音频可按路径导入。
	Local bool
	// TryLock 尝试占用推理引擎，繁忙时返回 false。
	TryLock func() bool
	Unlock  func()
	// Generate 生成音频，含超时与繁忙错误转换。
	Generate func(context.Context, schema.Draft) (schema.Generation, error)
	// Submit 提交异步任务，Get 查询或取消任务。
	Submit func(requestID string, d schema.Draft) (any, error)
	Get    func(requestID string, cancel bool) (any, error)
	// Audio 描述生成结果的位置：远程为 downloadPath，本机为 path。
	Audio func(schema.Generation) map[string]any
}

// RegisterRemoteTools 把对外 MCP 的全部工具注册到 server；HTTP 与 stdio 两种传输共用同一份定义。
func (w *Workbench) RegisterRemoteTools(server *mcp.Server, t RemoteTools) {
	download := "通过服务端地址加 downloadPath 下载 WAV，HTTP 请求需携带相同 Bearer Token；不返回服务器本地路径"
	upload := "上传 Base64 编码的参考音频，限 20 MB、1–60 秒，返回 voiceId。大文件优先通过 HTTP POST /v1/voices 上传"
	if t.Local {
		download = "path 为本机 WAV 文件的绝对路径"
		upload = "导入参考音频，返回 voiceId。传 path（本机音频文件路径，常见格式自动转换）或 Base64 编码的 audio，限 20 MB、1–60 秒"
	}
	w.registerStatusTool(server)
	w.registerListModelsTool(server)
	w.registerListVoicesTool(server)
	mcp.AddTool(server, &mcp.Tool{Name: "upload_voice", Description: upload}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Audio string `json:"audio,omitempty" jsonschema:"参考音频的纯 Base64 字符串，不含 data URL 前缀"`
		Path  string `json:"path,omitempty" jsonschema:"本机音频文件路径，仅 yovoice mcp（stdio）可用"`
		Name  string `json:"name,omitempty" jsonschema:"音色名称"`
	}) (*mcp.CallToolResult, any, error) {
		if !t.TryLock() {
			return nil, nil, fmt.Errorf("推理服务繁忙，请稍后重试")
		}
		defer t.Unlock()
		if in.Path != "" {
			// 远程服务不读取服务器上的任意路径，只有本机 stdio 模式允许。
			if !t.Local {
				return nil, nil, fmt.Errorf("远程服务不支持 path，请上传 Base64 音频")
			}
			voice, err := w.ImportVoice(ctx, in.Path, in.Name)
			return nil, voice, err
		}
		if len(in.Audio) > base64.StdEncoding.EncodedLen(20<<20) {
			return nil, nil, fmt.Errorf("音频超过 20 MB")
		}
		data, err := base64.StdEncoding.DecodeString(in.Audio)
		if err != nil {
			return nil, nil, err
		}
		voice, err := w.ImportVoiceFrom(ctx, bytes.NewReader(data), in.Name)
		return nil, voice, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "generate", Description: "同步生成，长耗时优先使用 submit_generation。完整生成语音，返回 id、duration 和音频位置。" + download}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Text     string         `json:"text" jsonschema:"需要朗读的正文"`
		Settings map[string]any `json:"settings,omitempty" jsonschema:"声音参数，与 HTTP API 同名，例如 modelId、voiceId、voxMode、voiceDescription、speaker、referenceText、seed、modelOptions"`
	}) (*mcp.CallToolResult, any, error) {
		if !t.TryLock() {
			return nil, nil, fmt.Errorf("推理服务繁忙，请稍后重试")
		}
		defer t.Unlock()
		d, err := decodeToolGeneration(in.Text, in.Settings)
		if err != nil {
			return nil, nil, err
		}
		g, err := t.Generate(ctx, d)
		if err != nil {
			return nil, nil, err
		}
		return nil, t.Audio(g), nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "submit_generation", Description: "提交异步语音生成，立即返回任务状态。requestId 为客户端生成的32位十六进制随机ID；相同ID和参数重试不会重复生成。随后用 get_generation 查询，完成后取得音频。优先使用此工具避免长连接超时"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		RequestID string         `json:"requestId"`
		Text      string         `json:"text"`
		Settings  map[string]any `json:"settings,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		d, err := decodeToolGeneration(in.Text, in.Settings)
		if err != nil {
			return nil, nil, err
		}
		job, err := t.Submit(in.RequestID, d)
		return nil, job, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "render_score", Description: "把编曲乐谱渲染为混音 WAV，返回 id、duration 和音频位置（远程为 downloadPath，本机为 path）。乐谱以小节和拍为单位：tempo、timeSignature、可选 sections（段落及变速），tracks 每个声部含 program（GM 音色号）或 drums、role（melody/piano/strings/bass/drums/pad/arp/other，决定默认电平）、notes（bar、beat 从 1 开始、pitch 为 MIDI 音高、length 以拍计、velocity），可选 level、pan、reverb、humanize、dynamics。soundFont 选择已安装的音色库（musescore-general-sf2 默认、generaluser-gs-sf2、sgm-v2-sf2）"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Title     string       `json:"title,omitempty" jsonschema:"作品标题"`
		SoundFont string       `json:"soundFont,omitempty" jsonschema:"音色库 ID，默认 musescore-general-sf2"`
		Score     schema.Score `json:"score" jsonschema:"乐谱"`
	}) (*mcp.CallToolResult, any, error) {
		if in.SoundFont == "" {
			in.SoundFont = "musescore-general-sf2"
		}
		if m, err := catalog.Lookup(in.SoundFont); err != nil || m.Family != "soundfont" {
			return nil, nil, fmt.Errorf("未知音色库：%s", in.SoundFont)
		}
		if !t.TryLock() {
			return nil, nil, fmt.Errorf("推理服务繁忙，请稍后重试")
		}
		defer t.Unlock()
		d := schema.DefaultDraft()
		d.Kind, d.ModelID, d.Title, d.Score = "score", in.SoundFont, in.Title, &in.Score
		if d.Title == "" {
			d.Title = "API 编曲"
		}
		if err := schema.Validate(d); err != nil {
			return nil, nil, err
		}
		g, err := t.Generate(ctx, d)
		if err != nil {
			return nil, nil, err
		}
		return nil, t.Audio(g), nil
	})
	for _, name := range []string{"get_generation", "cancel_generation"} {
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: map[string]string{"get_generation": "按 requestId 查询任务；建议每2–5秒查询，completed 后用 downloadPath 下载（本机模式直接返回 path）", "cancel_generation": "按 requestId 取消任务；取消后查询至终态，终态任务保持不变"}[name]}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			RequestID string `json:"requestId"`
		}) (*mcp.CallToolResult, any, error) {
			job, err := t.Get(in.RequestID, name == "cancel_generation")
			return nil, job, err
		})
	}
}

// decodeToolGeneration 组装 MCP 工具的生成参数并按对外接口规则解析。
func decodeToolGeneration(text string, settings map[string]any) (schema.Draft, error) {
	if settings == nil {
		settings = map[string]any{}
	}
	settings["text"] = text
	data, err := json.Marshal(settings)
	if err != nil {
		return schema.Draft{}, err
	}
	if len(data) > 1<<20 {
		return schema.Draft{}, fmt.Errorf("生成参数超过 1 MB")
	}
	return DecodeRemoteGeneration(bytes.NewReader(data))
}

// DecodeRemoteGeneration 解析对外接口（HTTP 与 MCP 共用）的生成参数：仅接受一个 JSON 对象，正文必填。
func DecodeRemoteGeneration(body io.Reader) (schema.Draft, error) {
	d := schema.DefaultDraft()
	d.Title, d.Text, d.Mode, d.EmotionText = "API 语音", "", "speaker", ""
	input := struct {
		Text string `json:"text"`
		schema.SynthesisSettings
	}{SynthesisSettings: d.SynthesisSettings}
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return d, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return d, fmt.Errorf("仅允许一个 JSON 对象")
	}
	d.Text, d.SynthesisSettings = input.Text, input.SynthesisSettings
	if strings.TrimSpace(d.Text) == "" {
		return d, fmt.Errorf("正文不能为空")
	}
	return d, schema.Validate(d)
}

// registerStatusTool 注册 status 工具，对外 MCP 与内置助手共用同一份定义。
func (w *Workbench) registerStatusTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "status", Description: "查看推理引擎及当前任务状态"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		s := w.Store.Read()
		return nil, map[string]any{"engineVersion": catalog.EngineVersion, "engineMinimum": catalog.EngineMinimum, "activity": s.Activity, "modelActivity": s.ModelActivity, "runtimeVersion": s.RuntimeVersion, "ready": RuntimeReady(s)}, nil
	})
}

// registerListModelsTool 注册 list_models 工具，对外 MCP 与内置助手共用同一份定义。
func (w *Workbench) registerListModelsTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "list_models", Description: "查询模型 ID、安装状态和高级参数定义；生成前选择已安装模型"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"catalog": catalog.Models, "installed": w.Store.Read().Models, "generationOptions": catalog.GenerationOptions}, nil
	})
}

// registerListVoicesTool 注册 list_voices 工具，对外 MCP 与内置助手共用同一份定义。
func (w *Workbench) registerListVoicesTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "list_voices", Description: "列出已上传参考音色及 voiceId"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"voices": w.Store.Read().Voices}, nil
	})
}
