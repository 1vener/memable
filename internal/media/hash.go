// hash.go：流式文件哈希。
// 代码注释使用中文。
package media

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"os"
)

// sha1ReadBuffer 文件读取缓冲大小：1MB 大块读减少系统调用次数，
// 对 SSD/NVMe 高带宽场景有明显收益（io.Copy 默认仅 32KB）。
const sha1ReadBuffer = 1 << 20

// SHA1File 流式计算文件 SHA1，避免大文件一次性读入内存。
func SHA1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha1.New()
	buf := make([]byte, sha1ReadBuffer)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
