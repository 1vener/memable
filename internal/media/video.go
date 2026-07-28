// video.go：ffprobe 视频 metadata 采集。
// 代码注释使用中文。
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// VideoMeta 视频基础 metadata。
type VideoMeta struct {
	Format     string
	DurationMs int64
	VideoCodec string
	AudioCodec string
	Width      int
	Height     int
	FrameRate  float64
	BitRate    int64
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		AvgFPS     string `json:"avg_frame_rate"`
		RFrameRate string `json:"r_frame_rate"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
}

// ProbeVideo 调用 ffprobe 采集视频 metadata。
func ProbeVideo(ctx context.Context, path string) (*VideoMeta, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe %q: %w", path, err)
	}

	var p ffprobeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, err
	}

	m := &VideoMeta{Format: p.Format.FormatName}
	if seconds, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil {
		m.DurationMs = int64(seconds * 1000)
	}
	if br, err := strconv.ParseInt(p.Format.BitRate, 10, 64); err == nil {
		m.BitRate = br
	}
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if m.VideoCodec == "" {
				m.VideoCodec = s.CodecName
				m.Width = s.Width
				m.Height = s.Height
				m.FrameRate = parseRational(firstNonEmpty(s.AvgFPS, s.RFrameRate))
			}
		case "audio":
			if m.AudioCodec == "" {
				m.AudioCodec = s.CodecName
			}
		}
	}
	return m, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseRational(s string) float64 {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	n, _ := strconv.ParseFloat(parts[0], 64)
	d, _ := strconv.ParseFloat(parts[1], 64)
	if d == 0 {
		return 0
	}
	return n / d
}
