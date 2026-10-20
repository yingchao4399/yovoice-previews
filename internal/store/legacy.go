package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"yovoice/internal/diag"
	"yovoice/internal/msg"
	"yovoice/internal/platform"
	"yovoice/internal/schema"
)

// 旧版把全部状态保存在 state.json，并在每次写入前把上一版复制到 state.backup.json。
// 首次以新版本打开时导入数据库，原文件改名为 state.migrated.json 留作回退依据。
const (
	legacyState    = "state.json"
	legacyBackup   = "state.backup.json"
	legacyMigrated = "state.migrated.json"
)

// readLegacy 读取旧版状态文件；主文件损坏时使用备份并保留损坏原件，绝不以空工程覆盖用户数据。
func readLegacy(root string) (state schema.State, found bool, err error) {
	state = schema.DefaultState()
	b, e := os.ReadFile(filepath.Join(root, legacyState))
	if e == nil {
		if string(b) != "null" && json.Unmarshal(b, &state) == nil {
			return withLegacyLocale(state), true, nil
		}
		backup, backupErr := os.ReadFile(filepath.Join(root, legacyBackup))
		recovered := schema.DefaultState()
		if backupErr != nil || string(backup) == "null" || json.Unmarshal(backup, &recovered) != nil {
			return state, false, msg.Err(msg.ErrStateCorrupt, nil)
		}
		if e = os.Rename(filepath.Join(root, legacyState), filepath.Join(root, "state.corrupt-"+time.Now().Format("20060102-150405.000000000")+".json")); e != nil {
			return state, false, e
		}
		diag.Log(root, "state.recovered", "source", legacyBackup)
		pruneCorrupt(root, keepCorrupt)
		return withLegacyLocale(recovered), true, nil
	} else if !os.IsNotExist(e) {
		return state, false, e
	}
	backup, e := os.ReadFile(filepath.Join(root, legacyBackup))
	if os.IsNotExist(e) {
		return state, false, nil
	} else if e != nil {
		return state, false, e
	}
	if string(backup) == "null" || json.Unmarshal(backup, &state) != nil {
		return state, false, msg.Err(msg.ErrStateCorrupt, nil)
	}
	diag.Log(root, "state.recovered", "source", legacyBackup)
	return withLegacyLocale(state), true, nil
}

// withLegacyLocale：旧版 state.json 没有 uiLocale 时视为中文。
func withLegacyLocale(state schema.State) schema.State {
	if state.Preferences.UiLocale == "" {
		state.Preferences.UiLocale = schema.UiLocaleZhCN
	}
	return state
}

// retireLegacy 在数据库就绪后移走旧状态文件，避免下次启动重复导入或被误认为当前数据。
func retireLegacy(root string) error {
	for _, name := range []string{legacyState, legacyBackup} {
		path := filepath.Join(root, name)
		if _, e := os.Stat(path); e != nil {
			continue
		}
		if e := os.Rename(path, filepath.Join(root, legacyMigrated)); e != nil {
			return e
		}
		break
	}
	if e := os.Remove(filepath.Join(root, legacyBackup)); e != nil && !os.IsNotExist(e) {
		return e
	}
	return nil
}

// keepCorrupt 限制损坏快照数量：保留最近几份用于排查，避免反复损坏时无限占用空间。
const keepCorrupt = 3

func pruneCorrupt(root string, keep int) {
	pruneSnapshots(root, "state.corrupt-*.json", keep)
	pruneSnapshots(root, "yovoice.corrupt-*.db", keep)
}

func pruneSnapshots(root, pattern string, keep int) {
	files, _ := filepath.Glob(filepath.Join(root, pattern))
	// 文件名带纳秒时间戳，字典序即时间序。
	slices.Sort(files)
	for len(files) > keep {
		removeDB(files[0])
		files = files[1:]
	}
}

func writeState(path string, state schema.State) error {
	b, e := json.MarshalIndent(state, "", "  ")
	if e != nil {
		return e
	}
	return writeFileAtomic(path, b)
}

// writeFileAtomic 先写临时文件并落盘再改名，读取方不会看到写到一半的内容。
func writeFileAtomic(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	syncDir(filepath.Dir(path))
	return nil
}

func PrepareDefault() error {
	target, e := DefaultDirectory()
	if e != nil {
		return e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	legacy := filepath.Join(home, "Library", "Application Support", "VoiceWorkbench")
	if runtime.GOOS == "windows" {
		legacy = filepath.Join(os.Getenv("LOCALAPPDATA"), "VoiceWorkbench")
	}
	return Migrate(legacy, target)
}

// Migrate 把旧版 VoiceWorkbench 数据目录移动为新目录，并改写指向旧目录的绝对路径。
// 改写后的 state.json 会在首次打开时导入数据库。
func Migrate(legacy, target string) error {
	if _, e := os.Stat(target); e == nil {
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	if _, e := os.Stat(legacy); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	lock, e := platform.Lock(filepath.Join(legacy, "service.lock"))
	if e != nil {
		return e
	}
	defer lock.Close()
	var state schema.State
	var private map[string]string
	// 旧目录可能已是新版数据库，也可能只有 state.json。
	fromDB := false
	if _, e = os.Stat(filepath.Join(legacy, DatabaseFile)); e == nil {
		fromDB = true
	} else if !os.IsNotExist(e) {
		return e
	}
	hasState := fromDB
	if fromDB {
		db, e := openDB(filepath.Join(legacy, DatabaseFile))
		if e != nil {
			return e
		}
		state, _, e = load(db)
		if e == nil {
			private, e = loadPrivate(db)
		}
		if ce := db.Close(); e == nil {
			e = ce
		}
		if e != nil {
			return e
		}
	} else {
		b, e := os.ReadFile(filepath.Join(legacy, legacyState))
		hasState = e == nil
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if hasState {
			if string(b) == "null" {
				return msg.Err(msg.ErrLegacyRestore, nil)
			}
			if e = json.Unmarshal(b, &state); e != nil {
				return e
			}
		}
	}
	if hasState {
		rebase := func(p *string) *string {
			if p == nil {
				return nil
			}
			rel, e := filepath.Rel(legacy, *p)
			if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				rebased := filepath.Join(target, rel)
				return &rebased
			}
			return p
		}
		state.RuntimePath = rebase(state.RuntimePath)
		state.Preferences.ModelDirectory = rebase(state.Preferences.ModelDirectory)
		for i := range state.Models {
			state.Models[i].Path = *rebase(&state.Models[i].Path)
		}
	}
	// Windows 文件锁不允许移动父目录；迁移只在启动新服务前执行。
	if runtime.GOOS == "windows" {
		lock.Close()
	}
	if e = os.Rename(legacy, target); e != nil {
		return e
	}
	if hasState {
		write := func() error { return writeState(filepath.Join(target, legacyState), state) }
		if fromDB {
			// createDB 先写临时库再改名，原子替换移动过来的数据库。
			write = func() error { return createDB(filepath.Join(target, DatabaseFile), state, private) }
		}
		if e = write(); e != nil {
			if rollback := os.Rename(target, legacy); rollback != nil {
				return msg.Err(msg.ErrUnknown, msg.Params{"detail": fmt.Sprintf("%v; rollback failed: %v", e, rollback)})
			}
			return e
		}
	}
	return nil
}
