// 包 cd2：CloudDrive2 gRPC API 客户端。
// 基于官方 clouddrive.proto v1.0.13 生成（internal/cd2/proto），零第三方运行时依赖
// （仅 google.golang.org/grpc + protobuf）。仅实现本应用需要的只读能力：
// 服务/Token 校验、目录列表、递归遍历（取文件 SHA1/大小）。
// 限速内置：全局限速器（基准 + jitter，见 limiter.go），每次 RPC 建立前等待一次，
// 重试同样重新限速；流内逐条返回不做额外等待。
// 代码注释使用中文。
package cd2

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"

	cd2proto "memable/internal/cd2/proto"
)

// DefaultAddr CloudDrive2 本地 gRPC 默认地址。
const DefaultAddr = "127.0.0.1:19798"

// FileInfo 网盘文件信息（遍历结果）。
type FileInfo struct {
	Name string // 文件名（不含路径）
	Size int64  // 文件大小（字节）
	Sha1 string // 文件 SHA1（内容指纹）
}

// DirEntry 目录直属项（目录树懒加载用）。
type DirEntry struct {
	Path        string `json:"path"`         // CD2 完整路径
	Name        string `json:"name"`         // 目录名
	HasChildren bool   `json:"has_children"` // 统一 true：点击展开时才请求，空目录显示为空
}

// ErrInvalidToken Token 无效/无权限错误。
type ErrInvalidToken struct{ msg string }

func (e *ErrInvalidToken) Error() string { return e.msg }

// ErrNotReady 服务未就绪（CD2 未登录/未启动）错误。
type ErrNotReady struct{ msg string }

func (e *ErrNotReady) Error() string { return e.msg }

// Client CloudDrive2 gRPC 客户端。
type Client struct {
	addr    string
	token   string
	once    sync.Once
	conn    *grpc.ClientConn
	stub    cd2proto.CloudDriveFileSrvClient
	dialErr error
	retries int
}

// testDialer 覆盖拨号方式（测试用：bufconn）。
var testDialer func(ctx context.Context, addr string) (net.Conn, error)

// SetDialer 设置测试拨号器；传 nil 恢复默认（测试用）。
func SetDialer(d func(ctx context.Context, addr string) (net.Conn, error)) {
	testDialer = d
}

// NewClient 创建 CD2 客户端。addr 为空时用默认地址；token 为 API Token。
func NewClient(addr, token string) *Client {
	return &Client{
		addr:    normalizeAddr(addr),
		token:   strings.TrimSpace(token),
		retries: 2,
	}
}

// normalizeAddr 归一化地址：去掉 http(s):// 前缀与末尾斜杠。
func normalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	addr = strings.TrimSuffix(addr, "/")
	if addr == "" {
		return DefaultAddr
	}
	return addr
}

// ensureConn 懒建立 gRPC 连接（幂等）。
func (c *Client) ensureConn() error {
	c.once.Do(func() {
		opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
		target := c.addr
		if testDialer != nil {
			// 测试拨号器场景需 passthrough 解析器，避免 DNS 解析产生零地址。
			target = "passthrough:///" + c.addr
			opts = append(opts, grpc.WithContextDialer(testDialer))
		}
		conn, err := grpc.NewClient(target, opts...)
		if err != nil {
			c.dialErr = err
			return
		}
		c.conn = conn
		c.stub = cd2proto.NewCloudDriveFileSrvClient(conn)
	})
	return c.dialErr
}

// retryable 是否值得重试的瞬时错误。
func retryable(err error) bool {
	return status.Code(err) == codes.Unavailable
}

// classify 将 gRPC 错误归类为业务错误。
func classify(err error) error {
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return &ErrInvalidToken{msg: "API Token 无效或已被撤销，请在 CloudDrive2 中重新创建"}
	case codes.Unavailable, codes.Aborted, codes.Internal:
		return fmt.Errorf("CloudDrive2 服务不可达: %w", err)
	case codes.FailedPrecondition:
		return &ErrNotReady{msg: "CloudDrive2 未就绪（可能未登录或未启动服务）: " + err.Error()}
	default:
		return err
	}
}

// call 执行一次 RPC：先限速，瞬时错误指数退避重试（每次重试重新限速）。
func (c *Client) call(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := c.ensureConn(); err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		if err := globalLimiter().Wait(ctx); err != nil {
			return err
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable(err) {
			break
		}
	}
	return classify(lastErr)
}

// authCtx 附加 Bearer Token 到出站 metadata（GetSubFiles 等授权方法必需）。
func (c *Client) authCtx(ctx context.Context) context.Context {
	if c.token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "Authorization", "Bearer "+c.token)
}

// Ping 探测 CD2 服务是否可达（GetSystemInfo 无需鉴权）。
func (c *Client) Ping(ctx context.Context) error {
	return c.call(ctx, func(ctx context.Context) error {
		_, err := c.stub.GetSystemInfo(ctx, &emptypb.Empty{})
		return err
	})
}

// VerifyToken 校验 API Token 有效且具备文件列表权限（GetApiTokenInfo 无需鉴权）。
func (c *Client) VerifyToken(ctx context.Context) error {
	if c.token == "" {
		return &ErrInvalidToken{msg: "未配置 CloudDrive2 API Token"}
	}
	return c.call(ctx, func(ctx context.Context) error {
		info, err := c.stub.GetApiTokenInfo(ctx, &cd2proto.StringValue{Value: c.token})
		if err != nil {
			return err
		}
		if info == nil || info.GetPermissions() == nil || !info.GetPermissions().GetAllowList() {
			return &ErrInvalidToken{msg: "API Token 无文件列表权限，请在 CloudDrive2 中授予 allow_list 权限"}
		}
		return nil
	})
}

// listPath 分页拉取指定路径的全部直属条目（服务端流式返回，逐条聚合）。
func (c *Client) listPath(ctx context.Context, path string) ([]*cd2proto.CloudDriveFile, error) {
	var files []*cd2proto.CloudDriveFile
	err := c.call(ctx, func(ctx context.Context) error {
		files = nil // 重试时清空
		// GetSubFiles 为授权方法，必须携带 Bearer Token。
		stream, err := c.stub.GetSubFiles(c.authCtx(ctx), &cd2proto.ListSubFileRequest{Path: path})
		if err != nil {
			return err
		}
		for {
			reply, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			files = append(files, reply.GetSubFiles()...)
		}
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// ListDirs 返回指定路径的直属子目录（目录树懒加载用）。
// 不预探测子目录是否有子项（会让请求量翻倍）；前端目录行统一显示展开箭头，
// 点击展开时才请求，空目录显示为空即可。
func (c *Client) ListDirs(ctx context.Context, path string) ([]DirEntry, error) {
	if path == "" {
		path = "/"
	}
	all, err := c.listPath(ctx, path)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(all))
	for _, f := range all {
		if !f.GetIsDirectory() {
			continue
		}
		out = append(out, DirEntry{Path: f.GetFullPathName(), Name: f.GetName(), HasChildren: true})
	}
	return out, nil
}

// WalkDir 递归遍历 path 子树，返回 map[相对路径]FileInfo（相对所传目录）。
// 仅包含文件（SHA1 非空）；目录层级深度上限防环。
func (c *Client) WalkDir(ctx context.Context, path string) (map[string]FileInfo, error) {
	result := make(map[string]FileInfo)
	root := strings.TrimSuffix(strings.TrimSpace(path), "/")
	if root == "" {
		root = "/"
	}
	var walk func(dir, prefix string, depth int) error
	walk = func(dir, prefix string, depth int) error {
		if depth > 32 {
			return fmt.Errorf("目录层级过深（>32），疑似循环引用: %s", prefix)
		}
		all, err := c.listPath(ctx, dir)
		if err != nil {
			return err
		}
		for _, f := range all {
			name := f.GetName()
			rel := name
			if prefix != "" {
				rel = prefix + "/" + name
			}
			if f.GetIsDirectory() {
				if err := walk(f.GetFullPathName(), rel, depth+1); err != nil {
					return err
				}
				continue
			}
			if sha1 := f.GetFileHashes()[uint32(cd2proto.CloudDriveFile_Sha1)]; sha1 != "" {
				result[rel] = FileInfo{Name: name, Size: f.GetSize(), Sha1: sha1}
			}
		}
		return nil
	}
	if err := walk(root, "", 0); err != nil {
		return nil, err
	}
	return result, nil
}
