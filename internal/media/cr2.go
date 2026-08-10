// cr2.go：CR2（Canon Raw）解码——提取内嵌的全尺寸 JPEG 预览。
// CR2 是 TIFF 变体（文件头 "II*\0"/"MM\0*"），相机拍摄时已把 JPEG 预览内嵌在
// IFD 链中：IFD0 的 0x0201/0x0202 标签（PreviewImageStart/Length），或
// Compression(0x0103)==6 的 Strip 数据（旧式 JPEG）。全尺寸预览字节数最大，
// 校验 SOI 魔数后取最大候选即可。提取后用 Go 标准库解码，无需 FFmpeg——
// FFmpeg 没有 CR2 demuxer，直接转码必然失败。
// 代码注释使用中文。
package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"sort"
)

// cr2MaxPreviewSize 防御性上限：CR2 内嵌预览一般 2~20MB，异常文件防超长分配。
const cr2MaxPreviewSize = 128 << 20

// cr2MaxCandidates 返回的最大候选数，防异常文件产生海量候选。
const cr2MaxCandidates = 16

// cr2WalkerDepthMax 防异常文件 IFD 环的最大递归深度。
const cr2WalkerDepthMax = 4

// cr2MaxIFDEntries IFD 条目数上限，防异常文件大分配。
const cr2MaxIFDEntries = 4096

// CR2 用到的 TIFF 标签号。
const (
	tiffTagCompression      = 0x0103
	tiffTagStripOffsets     = 0x0111
	tiffTagStripByteCounts  = 0x0117
	tiffTagSubIFDs          = 0x014A
	tiffTagJPEGInterchange  = 0x0201
	tiffTagJPEGInterchangeL = 0x0202
)

// jpegCompression TIFF 压缩方式 6 = 旧式 JPEG（内嵌预览/缩略图），
// 与 RAW 数据（1/5/7 等）区分。
const jpegCompression = 6

// jpegRange 文件中的一段字节范围。
type jpegRange struct {
	offset, length int64
}

// extractCR2PreviewJpeg 读取 CR2 文件，返回内嵌 JPEG 候选字节，按字节数降序排列。
// 遍历 IFD0、SubIFD 与下一 IFD 链，收集所有 JPEG 候选（JPEGInterchange 标签
// 或 Compression==6 的 Strip），校验 SOI 魔数后按字节数降序返回。
// 注意不能只取最大者：CR2 的 RAW 数据也以 FFD8 开头（Canon 无损 JPEG 编码，
// 同样标记 Compression==6），且覆盖到文件尾、字节数大于真预览；
// 真预览交由 decodeCR2EmbeddedPreview 用 DecodeConfig 逐候选甄别。
func extractCR2PreviewJpeg(path string) ([][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 CR2 %q: %w", path, err)
	}
	defer f.Close()

	var order binary.ByteOrder
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return nil, fmt.Errorf("读取 CR2 头部 %q: %w", path, err)
	}
	switch string(hdr[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("非 TIFF 文件头 %q", path)
	}
	if order.Uint16(hdr[2:4]) != 42 {
		return nil, fmt.Errorf("非 TIFF 魔数 %q", path)
	}

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("读取 CR2 文件信息 %q: %w", path, err)
	}

	w := &cr2Walker{f: f, order: order, size: info.Size(), visited: map[int64]bool{}}
	jpegRanges, err := w.walkIFD(int64(order.Uint32(hdr[4:8])), 0)
	if err != nil {
		return nil, err
	}

	var cands [][]byte
	for _, c := range jpegRanges {
		b, err := w.readRange(c.offset, c.length)
		if err != nil {
			continue
		}
		cands = append(cands, b)
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("CR2 %q 中未找到有效的内嵌 JPEG 预览", path)
	}
	sort.Slice(cands, func(i, j int) bool { return len(cands[i]) > len(cands[j]) })
	if len(cands) > cr2MaxCandidates {
		cands = cands[:cr2MaxCandidates]
	}
	return cands, nil
}

// cr2Walker 递归遍历 IFD 链收集 JPEG 候选。
type cr2Walker struct {
	f       *os.File
	order   binary.ByteOrder
	size    int64
	visited map[int64]bool // 防异常文件 IFD 环
}

// walkIFD 解析一个 IFD 目录，返回其中发现的 JPEG 字节范围。
// 递归处理 SubIFDs（0x014A）与"下一 IFD"指针，两者都受深度限制。
func (w *cr2Walker) walkIFD(offset int64, depth int) ([]jpegRange, error) {
	if depth > cr2WalkerDepthMax || offset < 8 || w.visited[offset] {
		return nil, nil
	}
	w.visited[offset] = true

	var countBuf [2]byte
	if _, err := w.f.ReadAt(countBuf[:], offset); err != nil {
		return nil, nil
	}
	count := int(w.order.Uint16(countBuf[:]))
	if count <= 0 || count > cr2MaxIFDEntries {
		return nil, nil
	}
	// 预读整段条目避免逐条 syscall
	buf := make([]byte, 12*count)
	if _, err := w.f.ReadAt(buf, offset+2); err != nil {
		return nil, nil
	}

	var cands []jpegRange
	var jpegInterchange int64 = -1
	var jpegInterchangeLen int64
	var compression uint16
	var stripOffsets, stripCounts []int64
	var subIFDs []int64

	for i := 0; i < count; i++ {
		e := buf[i*12 : (i+1)*12]
		tag := w.order.Uint16(e[0:2])
		typ := w.order.Uint16(e[2:4])
		n := w.order.Uint32(e[4:8])
		val := w.order.Uint32(e[8:12])

		switch tag {
		case tiffTagCompression:
			if isInlineValue(typ, n) && typ == 3 {
				// SHORT 内联值占字段前两个字节（大小端均如此）
				compression = w.order.Uint16(e[8:10])
			} else {
				compression = uint16(val)
			}
		case tiffTagJPEGInterchange:
			jpegInterchange = int64(val)
		case tiffTagJPEGInterchangeL:
			jpegInterchangeLen = int64(val)
		case tiffTagSubIFDs:
			if isInlineValue(typ, n) {
				subIFDs = append(subIFDs, int64(val))
			} else if vals, ok := w.readUint32Array(val, typ, n); ok {
				subIFDs = append(subIFDs, vals...)
			}
		case tiffTagStripOffsets:
			if isInlineValue(typ, n) {
				stripOffsets = append(stripOffsets, int64(val))
			} else if vals, ok := w.readUint32Array(val, typ, n); ok {
				stripOffsets = append(stripOffsets, vals...)
			}
		case tiffTagStripByteCounts:
			if isInlineValue(typ, n) {
				stripCounts = append(stripCounts, int64(val))
			} else if vals, ok := w.readUint32Array(val, typ, n); ok {
				stripCounts = append(stripCounts, vals...)
			}
		}
	}

	// 1) JPEGInterchange 标签（0x0201/0x0202）
	if jpegInterchange >= 0 && jpegInterchangeLen > 0 {
		cands = append(cands, jpegRange{offset: jpegInterchange, length: jpegInterchangeLen})
	}
	// 2) Compression==6 的 Strip：嵌入式 JPEG 可能分多条 Strip，合并为一段
	if compression == jpegCompression && len(stripOffsets) > 0 {
		var minOff, maxEnd int64 = -1, -1
		for i, off := range stripOffsets {
			var cnt int64
			if i < len(stripCounts) {
				cnt = stripCounts[i]
			} else {
				cnt = w.size - off // 异常文件缺 counts 时回退到文件尾，由 readRange 兜底
			}
			if cnt <= 0 {
				continue
			}
			if minOff < 0 || off < minOff {
				minOff = off
			}
			if end := off + cnt; end > maxEnd {
				maxEnd = end
			}
		}
		if minOff >= 0 && maxEnd > minOff {
			cands = append(cands, jpegRange{offset: minOff, length: maxEnd - minOff})
		}
	}
	// 3) 递归 SubIFD
	for _, sub := range subIFDs {
		subCands, err := w.walkIFD(sub, depth+1)
		if err != nil {
			return nil, err
		}
		cands = append(cands, subCands...)
	}
	// 4) 下一 IFD（IFD1 缩略图等）
	if nextOff := offset + 2 + int64(12*count); nextOff+4 <= w.size {
		var next [4]byte
		if _, err := w.f.ReadAt(next[:], nextOff); err == nil {
			if nxt := int64(w.order.Uint32(next[:])); nxt > 0 {
				subCands, err := w.walkIFD(nxt, depth+1)
				if err != nil {
					return nil, err
				}
				cands = append(cands, subCands...)
			}
		}
	}
	return cands, nil
}

// readRange 读取文件 [offset, offset+length) 并校验 JPEG SOI 魔数。
func (w *cr2Walker) readRange(offset, length int64) ([]byte, error) {
	if offset < 0 || length <= 0 || offset+length > w.size {
		return nil, fmt.Errorf("越界范围 %d+%d", offset, length)
	}
	if length > cr2MaxPreviewSize {
		return nil, fmt.Errorf("预览超过上限 %d", length)
	}
	buf := make([]byte, length)
	if _, err := w.f.ReadAt(buf, offset); err != nil {
		return nil, err
	}
	if length < 2 || buf[0] != 0xFF || buf[1] != 0xD8 {
		return nil, fmt.Errorf("非 JPEG SOI 魔数")
	}
	return buf, nil
}

// readUint32Array 从文件 offset 处读取 n 个元素（按 typ 的元素宽度）为 int64 数组。
func (w *cr2Walker) readUint32Array(off uint32, typ uint16, n uint32) ([]int64, bool) {
	if n == 0 || n > cr2MaxIFDEntries {
		return nil, false
	}
	es := tiffTypeSize(typ)
	need := int64(n) * int64(es)
	if int64(off)+need > w.size {
		return nil, false
	}
	buf := make([]byte, need)
	if _, err := w.f.ReadAt(buf, int64(off)); err != nil {
		return nil, false
	}
	out := make([]int64, n)
	for i := uint32(0); i < n; i++ {
		switch es {
		case 2:
			out[i] = int64(w.order.Uint16(buf[i*2 : (i+1)*2]))
		default:
			out[i] = int64(w.order.Uint32(buf[i*4 : (i+1)*4]))
		}
	}
	return out, true
}

// tiffTypeSize TIFF 字段类型的字节宽度。
func tiffTypeSize(typ uint16) int {
	switch typ {
	case 1, 2, 6, 7: // BYTE/ASCII/UNDEFINED/SBYTE
		return 1
	case 3, 8: // SHORT/SSHORT
		return 2
	case 4, 9, 11: // LONG/SLONG/FLOAT
		return 4
	default: // RATIONAL/SRATIONAL/DOUBLE 等
		return 8
	}
}

// isInlineValue 判断 count 个 typ 类型元素能否塞进 4 字节值字段。
func isInlineValue(typ uint16, n uint32) bool {
	return int64(n)*int64(tiffTypeSize(typ)) <= 4
}

// decodeCR2EmbeddedPreview 提取 CR2 内嵌预览 JPEG 并解码。
// 候选按字节数降序逐个尝试：RAW 数据流（Canon 无损 JPEG，FFC3 等标记）
// DecodeConfig 会失败被跳过，命中真预览为止。预览通常为全尺寸：像素超限
// （超过 maxDirectDecodePixels/边限）时写临时文件走 FFmpeg scale 限分辨率
// 转码（普通 JPEG FFmpeg 可解）；否则直接 Go jpeg 解码。
func decodeCR2EmbeddedPreview(ctx context.Context, srcPath string) (image.Image, string, error) {
	cands, err := extractCR2PreviewJpeg(srcPath)
	if err != nil {
		return nil, "", err
	}
	var lastErr error
	for _, data := range cands {
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			// 非 Go 可解码的 JPEG 流（如 RAW 无损 JPEG），跳过试下一个
			lastErr = fmt.Errorf("解析 CR2 预览 %q: %w", srcPath, err)
			continue
		}
		if needsScaledDecode(cfg.Width, cfg.Height) {
			img, format, err := decodeScaledJPEG(ctx, data, cfg.Width, cfg.Height)
			if err != nil {
				lastErr = fmt.Errorf("缩放解码 CR2 预览 %q (%dx%d): %w", srcPath, cfg.Width, cfg.Height, err)
				continue
			}
			return img, format, nil
		}
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			lastErr = fmt.Errorf("解码 CR2 预览 %q: %w", srcPath, err)
			continue
		}
		return img, "jpeg", nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("CR2 %q 无可解码的预览候选", srcPath)
	}
	return nil, "", lastErr
}

// decodeScaledJPEG 把内存中的 JPEG 写临时文件后经 FFmpeg 限分辨率解码。
func decodeScaledJPEG(ctx context.Context, data []byte, srcW, srcH int) (image.Image, string, error) {
	tmp, err := os.CreateTemp("", "memable-cr2-*.jpg")
	if err != nil {
		return nil, "", fmt.Errorf("创建临时预览文件: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return nil, "", fmt.Errorf("写入临时预览文件: %w", err)
	}
	tmp.Close()
	defer os.Remove(tmpPath)

	return decodeImageWithFFmpegScaled(ctx, tmpPath, srcW, srcH)
}
