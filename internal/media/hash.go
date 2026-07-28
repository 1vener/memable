// hash.go：流式文件哈希。
// 代码注释使用中文。
package media

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"os"
)

// SHA1File 流式计算文件 SHA1，避免大文件一次性读入内存。
func SHA1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
