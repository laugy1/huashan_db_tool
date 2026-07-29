package events

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const categoryCollection = "category"

const (
	categoryIDEventCategory = "event_category"
	categoryIDEventVenue    = "event_venue"
	// categoryNameHistorical 每筆活動的 category_event_category.selected 必含此項。
	categoryNameHistorical = "歷史活動"
)

type categoryKey struct {
	categoryID string
	related    string
	name       string
}

// CategoryIndex maps (categoryId, related, name) → MongoDB _id hex string.
type CategoryIndex struct {
	byKey map[categoryKey]string
}

// LoadCategoryIndex reads the category collection and builds a lookup index.
func LoadCategoryIndex(ctx context.Context, coll *mongo.Collection) (*CategoryIndex, error) {
	cur, err := coll.Find(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("find categories: %w", err)
	}
	defer cur.Close(ctx)

	idx := &CategoryIndex{byKey: make(map[categoryKey]string)}
	for cur.Next(ctx) {
		var doc struct {
			ID         bson.ObjectID `bson:"_id"`
			CategoryID string        `bson:"categoryId"`
			Related    string        `bson:"related"`
			Name       string        `bson:"name"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decode category: %w", err)
		}
		if doc.CategoryID == "" || doc.Related == "" || doc.Name == "" {
			continue
		}
		key := categoryKey{
			categoryID: doc.CategoryID,
			related:    doc.Related,
			name:       doc.Name,
		}
		idx.byKey[key] = doc.ID.Hex()
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}
	return idx, nil
}

// Lookup returns the _id hex for a category row, or ("", false) if not found.
func (idx *CategoryIndex) Lookup(categoryID, related, name string) (string, bool) {
	if idx == nil || name == "" {
		return "", false
	}
	id, ok := idx.byKey[categoryKey{categoryID: categoryID, related: related, name: name}]
	return id, ok
}

// Len returns the number of indexed (categoryId, related, name) entries.
func (idx *CategoryIndex) Len() int {
	if idx == nil {
		return 0
	}
	return len(idx.byKey)
}

// ResolveNames maps legacy names to category _id hex strings, preserving order and skipping unknown names.
func (idx *CategoryIndex) ResolveNames(categoryID, related string, names []string) []string {
	if len(names) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if id, ok := idx.Lookup(categoryID, related, name); ok {
			out = append(out, id)
		}
	}
	return out
}

// ResolveEventCategorySelected maps legacy category names to _id hex strings and always
// includes「歷史活動」(prepended when not already selected).
func (idx *CategoryIndex) ResolveEventCategorySelected(related string, names []string) []string {
	selected := idx.ResolveNames(categoryIDEventCategory, related, names)
	histID, ok := idx.Lookup(categoryIDEventCategory, related, categoryNameHistorical)
	if !ok {
		return selected
	}
	return prependIDIfMissing(selected, histID)
}

func prependIDIfMissing(ids []string, id string) []string {
	for _, x := range ids {
		if x == id {
			return ids
		}
	}
	return append([]string{id}, ids...)
}

// RequireCategory returns an error if the named category row is missing from the index.
func (idx *CategoryIndex) RequireCategory(categoryID, related, name string) error {
	if _, ok := idx.Lookup(categoryID, related, name); !ok {
		return fmt.Errorf("category not found: categoryId=%q related=%q name=%q", categoryID, related, name)
	}
	return nil
}
