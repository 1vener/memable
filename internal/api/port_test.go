// port_test.go：HTTP 端口配置与自动避让测试。
// 代码注释使用中文。
package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/repo"
)

func newPortTestServer(t *testing.T, port int) (*Server, func()) {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Path: ":memory:"},
		Server:   config.ServerConfig{Port: port},
	}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	lr := repo.NewLibraryRepo(dbh)
	sr := repo.NewSessionRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	tr := repo.NewTaskRepo(dbh)
	srv := NewServer(cfg, lr, sr, mr, tr, nil, nil, nil, nil, "", "", nil)
	done := make(chan error, 1)
	go func() { done <- srv.Start() }()
	// 等待端口确定（Start 内部避让后写入 ActualPort）
	deadline := time.Now().Add(5 * time.Second)
	for srv.ActualPort() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.ActualPort() == 0 {
		select {
		case err := <-done:
			t.Fatalf("服务器启动失败: %v", err)
		default:
			t.Fatal("服务器启动超时")
		}
	}
	cleanup := func() {
		_ = srv.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		_ = dbh.Close()
	}
	return srv, cleanup
}

// TestServerPortAutoAvoid 指定端口被占用时自动 +1 避让，且实际端口 health 可达。
func TestServerPortAutoAvoid(t *testing.T) {
	// 预占 12358（默认端口，绑定全接口使 Server 监听必然冲突）
	blocker, err := net.Listen("tcp", ":12358")
	if err != nil {
		t.Skipf("端口 12358 无法预占，跳过: %v", err)
	}
	defer blocker.Close()

	srv, cleanup := newPortTestServer(t, 12358)
	defer cleanup()
	port := srv.ActualPort()
	if port == 0 || port == 12358 {
		t.Fatalf("应避让到非 12358 端口，实际 %d", port)
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	if err != nil {
		t.Fatalf("health 请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health 状态码 = %d", resp.StatusCode)
	}
}

// TestServerPortRandom port=0 使用随机空闲端口。
func TestServerPortRandom(t *testing.T) {
	srv, cleanup := newPortTestServer(t, 0)
	defer cleanup()
	if srv.ActualPort() <= 0 {
		t.Fatalf("随机端口应 > 0，实际 %d", srv.ActualPort())
	}
}
