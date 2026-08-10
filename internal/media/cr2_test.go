// cr2_test.go：CR2 内嵌 JPEG 预览提取/解码测试（合成 TIFF/CR2 文件）。
// 代码注释使用中文。
package media

import (
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// makeTestJPEG 生成指定尺寸的 JPEG 字节。
func makeTestJPEG(t *testing.T, w, h int, quality int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytesBuffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	return buf.b
}

// putTIFFEntry 写入一条 12 字节 TIFF IFD 条目。
func putTIFFEntry(buf []byte, at int, order binary.ByteOrder, tag, typ uint16, n, val uint32) {
	o := at
	order.PutUint16(buf[o:o+2], tag)
	order.PutUint16(buf[o+2:o+4], typ)
	order.PutUint32(buf[o+4:o+8], n)
	order.PutUint32(buf[o+8:o+12], val)
}

// buildTestCR2 构造带内嵌缩略图（0x0201/0x0202）与全尺寸预览
// （SubIFD + Compression=6 Strip）的合成 CR2 文件，返回路径。
func buildTestCR2(t *testing.T, thumb, preview []byte) string {
	t.Helper()
	return buildTestCR2WithRawBlock(t, thumb, preview, nil)
}

// buildTestCR2WithRawBlock 同 buildTestCR2，并在第二个 SubIFD 追加一个
// Compression=6、覆盖到文件尾的 rawBlock（模拟 Canon RAW 无损 JPEG 流）。
func buildTestCR2WithRawBlock(t *testing.T, thumb, preview, rawBlock []byte) string {
	t.Helper()
	order := binary.LittleEndian

	rawBlockSize := len(rawBlock)
	if rawBlockSize == 0 {
		rawBlockSize = 0 // 无 raw 块时不追加该 IFD
	}
	hasRaw := len(rawBlock) > 0

	ifd0Off := 8
	subIFDOff := ifd0Off + 2 + 3*12 + 4 // IFD0：3 条目 + 下一 IFD 指针
	thumbOff := subIFDOff + 2 + 3*12 + 4
	previewOff := thumbOff + len(thumb)
	// 有 raw 块时再加一个 SubIFD 目录（2 条目 + 下一 IFD 指针）
	rawIFDOff := previewOff + len(preview)
	rawOff := rawIFDOff
	if hasRaw {
		rawIFDOff += 2 + 2*12 + 4
		rawOff = rawIFDOff
	}

	end := previewOff + len(preview)
	if hasRaw {
		end = rawOff + rawBlockSize
	}
	buf := make([]byte, end)
	copy(buf[0:2], "II")
	order.PutUint16(buf[2:4], 42)
	order.PutUint32(buf[4:8], uint32(ifd0Off))

	// IFD0：JPEGInterchange(0x0201/0x0202) → 缩略图；SubIFDs(0x014A) → 预览
	o := ifd0Off
	order.PutUint16(buf[o:o+2], 3)
	putTIFFEntry(buf, o+2, order, 0x0201, 4, 1, uint32(thumbOff))
	putTIFFEntry(buf, o+14, order, 0x0202, 4, 1, uint32(len(thumb)))
	putTIFFEntry(buf, o+26, order, 0x014A, 4, 1, uint32(subIFDOff))
	order.PutUint32(buf[o+38:o+42], uint32(rawIFDOff)) // 下一 IFD → raw 块 IFD

	// SubIFD：Compression=6 + StripOffsets/StripByteCounts → 预览 JPEG
	o = subIFDOff
	order.PutUint16(buf[o:o+2], 3)
	putTIFFEntry(buf, o+2, order, 0x0103, 3, 1, 6)
	putTIFFEntry(buf, o+14, order, 0x0111, 4, 1, uint32(previewOff))
	putTIFFEntry(buf, o+26, order, 0x0117, 4, 1, uint32(len(preview)))
	order.PutUint32(buf[o+38:o+42], 0)

	// raw 块 IFD：Compression=6 + Strip 覆盖到文件尾（Canon 无损 JPEG）
	if hasRaw {
		o = rawIFDOff
		order.PutUint16(buf[o:o+2], 2)
		putTIFFEntry(buf, o+2, order, 0x0103, 3, 1, 6)
		putTIFFEntry(buf, o+14, order, 0x0111, 4, 1, uint32(rawOff))
		putTIFFEntry(buf, o+26, order, 0x0117, 4, 1, uint32(rawBlockSize))
		order.PutUint32(buf[o+38:o+42], 0)
	}

	copy(buf[thumbOff:], thumb)
	copy(buf[previewOff:], preview)
	if hasRaw {
		copy(buf[rawOff:], rawBlock)
	}

	path := filepath.Join(t.TempDir(), "test.cr2")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// bytesBuffer 简易 io.Writer 缓冲。
type bytesBuffer struct{ b []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.b = append(b.b, p...)
	return len(p), nil
}

func TestExtractCR2PreviewJpeg(t *testing.T) {
	thumb := makeTestJPEG(t, 16, 16, 40, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	preview := makeTestJPEG(t, 64, 48, 92, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	if len(preview) <= len(thumb) {
		t.Fatalf("预览 JPEG 应大于缩略图: preview=%d thumb=%d", len(preview), len(thumb))
	}
	path := buildTestCR2(t, thumb, preview)

	cands, err := extractCR2PreviewJpeg(path)
	if err != nil {
		t.Fatalf("extractCR2PreviewJpeg: %v", err)
	}
	if len(cands) < 2 {
		t.Fatalf("候选数 %d，期望至少 2（缩略图+预览）", len(cands))
	}
	// 降序：第一个应为全尺寸预览
	if len(cands[0]) != len(preview) {
		t.Fatalf("最大候选字节数 %d，期望预览 %d", len(cands[0]), len(preview))
	}
	for i := range cands[0] {
		if cands[0][i] != preview[i] {
			t.Fatalf("预览字节不匹配，index=%d", i)
		}
	}
}

func TestDecodeCR2EmbeddedPreview(t *testing.T) {
	thumb := makeTestJPEG(t, 16, 16, 40, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	preview := makeTestJPEG(t, 64, 48, 92, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	path := buildTestCR2(t, thumb, preview)

	img, format, err := decodeCR2EmbeddedPreview(context.Background(), path)
	if err != nil {
		t.Fatalf("decodeCR2EmbeddedPreview: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("格式 %q，期望 jpeg", format)
	}
	b := img.Bounds()
	if b.Dx() != 64 || b.Dy() != 48 {
		t.Fatalf("尺寸 %dx%d，期望 64x48", b.Dx(), b.Dy())
	}
}

func TestDecodeCR2EmbeddedPreviewSkipsRawStream(t *testing.T) {
	thumb := makeTestJPEG(t, 16, 16, 40, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	preview := makeTestJPEG(t, 64, 48, 92, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	// 模拟 Canon 无损 JPEG RAW 流：FFD8 开头 + SOF3（FFC3，Go jpeg 不支持），
	// 字节数大于真预览，必须被 DecodeConfig 甄别跳过（真实 CR2 的踩坑场景）
	rawStream := append([]byte{0xFF, 0xD8, 0xFF, 0xC3}, make([]byte, 6000)...)
	path := buildTestCR2WithRawBlock(t, thumb, preview, rawStream)

	img, format, err := decodeCR2EmbeddedPreview(context.Background(), path)
	if err != nil {
		t.Fatalf("decodeCR2EmbeddedPreview: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("格式 %q，期望 jpeg", format)
	}
	b := img.Bounds()
	if b.Dx() != 64 || b.Dy() != 48 {
		t.Fatalf("尺寸 %dx%d，期望 64x48", b.Dx(), b.Dy())
	}
}

func TestExtractCR2PreviewJpegInvalid(t *testing.T) {
	// 非 TIFF 文件
	path := filepath.Join(t.TempDir(), "bad.cr2")
	if err := os.WriteFile(path, []byte("not a tiff"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := extractCR2PreviewJpeg(path); err == nil {
		t.Fatal("期望非 TIFF 文件报错")
	}
}
