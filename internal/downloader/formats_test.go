package downloader

import (
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTikTokPortraitQualities(t *testing.T) {
	metadata := videoMetadata{Formats: []videoFormat{
		{ID: "audio", VCodec: "none"},
		{ID: "portrait", Width: 1080, Height: 1920, ACodec: "aac"},
	}}
	for _, test := range []struct {
		convert bool
		want    []QualityOption
	}{
		{false, []QualityOption{{1080, false}, {0, false}}},
		{true, []QualityOption{{1080, false}, {720, true}, {480, true}, {0, false}}},
	} {
		if got := qualityOptions(metadata, test.convert); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("convert=%v: got %v, want %v", test.convert, got, test.want)
		}
	}
	plan, err := planQuality(metadata, 1080, false)
	if err != nil || plan.selector != "portrait" || plan.convert {
		t.Fatalf("native portrait: plan=%+v err=%v", plan, err)
	}
	plan, err = planQuality(metadata, 480, true)
	if err != nil || !plan.convert || plan.selector != "portrait" {
		t.Fatalf("480p conversion: plan=%+v err=%v", plan, err)
	}
	if _, err := planQuality(metadata, 480, false); !errors.Is(err, ErrQualityUnavailable) {
		t.Fatalf("480p without ffmpeg: %v", err)
	}
	if _, err := planQuality(metadata, 2160, true); !errors.Is(err, ErrQualityUnavailable) {
		t.Fatalf("must not upscale: %v", err)
	}
}

func TestNativeQualitiesAndUnknownDimensions(t *testing.T) {
	metadata := videoMetadata{Formats: []videoFormat{
		{ID: "360", Width: 640, Height: 360, ACodec: "aac"},
		{ID: "audio", VCodec: "none", ACodec: "aac"},
		{ID: "720", Width: 1280, Height: 720, ACodec: "none"},
		{ID: "drm", Width: 1920, Height: 1080, HasDRM: true},
		{ID: "unknown", ACodec: "aac"},
	}}
	want := []QualityOption{{720, false}, {480, true}, {360, false}, {0, false}}
	if got := qualityOptions(metadata, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	plan, err := planQuality(metadata, 720, true)
	if err != nil || plan.selector != "720+bestaudio" || plan.convert {
		t.Fatalf("separate audio: plan=%+v err=%v", plan, err)
	}
	want = []QualityOption{{360, false}, {0, false}}
	if got := qualityOptions(metadata, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("without ffmpeg: got %v, want %v", got, want)
	}
	unknown := videoMetadata{Formats: []videoFormat{{ID: "unknown"}}}
	if got := qualityOptions(unknown, true); !reflect.DeepEqual(got, []QualityOption{{0, false}}) {
		t.Fatalf("unknown dimensions must offer only original: %v", got)
	}
}

func TestConversionDimensions(t *testing.T) {
	if !ffmpegAvailable() {
		t.Skip("ffmpeg is not installed")
	}
	for _, test := range []struct{ size, want string }{
		{"1080x1920", "480x854"}, {"1920x1080", "854x480"},
	} {
		t.Run(test.size, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input.mp4")
			data, err := exec.Command("ffmpeg", "-nostdin", "-y", "-f", "lavfi", "-i", "color=size="+test.size+":rate=1", "-t", "1", "-c:v", "libx264", input).CombinedOutput()
			if err != nil {
				t.Fatalf("fixture: %v: %s", err, data)
			}
			output, err := convertVideo(input, 480)
			if err != nil {
				t.Fatal(err)
			}
			data, err = exec.Command("ffmpeg", "-nostdin", "-i", output, "-frames:v", "1", "-vf", "showinfo", "-f", "null", "-").CombinedOutput()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "s:"+test.want) {
				t.Fatalf("dimensions: %q, want %s", data, test.want)
			}
		})
	}
}
