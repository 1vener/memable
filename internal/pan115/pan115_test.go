// pan115_test.go：115 客户端测试（httptest mock webapi）。
// 代码注释使用中文。
package pan115

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// newMockServer 构造 mock 115 webapi：
// files 接口按 cid+offset 返回配置的条目。
func newMockServer(t *testing.T, respond func(cid, offset string, w http.ResponseWriter)) *httptest.Server {
	return newMockServerWithStatus(t, `{"state":true}`, respond)
}

func newMockServerWithStatus(t *testing.T, statusBody string, respond func(cid, offset string, w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			if r.URL.Query().Get("ct") != "guide" || r.URL.Query().Get("ac") != "status" || r.URL.Query().Get("_") == "" {
				t.Errorf("Cookie 状态接口参数错误: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, statusBody)
		case "/files":
			assertFileListQuery(t, r.URL.Query())
			respond(r.URL.Query().Get("cid"), r.URL.Query().Get("offset"), w)
		default:
			http.Error(w, "unknown", 404)
		}
	}))
}

func assertFileListQuery(t *testing.T, q url.Values) {
	t.Helper()
	expected := map[string]string{
		"aid":              "1",
		"o":                "user_ptime",
		"asc":              "1",
		"show_dir":         "1",
		"limit":            strconv.Itoa(fileListLimit),
		"snap":             "0",
		"natsort":          "0",
		"record_open_time": "1",
		"format":           "json",
		"fc_mix":           "0",
	}
	for key, want := range expected {
		if got := q.Get(key); got != want {
			t.Errorf("files 参数 %s=%q，期望 %q", key, got, want)
		}
	}
}

func TestListDirFiltersFiles(t *testing.T) {

	srv := newMockServer(t, func(cid, offset string, w http.ResponseWriter) {
		if cid == "0" {
			fmt.Fprint(w, `{"state":true,"count":2,"data":[
				{"cid":"10","pid":"0","fid":"","n":"电影","ico":"folder"},
				{"cid":"11","pid":"0","fid":"11","n":"a.mp4","s":"100","sha":"aa"}
			]}`)
			return
		}
		fmt.Fprint(w, `{"state":true,"count":0,"data":[]}`)
	})
	c := NewClient("UID=1;CID=2;SEID=3", 0)
	c.SetBaseURL(srv.URL)
	dirs, err := c.ListDir(context.Background(), "0")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(dirs) != 1 || dirs[0].Name != "电影" || dirs[0].CID != "10" || !dirs[0].HasChildren {
		t.Fatalf("应只返回目录且带展开标记: %+v", dirs)
	}
}

func TestWalkDirRecursiveAndPaging(t *testing.T) {

	srv := newMockServer(t, func(cid, offset string, w http.ResponseWriter) {
		switch cid {
		case "0":
			fmt.Fprint(w, `{"state":true,"count":1,"data":[{"cid":"10","pid":"0","fid":"","n":"dir","ico":"folder"}]}`)
		case "10":
			if offset == "0" {
				fmt.Fprint(w, `{"state":true,"count":2,"data":[{"cid":"101","pid":"10","fid":"101","n":"a.jpg","s":"5","sha":"1111"}]}`)
			} else {
				fmt.Fprint(w, `{"state":true,"count":2,"data":[{"cid":"102","pid":"10","fid":"102","n":"b.jpg","s":"6","sha":"2222"}]}`)
			}
		default:
			fmt.Fprint(w, `{"state":true,"count":0,"data":[]}`)
		}
	})
	c := NewClient("UID=1", 0)
	c.SetBaseURL(srv.URL)
	files, err := c.WalkDir(context.Background(), "0")
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("应递归到 2 个文件，实际 %d: %+v", len(files), files)
	}
	a, ok := files["dir/a.jpg"]
	if !ok || a.Sha1 != "1111" || a.Size != 5 {
		t.Fatalf("dir/a.jpg 映射错误: %+v", files)
	}
	b, ok := files["dir/b.jpg"]
	if !ok || b.Sha1 != "2222" {
		t.Fatalf("dir/b.jpg 映射错误: %+v", files)
	}
}

func TestLoginCheckInvalidCookie(t *testing.T) {

	srv := newMockServerWithStatus(t, `{"state":false}`, func(cid, offset string, w http.ResponseWriter) {
		t.Errorf("无效 Cookie 不应请求文件列表")
	})
	c := NewClient("UID=bad", 0)
	c.SetBaseURL(srv.URL)
	err := c.LoginCheck(context.Background())
	if err == nil {
		t.Fatal("无效 Cookie 应报错")
	}
	if _, ok := err.(*ErrInvalidCookie); !ok {
		t.Fatalf("应为 ErrInvalidCookie，实际 %T: %v", err, err)
	}
}

func TestLoginCheckValidCookie(t *testing.T) {
	var gotQuery url.Values
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("Cookie 校验路径错误: %s", r.URL.Path)
		}
		gotQuery = r.URL.Query()
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"state":true}`)
	}))
	defer srv.Close()

	c := NewClient("UID=1;CID=2;SEID=3", 0)
	c.SetBaseURL(srv.URL)
	if err := c.LoginCheck(context.Background()); err != nil {
		t.Fatalf("有效 Cookie 不应报错: %v", err)
	}
	if gotCookie != "UID=1;CID=2;SEID=3" {
		t.Fatalf("Cookie 未正确发送: %q", gotCookie)
	}
	if gotQuery.Get("ct") != "guide" || gotQuery.Get("ac") != "status" {
		t.Fatalf("Cookie 校验参数错误: %v", gotQuery)
	}
	ts, err := strconv.ParseInt(gotQuery.Get("_"), 10, 64)
	if err != nil {
		t.Fatalf("_ 参数不是毫秒时间戳: %q", gotQuery.Get("_"))
	}
	if d := time.Since(time.UnixMilli(ts)); d < -time.Second || d > time.Second {
		t.Fatalf("_ 参数时间不合理: %v", d)
	}
}

func TestWalkDirRiskControl(t *testing.T) {

	srv := newMockServer(t, func(cid, offset string, w http.ResponseWriter) {
		fmt.Fprint(w, `{"state":false,"errno":999,"msg":"验证码"}`)
	})
	c := NewClient("UID=1", 0)
	c.SetBaseURL(srv.URL)
	_, err := c.WalkDir(context.Background(), "0")
	if err == nil {
		t.Fatal("风控应报错")
	}
	if _, ok := err.(*ErrRiskControl); !ok {
		t.Fatalf("应为 ErrRiskControl，实际 %T: %v", err, err)
	}
}
