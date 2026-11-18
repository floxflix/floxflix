package queue

import (
	"encoding/json"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// mediaInfo is the subset of ffprobe's output the conversion pipeline needs.
// Anything ffprobe cannot answer comes back zero-valued; callers must cope.
type mediaInfo struct {
	VideoCodec   string
	AudioCodec   string
	PixFmt       string
	Duration     time.Duration
	VideoProfile string
	AudioProfile string
	Level        int
	FieldOrder   string
	FrameRate    float64
}

// ffprobeOutput mirrors the shape of `ffprobe -of json`. Only the fields we
// consume are declared — the rest of the document is ignored.
type ffprobeOutput struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		PixFmt     string `json:"pix_fmt"`
		Duration   string `json:"duration"`
		Profile    string `json:"profile"`
		Level      int    `json:"level"`
		FieldOrder string `json:"field_order"`
		FrameRate  string `json:"r_frame_rate"`
	} `json:"streams"`
}

// probeMedia runs a single ffprobe pass for codecs, pixel format and duration.
// JSON rather than CSV: the fields are named instead of positional, and one
// call replaces the two the pipeline used to make.
func probeMedia(inputPath string) mediaInfo {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_format", "-show_streams",
		"-of", "json", inputPath).Output()
	if err != nil {
		return mediaInfo{}
	}
	return parseProbeOutput(out)
}

func parseProbeOutput(b []byte) mediaInfo {
	var raw ffprobeOutput
	if err := json.Unmarshal(b, &raw); err != nil {
		return mediaInfo{}
	}

	var info mediaInfo
	// ffmpeg maps 0:v:0 and 0:a:0, so the first stream of each kind is the
	// one whose codec decides copy-vs-re-encode.
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			if info.VideoCodec == "" {
				info.VideoCodec, info.PixFmt, info.VideoProfile, info.Level, info.FieldOrder, info.FrameRate = s.CodecName, s.PixFmt, s.Profile, s.Level, s.FieldOrder, ParseFramerate(s.FrameRate)
			}
		case "audio":
			if info.AudioCodec == "" {
				info.AudioCodec, info.AudioProfile = s.CodecName, s.Profile
			}
		}
	}

	info.Duration = parseProbeSeconds(raw.Format.Duration)
	if info.Duration <= 0 {
		// Some containers (Matroska especially) omit format.duration; the
		// longest stream is then the best available answer.
		for _, s := range raw.Streams {
			if d := parseProbeSeconds(s.Duration); d > info.Duration {
				info.Duration = d
			}
		}
	}

	return info
}

// parseProbeSeconds converts an ffprobe seconds field to a Duration, returning
// 0 for anything unusable ("N/A", empty, negative, NaN, ±Inf).
func parseProbeSeconds(v string) time.Duration {
	secs, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || secs <= 0 || math.IsInf(secs, 0) || math.IsNaN(secs) {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

func ParseFramerate(rateStr string) float64 {
	parts := strings.Split(rateStr, "/")
	if len(parts) != 2 {
		return 0.0
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0.0
	}
	return num / den
}
