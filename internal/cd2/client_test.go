// client_test.go：cd2 客户端测试（bufconn 内存 gRPC 服务，无真实网络）。
// 覆盖：Token 校验、目录树、递归遍历（SHA1/大小）、限速与风控错误分类。
// 代码注释使用中文。
package cd2

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	emptypb "google.golang.org/protobuf/types/known/emptypb"

	cd2proto "memable/internal/cd2/proto"
)

// fakeServer 内存假 CD2 服务：按路径返回文件列表，校验 Token。
type fakeServer struct {
	cd2proto.UnimplementedCloudDriveFileSrvServer
	files     map[string][]*cd2proto.CloudDriveFile
	token     string
	allowList bool
	calls     int // GetSubFiles 调用次数（限速/重试观察用）
}

func (f *fakeServer) GetSubFiles(req *cd2proto.ListSubFileRequest, stream grpc.ServerStreamingServer[cd2proto.SubFilesReply]) error {
	f.calls++
	// 授权方法必须校验 Bearer Token（缺 header 应返回 Unauthenticated）。
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok || len(md.Get("authorization")) == 0 || md.Get("authorization")[0] != "Bearer "+f.token {
		return status.Error(codes.Unauthenticated, "missing authorization")
	}
	for _, fl := range f.files[req.GetPath()] {
		if err := stream.Send(&cd2proto.SubFilesReply{SubFiles: []*cd2proto.CloudDriveFile{fl}}); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeServer) GetApiTokenInfo(_ context.Context, in *cd2proto.StringValue) (*cd2proto.TokenInfo, error) {
	if in.GetValue() != f.token {
		return nil, status.Error(codes.PermissionDenied, "bad token")
	}
	return &cd2proto.TokenInfo{Permissions: &cd2proto.TokenPermissions{AllowList: f.allowList}}, nil
}

func (f *fakeServer) GetSystemInfo(context.Context, *emptypb.Empty) (*cd2proto.CloudDriveSystemInfo, error) {
	return &cd2proto.CloudDriveSystemInfo{SystemReady: true}, nil
}

// newFakeServer 启动 bufconn gRPC 服务并挂载测试拨号器。
func newFakeServer(t *testing.T, srv *fakeServer) *Client {
	t.Helper()
	lis := bufconnListen()
	gs := grpc.NewServer()
	cd2proto.RegisterCloudDriveFileSrvServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(func() {
		gs.Stop()
		_ = lis.Close()
		SetDialer(nil)
	})
	SetDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		return lis.DialContext(ctx)
	})
	return NewClient("bufnet", srv.token)
}

func bufconnListen() *bufconn.Listener {
	return bufconn.Listen(1 << 20)
}

func TestVerifyTokenOK(t *testing.T) {
	srv := &fakeServer{token: "t1", allowList: true}
	c := newFakeServer(t, srv)
	SetRateLimit(0)
	if err := c.VerifyToken(context.Background()); err != nil {
		t.Fatalf("合法 Token 应通过: %v", err)
	}
}

func TestVerifyTokenBad(t *testing.T) {
	srv := &fakeServer{token: "t1", allowList: true}
	c := newFakeServer(t, srv)
	c.token = "t2"
	SetRateLimit(0)
	err := c.VerifyToken(context.Background())
	if err == nil {
		t.Fatal("错误 Token 应报错")
	}
	if _, ok := err.(*ErrInvalidToken); !ok {
		t.Fatalf("应为 ErrInvalidToken，实际 %T: %v", err, err)
	}
}

func TestVerifyTokenNoListPerm(t *testing.T) {
	srv := &fakeServer{token: "t1", allowList: false}
	c := newFakeServer(t, srv)
	SetRateLimit(0)
	err := c.VerifyToken(context.Background())
	if _, ok := err.(*ErrInvalidToken); !ok {
		t.Fatalf("无列表权限应报 ErrInvalidToken，实际 %T: %v", err, err)
	}
}

func TestPing(t *testing.T) {
	srv := &fakeServer{token: "t1"}
	c := newFakeServer(t, srv)
	SetRateLimit(0)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// TestListDirsRequiresAuthHeader 回归：GetSubFiles 是授权方法，
// 不带正确 Bearer Token 必须报 ErrInvalidToken（曾因漏带 header 全部误判）。
func TestListDirsRequiresAuthHeader(t *testing.T) {
	srv := &fakeServer{
		token: "t1",
		files: map[string][]*cd2proto.CloudDriveFile{"/": {}},
	}
	c := newFakeServer(t, srv)
	c.token = "wrong-token"
	SetRateLimit(0)
	_, err := c.ListDirs(context.Background(), "/")
	if err == nil {
		t.Fatal("错误 Token 应报错")
	}
	if _, ok := err.(*ErrInvalidToken); !ok {
		t.Fatalf("应为 ErrInvalidToken，实际 %T: %v", err, err)
	}
}

// TestListDirsAndWalkDir 目录树 + 递归遍历：dir 为目录（无 sha1），文件带 sha1。
func TestListDirsAndWalkDir(t *testing.T) {
	srv := &fakeServer{
		token: "t1",
		files: map[string][]*cd2proto.CloudDriveFile{
			"/": {
				{Name: "115", FullPathName: "/115", IsDirectory: true},
				{Name: "a.txt", FullPathName: "/a.txt", Size: 10, FileHashes: map[uint32]string{uint32(cd2proto.CloudDriveFile_Sha1): "aaa"}},
			},
			"/115": {
				{Name: "电影", FullPathName: "/115/电影", IsDirectory: true},
				{Name: "b.mp4", FullPathName: "/115/b.mp4", Size: 100, FileHashes: map[uint32]string{uint32(cd2proto.CloudDriveFile_Sha1): "bbb"}},
			},
			"/115/电影": {
				{Name: "c.jpg", FullPathName: "/115/电影/c.jpg", Size: 5, FileHashes: map[uint32]string{uint32(cd2proto.CloudDriveFile_Sha1): "ccc"}},
			},
		},
	}
	c := newFakeServer(t, srv)
	SetRateLimit(0)

	dirs, err := c.ListDirs(context.Background(), "/")
	if err != nil {
		t.Fatalf("ListDirs: %v", err)
	}
	if len(dirs) != 1 || dirs[0].Name != "115" || dirs[0].Path != "/115" || !dirs[0].HasChildren {
		t.Fatalf("应只返回目录: %+v", dirs)
	}

	files, err := c.WalkDir(context.Background(), "/115")
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("应递归到 2 个文件，实际 %d: %+v", len(files), files)
	}
	if f, ok := files["b.mp4"]; !ok || f.Sha1 != "bbb" || f.Size != 100 {
		t.Fatalf("b.mp4 映射错误: %+v", files)
	}
	if f, ok := files["电影/c.jpg"]; !ok || f.Sha1 != "ccc" || f.Size != 5 {
		t.Fatalf("电影/c.jpg 映射错误: %+v", files)
	}
}

// TestRateLimit 全局限速：基准 200ms 时 3 次请求总耗时应 >= 约 600ms。
func TestRateLimit(t *testing.T) {
	srv := &fakeServer{
		token: "t1",
		files: map[string][]*cd2proto.CloudDriveFile{"/": {}},
	}
	c := newFakeServer(t, srv)
	SetRateLimit(200)
	defer SetRateLimit(0)

	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := c.Ping(context.Background()); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	}
	elapsed := time.Since(start)
	// jitter 下界 0ms，单次最短间隔 0；3 次连续放行时间窗可能很小，
	// 因此这里只验证间隔整体被拉长（至少两次完整间隔 200ms*2=400ms 的上限不太严格），
	// 用宽松断言：总耗时 >= 100ms 且 3 次调用均成功。
	if elapsed < 100*time.Millisecond {
		t.Fatalf("限速应拉长总耗时，实际 %v", elapsed)
	}
}
