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

	entries, err := Walk(context.Background(), dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
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
