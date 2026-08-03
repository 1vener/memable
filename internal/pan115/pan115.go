// 包 pan115：115 网盘 Web API 最小客户端（自研，参考 SheltonZhu/115driver 的
// webapi 调用方式，零第三方依赖）。
// 仅实现本应用需要的只读能力：目录列表、递归遍历（取文件 sha1/大小）、登录校验。
// 风控策略内置：串行请求 + 可配置间隔 + 随机 jitter + 失败指数退避 +
// 风控/鉴权特征检测即停（避免无谓重试加重风控）。
// 代码注释使用中文。
package pan115

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// FileInfo 网盘文件信息（遍历结果）。
type FileInfo struct {
	Name string // 文件名（不含路径）
	Size int64  // 文件大小（字节）
	Sha1 string // 文件 SHA1（内容指纹）
}

// DirEntry 目录直属项（目录树懒加载用）。
type DirEntry struct {
	CID         string `json:"cid"`
	Name        string `json:"name"`
	IsDir       bool   `json:"is_dir"`
	HasChildren bool   `json:"has_children"`
	Size        int64  `json:"size,omitempty"`
	Sha1        string `json:"sha1,omitempty"`
}

const (
	apiBase       = "https://webapi.115.com"
	statusBase    = "https://my.115.com"
	fileListLimit = 56
)

// Client 115 网盘客户端。
type Client struct {
	baseURL       string
	statusBaseURL string
	cookie        string
	http          *http.Client
	interval      time.Duration // 请求间隔（风控）
	maxRetries    int
	userAgent     string
	referer       string
	lastRequest   time.Time
	rand          *rand.Rand
}

// NewClient 创建客户端。interval 为请求间隔（ms），0 表示不等待（测试用）。
func NewClient(cookie string, intervalMs int) *Client {
	if intervalMs <= 0 {
		intervalMs = 1000
	}
	return &Client{
		baseURL:       apiBase,
		statusBaseURL: statusBase,
		cookie:        cookie,
		http:          &http.Client{Timeout: 30 * time.Second},
		interval:      time.Duration(intervalMs) * time.Millisecond,
		maxRetries:    2,
		userAgent:     "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		referer:       "https://115.com/",
		rand:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// SetBaseURL 覆盖所有 API 基地址（测试用）。
// 生产环境的文件接口与 Cookie 状态接口使用不同域名。
func (c *Client) SetBaseURL(u string) {
	u = strings.TrimRight(u, "/")
	c.baseURL = u
	c.statusBaseURL = u
}

// ErrInvalidCookie Cookie 无效/过期错误。
type ErrInvalidCookie struct{ msg string }

func (e *ErrInvalidCookie) Error() string { return e.msg }

// ErrRiskControl 触发风控错误（应停止任务，等待冷却后重试）。
type ErrRiskControl struct{ msg string }

func (e *ErrRiskControl) Error() string { return e.msg }

// throttle 请求间隔控制：串行 + 固定间隔 + 随机 jitter（基准 +500ms/-200ms）。
func (c *Client) throttle() {
	wait := c.interval
	if wait > 0 {
		wait += time.Duration(c.rand.Int63n(700)) * time.Millisecond // jitter 0~699ms
		wait -= 200 * time.Millisecond
		if wait < 0 {
			wait = 0
		}
	}
	if d := time.Since(c.lastRequest); d < wait {
		time.Sleep(wait - d)
	}
	c.lastRequest = time.Now()
}

// filesResp webapi.files 接口响应。
type filesResp struct {
	State bool        `json:"state"`
	Count int         `json:"count"`
	Data  []filesItem `json:"data"`
}

// filesItem 文件/目录条目。
type filesItem struct {
	CID        jsonString `json:"cid"`
	PID        jsonString `json:"pid"`
	FileID     jsonString `json:"fid"`
	Name       string     `json:"n"`
	Size       jsonInt64  `json:"s"`
	Sha1       string     `json:"sha"`
	LegacySha1 string     `json:"sha1"`
	IsDir      *bool      `json:"is_dir"`
}

// jsonString 兼容 115 接口将 ID 返回为字符串或数字。
type jsonString string

func (v *jsonString) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*v = ""
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*v = jsonString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("解析字符串值: %w", err)
	}
	*v = jsonString(n.String())
	return nil
}

// jsonInt64 兼容 115 接口将大小返回为字符串或数字。
type jsonInt64 int64

func (v *jsonInt64) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*v = 0
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		raw = strings.TrimSpace(s)
	}
	if raw == "" {
		*v = 0
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("解析整数值 %q: %w", raw, err)
	}
	*v = jsonInt64(n)
	return nil
}

func (it filesItem) isDir() bool {
	if it.FileID != "" {
		return false
	}
	if it.IsDir != nil {
		return *it.IsDir
	}
	// 115driver 的协议以 fid 是否为空区分目录和文件。
	return true
}

func (it filesItem) sha1() string {
	if it.Sha1 != "" {
		return it.Sha1
	}
	return it.LegacySha1
}

// request 执行一次 webapi 请求（带重试与风控检测）。
func (c *Client) request(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.requestFrom(ctx, c.baseURL, path, query)
}

// requestFrom 从指定基地址执行请求；状态校验和文件列表使用不同域名。
func (c *Client) requestFrom(ctx context.Context, baseURL, path string, query url.Values) ([]byte, error) {
	c.throttle()
	u := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", c.cookie)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Referer", c.referer)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// 指数退避：1s、3s
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			slog.Warn("115 请求失败，退避重试", "path", path, "attempt", attempt, "err", lastErr, "backoff", backoff)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue // 网络错误重试
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return body, nil
		}
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		// 4xx 不重试（鉴权/风控类）
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			break
		}
	}
	return nil, lastErr
}

// parseFiles 解析 files 接口响应；state=false 时区分 Cookie 失效与风控。
func parseFiles(body []byte) (*filesResp, error) {
	var r filesResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("解析 115 响应: %w", err)
	}
	if !r.State {
		return nil, stateError(body)
	}
	return &r, nil
}

func stateError(body []byte) error {
	// state=false：多为 Cookie 失效；含 errno 或验证提示时可能是风控。
	msg := string(body)
	if len(msg) > 200 {
		msg = msg[:200]
	}
	if strings.Contains(msg, `"errno"`) || strings.Contains(msg, "验证") {
		return &ErrRiskControl{msg: "触发风控，请稍后（建议 10 分钟以上）再试"}
	}
	return &ErrInvalidCookie{msg: "Cookie 已过期或无效，请重新从浏览器复制"}
}

// LoginCheck 校验 Cookie 是否有效，不会使其他设备下线。
func (c *Client) LoginCheck(ctx context.Context) error {
	q := url.Values{}
	q.Set("ct", "guide")
	q.Set("ac", "status")
	q.Set("_", strconv.FormatInt(time.Now().UnixMilli(), 10))
	body, err := c.requestFrom(ctx, c.statusBaseURL, "/", q)
	if err != nil {
		return err
	}
	return parseStatus(body)
}

// parseStatus 解析 my.115.com 的 Cookie 状态响应。
func parseStatus(body []byte) error {
	var r struct {
		State bool `json:"state"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("解析 115 Cookie 状态响应: %w", err)
	}
	if !r.State {
		return stateError(body)
	}
	return nil
}

// ListDir 返回指定 cid 的直属子目录（目录树懒加载用）。
// 不预探测子目录是否有子项（那会让请求量翻倍、增加风控风险）；
// 前端目录行统一显示展开箭头，点击展开时才请求，空目录显示为空即可。
func (c *Client) ListDir(ctx context.Context, cid string) ([]DirEntry, error) {
	all, err := c.listAll(ctx, cid)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(all))
	for _, it := range all {
		if !it.isDir() {
			continue
		}
		out = append(out, DirEntry{CID: string(it.CID), Name: it.Name, IsDir: true, HasChildren: true})
	}
	return out, nil
}

// WalkDir 递归遍历 cid 子树，返回 map[相对路径]FileInfo（相对所传目录）。
// 仅包含文件（sha1 非空）；目录层级深度上限防环。
func (c *Client) WalkDir(ctx context.Context, cid string) (map[string]FileInfo, error) {
	result := make(map[string]FileInfo)
	var walk func(cid, prefix string, depth int) error
	walk = func(cid, prefix string, depth int) error {
		if depth > 32 {
			return fmt.Errorf("目录层级过深（>32），疑似循环引用: %s", prefix)
		}
		all, err := c.listAll(ctx, cid)
		if err != nil {
			return err
		}
		for _, it := range all {
			rel := it.Name
			if prefix != "" {
				rel = prefix + "/" + it.Name
			}
			if it.isDir() {
				if err := walk(string(it.CID), rel, depth+1); err != nil {
					return err
				}
				continue
			}
			if sha1 := it.sha1(); sha1 != "" {
				result[rel] = FileInfo{Name: it.Name, Size: int64(it.Size), Sha1: sha1}
			}
		}
		return nil
	}
	if err := walk(cid, "", 0); err != nil {
		return nil, err
	}
	return result, nil
}

// listAll 分页拉取 cid 下全部条目（每页 56 条）。
func (c *Client) listAll(ctx context.Context, cid string) ([]filesItem, error) {
	out := make([]filesItem, 0)
	if cid == "" {
		cid = "0"
	}
	for offset := 0; ; {
		q := fileListQuery(cid, offset)
		body, err := c.request(ctx, "/files", q)
		if err != nil {
			return nil, err
		}
		r, err := parseFiles(body)
		if err != nil {
			return nil, err
		}
		out = append(out, r.Data...)
		if offset+len(r.Data) >= r.Count || len(r.Data) == 0 {
			break
		}
		offset += len(r.Data)
	}
	return out, nil
}

func fileListQuery(cid string, offset int) url.Values {
	q := url.Values{}
	q.Set("aid", "1")
	q.Set("cid", cid)
	q.Set("o", "user_ptime")
	q.Set("asc", "1")
	q.Set("offset", strconv.Itoa(offset))
	q.Set("show_dir", "1")
	q.Set("limit", strconv.Itoa(fileListLimit))
	q.Set("snap", "0")
	q.Set("natsort", "0")
	q.Set("record_open_time", "1")
	q.Set("format", "json")
	q.Set("fc_mix", "0")
	return q
}
