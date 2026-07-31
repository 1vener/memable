// media 包测试：遍历、图片 metadata、SHA1、ffprobe。
// 代码注释使用中文。
package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWalkProbeImageAndSHA1(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "a", "b.png")
	if err := os.MkdirAll(filepath.Dir(imgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestPNG(t, imgPath, 8, 6)
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := Walk(context.Background(), dir)
	entries := result.Entries
	if len(entries) != 1 {
		t.Fatalf("expected 1 media file, got %d", len(entries))
	}
	if entries[0].RelativePath != "a/b.png" || entries[0].Kind != KindImage {
		t.Fatalf("entry mismatch: %+v", entries[0])
	}

	meta, err := ProbeImage(imgPath)
	if err != nil {
		t.Fatalf("ProbeImage: %v", err)
	}
	if meta.Format != "png" || meta.Width != 8 || meta.Height != 6 {
		t.Fatalf("image meta mismatch: %+v", meta)
	}

	h, err := SHA1File(imgPath)
	if err != nil {
		t.Fatalf("SHA1File: %v", err)
	}
	if len(h) != 40 {
		t.Fatalf("sha1 length mismatch: %q", h)
	}
}

func TestProbeVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg 未安装，跳过视频 metadata 测试")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe 未安装，跳过视频 metadata 测试")
	}

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "sample.mp4")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "color=c=blue:s=16x12:d=1", "-pix_fmt", "yuv420p", videoPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg failed: %v\n%s", err, string(out))
	}

	meta, err := ProbeVideo(context.Background(), videoPath)
	if err != nil {
		t.Fatalf("ProbeVideo: %v", err)
	}
	if meta.Width != 16 || meta.Height != 12 || meta.DurationMs <= 0 || meta.VideoCodec == "" {
		t.Fatalf("video meta mismatch: %+v", meta)
	}
}

// TestDecodeGoFallsBackToFFmpegForMislabeledFile 验证扩展名为 .jpg 但内容为
// 其他格式（此处用 PNG 字节模拟）时，原生解码失败后回退 FFmpeg 仍能成功。
func TestDecodeGoFallsBackToFFmpegForMislabeledFile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg 未安装，跳过回退解码测试")
	}
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "mislabeled.jpg")
	writeTestPNG(t, imgPath, 8, 8) // 内容实际是 PNG

	decoded, err := DecodeImage(context.Background(), imgPath, DecoderGo)
	if err != nil {
		t.Fatalf("扩展名与内容不符的图片应回退 FFmpeg 解码成功: %v", err)
	}
	b := decoded.Image.Bounds()
	if b.Dx() != 8 || b.Dy() != 8 {
		t.Fatalf("解码尺寸异常: %+v", b)
	}
}

func TestImagePerceptualHashesStable(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "hash.png")
	writeTestPNG(t, imgPath, 16, 16)

	h1, err := ImagePerceptualHashes(imgPath)
	if err != nil {
		t.Fatalf("ImagePerceptualHashes: %v", err)
	}
	h2, err := ImagePerceptualHashes(imgPath)
	if err != nil {
		t.Fatalf("ImagePerceptualHashes second: %v", err)
	}
	for name, val := range map[string]string{"ahash": h1.AHash, "dhash": h1.DHash, "phash": h1.PHash} {
		if len(val) != 16 {
			t.Fatalf("%s length mismatch: %q", name, val)
		}
	}
	if *h1 != *h2 {
		t.Fatalf("hashes not stable: %+v %+v", h1, h2)
	}

	d, err := HammingHex64(h1.PHash, h2.PHash)
	if err != nil || d != 0 {
		t.Fatalf("HammingHex64 expected 0, got %d err=%v", d, err)
	}
}

func TestOSHashReaderCompatibleWithStash(t *testing.T) {
	makeByteArray := func(base []byte, mag int) []byte {
		ret := base
		for i := 0; i < mag; i++ {
			ret = append(ret, ret...)
		}
		return ret
	}
	makeTailArray := func(base []byte, tail []byte) []byte {
		ret := base
		t := make([]byte, osHashChunkSize)
		copy(t[len(t)-len(tail):], tail)
		return append(ret, t...)
	}

	tests := []struct {
		name    string
		data    []byte
		want    string
		wantErr bool
	}{
		{name: "empty", data: []byte{}, wantErr: true},
		{name: "regular", data: makeByteArray([]byte("this is a test"), 15), want: "6a0eba04654d0b9b"},
		{name: "< chunk size", data: []byte("hello world"), want: "d3e392dee38cd4df"},
		{name: "< 8", data: []byte("hello"), wantErr: true},
		{name: "identical #1", data: makeTailArray(make([]byte, osHashChunkSize), []byte("this is dumb")), want: "d5d6ddd820756920"},
		{name: "identical #2", data: makeTailArray(make([]byte, osHashChunkSize), []byte("dumb is this")), want: "d5d6ddd820756920"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := OSHashReader(bytes.NewReader(tt.data), int64(len(tt.data)))
			if (err != nil) != tt.wantErr {
				t.Fatalf("OSHashReader error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("OSHashReader = %q, want %q", got, tt.want)
			}
		})
	}
}

func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// TestSupportedFormat 表驱动测试：覆盖全部白名单格式的 Kind 和 Decoder。
func TestSupportedFormat(t *testing.T) {
	tests := []struct {
		path    string
		kind    Kind
		decoder DecoderKind
	}{
		// 图片 / Go 原生解码
		{".jpg", KindImage, DecoderGo},
		{".jpeg", KindImage, DecoderGo},
		{".jfif", KindImage, DecoderGo},
		{".png", KindImage, DecoderGo},
		{".bmp", KindImage, DecoderGo},

		// 图片 / 跳过
		{".gif", KindImage, DecoderSkip},

		// 图片 / FFmpeg 转码
		{".heic", KindImage, DecoderFFmpeg},
		{".cr2", KindImage, DecoderFFmpeg},

		// 视频 / FFmpeg
		{".mp4", KindVideo, DecoderFFmpeg},
		{".mov", KindVideo, DecoderFFmpeg},
		{".avi", KindVideo, DecoderFFmpeg},
		{".mpg", KindVideo, DecoderFFmpeg},
		{".mpeg", KindVideo, DecoderFFmpeg},
		{".m4v", KindVideo, DecoderFFmpeg},
		{".webm", KindVideo, DecoderFFmpeg},
		{".wmv", KindVideo, DecoderFFmpeg},
		{".flv", KindVideo, DecoderFFmpeg},
		{".3gp", KindVideo, DecoderFFmpeg},
		{".ts", KindVideo, DecoderFFmpeg},
		{".mkv", KindVideo, DecoderFFmpeg},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			// 正常扩展名
			f, ok := SupportedFormat("file" + tt.path)
			if !ok {
				t.Fatalf("expected %s to be supported", tt.path)
			}
			if f.Kind != tt.kind {
				t.Fatalf("kind: got %q want %q", f.Kind, tt.kind)
			}
			if f.Decoder != tt.decoder {
				t.Fatalf("decoder: got %q want %q", f.Decoder, tt.decoder)
			}
			// 大小写不敏感
			for _, casePath := range []string{
				"file" + tt.path,
				"FILE" + tt.path,
				"/some/PATH/file" + tt.path,
			} {
				f2, ok2 := SupportedFormat(casePath)
				if !ok2 || f2.Kind != tt.kind || f2.Decoder != tt.decoder {
					t.Fatalf("case-insensitive failed: %q", casePath)
				}
			}
		})
	}
}

// TestSupportedFormatSkip 验证 GIF 跳过策略。
func TestSupportedFormatSkip(t *testing.T) {
	f, ok := SupportedFormat("photo.gif")
	if !ok {
		t.Fatal("GIF should be known")
	}
	if f.Decoder != DecoderSkip {
		t.Fatalf("GIF decoder should be skip, got %q", f.Decoder)
	}
	if f.SkipReason == "" {
		t.Fatal("GIF skip should have a reason")
	}
}

// TestSupportedFormatUnknown 验证不支持的扩展名。
func TestSupportedFormatUnknown(t *testing.T) {
	for _, ext := range []string{".exe", ".zip", ".docx", "", ".unknown"} {
		f, ok := SupportedFormat("file" + ext)
		if ok {
			t.Fatalf("%q should not be supported, got %+v", ext, f)
		}
	}
}

// TestWalkSkipsGIF 验证 Walk 跳过 GIF 文件。
func TestWalkSkipsGIF(t *testing.T) {
	dir := t.TempDir()
	// 创建一个 PNG 和一个 GIF（扩展名即可，不必真实 GIF 内容）
	if err := os.WriteFile(filepath.Join(dir, "a.png"), []byte("png-fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.gif"), []byte("gif-fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Walk(context.Background(), dir)
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry (PNG), got %d", len(result.Entries))
	}
	if result.Entries[0].Kind != KindImage || result.Entries[0].Decoder != DecoderGo {
		t.Fatalf("expected image/go, got %+v", result.Entries[0])
	}
	if result.SkippedGIF != 1 {
		t.Fatalf("expected 1 skipped GIF, got %d", result.SkippedGIF)
	}
	if result.Unsupported > 0 {
		t.Fatalf("expected 0 unsupported, got %d", result.Unsupported)
	}
}

// TestFileEntryDecoder 验证 FileEntry 的 Decoder 字段正确。
func TestFileEntryDecoder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("jpg-fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "movie.mp4"), []byte("mp4-fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Walk(context.Background(), dir)
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}
	for _, e := range result.Entries {
		switch e.Kind {
		case KindImage:
			if e.Decoder != DecoderGo {
				t.Fatalf("JPG decoder should be go, got %q", e.Decoder)
			}
		case KindVideo:
			if e.Decoder != DecoderFFmpeg {
				t.Fatalf("MP4 decoder should be ffmpeg, got %q", e.Decoder)
			}
		}
	}
}
