package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"time"
)

var ErrQualityUnavailable = errors.New("requested video quality is unavailable")

type videoFormat struct {
	ID     string `json:"format_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	VCodec string `json:"vcodec"`
	ACodec string `json:"acodec"`
	Ext    string `json:"ext"`
	HasDRM bool   `json:"has_drm"`
}

type videoMetadata struct {
	Formats []videoFormat `json:"formats"`
}

type QualityOption struct {
	Resolution int
	Convert    bool
}

func ytdlpPath() string {
	name := "./yt-dlp_linux"
	if runtime.GOOS == "windows" {
		name = "./yt-dlp.exe"
	}
	path, _ := filepath.Abs(name)
	return path
}

func ffmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func probeVideo(url string) (videoMetadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ytdlpPath(), "--ignore-config", "--no-playlist", "--skip-download", "--dump-single-json", "--no-warnings", "--socket-timeout", "15", "--", url)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return videoMetadata{}, fmt.Errorf("probe video: %w: %s", err, stderr.String())
	}
	var metadata videoMetadata
	if err := json.Unmarshal(stdout.Bytes(), &metadata); err != nil {
		return metadata, fmt.Errorf("decode video formats: %w", err)
	}
	return metadata, nil
}

func (f videoFormat) resolution() int {
	if f.Width <= 0 || f.Height <= 0 {
		return 0 // Do not advertise an unverified resolution.
	}
	return min(f.Width, f.Height)
}

func (f videoFormat) downloadable() bool {
	return f.ID != "" && !f.HasDRM && f.VCodec != "none" &&
		f.Ext != "mhtml" && f.Ext != "jpg" && f.Ext != "png"
}

func (m videoMetadata) hasSeparateAudio() bool {
	for _, f := range m.Formats {
		if f.VCodec == "none" && f.ACodec != "none" && !f.HasDRM {
			return true
		}
	}
	return false
}

func (m videoMetadata) videoFormats(canMerge bool) []videoFormat {
	formats := make([]videoFormat, 0, len(m.Formats))
	hasAudio := m.hasSeparateAudio()
	for _, f := range m.Formats {
		if f.downloadable() && (canMerge || f.ACodec != "none" || !hasAudio) {
			formats = append(formats, f)
		}
	}
	return formats
}

func qualityOptions(metadata videoMetadata, canConvert bool) []QualityOption {
	native := make(map[int]bool)
	maximum := 0
	for _, f := range metadata.videoFormats(canConvert) {
		resolution := f.resolution()
		if resolution > 0 {
			native[resolution] = true
			maximum = max(maximum, resolution)
		}
	}
	options := make([]QualityOption, 0, len(native)+3)
	for resolution := range native {
		options = append(options, QualityOption{Resolution: resolution})
	}
	if canConvert {
		for _, resolution := range []int{1080, 720, 480} {
			if resolution < maximum && !native[resolution] {
				options = append(options, QualityOption{Resolution: resolution, Convert: true})
			}
		}
	}
	sort.Slice(options, func(i, j int) bool { return options[i].Resolution > options[j].Resolution })
	// Original is always explicit, including when the source omits dimensions.
	return append(options, QualityOption{Resolution: 0})
}

func AvailableVideoQualities(url string) ([]QualityOption, error) {
	metadata, err := probeVideo(url)
	if err != nil {
		return nil, err
	}
	canConvert := ffmpegAvailable()
	if len(metadata.videoFormats(canConvert)) > 0 {
		return qualityOptions(metadata, canConvert), nil
	}
	return nil, ErrQualityUnavailable
}

type qualityPlan struct {
	selector string
	convert  bool
}

func planQuality(metadata videoMetadata, quality int, canConvert bool) (qualityPlan, error) {
	var native, source *videoFormat
	// yt-dlp supplies formats from worst to best; keep the best matching stream.
	formats := metadata.videoFormats(canConvert)
	for i := range formats {
		f := &formats[i]
		if f.resolution() == quality {
			native = f
		}
		if f.resolution() > quality {
			source = f
		}
	}
	selected := native
	convert := false
	if selected == nil && canConvert {
		selected, convert = source, true
	}
	if selected == nil {
		return qualityPlan{}, ErrQualityUnavailable
	}
	selector := selected.ID
	if selected.ACodec == "none" && metadata.hasSeparateAudio() {
		selector += "+bestaudio"
	}
	return qualityPlan{selector: selector, convert: convert}, nil
}

// Scale the short side, preserving orientation and aspect ratio. Never upscale.
func conversionArgs(input, output string, quality int) []string {
	q := strconv.Itoa(quality)
	scale := "scale=w='if(lte(iw,ih),min(iw," + q + "),-2)':h='if(lte(iw,ih),-2,min(ih," + q + "))'"
	return []string{"-nostdin", "-y", "-i", input, "-map", "0:v:0", "-map", "0:a:0?", "-vf", scale,
		"-c:v", "libx264", "-preset", "fast", "-crf", "23", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart", output}
}

func convertVideo(input string, quality int) (string, error) {
	output := input + ".converted.mp4"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	data, err := exec.CommandContext(ctx, "ffmpeg", conversionArgs(input, output, quality)...).CombinedOutput()
	if err != nil {
		_ = os.Remove(output)
		return "", fmt.Errorf("convert video to %dp: %w: %s", quality, err, data)
	}
	_ = os.Remove(input)
	return output, nil
}
