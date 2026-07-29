package events

import (
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	got := slugify("實驗日落藝術節：色彩", "event-")
	want := "event-實驗日落藝術節色彩"
	if got != want {
		t.Fatalf("slugify() = %q, want %q", got, want)
	}

	got = slugify("Hello, World! 2026", "umayevent-")
	want = "umayevent-HelloWorld2026"
	if got != want {
		t.Fatalf("slugify() = %q, want %q", got, want)
	}
}

func TestJoinNames(t *testing.T) {
	got := joinNames([]string{"一般民眾", "親子"})
	want := "一般民眾、親子"
	if got != want {
		t.Fatalf("joinNames() = %q, want %q", got, want)
	}
}

func TestResolveEndTime(t *testing.T) {
	start := time.Date(2026, 7, 14, 19, 30, 0, 0, time.UTC)

	tests := []struct {
		name  string
		end   any
		start string
		want  string
	}{
		{name: "nil end uses start", end: nil, start: "19:30", want: "19:30"},
		{name: "zero time uses start", end: time.Time{}, start: "19:30", want: "19:30"},
		{name: "empty string uses start", end: "", start: "19:30", want: "19:30"},
		{name: "whitespace uses start", end: "   ", start: "19:30", want: "19:30"},
		{name: "valid end time", end: start, start: "19:30", want: "19:30"},
		{name: "valid end string", end: "21:00:00", start: "19:30", want: "21:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveEndTime(tt.start, tt.end)
			if got != tt.want {
				t.Fatalf("resolveEndTime() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveEndDate(t *testing.T) {
	start := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	if got := resolveEndDate(start, time.Time{}); !got.Equal(start) {
		t.Fatalf("resolveEndDate(zero) = %v, want %v", got, start)
	}
	if got := resolveEndDate(start, end); !got.Equal(end) {
		t.Fatalf("resolveEndDate(valid) = %v, want %v", got, end)
	}
}

func TestBuildImageURL(t *testing.T) {
	got := buildImageURL("huashan1914", "exhibition", "1920 1080_25120516572868403.jpg")
	want := "https://media.huashan1914.com/WebUPD/huashan1914/exhibition/1920%201080_25120516572868403.jpg"
	if got != want {
		t.Fatalf("buildImageURL() = %q, want %q", got, want)
	}
}
