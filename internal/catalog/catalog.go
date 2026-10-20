// Package catalog 提供受支持的模型包、推理运行时包、生成参数定义与 OmniVoice 属性。
package catalog

import (
	_ "embed"
	"encoding/json"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"yovoice/internal/msg"
)

const Revision = "6d5436fc85f7a20c2e9f4e472b7f3a532f686444"

type ModelPackage struct {
	Revision string `json:"revision,omitempty"`
	// Repo 为空时来自 audio.cpp 的模型仓库；音色库等其他资源写明 Hugging Face 仓库路径，
	// ModelScope 没有镜像时改用 hf-mirror。
	Repo string `json:"repo,omitempty"`
	// ModelScope 是 Repo 在 ModelScope 上的同名镜像仓库，填写后 ModelScope 源直接从该仓库下载。
	ModelScope string `json:"modelScope,omitempty"`
	// EngineMinimum 是该模型需要的最低内核版本，为空时只要求应用的最低版本。
	EngineMinimum string   `json:"engineMinimum,omitempty"`
	Voices        []string `json:"voices,omitempty"`
	Variant       string   `json:"variant,omitempty"`
	// LoadOptions、SessionOptions 是启动内核时随模型传入的参数，例如 ACE-Step XL 需要指定 DiT 变体，
	// YuE2 需要指定主模型文件。
	LoadOptions    map[string]string `json:"loadOptions,omitempty"`
	SessionOptions map[string]string `json:"sessionOptions,omitempty"`
	// Files 不为空时是多文件模型包：各文件按相对路径下载到 Directory，登记的模型路径是该目录。
	// RemotePath 仍指向主模型文件，用于导入时识别。
	Files      []ModelFile `json:"files,omitempty"`
	Directory  string      `json:"directory,omitempty"`
	Task       string      `json:"task,omitempty"`
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Family     string      `json:"family"`
	Version    string      `json:"version"`
	Precision  string      `json:"precision"`
	RemotePath string      `json:"remotePath"`
	Size       int64       `json:"size"`
	SHA256     string      `json:"sha256"`
}

// ModelFile 是多文件模型包中的一个文件。
type ModelFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

//go:embed catalog.json
var catalogJSON []byte
var Models = []ModelPackage{}

// engine.json 是 audio.cpp 版本与运行包校验值的唯一来源，桌面打包脚本也读取它。
//
//go:embed engine.json
var engineJSON []byte

// ocho.json 是随包分发的 ocho 运行时版本与校验值的唯一来源，打包脚本也读取它。
//
//go:embed ocho.json
var ochoJSON []byte

// EngineVersion 是在线安装与打包使用的推荐 audio.cpp 版本；EngineMinimum 是当前应用
// 请求格式与模型包所需的最低版本。内核不低于最低版本即可使用，升级到推荐版本可选。
var EngineVersion, EngineMinimum string
var archives = map[string]string{}

// OchoVersion 是随包分发的 ocho 版本；校验值为 64 个 0 时表示版本尚未发布，
// 首版上架后由打包脚本回填真实摘要。
var OchoVersion string
var ochoArchives = map[string]string{}

func init() {
	if e := json.Unmarshal(catalogJSON, &Models); e != nil {
		panic(e)
	}
	var engine struct {
		Version  string            `json:"version"`
		Minimum  string            `json:"minimum"`
		Archives map[string]string `json:"archives"`
	}
	if e := json.Unmarshal(engineJSON, &engine); e != nil {
		panic(e)
	}
	EngineVersion, EngineMinimum, archives = engine.Version, engine.Minimum, engine.Archives
	if parseVersion(EngineVersion) == nil || parseVersion(EngineMinimum) == nil || !VersionAtLeast(EngineVersion, EngineMinimum) {
		panic("engine.json 版本无效")
	}
	var ocho struct {
		Version  string            `json:"version"`
		Archives map[string]string `json:"archives"`
	}
	if e := json.Unmarshal(ochoJSON, &ocho); e != nil {
		panic(e)
	}
	OchoVersion, ochoArchives = ocho.Version, ocho.Archives
	if parseVersion(OchoVersion) == nil {
		panic("ocho.json 版本无效")
	}
}

// parseVersion 解析 vX.Y.Z，格式无效时返回 nil。
func parseVersion(v string) []int {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 || !strings.HasPrefix(v, "v") {
		return nil
	}
	result := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		result[i] = n
	}
	return result
}

// VersionAtLeast 比较 vX.Y.Z 版本号；任一方无效时返回 false。
func VersionAtLeast(v, minimum string) bool {
	a, b := parseVersion(v), parseVersion(minimum)
	if a == nil || b == nil {
		return false
	}
	return slices.Compare(a, b) >= 0
}

// ServerBackend 把界面后端映射为 audio.cpp 的 backend 参数；两种 CUDA 构建都使用 cuda。
func ServerBackend(backend string) string {
	if backend == "cuda13" {
		return "cuda"
	}
	return backend
}

// IsMusic 判断模型家族是否为整首歌曲生成（风格 + 歌词）。
func IsMusic(family string) bool { return family == "ace_step" || family == "yue2" }

// PackageFiles 返回需要下载的文件；单文件模型只有主模型一项。
func (m ModelPackage) PackageFiles() []ModelFile {
	if len(m.Files) > 0 {
		return m.Files
	}
	return []ModelFile{{Path: m.RemotePath, Size: m.Size, SHA256: m.SHA256}}
}

func Lookup(id string) (ModelPackage, error) {
	for _, m := range Models {
		if m.ID == id {
			return m, nil
		}
	}
	return ModelPackage{}, msg.Err(msg.ErrModelUnsupported, nil)
}
func (m ModelPackage) URL(source string) (string, error) {
	return m.FileURL(source, m.RemotePath)
}

// FileURL 返回模型包内某个文件的下载地址。
func (m ModelPackage) FileURL(source, path string) (string, error) {
	if path == "" {
		return "", msg.Err(msg.ErrModelUnsupported, nil)
	}
	revision := m.Revision
	if revision == "" {
		revision = Revision
	}
	if m.Repo != "" {
		switch source {
		case "huggingface":
			return "https://huggingface.co/" + m.Repo + "/resolve/" + revision + "/" + path, nil
		case "modelscope":
			if m.ModelScope != "" {
				return "https://modelscope.cn/models/" + m.ModelScope + "/resolve/master/" + path, nil
			}
			return "https://hf-mirror.com/" + m.Repo + "/resolve/" + revision + "/" + path, nil
		case "mirror":
			return "https://hf-mirror.com/" + m.Repo + "/resolve/" + revision + "/" + path, nil
		}
		return "", msg.Err(msg.ErrDownloadSource, nil)
	}
	switch source {
	case "huggingface":
		return "https://huggingface.co/audio-cpp/audio.cpp-gguf/resolve/" + revision + "/" + path, nil
	case "mirror":
		return "https://hf-mirror.com/audio-cpp/audio.cpp-gguf/resolve/" + revision + "/" + path, nil
	case "modelscope":
		return "https://modelscope.cn/models/HereIsMark/audio.cpp-gguf/resolve/master/" + path, nil
	}
	return "", msg.Err(msg.ErrDownloadSource, nil)
}

type RuntimeArchive struct{ Name, Hash string }

func RuntimeArchives(backend string) ([]RuntimeArchive, error) {
	var names []string
	if runtime.GOOS == "darwin" {
		if backend != "cpu" && backend != "metal" {
			return nil, msg.Err(msg.ErrMacBackend, nil)
		}
		if runtime.GOARCH != "arm64" {
			return nil, msg.Err(msg.ErrMacAppleSilicon, nil)
		}
		names = []string{"bin-macos-arm64-metal.tar.gz"}
	} else if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		switch backend {
		case "cpu":
			names = []string{"bin-windows-x64-cpu-portable.zip"}
		case "vulkan":
			// 上游新构建倾向 AVX-512，便携包兼容更多 CPU。
			names = []string{"bin-windows-x64-vulkan-portable.zip"}
		case "cuda":
			names = []string{"bin-windows-x64-cuda12.4.zip", "cudart-windows-x64-cuda12.4.zip"}
		case "cuda13":
			// CUDA 13 需要较新的驱动，且不再支持部分旧显卡，与 12.4 并列提供。
			names = []string{"bin-windows-x64-cuda13.3.zip", "cudart-windows-x64-cuda13.3.zip"}
		}
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		switch backend {
		case "cpu", "vulkan":
			names = []string{"bin-ubuntu-x64-" + backend + "-portable.tar.gz"}
		}
	}
	if len(names) == 0 {
		return nil, msg.Err(msg.ErrBackendUnsupported, nil)
	}
	result := []RuntimeArchive{}
	for _, n := range names {
		n = "audio-" + EngineVersion + "-" + n
		h, ok := archives[n]
		if !ok {
			return nil, msg.Err(msg.ErrPlatformArch, nil)
		}
		result = append(result, RuntimeArchive{n, h})
	}
	return result, nil
}

// OchoArchiveFor 返回指定平台（仅支持 darwin/arm64、windows/amd64、linux/amd64）
// 的 ocho 压缩包资产名与摘要，打包脚本据此下载并校验。
func OchoArchiveFor(goos, goarch string) (RuntimeArchive, error) {
	var suffix string
	switch {
	case goos == "darwin" && goarch == "arm64":
		suffix = "darwin-arm64.tar.gz"
	case goos == "windows" && goarch == "amd64":
		suffix = "windows-amd64.zip"
	case goos == "linux" && goarch == "amd64":
		suffix = "linux-amd64.tar.gz"
	default:
		return RuntimeArchive{}, msg.Err(msg.ErrPlatformArch, nil)
	}
	name := "ocho-" + OchoVersion + "-" + suffix
	hash, ok := ochoArchives[name]
	if !ok {
		return RuntimeArchive{}, msg.Err(msg.ErrPlatformArch, nil)
	}
	return RuntimeArchive{name, hash}, nil
}
