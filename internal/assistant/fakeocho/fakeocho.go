// Package fakeocho 在测试里扮演 ocho 运行时：测试二进制被子进程启动时经
// YOVOICE_FAKE_OCHO_PLAN 切换为假运行时，按计划执行 MCP 握手、工具调用、结果与崩溃等动作。
// 只被各包的 _test 引用，不进入发布程序。
package fakeocho

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	// PlanEnv 指向计划 JSON 文件；设置后测试二进制进入假运行时模式。
	PlanEnv = "YOVOICE_FAKE_OCHO_PLAN"
	// DumpEnv 指向转储文件：记录子进程的命令行与环境变量，用于检查密钥去向。
	DumpEnv = "YOVOICE_FAKE_OCHO_DUMP"
)

// Step 是计划中的一个动作。
type Step struct {
	MCP           string          `json:"mcp,omitempty"`    // 发送一个 JSON-RPC 请求并等响应
	Notify        string          `json:"notify,omitempty"` // 发送一个 JSON-RPC 通知
	Call          string          `json:"call,omitempty"`   // 发送 tools/call，并在流上发出 tool_use/tool_result
	Args          json.RawMessage `json:"args,omitempty"`
	Assistant     string          `json:"assistant,omitempty"`    // 发一条助手文本
	Delta         string          `json:"delta,omitempty"`        // 发一条流式文本增量
	Thinking      string          `json:"thinking,omitempty"`     // 发一条流式思考增量，再发定稿的思考块
	AssistantPID  bool            `json:"assistantPid,omitempty"` // 发一条内容为本进程 PID 的助手文本
	SleepMS       int             `json:"sleepMs,omitempty"`
	Result        string          `json:"result,omitempty"`        // 结束本轮（success 或 error_*）
	Crash         int             `json:"crash,omitempty"`         // 立即以状态码退出
	WaitInterrupt bool            `json:"waitInterrupt,omitempty"` // 等宿主 interrupt 后回 error_interrupted
	BadLine       string          `json:"badLine,omitempty"`       // 向 stdout 输出一行非 JSON
	LateDelta     string          `json:"lateDelta,omitempty"`     // 与 WaitInterrupt 同用：收到 interrupt 后、回 error_interrupted 前再发一条迟到的文本增量
}

// Plan 是假 ocho 的执行计划。
type Plan struct {
	CrashEarly  int      `json:"crashEarly,omitempty"` // 收到任何输入前立即退出
	HoldForever bool     `json:"holdForever,omitempty"`
	EchoUser    string   `json:"echoUser,omitempty"` // 把收到的每条用户消息文本追加到此文件，供测试断言前缀
	Turns       [][]Step `json:"turns,omitempty"`
}

// Run 在设置了 PlanEnv 时进入假运行时并结束进程；否则原样返回，交回测试入口。
func Run() {
	plan := os.Getenv(PlanEnv)
	if plan == "" {
		return
	}
	os.Exit(main(plan))
}

// main 是假 ocho 的主体：先完成与宿主的 initialize 握手和 MCP 握手，
// 之后的每个 user 消息按 Turns 里对应的一段计划执行。
func main(planPath string) int {
	data, err := os.ReadFile(planPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake ocho:", err)
		return 2
	}
	var plan Plan
	if err = json.Unmarshal(data, &plan); err != nil {
		fmt.Fprintln(os.Stderr, "fake ocho:", err)
		return 2
	}
	if dump := os.Getenv(DumpEnv); dump != "" {
		_ = os.WriteFile(dump, []byte("ARGV\n"+strings.Join(os.Args, "\n")+"\nENV\n"+strings.Join(os.Environ(), "\n")+"\n"), 0600)
	}
	if plan.CrashEarly > 0 {
		os.Exit(plan.CrashEarly)
	}
	if plan.HoldForever {
		time.Sleep(time.Hour)
		return 0
	}
	out := bufio.NewWriter(os.Stdout)
	write := func(v any) {
		payload, _ := json.Marshal(v)
		_, _ = out.Write(append(payload, '\n'))
		_ = out.Flush()
	}
	in := bufio.NewReader(os.Stdin)
	nextID := 100
	waitingInterrupt := false
	lateDelta := ""
	// 轮次进度落在计划文件旁：进程重启（崩溃恢复测试）后从下一个 turn 继续。
	statePath := planPath + ".state"
	turnBytes, _ := os.ReadFile(statePath)
	turn := 0
	_, _ = fmt.Sscanf(string(turnBytes), "%d", &turn)
	// queued 存放等待 MCP 响应期间提前收到的行（如宿主的 user 消息），主循环稍后处理。
	queued := []map[string]any{}
	// waitControl 只读 stdin，等待指定 control 响应；期间收到的其他行排队留给主循环。
	waitControl := func(id string) map[string]any {
		for {
			raw, err := in.ReadString('\n')
			if err != nil {
				os.Exit(0)
			}
			var line map[string]any
			if json.Unmarshal([]byte(raw), &line) != nil {
				continue
			}
			if line["type"] != "control_response" {
				queued = append(queued, line)
				continue
			}
			body, _ := line["response"].(map[string]any)
			if body["request_id"] == id {
				return body
			}
		}
	}
	// next 供主循环用：先消费排队的行，再读 stdin。
	next := func() (map[string]any, bool) {
		if len(queued) > 0 {
			line := queued[0]
			queued = queued[1:]
			return line, true
		}
		raw, err := in.ReadString('\n')
		if err != nil {
			return nil, false
		}
		var line map[string]any
		if json.Unmarshal([]byte(raw), &line) != nil {
			return nil, true
		}
		return line, true
	}
	controlSuccess := func(id string, response map[string]any) {
		write(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": id, "response": response}})
	}
	// sendMCP 发送 mcp_message 并等宿主的 control_response，返回其中的 JSON-RPC 响应。
	sendMCP := func(payload map[string]any) map[string]any {
		id := fmt.Sprintf("fake-%d", nextID)
		nextID++
		write(map[string]any{"type": "control_request", "request_id": id, "request": map[string]any{"subtype": "mcp_message", "server_name": "yovoice", "message": payload}})
		body := waitControl(id)
		if body["subtype"] != "success" {
			fmt.Fprintf(os.Stderr, "fake ocho: mcp control failed: %v\n", body)
			os.Exit(3)
		}
		response, _ := body["response"].(map[string]any)
		mcpResponse, _ := response["mcp_response"].(map[string]any)
		return mcpResponse
	}
	callMCP := func(method string, params map[string]any) map[string]any {
		id := float64(nextID)
		nextID++
		payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			payload["params"] = params
		}
		return sendMCP(payload)
	}
	notifyMCP := func(method string) {
		sendMCP(map[string]any{"jsonrpc": "2.0", "method": method})
	}
	for {
		line, ok := next()
		if !ok {
			return 0
		}
		if line == nil {
			continue
		}
		switch line["type"] {
		case "control_request":
			id, _ := line["request_id"].(string)
			request, _ := line["request"].(map[string]any)
			subtype, _ := request["subtype"].(string)
			switch subtype {
			case "initialize":
				controlSuccess(id, map[string]any{"pid": os.Getpid()})
				// 真实 ocho 在初始化期间完成 MCP 握手，这里同样先握手再等用户消息。
				if response := callMCP("initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "fake-ocho", "version": "1"}}); response == nil {
					fmt.Fprintln(os.Stderr, "fake ocho: initialize response missing")
					os.Exit(3)
				}
				notifyMCP("notifications/initialized")
				tools := callMCP("tools/list", map[string]any{})
				result, _ := tools["result"].(map[string]any)
				fmt.Fprintf(os.Stderr, "fake ocho tools: %v\n", result["tools"])
			case "set_model":
				controlSuccess(id, map[string]any{})
			case "interrupt":
				controlSuccess(id, map[string]any{})
				if waitingInterrupt {
					waitingInterrupt = false
					if lateDelta != "" {
						write(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": lateDelta}}})
						lateDelta = ""
					}
					write(map[string]any{"type": "result", "subtype": "error_interrupted"})
				}
			default:
				write(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": id, "error": "unsupported"}})
			}
		case "user":
			if plan.EchoUser != "" {
				if content, ok := line["message"].(map[string]any)["content"].(string); ok {
					file, e := os.OpenFile(plan.EchoUser, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
					if e == nil {
						_, _ = file.WriteString(content + "\n--MSG--\n")
						_ = file.Close()
					}
				}
			}
			if len(plan.Turns) == 0 {
				continue
			}
			steps := plan.Turns[min(turn, len(plan.Turns)-1)]
			turn++
			_ = os.WriteFile(statePath, []byte(fmt.Sprintf("%d", turn)), 0600)
			for _, step := range steps {
				switch {
				case step.MCP != "":
					callMCP(step.MCP, nil)
				case step.Notify != "":
					notifyMCP(step.Notify)
				case step.Call != "":
					var args any
					if len(step.Args) > 0 {
						_ = json.Unmarshal(step.Args, &args)
					}
					// 与真实 ocho 一致：模型发起工具调用时先输出 tool_use 块，结果以 tool_result 块回给模型。
					useID := fmt.Sprintf("toolu-%d", nextID)
					write(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": useID, "name": "mcp__yovoice__" + step.Call, "input": args}}}})
					// JSON-RPC 层用不带前缀的工具名，ocho 的 mcp__yovoice__ 前缀只在流消息里出现。
					response := callMCP("tools/call", map[string]any{"name": step.Call, "arguments": args})
					result, _ := response["result"].(map[string]any)
					isError, _ := result["isError"].(bool)
					if _, failed := response["error"]; failed {
						isError = true
					}
					write(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": useID, "is_error": isError}}}})
				case step.Assistant != "" || step.AssistantPID:
					text := step.Assistant
					if step.AssistantPID {
						text = fmt.Sprint(os.Getpid())
					}
					write(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}})
				case step.Delta != "":
					write(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": step.Delta}}})
				case step.Thinking != "":
					write(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": step.Thinking}}})
					write(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": step.Thinking}}}})
				case step.SleepMS > 0:
					time.Sleep(time.Duration(step.SleepMS) * time.Millisecond)
				case step.BadLine != "":
					_, _ = fmt.Fprintln(out, step.BadLine)
					_ = out.Flush()
				case step.Crash > 0:
					_ = out.Flush()
					os.Exit(step.Crash)
				case step.WaitInterrupt:
					waitingInterrupt = true
					lateDelta = step.LateDelta
				case step.Result != "":
					write(map[string]any{"type": "result", "subtype": step.Result})
				}
			}
		case "control_response":
			// 假 ocho 不等待宿主主动发来的响应；忽略。
		}
	}
}
