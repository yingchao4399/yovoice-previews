package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/schema"
)

// 版本 1 的旧数据打开时升级一次：作品补齐、助手旧消息转成内容块、内核补记版本，并留下升级前的副本。
func TestMigrateLegacyData(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	must(t, err)
	// 构造真实的官方版本 1 表集合，不能只改版本号而保留后来加入的定制表。
	for _, name := range []string{"instrument_sources", "arrangement_materials", "band_presets", "arrangement_jobs", "arrangement_scores"} {
		_, err = s.db.Exec("DROP TABLE " + name)
		must(t, err)
	}
	runtimePath, _ := json.Marshal(filepath.Join(root, "runtime", "v1.2.3-cuda-123", "bin", "audiocpp_server"))
	for _, step := range []struct {
		query string
		args  []any
	}{
		{"DELETE FROM drafts", nil},
		{"INSERT INTO drafts (key, pos, data) VALUES (?, ?, ?)", []any{"story", 0, `{"id":"story","title":"旧故事","kind":"story","subtitles":{"speakers":[],"cues":[{"start":0,"end":1,"text":"a","speakerId":"1"},{"id":"kept","start":1,"end":2,"text":"b","speakerId":"1"}]}}`}},
		{"INSERT INTO drafts (key, pos, data) VALUES (?, ?, ?)", []any{"segmented", 1, `{"id":"segmented","title":"分句","kind":"story"}`}},
		{"INSERT INTO drafts (key, pos, data) VALUES (?, ?, ?)", []any{"score", 2, `{"id":"score","title":"编曲","kind":"score"}`}},
		{"INSERT INTO drafts (key, pos, data) VALUES (?, ?, ?)", []any{"tracks", 3, `{"id":"tracks","title":"编曲","kind":"score","score":{"tempo":90,"timeSignature":[3,4],"tracks":null}}`}},
		{"INSERT INTO history (key, pos, data) VALUES (?, ?, ?)", []any{"new", 0, `{"id":"new","duration":3,"settings":{"id":"story"}}`}},
		{"INSERT INTO history (key, pos, data) VALUES (?, ?, ?)", []any{"old", 1, `{"id":"old","duration":5,"settings":{"id":"story"}}`}},
		{"INSERT INTO history (key, pos, data) VALUES (?, ?, ?)", []any{"whole", 2, `{"id":"whole","duration":5,"settings":{"id":"segmented"}}`}},
		{"INSERT INTO history (key, pos, data) VALUES (?, ?, ?)", []any{"line", 3, `{"id":"line","duration":1,"settings":{"id":"segmented"},"segment":{"cueId":"c","speakerId":"1","speakerName":"","batchId":"b","index":0}}`}},
		{"INSERT INTO settings (key, pos, data) VALUES ('assistantSessions', 0, ?) ON CONFLICT(key) DO UPDATE SET data = excluded.data", []any{`{"currentId":"s","sessions":[{"id":"s","title":"t","messages":[{"id":"m","role":"assistant","text":"好了","tools":[{"tool":"create_work","state":"done"}],"createdAt":"2026-10-01T00:00:00Z"}],"updatedAt":"2026-10-01T00:00:00Z"}]}`}},
		{"INSERT INTO settings (key, pos, data) VALUES ('runtimePath', 0, ?) ON CONFLICT(key) DO UPDATE SET data = excluded.data", []any{string(runtimePath)}},
		{"DELETE FROM settings WHERE key = 'runtimeVersion'", nil},
		{"PRAGMA user_version = 1", nil},
	} {
		_, err = s.db.Exec(step.query, step.args...)
		must(t, err)
	}
	must(t, s.Close())

	s, err = New(root)
	must(t, err)
	defer s.Close()
	state := s.Read()
	drafts := map[string]schema.Draft{}
	for _, d := range state.Drafts {
		drafts[d.ID] = d
	}
	if cues := drafts["story"].Subtitles.Cues; cues[0].ID != "legacy-story-0" || cues[1].ID != "kept" {
		t.Fatal("台词应补上与旧版一致的 ID", cues)
	}
	if lanes := drafts["story"].Timeline.Tracks; len(lanes) != 1 || lanes[0].ID != "legacy-new" || lanes[0].Clips[0].GenerationID != "new" || lanes[0].Clips[0].Duration != 3 {
		t.Fatal("旧故事应把最近一次整篇音频放上时间线", lanes)
	}
	if drafts["segmented"].Timeline != nil {
		t.Fatal("已有分句生成的作品不应放入整篇音频")
	}
	if score := drafts["score"].Score; score == nil || score.Tempo != 100 {
		t.Fatal("缺少乐谱的编曲作品应补上空乐谱")
	}
	var data string
	must(t, s.db.QueryRow("SELECT data FROM drafts WHERE key = 'tracks'").Scan(&data))
	if !strings.Contains(data, `"tracks":[]`) {
		t.Fatal("乐谱的 tracks 应保存为数组", data)
	}
	message := state.AssistantSessions.Sessions[0].Messages[0]
	if len(message.Blocks) != 2 || message.Blocks[0].Card.Tool != "create_work" || message.Blocks[1].Text != "好了" {
		t.Fatal("助手旧消息的工具卡片应转成内容块", message.Blocks)
	}
	must(t, s.db.QueryRow("SELECT data FROM settings WHERE key = 'assistantSessions'").Scan(&data))
	if strings.Contains(data, `"tools"`) {
		t.Fatal("升级后不应再保存旧的 tools 字段")
	}
	if state.RuntimeVersion == nil || *state.RuntimeVersion != "v1.2.3" {
		t.Fatal("应从安装目录名补记旧内核版本")
	}
	var version int
	must(t, s.db.QueryRow("PRAGMA user_version").Scan(&version))
	if version != schemaVersion {
		t.Fatal("升级后版本应为当前版本", version)
	}
	if files, _ := filepath.Glob(filepath.Join(root, upgradeSnapshot+"-v1-*.db")); len(files) != 1 {
		t.Fatal("升级前应留一份副本", files)
	}
}

// 新建的状态库直接是当前版本，不留升级副本。
func TestNewDatabaseIsCurrent(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	must(t, err)
	defer s.Close()
	var version int
	must(t, s.db.QueryRow("PRAGMA user_version").Scan(&version))
	if version != schemaVersion {
		t.Fatal(version)
	}
	if files, _ := filepath.Glob(filepath.Join(root, upgradeSnapshot+"-*.db")); len(files) != 0 {
		t.Fatal("新建的状态库不应留升级副本", files)
	}
}

func TestCustomSixMigrationPreservesArrangementRows(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	must(t, err)
	defer s.Close()
	tables := []string{"instrument_sources", "arrangement_materials", "band_presets", "arrangement_jobs", "arrangement_scores"}
	data := `{"id":"kept","sourceId":"source-kept","plugin":{"stateData":"AQID"},"steps":[{"partId":"part-kept"}]}`
	for _, table := range tables {
		_, err = s.db.Exec("INSERT INTO "+table+" (key,pos,data) VALUES ('kept',0,?)", data)
		must(t, err)
	}
	_, err = s.db.Exec("PRAGMA user_version=6")
	must(t, err)
	must(t, migrate(s.db, root))
	for _, table := range tables {
		var actual string
		must(t, s.db.QueryRow("SELECT data FROM "+table+" WHERE key='kept'").Scan(&actual))
		if actual != data {
			t.Fatal("升级改写了定制记录", table)
		}
	}
	var version int
	must(t, s.db.QueryRow("PRAGMA user_version").Scan(&version))
	if version != schemaVersion {
		t.Fatal(version)
	}
}

func TestOfficialTwoMigrationAndFailureRollback(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "official-two", true: "rollback"}[broken], func(t *testing.T) {
			root := t.TempDir()
			s, err := New(root)
			must(t, err)
			defer s.Close()
			for _, name := range []string{"instrument_sources", "arrangement_materials", "band_presets", "arrangement_jobs", "arrangement_scores"} {
				_, err = s.db.Exec("DROP TABLE " + name)
				must(t, err)
			}
			_, err = s.db.Exec("PRAGMA user_version=2")
			must(t, err)
			if broken {
				_, err = s.db.Exec("UPDATE drafts SET data='broken-json'")
				must(t, err)
			}
			err = migrate(s.db, root)
			if broken {
				if err == nil {
					t.Fatal("损坏记录没有阻止升级")
				}
				var version, count int
				must(t, s.db.QueryRow("PRAGMA user_version").Scan(&version))
				must(t, s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='instrument_sources'").Scan(&count))
				if version != 2 || count != 0 {
					t.Fatal("失败升级没有完整回滚", version, count)
				}
				return
			}
			must(t, err)
			for _, table := range []string{"instrument_sources", "arrangement_materials", "band_presets", "arrangement_jobs", "arrangement_scores"} {
				var count int
				must(t, s.db.QueryRow("SELECT count(*) FROM "+table).Scan(&count))
				if count != 0 {
					t.Fatal("官方库新增表应为空", table)
				}
			}
			var count int
			must(t, s.db.QueryRow("SELECT count(*) FROM drafts").Scan(&count))
			if count == 0 {
				t.Fatal("原作品丢失")
			}
		})
	}
}
