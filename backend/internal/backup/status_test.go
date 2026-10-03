package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestReadMissing 文件缺失 → 可见的「尚未执行过备份」异常态，而非静默 ok。
func TestReadMissing(t *testing.T) {
	s := Read(filepath.Join(t.TempDir(), "nope.json"))
	if s.OK {
		t.Fatal("缺失文件不应 ok")
	}
	if s.Push != "unknown" || s.Error == "" {
		t.Fatalf("应给出 unknown+原因, got push=%q err=%q", s.Push, s.Error)
	}
}

// TestReadExplicitPath env 指定路径时只看该路径（容器内挂载场景）。
func TestReadExplicitPath(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "last-backup.json",
		`{"last_run_at":"2026-10-03T03:00:00+0800","last_success_at":"2026-10-03T03:00:00+0800","ok":true,"file":"jizhang-20261003-030000.sql.gz","size":"12K","push":"ok","error":""}`)
	s := Read(p)
	if !s.OK || s.LastSuccessAt == "" || s.Push != "ok" || s.File == "" {
		t.Fatalf("解析结果不对: %+v", s)
	}
	// 指定了 env 路径时不应再回退到候选路径
	if got := Read(filepath.Join(dir, "other.json")); got.OK || got.Error == "" {
		t.Fatalf("显式路径缺失应报异常, got %+v", got)
	}
}

// TestReadCorrupt 损坏文件 → health 可见异常。
func TestReadCorrupt(t *testing.T) {
	p := writeFile(t, t.TempDir(), "last-backup.json", `{"ok": tru`)
	s := Read(p)
	if s.OK || s.Error == "" {
		t.Fatalf("损坏文件应报异常, got %+v", s)
	}
}

// TestReadFailedRun 失败态保留上次成功时间（供「多久没成功备份」判断）。
func TestReadFailedRun(t *testing.T) {
	p := writeFile(t, t.TempDir(), "last-backup.json",
		`{"last_run_at":"2026-10-03T03:00:00+0800","last_success_at":"2026-10-02T03:00:00+0800","ok":false,"push":"failed","error":"rclone push failed: 403"}`)
	s := Read(p)
	if s.OK {
		t.Fatal("失败态 ok 应为 false")
	}
	if s.LastSuccessAt != "2026-10-02T03:00:00+0800" {
		t.Fatalf("last_success_at 应保留上次成功, got %q", s.LastSuccessAt)
	}
	if s.Push != "failed" || s.Error == "" {
		t.Fatalf("push/error 应保留, got %+v", s)
	}
}

// TestCandidates 兜底候选：仓库根与 backend/ 两种启动方式都能命中。
func TestCandidates(t *testing.T) {
	if got := candidates("/x/y.json"); len(got) != 1 || got[0] != "/x/y.json" {
		t.Fatalf("env 指定应单路径, got %v", got)
	}
	got := candidates("")
	if len(got) != 2 {
		t.Fatalf("无 env 应两个候选, got %v", got)
	}
}
