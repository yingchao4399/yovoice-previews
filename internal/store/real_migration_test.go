package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// 只接收 SQLite 在线备份副本，绝不打开正在使用的真实数据目录。
func TestRealDatabaseAppendOnlyMigration(t *testing.T) {
	fixture := os.Getenv("YOVOICE_DATABASE_BACKUP_FIXTURE")
	if fixture == "" {
		t.Skip("需要真实数据库的只读备份副本")
	}
	body, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, DatabaseFile)
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	read := func() (int, map[string]string) {
		t.Helper()
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var version int
		if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			t.Fatal(err)
		}
		records := map[string]string{}
		for _, table := range []string{"drafts", "characters", "voices", "models", "history", "ai_providers", "private", "instrument_sources", "arrangement_materials", "band_presets"} {
			rows, err := db.Query("SELECT key,data FROM " + table)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for rows.Next() {
				var key, data string
				if err = rows.Scan(&key, &data); err != nil {
					t.Fatal(err)
				}
				records[table+"/"+key] = data
				count++
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			t.Logf("%s: %d 条（不输出内容）", table, count)
		}
		return version, records
	}
	before, records := read()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	after, migrated := read()
	if after != schemaVersion || len(records) != len(migrated) {
		t.Fatal("迁移版本或既有记录数量不符")
	}
	for key, data := range records {
		if migrated[key] != data {
			t.Fatalf("追加迁移改写了既有记录 %s", key)
		}
	}
	t.Logf("真实备份版本 %d → %d，既有作品、音源、配置与密钥记录逐字节保留", before, after)
}
