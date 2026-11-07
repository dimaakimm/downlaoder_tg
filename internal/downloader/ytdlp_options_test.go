package downloader

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestYouTubeSettingsSharedByProbeAndDownload(t *testing.T) {
	t.Setenv("YTDLP_JS_RUNTIME", "node:/runtime/node")
	t.Setenv("YTDLP_COOKIES_FILE", "/config/youtube cookies.txt")
	t.Setenv("YTDLP_PROXY", "http://proxy:8080")
	for _, args := range [][]string{{"--dump-single-json"}, {"-f", "137+bestaudio"}} {
		got := ytdlpArgs("https://youtu.be/example", args, "android_vr")
		joined := strings.Join(got, "|")
		for _, want := range []string{"--js-runtimes|node:/runtime/node", "--cookies|/config/youtube cookies.txt", "--proxy|http://proxy:8080", "--extractor-args|youtube:player_client=android_vr"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("missing %s in %v", want, got)
			}
		}
		if !reflect.DeepEqual(got[len(got)-2:], []string{"--", "https://youtu.be/example"}) {
			t.Fatal(got)
		}
	}
	got := strings.Join(ytdlpArgs("https://vt.tiktok.com/example", nil, "android_vr"), "|")
	if strings.Contains(got, "--cookies") || strings.Contains(got, "--extractor-args") || strings.Contains(got, "--js-runtimes") {
		t.Fatalf("YouTube settings leaked to another provider: %s", got)
	}
}

func TestYouTubeURLDetection(t *testing.T) {
	for _, raw := range []string{"https://youtu.be/abc", "https://www.youtube.com/watch?v=abc", "https://m.youtube.com/shorts/abc", "https://www.youtube-nocookie.com/embed/abc"} {
		if !isYouTubeURL(raw) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"https://youtube.com.example.org/", "https://example.com/?u=youtube.com", "https://vt.tiktok.com/abc"} {
		if isYouTubeURL(raw) {
			t.Fatal(raw)
		}
	}
}

func TestAccessErrorsDoNotBlamePublicVideos(t *testing.T) {
	for _, tc := range []struct{ message, key string }{
		{"Sign in to confirm you’re not a bot", "youtube_access_limited"},
		{"HTTP Error 429: Too Many Requests", "youtube_access_limited"},
		{"Private video", "video_unavailable"},
		{"The page needs to be reloaded", "download_failed"},
	} {
		if got := ErrorMessageKey(errors.New(tc.message)); got != tc.key {
			t.Fatalf("%s: %s", tc.message, got)
		}
	}
}

func TestInstalledVersionMustMatchDatabase(t *testing.T) {
	if sameYTDLPVersion("2026.10.26", "2025.10.14") {
		t.Fatal("a current DB record cannot validate an old executable")
	}
	if sameYTDLPVersion("2026.10.26", "") {
		t.Fatal("missing binary is not current")
	}
	if !sameYTDLPVersion("v2026.10.26", "2026.10.26") {
		t.Fatal("release tag prefix should be ignored")
	}
}
