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

func TestDefaultMenuSN(t *testing.T) {
	if got := defaultMenuSN("huashan1914"); got != "exhibition" {
		t.Fatalf("defaultMenuSN(huashan1914) = %q", got)
	}
	if got := defaultMenuSN("umaytheater"); got != "performance" {
		t.Fatalf("defaultMenuSN(umaytheater) = %q", got)
	}
	if got := defaultMenuSN("unknown"); got != "" {
		t.Fatalf("defaultMenuSN(unknown) = %q", got)
	}
}

func TestParseParagraphsSortsBySortThenID(t *testing.T) {
	raw := `[
		{"id":"25081517281340986","title":"【活動資訊】","contents":"<p>資訊</p>","sort":2},
		{"id":"25081517184004447","title":"放影展","contents":"<p>開頭</p>","sort":0},
		{"id":"25081517493638713","title":"【活動簡介】","contents":"<p>簡介</p>","sort":1}
	]`
	got, err := parseParagraphs(raw)
	if err != nil {
		t.Fatalf("parseParagraphs() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantIDs := []string{"25081517184004447", "25081517493638713", "25081517281340986"}
	for i, id := range wantIDs {
		if got[i].ID != id {
			t.Fatalf("got[%d].ID = %q, want %q", i, got[i].ID, id)
		}
	}
}

func TestMergeParagraphsHTML(t *testing.T) {
	tests := []struct {
		name string
		in   []paragraph
		want string
	}{
		{
			name: "empty",
			in:   nil,
			want: "",
		},
		{
			name: "skip blank paragraphs",
			in:   []paragraph{{Title: "  ", Contents: "\n"}},
			want: "",
		},
		{
			name: "title only",
			in:   []paragraph{{Title: "【活動簡介】"}},
			want: "<h2>【活動簡介】</h2>",
		},
		{
			name: "contents only",
			in:   []paragraph{{Contents: "<p>開頭</p>"}},
			want: "<p>開頭</p>",
		},
		{
			name: "escape title html",
			in:   []paragraph{{Title: `A <b>B</b> & C`}},
			want: "<h2>A &lt;b&gt;B&lt;/b&gt; &amp; C</h2>",
		},
		{
			name: "multiple sections",
			in: []paragraph{
				{Title: "放影展", Contents: "<p>開頭</p>"},
				{Title: "【活動簡介】", Contents: "<p>簡介</p>"},
				{Title: "【活動資訊】", Contents: "<p>資訊</p>"},
			},
			want: "<h2>放影展</h2>\n<p>開頭</p>\n<h2>【活動簡介】</h2>\n<p>簡介</p>\n<h2>【活動資訊】</h2>\n<p>資訊</p>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeParagraphsHTML(tt.in)
			if got != tt.want {
				t.Fatalf("mergeParagraphsHTML() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeMergedParagraphs(t *testing.T) {
	paras := []paragraph{
		{Title: "放影展", Contents: `<p style="text-align:center">開頭</p>`},
		{Title: "【活動簡介】", Contents: `<p><font size="3">簡介</font></p>`},
	}
	got := sanitizeEventHTML(mergeParagraphsHTML(paras))
	want := "<h2>放影展</h2>\n<p>開頭</p>\n<h2>【活動簡介】</h2>\n<p>簡介</p>"
	if got != want {
		t.Fatalf("sanitize merged = %q, want %q", got, want)
	}
}
