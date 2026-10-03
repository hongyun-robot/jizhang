// Package backup 读取备份脚本产出的状态文件，供 /api/v1/health 暴露最近备份情况
// （design.md D10：备份失败无人知晓 → health 暴露最近备份时间与错误）。
package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Status docker/backup.sh 写入的最近一次备份状态（last-backup.json）。
type Status struct {
	LastRunAt     string `json:"last_run_at"`     // 最近一次执行时间
	LastSuccessAt string `json:"last_success_at"` // 最近一次本机 dump 成功时间
	OK            bool   `json:"ok"`              // 最近一次执行整体成功（dump 成功且 OSS 推送成功/按配置跳过）
	File          string `json:"file,omitempty"`  // 产出文件名
	Size          string `json:"size,omitempty"`  // 产出文件大小
	Push          string `json:"push"`            // oss: ok / skipped / failed
	Error         string `json:"error,omitempty"` // 失败原因（最近一次失败保留）
}

// candidates 状态文件候选路径：env 指定时只看该路径；
// 否则兼容「仓库根启动」与「backend/ 启动」两种本地开发方式。
func candidates(envPath string) []string {
	if envPath != "" {
		return []string{envPath}
	}
	return []string{
		filepath.Join("backups", "last-backup.json"),
		filepath.Join("..", "backups", "last-backup.json"),
	}
}

// Read 读取最近备份状态；文件缺失或损坏时不报错，返回可见的异常状态
// （health 语义：拿不到状态本身就是需要被看见的异常）。
func Read(envPath string) Status {
	notRun := Status{OK: false, Push: "unknown", Error: "尚未执行过备份"}
	for _, p := range candidates(envPath) {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var s Status
		if err := json.Unmarshal(data, &s); err != nil {
			return Status{OK: false, LastRunAt: s.LastRunAt, Push: "unknown", Error: "备份状态文件损坏"}
		}
		if s.Push == "" {
			s.Push = "unknown"
		}
		return s
	}
	if envPath != "" {
		notRun.Error = "备份状态文件不存在: " + envPath
	}
	return notRun
}
