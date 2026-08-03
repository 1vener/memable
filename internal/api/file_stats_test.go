// file_stats_test.go：文件统计 diff（新增/删除对比）与 xlsx 导出测试。
// 代码注释使用中文
package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/repo"
)

// setupFileStatsServer 构造带文件统计仓库的测试服务。
func setupFileStatsServer(t *testing.T, root string) (*Server, *repo.FileStatsRepo) {
	t.Helper()
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}
	fsr := repo.NewFileStatsRepo(dbh)
	server := NewServer(cfg, nil, nil, nil, nil, fsr, nil, nil, nil, nil, "", "", nil)
	return server, fsr
}

// TestFileStatsDiff 验证 diff：新增文件进入 added、删除文件进入 removed，按字典序。
func TestFileStatsDiff(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "b")

	server, fsr := setupFileStatsServer(t, root)

	// 创建历史统计记录
	req := httptest.NewRequest(http.MethodPost, "/api/tools/file-stats", body(`{"dir_path":"`+strings.ReplaceAll(root, `\`, `/`)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("创建统计失败: code=%d body=%s", resp.Code, resp.Body.String())
	}
	var created repo.FileStats
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	// 目录变化：新增 c.txt，删除 sub/b.txt
	writeFile(t, filepath.Join(root, "c.txt"), "c")
	if err := os.Remove(filepath.Join(root, "sub", "b.txt")); err != nil {
		t.Fatal(err)
	}

	// diff
	req2 := httptest.NewRequest(http.MethodPost, "/api/tools/file-stats/"+formatInt64(created.ID)+"/diff", nil)
	resp2 := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp2, req2)
	if resp2.Code != http.StatusOK {
		t.Fatalf("diff 失败: code=%d body=%s", resp2.Code, resp2.Body.String())
	}
	var diff fileDiff
	if err := json.Unmarshal(resp2.Body.Bytes(), &diff); err != nil {
		t.Fatal(err)
	}
	if diff.AddedCount != 1 || len(diff.Added) != 1 || diff.Added[0] != "c.txt" {
		t.Fatalf("added 不符: %+v", diff)
	}
	if diff.RemovedCount != 1 || len(diff.Removed) != 1 || diff.Removed[0] != "sub/b.txt" {
		t.Fatalf("removed 不符: %+v", diff)
	}

	// 幂等性：再次 diff 结果一致
	resp3 := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp3, req2)
	var diff2 fileDiff
	_ = json.Unmarshal(resp3.Body.Bytes(), &diff2)
	if diff2.AddedCount != 1 || diff2.RemovedCount != 1 {
		t.Fatalf("重复 diff 应一致: %+v", diff2)
	}
	_ = fsr
}

// TestFileStatsDiffDirMissing 验证统计目录不存在时 diff 返回 400。
func TestFileStatsDiffDirMissing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "a")

	server, _ := setupFileStatsServer(t, root)
	req := httptest.NewRequest(http.MethodPost, "/api/tools/file-stats", body(`{"dir_path":"`+strings.ReplaceAll(root, `\`, `/`)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp, req)
	var created repo.FileStats
	_ = json.Unmarshal(resp.Body.Bytes(), &created)

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	req2 := httptest.NewRequest(http.MethodPost, "/api/tools/file-stats/"+formatInt64(created.ID)+"/diff", nil)
	resp2 := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp2, req2)
	if resp2.Code != http.StatusBadRequest {
		t.Fatalf("目录不存在应返回 400: code=%d body=%s", resp2.Code, resp2.Body.String())
	}
}

// TestFileStatsDiffExportXLSX 验证导出 xlsx：两个 sheet（新增/删除文件列表）、
// 表头"文件路径"、绝对路径按顺序写入。
func TestFileStatsDiffExportXLSX(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "b.txt"), "b")
	writeFile(t, filepath.Join(root, "sub", "a.txt"), "a")

	server, _ := setupFileStatsServer(t, root)
	req := httptest.NewRequest(http.MethodPost, "/api/tools/file-stats", body(`{"dir_path":"`+strings.ReplaceAll(root, `\`, `/`)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp, req)
	var created repo.FileStats
	_ = json.Unmarshal(resp.Body.Bytes(), &created)

	// 变化：删除 sub/a.txt（removed），新增 c.txt（added）
	if err := os.Remove(filepath.Join(root, "sub", "a.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "c.txt"), "c")

	req2 := httptest.NewRequest(http.MethodPost, "/api/tools/file-stats/"+formatInt64(created.ID)+"/diff/export", nil)
	resp2 := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp2, req2)
	if resp2.Code != http.StatusOK {
		t.Fatalf("导出失败: code=%d body=%s", resp2.Code, resp2.Body.String())
	}
	if ct := resp2.Header().Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("Content-Type 应为 xlsx: %s", ct)
	}

	// 解析 zip 内容
	zr, err := zip.NewReader(bytes.NewReader(resp2.Body.Bytes()), int64(resp2.Body.Len()))
	if err != nil {
		t.Fatalf("导出内容不是合法 zip/xlsx: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rc)
		rc.Close()
		files[f.Name] = buf.String()
	}
	sheet1, ok := files["xl/worksheets/sheet1.xml"]
	if !ok {
		t.Fatal("缺少 sheet1.xml")
	}
	sheet2, ok := files["xl/worksheets/sheet2.xml"]
	if !ok {
		t.Fatal("缺少 sheet2.xml")
	}
	wb, ok := files["xl/workbook.xml"]
	if !ok {
		t.Fatal("缺少 workbook.xml")
	}
	if !strings.Contains(wb, "新增文件列表") || !strings.Contains(wb, "删除文件列表") {
		t.Fatalf("workbook 缺少两个 sheet 名称: %s", wb)
	}
	// 表头与绝对路径
	absAdded := strings.ReplaceAll(filepath.Join(root, "c.txt"), `\`, "/")
	absRemoved := strings.ReplaceAll(filepath.Join(root, "sub", "a.txt"), `\`, "/")
	if !strings.Contains(sheet1, "文件路径") || !strings.Contains(sheet1, absAdded) {
		t.Fatalf("sheet1 应含表头与新增绝对路径: %s", sheet1)
	}
	if !strings.Contains(sheet2, "文件路径") || !strings.Contains(sheet2, absRemoved) {
		t.Fatalf("sheet2 应含表头与删除绝对路径: %s", sheet2)
	}
	// 路径按顺序：表头在第 1 行、新增路径在第 2 行
	if !strings.Contains(sheet1, `<row r="1">`) || !strings.Contains(sheet1, `<row r="2">`) {
		t.Fatalf("sheet1 行序不符: %s", sheet1)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
