package workbench

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallPluginCopiesCompanionInstrumentBank(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("VST3 手动安装只支持 Windows")
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	sourceDir := t.TempDir()
	plugin := filepath.Join(sourceDir, "DSK Asian DreamZ.vst3")
	if err := os.WriteFile(plugin, []byte("test plugin"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPlugin(plugin); err == nil || !strings.Contains(err.Error(), "配套音色目录") {
		t.Fatalf("缺少 DSK 音色目录时应该拒绝安装: %v", err)
	}
	bank := filepath.Join(sourceDir, "DSK Asian DreamZ.instruments")
	if err := os.MkdirAll(bank, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bank, "01 Pipa.mse"), []byte("sample bank"), 0600); err != nil {
		t.Fatal(err)
	}
	installed, err := InstallPlugin(plugin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installed.Path); err != nil {
		t.Fatal(err)
	}
	copy, err := os.ReadFile(filepath.Join(filepath.Dir(installed.Path), "DSK Asian DreamZ.instruments", "01 Pipa.mse"))
	if err != nil || string(copy) != "sample bank" {
		t.Fatalf("配套音色没有随插件安装: %v", err)
	}
}
