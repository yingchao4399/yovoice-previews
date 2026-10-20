package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

func TestStoreDoesNotReplayPreviousFailure(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	must(t, err)
	for _, status := range []string{"failed", "running"} {
		must(t, store.Update(func(state *schema.State) {
			code := msg.ErrAudioDecode
			state.Activity = &schema.Activity{Kind: "generate", Status: status, Code: msg.ActivityFailed, ErrorCode: &code}
			state.ModelActivity = &schema.Activity{Kind: "download", Status: status, Code: msg.ActivityFailed, ErrorCode: &code}
		}, true))
		reopened, err := New(root)
		must(t, err)
		// 生成与模型下载分开记录，两者都按同样的规则整理。
		for _, activity := range []*schema.Activity{reopened.Read().Activity, reopened.Read().ModelActivity} {
			if status == "failed" && activity != nil {
				t.Fatal("重启不应重新显示历史失败提示")
			}
			if status == "running" && (activity == nil || activity.Status != "interrupted" || activity.ErrorCode != nil) {
				t.Fatal("未结束的操作仍应标记为中断")
			}
		}
	}
}

func TestCorruptSnapshotsAreBounded(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	must(t, err)
	must(t, s.Update(func(state *schema.State) { state.Drafts[0].Title = "保留" }, true))
	must(t, s.Close())
	// 重新打开会刷新备份。
	s, err = New(root)
	must(t, err)
	must(t, s.Close())
	for i := 0; i < keepCorrupt+2; i++ {
		must(t, os.WriteFile(filepath.Join(root, DatabaseFile), []byte("broken"), 0600))
		reopened, err := New(root)
		must(t, err)
		if reopened.Read().Drafts[0].Title != "保留" {
			t.Fatal("备份未恢复数据")
		}
		must(t, reopened.Close())
	}
	files, _ := filepath.Glob(filepath.Join(root, "yovoice.corrupt-*.db"))
	if len(files) != keepCorrupt {
		t.Fatal("损坏快照数量未受限", len(files))
	}
}

func TestWriteReportsMissingDataRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	err = s.Update(func(state *schema.State) { state.Drafts = append(state.Drafts, schema.DefaultDraft()) }, true)
	if ce, ok := err.(*msg.CallError); !ok || ce.Code != msg.ErrDataRootMissing {
		t.Fatal("数据目录被删除后应返回明确的错误", err)
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("不能自动重建已删除的数据目录")
	}
}

func TestLegacyStateImported(t *testing.T) {
	root := t.TempDir()
	legacy := schema.DefaultState()
	legacy.Drafts[0].Title = "旧作品"
	legacy.Voices = []schema.Voice{{ID: "v1", Name: "旁白"}}
	legacy.Preferences.UiLocale = ""
	must(t, writeState(filepath.Join(root, legacyState), legacy))
	must(t, writeState(filepath.Join(root, legacyBackup), schema.DefaultState()))
	s, err := New(root)
	must(t, err)
	state := s.Read()
	if state.Drafts[0].Title != "旧作品" || len(state.Voices) != 1 || state.Voices[0].Name != "旁白" {
		t.Fatal("旧状态未导入", state.Drafts, state.Voices)
	}
	if state.Preferences.UiLocale != schema.UiLocaleZhCN {
		t.Fatal("旧版没有界面语言时应视为中文", state.Preferences.UiLocale)
	}
	for name, want := range map[string]bool{legacyState: false, legacyBackup: false, legacyMigrated: true, DatabaseFile: true, backupFile: true} {
		if _, err := os.Stat(filepath.Join(root, name)); (err == nil) != want {
			t.Fatal("导入后文件状态不正确", name, err)
		}
	}
	host, err := os.ReadFile(filepath.Join(root, hostPreferences))
	must(t, err)
	if !strings.Contains(string(host), `"uiLocale":"zh-CN"`) {
		t.Fatal("宿主语言文件不正确", string(host))
	}
	must(t, s.Update(func(state *schema.State) { state.Preferences.UiLocale = schema.UiLocaleEn }, true))
	host, err = os.ReadFile(filepath.Join(root, hostPreferences))
	must(t, err)
	if !strings.Contains(string(host), `"uiLocale":"en"`) {
		t.Fatal("切换语言后宿主语言文件未更新", string(host))
	}
	must(t, s.Close())
	reopened, err := New(root)
	must(t, err)
	if reopened.Read().Drafts[0].Title != "旧作品" || reopened.Read().Preferences.UiLocale != schema.UiLocaleEn {
		t.Fatal("重新打开后数据丢失")
	}
}

func TestLegacyCorruptStateUsesBackup(t *testing.T) {
	root := t.TempDir()
	backup := schema.DefaultState()
	backup.Drafts[0].Title = "备份"
	must(t, writeState(filepath.Join(root, legacyBackup), backup))
	must(t, os.WriteFile(filepath.Join(root, legacyState), []byte("broken"), 0600))
	s, err := New(root)
	must(t, err)
	if s.Read().Drafts[0].Title != "备份" {
		t.Fatal("未从旧版备份导入")
	}
	if files, _ := filepath.Glob(filepath.Join(root, "state.corrupt-*.json")); len(files) != 1 {
		t.Fatal("损坏原件未保留")
	}
}

func TestStateRoundTrip(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	must(t, err)
	path, backend := "/engine", "cpu"
	must(t, s.Update(func(state *schema.State) {
		second := schema.DefaultDraft()
		second.Title = "第二个"
		// 重复或缺失的 ID 也要完整保存，顺序保持不变。
		dup := schema.DefaultDraft()
		dup.ID = state.Drafts[0].ID
		state.Drafts = append(state.Drafts, second, dup, schema.Draft{Title: "无 ID"})
		state.Characters = []schema.Character{{ID: "c", Name: "角色"}}
		state.Voices = []schema.Voice{{ID: "v", Name: "音频"}}
		state.Models = []schema.InstalledModel{{ID: "m", Path: "/m"}}
		state.History = []schema.Generation{{ID: "g", Title: "历史"}}
		state.AIProviders = []schema.AIProvider{{ID: "p"}}
		state.AIProviderID = "p"
		state.RuntimePath, state.RuntimeBackend = &path, &backend
		state.Runtimes = map[string]schema.InstalledRuntime{"cpu": {Path: path, Version: "1"}}
		state.Preferences.Backend = "metal"
		state.AssistantSessions = &schema.AssistantSessions{CurrentID: "s1", Sessions: []schema.AssistantSession{{ID: "s1", Title: "你好", Messages: []schema.AssistantMessage{{ID: "m1", Role: "user", Text: "你好"}}}}}
		state.AssistantLive = &schema.AssistantLive{Queued: 2}
	}, true))
	want := s.Read()
	must(t, s.Close())
	reopened, err := New(root)
	must(t, err)
	got := reopened.Read()
	// 助手实时状态只属于会话，重开后清除；会话记录保留。
	want.AssistantLive = nil
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatalf("状态往返不一致\n%s\n%s", a, b)
	}
	// 删除记录后不应残留。
	must(t, reopened.Update(func(state *schema.State) { state.Drafts = state.Drafts[:1]; state.History = nil }, true))
	must(t, reopened.Close())
	again, err := New(root)
	must(t, err)
	if len(again.Read().Drafts) != 1 || len(again.Read().History) != 0 {
		t.Fatal("删除的记录被保留", len(again.Read().Drafts), len(again.Read().History))
	}
}

// 新增 State 字段时必须决定是否持久化，避免字段被静默丢弃。
func TestEveryStateFieldIsPersistedOrSessionOnly(t *testing.T) {
	covered := slices.Clone(sessionFields)
	for _, f := range settingFields {
		covered = append(covered, f.field)
	}
	for _, t := range tables {
		if t.field != "" {
			covered = append(covered, t.field)
		}
	}
	typ := reflect.TypeFor[schema.State]()
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if !slices.Contains(covered, name) {
			t.Errorf("State.%s 未纳入状态库", typ.Field(i).Name)
		}
	}
}

func TestFailedCommitKeepsState(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	must(t, err)
	journal := filepath.Join(root, DatabaseFile+"-journal")
	must(t, os.Mkdir(journal, 0700))
	if s.Update(func(state *schema.State) { state.Drafts[0].Title = "失败" }, true) == nil {
		t.Fatal("应写入失败")
	}
	if s.Read().Drafts[0].Title == "失败" {
		t.Fatal("写入失败后内存状态被修改")
	}
	must(t, os.Remove(journal))
	must(t, s.Update(func(state *schema.State) { state.Drafts[0].Title = "成功" }, true))
	must(t, s.Close())
	reopened, err := New(root)
	must(t, err)
	if reopened.Read().Drafts[0].Title != "成功" {
		t.Fatal("恢复后的写入未保存")
	}
}

func TestNewerSchemaIsRejected(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	must(t, err)
	_, err = s.db.Exec("PRAGMA user_version = 99")
	must(t, err)
	must(t, s.Close())
	if _, err = New(root); err == nil {
		t.Fatal("不应打开更高版本的状态库")
	}
}
