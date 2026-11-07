package downloader

import (
	"net/url"
	"os"
	"os/exec"
	"strings"
)

func isYouTubeURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "youtu.be" || host == "youtube.com" || strings.HasSuffix(host, ".youtube.com") || host == "youtube-nocookie.com" || strings.HasSuffix(host, ".youtube-nocookie.com")
}

// Use the same network, authentication and JS settings for probing and downloading.
func ytdlpArgs(videoURL string, args []string, playerClient string) []string {
	result := append([]string{}, args...)
	result = append(result, "--ignore-config", "--no-playlist", "--socket-timeout", "15", "--extractor-retries", "1")
	if isYouTubeURL(videoURL) {
		if runtime := strings.TrimSpace(os.Getenv("YTDLP_JS_RUNTIME")); runtime != "" {
			result = append(result, "--js-runtimes", runtime)
		} else if _, err := exec.LookPath("node"); err == nil {
			result = append(result, "--js-runtimes", "node")
		}
		if cookies := strings.TrimSpace(os.Getenv("YTDLP_COOKIES_FILE")); cookies != "" {
			result = append(result, "--cookies", cookies)
		}
		if playerClient != "" {
			result = append(result, "--extractor-args", "youtube:player_client="+playerClient)
		}
	}
	if proxy := strings.TrimSpace(os.Getenv("YTDLP_PROXY")); proxy != "" {
		result = append(result, "--proxy", proxy)
	}
	return append(result, "--", videoURL)
}

// ErrorMessageKey keeps yt-dlp diagnostics in logs while explaining access failures.
func ErrorMessageKey(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	for _, text := range []string{"sign in to confirm you", "not a bot", "http error 429", "too many requests", "po token", "ip address"} {
		if strings.Contains(message, text) {
			return "youtube_access_limited"
		}
	}
	for _, text := range []string{"private video", "video unavailable", "has been removed", "members-only", "confirm your age", "not available in your country"} {
		if strings.Contains(message, text) {
			return "video_unavailable"
		}
	}
	return "download_failed"
}
