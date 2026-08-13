// transcode_test.go：转码兜底接口测试（缓存命中、状态查询、产物服务）。
// 代码注释使用中文。
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"memable/internal/repo"
)

// TestTranscodeCacheHitAndFile 转码产物已存在时直接返回 done，且产物可经
// /api/transcode/{name} 下载（避免真实转码耗时，直接预置产物文件）。
func TestTranscodeCacheHitAndFile(t *testing.T) {
	server, mr, libID, root := setupMediaFileServer(t)
	// 需要视频媒体：复用图片库，另建一条视频记录（真实文件，resolveMediaAbsPath 会 stat）
	srcAbs := filepath.Join(root, "sub", "v.mov")
	if err := os.MkdirAll(filepath.Dir(srcAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcAbs, []byte("fake-mov"), 0o644); err != nil {
		t.Fatal(err)
	}
	video := &repo.Media{
		LibraryID: libID, Kind: "video", RelativePath: "sub/v.mov",
		FileSize: 8, Mtime: time.Now().UTC(),
	}
	if err := mr.Upsert(video); err != nil {
		t.Fatal(err)
	}
	// 预置转码产物（内容寻址 = id + mtime）
	out := transcodeOutPath(video.ID, video.Mtime)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("fake-mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(out)) })

	// 1) 触发转码 → 命中缓存直接 done
	req := httptest.NewRequest(http.MethodPost,
		"/api/media/"+formatInt64(video.ID)+"/transcode", nil)
	rec := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("transcode code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != transcodeDone {
		t.Fatalf("status = %v, want done", resp["status"])
	}
	name, _ := resp["name"].(string)
	if name == "" {
		t.Fatal("缺少产物文件名")
	}

	// 2) 状态查询 → done
	req = httptest.NewRequest(http.MethodGet,
		"/api/media/"+formatInt64(video.ID)+"/transcode-status", nil)
	rec = httptest.NewRecorder()
	server.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code=%d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != transcodeDone {
		t.Fatalf("status = %v, want done", resp["status"])
	}

	// 3) 产物下载
	req = httptest.NewRequest(http.MethodGet, "/api/transcode/"+name, nil)
	rec = httptest.NewRecorder()
	server.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("产物下载 code=%d", rec.Code)
	}
	if rec.Body.String() != "fake-mp4" {
		t.Fatalf("产物内容 = %q", rec.Body.String())
	}

	// 4) 路径穿越防护：非 .mp4 / 含路径分隔符 → 400/404
	for _, bad := range []string{"..%2F..%2Fsecret.mp4", "x.txt"} {
		req = httptest.NewRequest(http.MethodGet, "/api/transcode/"+bad, nil)
		rec = httptest.NewRecorder()
		server.http.Handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("非法文件名 %q 应被拒绝", bad)
		}
	}
}

// TestTranscodeInvalid 非视频媒体拒绝转码。
func TestTranscodeInvalid(t *testing.T) {
	server, mr, libID, _ := setupMediaFileServer(t)
	medias, err := mr.ListByLibrary(libID)
	if err != nil || len(medias) != 1 {
		t.Fatalf("读取媒体失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost,
		"/api/media/"+formatInt64(medias[0].ID)+"/transcode", nil)
	rec := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("图片转码应 400, got %d", rec.Code)
	}
}
