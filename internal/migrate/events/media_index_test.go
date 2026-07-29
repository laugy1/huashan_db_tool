package events

import "testing"

func TestMediaIndex_Lookup(t *testing.T) {
	idx := &MediaIndex{
		byID: map[string]map[string]any{
			"banner.png": {
				"_id":  "banner.png",
				"path": "http://localhost:9453/api/files/banner.png",
				"ext":  ".png",
			},
		},
	}

	meta, ok := idx.Lookup("banner.png")
	if !ok || meta["_id"] != "banner.png" {
		t.Fatalf("Lookup() = %v, %v", meta, ok)
	}

	_, ok = idx.Lookup("missing.jpg")
	if ok {
		t.Fatal("expected missing.jpg not found")
	}
}

func TestLegacyImageLookupKey(t *testing.T) {
	key := legacyImageLookupKey(legacyImage{Img: "path/to/foo.jpg"})
	if key != "foo.jpg" {
		t.Fatalf("key = %q, want foo.jpg", key)
	}
}
