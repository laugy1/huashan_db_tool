package events

import (
	"strings"
	"testing"
)

func TestSanitizeEventHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty stays empty",
			in:   "",
			want: "",
		},
		{
			name: "whitespace only preserved",
			in:   "   ",
			want: "   ",
		},
		{
			name: "strip style double quotes",
			in:   `<p style="color:red; font-size:12px">hello</p>`,
			want: `<p>hello</p>`,
		},
		{
			name: "strip style single quotes and case-insensitive attr",
			in:   `<span STYLE='color:blue' class="x">hi</span>`,
			want: `<span class="x">hi</span>`,
		},
		{
			name: "strip unquoted style",
			in:   `<div style=color:red class="a">x</div>`,
			want: `<div class="a">x</div>`,
		},
		{
			name: "unwrap font keep nested content",
			in:   `<p><font color="red">a <b>bold</b> b</font></p>`,
			want: `<p>a <b>bold</b> b</p>`,
		},
		{
			name: "unwrap nested font case-insensitive",
			in:   `<FONT>outer <Font>inner</Font> tail</FONT>`,
			want: `outer inner tail`,
		},
		{
			name: "drop style tag with content",
			in:   `<p>before</p><style>.a{color:red}</style><p>after</p>`,
			want: `<p>before</p><p>after</p>`,
		},
		{
			name: "remove style/xml blocks inside comments",
			in:   `<!-- <style>.a{color:red}</style> --><p>ok</p><!--<XML><o:p>x</o:p></XML>-->`,
			want: `<p>ok</p>`,
		},
		{
			name: "drop link meta xml",
			in:   `<link rel="stylesheet" href="a.css"><meta charset="utf-8"><xml><o:p>x</o:p></xml><p>ok</p>`,
			want: `<p>ok</p>`,
		},
		{
			name: "drop STYLE LINK META XML case-insensitive",
			in:   `<STYLE>x</STYLE><LINK href="a"><META name="y"><XML>z</XML>ok`,
			want: `ok`,
		},
		{
			name: "h1 to h2 keep attrs and content",
			in:   `<h1 class="title" id="t">標題 <em>強調</em></h1>`,
			want: `<h2 class="title" id="t">標題 <em>強調</em></h2>`,
		},
		{
			name: "combined rules",
			in: `<h1 style="margin:0">標題</h1>
<style>body{}</style>
<p STYLE="color:red"><font size="3">內文 <b>粗</b></font></p>
<link href="x.css">
<meta name="viewport">
<xml>junk</xml>`,
			// Deleting tags leaves surrounding newlines; that is intentional.
			want: `<h2>標題</h2>

<p>內文 <b>粗</b></p>


`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeEventHTML(tt.in)
			if normalizeWS(got) != normalizeWS(tt.want) {
				t.Fatalf("sanitizeEventHTML()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeEventHTML_ConditionalCommentXML(t *testing.T) {
	// Simulate Word exported conditional comment: <xml> lives as raw text inside <!-- ... -->
	// Even if the closing </xml> isn't present in this substring, we still need to remove "<xml" start.
	in := `<!--[if gte mso 9]><xml> <w:WordDocument> <w:View>Normal</w:View>`
	got := sanitizeEventHTML(in)
	if strings.Contains(strings.ToLower(got), "<xml") {
		t.Fatalf("expected <xml removed, got: %q", got)
	}
}

func normalizeWS(s string) string {
	// Collapse pure trailing/leading newlines differences from html.Render,
	// but keep intentional blank lines comparable via TrimSpace on whole string.
	return strings.TrimSpace(s)
}
