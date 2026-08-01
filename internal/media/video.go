// video.go：ffprobe 视频 metadata 采集。
// 代码注释使用中文。
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
		Duration   string `json:"duration"`
		DurationTS int64  `json:"duration_ts"`
		TimeBase   string `json:"time_base"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
}

// ProbeVideo 调用 ffprobe 采集视频 metadata。
func ProbeVideo(ctx context.Context, path string) (*VideoMeta, error) {
	// 正常视频 probe <1s，10s 超时已足够；过长超时会让损坏/超大文件拖慢整批扫描。
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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

	// 时长获取：format → 视频流 duration → duration_ts × time_base
	m.DurationMs = parseDurationMs(p.Format.Duration)
	if m.DurationMs <= 0 {
		for _, s := range p.Streams {
			if s.CodecType != "video" {
				continue
			}
			if ms := parseDurationMs(s.Duration); ms > 0 {
				m.DurationMs = ms
				break
			}
			if s.DurationTS > 0 {
				tb := parseRational(s.TimeBase)
				if tb > 0 {
					m.DurationMs = int64(float64(s.DurationTS) * tb * 1000)
					break
				}
			}
		}
	}
	// 未知时长不报错，后续封面回退到 0s
	if m.DurationMs <= 0 {
		slog.Warn("未知视频时长", "path", path)
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

// parseDurationMs 解析 ffprobe 的 duration 字符串为毫秒。
func parseDurationMs(s string) int64 {
	if s == "" || s == "N/A" {
		return 0
	}
	seconds, err := strconv.ParseFloat(s, 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return int64(seconds * 1000)
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
