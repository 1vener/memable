// settings_repo.go：settings 杂项参数表（key/value）仓库。
// 用于存储 115 网盘 Cookie 等扩展配置，避免每次扩展都改 config.yaml。
// 代码注释使用中文。
package repo

import (
	"database/sql"
	"time"

	"memable/internal/errx"
)

// SettingsRepo 杂项参数仓库。
type SettingsRepo struct{ db *sql.DB }

func NewSettingsRepo(db *sql.DB) *SettingsRepo { return &SettingsRepo{db: db} }

// Get 读取参数值；不存在时返回 ("", nil)。
func (r *SettingsRepo) Get(key string) (string, error) {
	var v string
	err := r.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", errx.Wrapf(err, "读取参数 %s", key)
	}
	return v, nil
}

// Set 写入或更新参数值。
func (r *SettingsRepo) Set(key, value string) error {
	_, err := r.db.Exec(
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, value,
	)
	if err != nil {
		return errx.Wrapf(err, "写入参数 %s", key)
	}
	return nil
}

// Delete 删除参数；不存在时静默成功。
func (r *SettingsRepo) Delete(key string) error {
	if _, err := r.db.Exec(`DELETE FROM settings WHERE key = ?`, key); err != nil {
		return errx.Wrapf(err, "删除参数 %s", key)
	}
	return nil
}

// List 返回全部参数（key/value/updated_at），供设置页展示。
func (r *SettingsRepo) List() ([]SettingEntry, error) {
	rows, err := r.db.Query(`SELECT key, value, updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, errx.Wrapf(err, "列出参数")
	}
	defer rows.Close()
	out := make([]SettingEntry, 0)
	for rows.Next() {
		var e SettingEntry
		if err := rows.Scan(&e.Key, &e.Value, &e.UpdatedAt); err != nil {
			return nil, errx.Wrapf(err, "扫描参数行")
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SettingEntry 杂项参数条目。
type SettingEntry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}
