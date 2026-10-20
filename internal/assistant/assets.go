package assistant

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Skill 资源随二进制分发：启动助手前写入工作区，正文变化才覆盖。
//
//go:embed skills
var skillFS embed.FS

//go:embed system.md
var systemPrompt []byte

// skillNames 是随包分发的全部 Skill。Skill 目录下的 references/*.md 是按需读取的参考文档。
var skillNames = []string{"yovoice-product", "yovoice-score", "yovoice-lyrics", "yovoice-music", "yovoice-workflows", "yovoice-voice"}

// writeAssets 把系统说明与 Skill 写入数据目录，内容没有变化时不重写文件。
func writeAssets(root string) error {
	base := filepath.Join(root, "assistant")
	for _, dir := range []string{filepath.Join(base, "workspace", ".agents", "skills"), filepath.Join(base, "config")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	if err := writeFileIfChanged(systemPromptPath(root), systemPrompt); err != nil {
		return err
	}
	for _, name := range skillNames {
		dir := "skills/" + name
		if _, err := skillFS.ReadFile(dir + "/SKILL.md"); err != nil {
			return fmt.Errorf("embedded skill %s: %w", name, err)
		}
		// 整个 Skill 目录一起写出，参考文档与 SKILL.md 保持同步。
		err := fs.WalkDir(skillFS, dir, func(file string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			body, err := skillFS.ReadFile(file)
			if err != nil {
				return err
			}
			return writeFileIfChanged(filepath.Join(base, "workspace", ".agents", filepath.FromSlash(file)), body)
		})
		if err != nil {
			return err
		}
	}
	// 旧版本分发、现已移除或改名的 Skill 从工作区删掉，免得助手读到过时说明；用户自己放的 Skill 不动。
	skillsDir := filepath.Join(base, "workspace", ".agents", "skills")
	entries, _ := os.ReadDir(skillsDir)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "yovoice-") && !slices.Contains(skillNames, entry.Name()) {
			if err := os.RemoveAll(filepath.Join(skillsDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeFileIfChanged(path string, body []byte) error {
	if current, err := os.ReadFile(path); err == nil && string(current) == string(body) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0600)
}
