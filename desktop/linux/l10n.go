package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// 原生对话框与标题栏文字跟随设置中的 preferences.uiLocale（与网页一致），尚未设置时按系统语言选择；
// 与 Windows 宿主一样只备中英两份，中文以外的语言用英文。调用处同时写出中英文，便于对照维护。
var english atomic.Bool

func init() {
	languages := systemLanguages()
	english.Store(len(languages) == 0 || !strings.HasPrefix(languages[0], "zh"))
}

// systemLanguages 按 LANGUAGE、LC_ALL、LC_MESSAGES、LANG 的顺序取系统语言，转换为 BCP 47 写法（zh_CN.UTF-8 → zh-CN）。
func systemLanguages() []string {
	var languages []string
	for _, key := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		for _, value := range strings.Split(os.Getenv(key), ":") {
			value, _, _ = strings.Cut(value, ".")
			value, _, _ = strings.Cut(value, "@")
			if value == "" || value == "C" || value == "POSIX" {
				continue
			}
			languages = append(languages, strings.ReplaceAll(value, "_", "-"))
		}
		if len(languages) > 0 {
			return languages
		}
	}
	return nil
}

func T(zh, en string) string {
	if english.Load() {
		return en
	}
	return zh
}

// loadLocale 读取数据目录中保存的界面语言：服务把它写在 host-preferences.json；
// 没有该字段时与服务端一致视为中文。
func loadLocale(root string) {
	b, err := os.ReadFile(filepath.Join(root, "host-preferences.json"))
	if err != nil {
		return
	}
	var saved struct {
		Preferences *struct {
			UILocale string `json:"uiLocale"`
		} `json:"preferences"`
	}
	if json.Unmarshal(b, &saved) != nil || saved.Preferences == nil {
		return
	}
	applyLocale(saved.Preferences.UILocale)
}

// applyLocale 切换原生文字的语言，返回是否有变化。
func applyLocale(locale string) bool {
	if locale == "" {
		locale = "zh-CN"
	}
	next := !strings.HasPrefix(locale, "zh")
	return english.Swap(next) != next
}

// hostLog 与服务日志分开记录宿主的关键步骤，服务不可用时也能留下原因。
func hostLog(root, stage string, err error) {
	folder := filepath.Join(root, "logs")
	if os.MkdirAll(folder, 0700) != nil {
		return
	}
	path := filepath.Join(folder, "host.log")
	if info, e := os.Stat(path); e == nil && info.Size() >= 5*1024*1024 {
		_ = os.Rename(path, path+".1")
	}
	entry := map[string]any{"time": time.Now().Format(time.RFC3339Nano), "stage": stage}
	if err != nil {
		entry["error"] = err.Error()
	}
	line, _ := json.Marshal(entry)
	file, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(line, '\n'))
}
