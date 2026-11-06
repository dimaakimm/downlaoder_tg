package bot

import "testing"

func TestVideoQualityPayloadAndCache(t *testing.T) {
	url := "https://example.com/watch?v=abc"
	for _, quality := range []int{0, 2160, 1080, 720, 480, 360} {
		gotQuality, gotURL := parseVideoPayload(videoPayload(url, quality)[len("video|"):])
		if gotQuality != quality || gotURL != url {
			t.Fatalf("round trip: got (%d, %q), want (%d, %q)", gotQuality, gotURL, quality, url)
		}
		if !validQuality(quality) {
			t.Fatalf("quality %d should be valid", quality)
		}
	}
	if videoCacheKey(url, 480) == videoCacheKey(url, 1080) {
		t.Fatal("different resolutions must not share a cached Telegram file")
	}
	if quality, legacyURL := parseVideoPayload(url); quality != 1080 || legacyURL != url {
		t.Fatalf("legacy payload: got (%d, %q)", quality, legacyURL)
	}
}
