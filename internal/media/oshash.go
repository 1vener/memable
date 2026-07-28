// oshash.go：兼容 Stash/OpenSubtitles 的视频 OSHash。
// 代码注释使用中文。
package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const osHashChunkSize int64 = 64 * 1024

var errOSHashLen = errors.New("buffer is not a multiple of 8")

// OSHashFile 计算兼容 Stash 的 OpenSubtitles OSHash。
func OSHashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	return OSHashReader(f, info.Size())
}

// OSHashReader 从 io.ReadSeeker 计算 OSHash。
func OSHashReader(src io.ReadSeeker, fileSize int64) (string, error) {
	if fileSize <= 8 {
		return "", fmt.Errorf("cannot calculate oshash where size < 8 (%d)", fileSize)
	}

	chunkSize := osHashChunkSize
	if fileSize < chunkSize {
		chunkSize = (fileSize / 8) * 8 // 必须为 8 的倍数
	}

	head := make([]byte, chunkSize)
	tail := make([]byte, chunkSize)
	if _, err := io.ReadFull(src, head); err != nil {
		return "", err
	}
	if _, err := src.Seek(-chunkSize, io.SeekEnd); err != nil {
		return "", err
	}
	if _, err := io.ReadFull(src, tail); err != nil {
		return "", err
	}

	headSum, err := sumOSHashBytes(head)
	if err != nil {
		return "", fmt.Errorf("oshash head: %w", err)
	}
	tailSum, err := sumOSHashBytes(tail)
	if err != nil {
		return "", fmt.Errorf("oshash tail: %w", err)
	}
	return fmt.Sprintf("%016x", headSum+tailSum+uint64(fileSize)), nil
}

func sumOSHashBytes(buf []byte) (uint64, error) {
	if len(buf)%8 != 0 {
		return 0, errOSHashLen
	}
	var sum uint64
	for i := 0; i < len(buf); i += 8 {
		sum += binary.LittleEndian.Uint64(buf[i : i+8])
	}
	return sum, nil
}
