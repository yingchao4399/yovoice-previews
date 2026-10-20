// Package testkit 提供多个包共用的测试辅助，仅由测试引用，不进入发布程序。
package testkit

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func Must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// WAV 返回一秒 16 kHz 单声道静音。
func WAV() []byte {
	b := make([]byte, 44+32000)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 32000)
	return b
}

// WAVSeconds keeps the fake music engine's output length aligned with its
// requested phrase duration so arrangement tests exercise placement checks.
func WAVSeconds(seconds int) []byte {
	if seconds < 1 {
		seconds = 1
	}
	if seconds > 30 {
		seconds = 30
	}
	b := make([]byte, 44+seconds*32000)
	copy(b, WAV()[:44])
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	binary.LittleEndian.PutUint32(b[40:], uint32(len(b)-44))
	for i := 44; i+1 < len(b); i += 2 {
		sample := int16(5000 * math.Sin(2*math.Pi*440*float64((i-44)/2)/16000))
		binary.LittleEndian.PutUint16(b[i:], uint16(sample))
	}
	return b
}

// RepoFile 定位仓库内文件，跨端一致性检查不依赖测试的工作目录。
func RepoFile(t testing.TB, parts ...string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(append([]string{filepath.Dir(file), "..", ".."}, parts...)...)
}

// RunFakeEngine 在测试二进制被当作推理引擎启动时提供模拟服务，并在服务结束后退出进程。
// 正文为“等待取消”时挂起至请求取消；以“模拟生成失败”结尾时返回错误。
func RunFakeEngine() {
	if len(os.Args) <= 2 || os.Args[1] != "--config" {
		return
	}
	b, e := os.ReadFile(os.Args[2])
	if e != nil {
		os.Exit(2)
	}
	var config struct {
		Port int `json:"port"`
	}
	if json.Unmarshal(b, &config) != nil {
		os.Exit(2)
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{}")) })
	http.HandleFunc("/v1/tasks/run", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Request struct {
				Text    string         `json:"text"`
				Audio   string         `json:"audio"`
				Options map[string]any `json:"options"`
			} `json:"request"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p.Request.Text == "等待取消" {
			<-r.Context().Done()
			return
		}
		if strings.HasSuffix(p.Request.Text, "模拟生成失败") || strings.Contains(p.Request.Text, "模拟编曲失败") {
			http.Error(w, "模拟生成失败", http.StatusInternalServerError)
			return
		}
		result := map[string]any{}
		// YuE2 自己规划（cot 非 off 且未给谱）或 SheetSage2 扒谱时附带旋律谱；只出谱或扒谱时没有音频。
		_, given := p.Request.Options["abc"]
		if (p.Request.Options["cot"] != nil && p.Request.Options["cot"] != "off" && !given) || p.Request.Audio != "" {
			result["artifacts"] = []any{map[string]any{"id": "score", "kind": "custom", "payload": base64.StdEncoding.EncodeToString([]byte(ABC)), "meta": map[string]string{"format": "abc", "truncated": "false"}}}
		}
		if p.Request.Options["stop_after"] != "abc" && p.Request.Audio == "" {
			audio := WAV()
			if strings.HasPrefix(p.Request.Text, "Dry isolated solo ") {
				audio = WAVSeconds(10)
				if seconds, ok := p.Request.Options["duration_seconds"].(float64); ok {
					audio = WAVSeconds(int(seconds))
				}
			}
			result["audio"] = base64.StdEncoding.EncodeToString(audio)
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	_ = http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", config.Port), nil)
	os.Exit(0)
}

// ABC 是模拟引擎返回的 YuE2 旋律谱：两小节，带和弦与段落。
const ABC = `X:1
T:
M:4/4
L:1/32
Q:1/4=120
V: Vocal clef=treble name="Vocal Melody" snm="Vocal"
V: Ins clef=treble name="Ins Melody" snm="Inst."
K:C
% verse
V: Vocal
"C"E8G8c16|"G"B8G8D16|
V: Ins
Z2|
`
