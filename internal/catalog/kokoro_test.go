package catalog

import (
	"path"
	"regexp"
	"strings"
	"testing"
)

// 每个受支持平台都需要 eSpeak 与 MeCab 两个 wheel，镜像地址与 PyPI 路径一致。
func TestKokoroWheels(t *testing.T) {
	digest := regexp.MustCompile(`^[0-9a-f]{64}$`)
	projects := map[string][]string{}
	for _, w := range kokoroWheels {
		projects[w.Platform] = append(projects[w.Platform], w.Project)
		if !digest.MatchString(w.SHA256) || w.Size <= 0 || !strings.HasSuffix(w.Path, ".whl") {
			t.Errorf("wheel 清单无效：%+v", w)
		}
		official, err := w.URL("huggingface")
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range []string{"modelscope", "mirror"} {
			mirror, err := w.URL(source)
			if err != nil || path.Base(mirror) != path.Base(official) || !strings.HasPrefix(mirror, "https://mirrors.aliyun.com/pypi/packages/") {
				t.Errorf("%s 源地址无效：%s", source, mirror)
			}
		}
		if _, err := w.URL("unknown"); err == nil {
			t.Error("未知下载源应返回错误")
		}
	}
	for _, platform := range []string{"darwin/arm64", "windows/amd64", "linux/amd64"} {
		if strings.Join(projects[platform], ",") != "espeakng-loader,fugashi" {
			t.Errorf("%s 的发音库 wheel 不完整：%v", platform, projects[platform])
		}
	}
}
