// media_file.go：按 id 提供媒体源文件字节（应用内查看器用），复用路径安全校验。
// 代码注释使用中文。
package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"memable/internal/repo"
)

// httpErr 携带 HTTP 状态码的处理器错误。
type httpErr struct {
	code int
	msg  string
}

func (e *httpErr) Error() string { return e.msg }

// resolveMediaAbsPath 查询媒体与所属收藏库，校验拼接后的绝对路径位于库根目录内
// 且文件真实存在，返回媒体记录与绝对路径。
// 供打开（POST /open）与查看（GET /file）等按 id 访问源文件的操作共用。
func (s *Server) resolveMediaAbsPath(id int64) (*repo.Media, string, error) {
	m, err := s.media.GetByID(id)
	if err != nil {
		return nil, "", &httpErr{code: 500, msg: "查询媒体失败: " + err.Error()}
	}
	if m == nil {
		return nil, "", &httpErr{code: 404, msg: "媒体不存在"}
	}
	lib, err := s.libraries.GetByID(m.LibraryID)
	if err != nil {
		return nil, "", &httpErr{code: 500, msg: "查询收藏库失败: " + err.Error()}
	}
	if lib == nil {
		return nil, "", &httpErr{code: 404, msg: "收藏库不存在"}
	}

	fullPath := filepath.Join(lib.Path, filepath.FromSlash(m.RelativePath))
	libAbs, err := filepath.Abs(lib.Path)
	if err != nil {
		return nil, "", &httpErr{code: 500, msg: "解析库路径失败"}
	}
	fileAbs, err := filepath.Abs(fullPath)
	if err != nil {
		return nil, "", &httpErr{code: 500, msg: "解析文件路径失败"}
	}
	// 安全校验：文件必须在收藏库根目录内
	rel, err := filepath.Rel(libAbs, fileAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", &httpErr{code: 403, msg: "文件路径越界"}
	}
	if _, err := os.Stat(fileAbs); err != nil {
		return nil, "", &httpErr{code: 404, msg: "文件已不存在"}
	}
	return m, fileAbs, nil
}

// handleMediaFile 返回媒体源文件字节。http.ServeContent 自动处理
// Content-Type（按扩展名）、Range 分段请求（视频拖动进度必需）、
// Last-Modified/304 与 Content-Disposition。
func (s *Server) handleMediaFile(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的媒体 ID")
		return
	}
	m, absPath, err := s.resolveMediaAbsPath(id)
	if err != nil {
		var he *httpErr
		if errors.As(err, &he) {
			writeError(w, he.code, he.msg)
			return
		}
		writeError(w, 500, err.Error())
		return
	}
	f, err := os.Open(absPath)
	if err != nil {
		writeError(w, 404, "文件已不存在")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, m.RelativePath, m.Mtime, f)
}

// handleMediaPath 返回媒体源文件的本地绝对路径。
// 桌面端播放器直接用本地文件播放（mpv 本地寻址，moov 在文件尾等结构也能正常
// 打开），避免 HTTP 流 seek 受限；仅本机可信环境使用（服务默认绑 127.0.0.1
// 或局域网内网，路径越界校验与 /file、/open 一致）。
func (s *Server) handleMediaPath(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的媒体 ID")
		return
	}
	_, absPath, err := s.resolveMediaAbsPath(id)
	if err != nil {
		var he *httpErr
		if errors.As(err, &he) {
			writeError(w, he.code, he.msg)
			return
		}
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"path": absPath})
}

// externalURLs 探测本机非回环 IPv4 地址，生成 http://ip:port 形式的外部访问
// 地址列表（供前端生成"外部设备播放地址"，局域网内其它设备/VLC/浏览器直接打开）。
func externalURLs(port int) []string {
	urls := make([]string, 0)
	ifaces, err := net.Interfaces()
	if err != nil {
		return urls
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			urls = append(urls, fmt.Sprintf("http://%s:%d", ip.String(), port))
		}
	}
	return urls
}
