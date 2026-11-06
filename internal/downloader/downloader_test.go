package downloader

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestQualityFormatCapsEveryFallback(t *testing.T) {
	for _, quality := range []int{1080, 720, 480} {
		format, err := qualityFormat(quality)
		if err != nil {
			t.Fatal(err)
		}
		for _, fallback := range strings.Split(format, "/") {
			if !strings.Contains(fallback, "[height<="+strconv.Itoa(quality)+"]") {
				t.Fatalf("uncapped fallback for %dp: %q", quality, fallback)
			}
		}
	}
	if _, err := qualityFormat(360); err == nil {
		t.Fatal("unexpectedly accepted unsupported resolution")
	}
}

func TestProgressOutputHandlesChunkedYTDLPOutput(t *testing.T) {
	var got []int
	output := &progressOutput{onUpdate: func(percent int) { got = append(got, percent) }}
	chunks := []string{
		"[download]   4.9% of 10MiB\n[down",
		"load]  15.2% of 10MiB\n[download]  15.8% of 10MiB\r",
		"[download]  100.0% of 10MiB\n",
	}
	for _, chunk := range chunks {
		if _, err := output.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if want := []int{15, 99}; !reflect.DeepEqual(got, want) {
		t.Fatalf("progress = %v, want %v", got, want)
	}
}
