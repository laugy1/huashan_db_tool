package events

import "testing"

func TestCategoryIndex_LookupAndResolve(t *testing.T) {
	idx := &CategoryIndex{
		byKey: map[categoryKey]string{
			{categoryID: "event_category", related: "events", name: "展覽"}: "69c4f3584dabdfd40972a815",
			{categoryID: "event_venue", related: "events", name: "東2館"}:   "69c4f3584dabdfd40972a816",
		},
	}

	id, ok := idx.Lookup("event_category", "events", "展覽")
	if !ok || id != "69c4f3584dabdfd40972a815" {
		t.Fatalf("Lookup() = %q, %v", id, ok)
	}

	got := idx.ResolveNames("event_category", "events", []string{"展覽", "不存在", "展覽"})
	want := []string{"69c4f3584dabdfd40972a815", "69c4f3584dabdfd40972a815"}
	if len(got) != len(want) {
		t.Fatalf("ResolveNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ResolveNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if empty := idx.ResolveNames("event_category", "events", nil); len(empty) != 0 {
		t.Fatalf("ResolveNames(nil) = %v, want empty slice", empty)
	}
}

func TestResolveEventCategorySelected_alwaysHistorical(t *testing.T) {
	idx := &CategoryIndex{
		byKey: map[categoryKey]string{
			{categoryID: "event_category", related: "events", name: "展覽"}:     "id-exhibition",
			{categoryID: "event_category", related: "events", name: "歷史活動"}: "id-historical",
		},
	}

	got := idx.ResolveEventCategorySelected("events", []string{"展覽"})
	want := []string{"id-historical", "id-exhibition"}
	if len(got) != len(want) {
		t.Fatalf("ResolveEventCategorySelected() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ResolveEventCategorySelected()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// 舊資料已含「歷史活動」時不重複
	got = idx.ResolveEventCategorySelected("events", []string{"歷史活動", "展覽"})
	want = []string{"id-historical", "id-exhibition"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dedupe: [%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// 無其他類別時仍只有歷史活動
	got = idx.ResolveEventCategorySelected("events", nil)
	if len(got) != 1 || got[0] != "id-historical" {
		t.Fatalf("only historical = %v", got)
	}
}
