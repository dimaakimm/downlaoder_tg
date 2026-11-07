package downloader

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Run this test executable as a fake yt-dlp, keeping regressions independent of YouTube.
func TestMain(m *testing.M) {
	if mode := os.Getenv("YTDLP_TEST_PROCESS"); mode != "" {
		fallback := strings.Contains(strings.Join(os.Args, " "), "player_client=android_vr")
		if mode == "private" && !fallback {
			fmt.Fprintln(os.Stderr, "Private video")
			os.Exit(1)
		}
		if mode == "partial" || fallback {
			fmt.Println(`{"formats":[{"format_id":"video","width":1920,"height":1080,"vcodec":"h264","acodec":"aac"}]}`)
			if mode == "partial" {
				os.Exit(1)
			}
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "The page needs to be reloaded")
		fmt.Println("null")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestProbeYouTubeClientFallback(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_PATH", executable)
	t.Setenv("YTDLP_TEST_PROCESS", "retry")
	metadata, err := probeVideo("https://www.youtube.com/watch?v=example")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planQuality(metadata, 1080, true)
	if err != nil || plan.playerClient != "android_vr" || plan.selector != "video" {
		t.Fatalf("download must retain the successful client: %+v %v", plan, err)
	}
}

func TestProbeAcceptsUsableMetadataWithoutDefaultSelection(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_PATH", executable)
	t.Setenv("YTDLP_TEST_PROCESS", "partial")
	metadata, err := probeVideo("https://youtu.be/example")
	if err != nil || len(metadata.Formats) != 1 || metadata.playerClient != "" {
		t.Fatalf("usable formats should survive selection failure: %+v %v", metadata, err)
	}
}

func TestProbeDoesNotRetryPrivateVideo(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_PATH", executable)
	t.Setenv("YTDLP_TEST_PROCESS", "private")
	_, err = probeVideo("https://youtu.be/example")
	if err == nil || ErrorMessageKey(err) != "video_unavailable" {
		t.Fatalf("private video: %v", err)
	}
}
