package downloader

import (
	"reflect"
	"testing"
)

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
