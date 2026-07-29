package events

import (
	"context"
	"fmt"
	"net/url"
	"path"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const mediaCollection = "media"

// MediaIndex maps CMS media _id (檔名) → meta document for hero_img.
type MediaIndex struct {
	byID map[string]map[string]any
}

// LoadMediaIndex reads all documents from the CMS media collection.
func LoadMediaIndex(ctx context.Context, coll *mongo.Collection) (*MediaIndex, error) {
	cur, err := coll.Find(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("find media: %w", err)
	}
	defer cur.Close(ctx)

	idx := &MediaIndex{byID: make(map[string]map[string]any)}
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decode media: %w", err)
		}
		id := mediaIDString(doc["_id"])
		if id == "" {
			continue
		}
		meta := bsonMToMap(doc)
		meta["_id"] = id
		idx.byID[id] = meta
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("iterate media: %w", err)
	}
	return idx, nil
}

func (idx *MediaIndex) Len() int {
	if idx == nil {
		return 0
	}
	return len(idx.byID)
}

// Lookup finds media meta by legacy filename (_id in media collection).
func (idx *MediaIndex) Lookup(filename string) (map[string]any, bool) {
	if idx == nil || filename == "" {
		return nil, false
	}
	if meta, ok := idx.byID[filename]; ok {
		return meta, true
	}
	if base := path.Base(filename); base != filename {
		if meta, ok := idx.byID[base]; ok {
			return meta, true
		}
	}
	return nil, false
}

func mediaIDString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}

// legacyImageLookupKey is the cache / lookup key for a legacy image row.
func legacyImageLookupKey(li legacyImage) string {
	if li.Img != "" {
		if unescaped, err := url.PathUnescape(li.Img); err == nil {
			return path.Base(unescaped)
		}
		return path.Base(li.Img)
	}
	return li.URL
}
