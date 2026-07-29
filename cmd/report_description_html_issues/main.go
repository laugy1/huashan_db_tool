package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/tommyliu/huashan_db_tool/internal/config"
)

func main() {
	var (
		cfgPath     = flag.String("c", "config.yaml", "config path")
		sampleLimit = flag.Int("sample-limit", 3, "sample offending document ids")
	)
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	must(err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI()))
	must(err)
	defer client.Disconnect(ctx)

	db := client.Database(cfg.MongoDB.Database)

	for _, coll := range []string{"events", "umaytheater_events"} {
		fmt.Printf("== collection: %s ==\n", coll)
		runChecks(ctx, db.Collection(coll), *sampleLimit)
	}
}

func runChecks(ctx context.Context, coll *mongo.Collection, sampleLimit int) {
	type check struct {
		key     string
		pattern string
	}

	checks := []check{
		{key: "inline_style_attr", pattern: `\bstyle\s*=`},
		{key: "font_tag", pattern: `<\s*font\b`},
		{key: "h1_tag", pattern: `<\s*h1\b`},
		{key: "style_tag", pattern: `<\s*style\b`},
		{key: "link_tag", pattern: `<\s*link\b`},
		{key: "meta_tag", pattern: `<\s*meta\b`},
		{key: "xml_tag", pattern: `<\s*xml\b`},
	}

	for _, ch := range checks {
		filter := bson.M{
			"article.event_content.description": bson.M{
				"$regex":   ch.pattern,
				"$options": "i",
			},
		}

		n, err := coll.CountDocuments(ctx, filter)
		if err != nil {
			fmt.Printf("- %s: count error: %v\n", ch.key, err)
			continue
		}

		if n == 0 {
			fmt.Printf("- %-20s : %d\n", ch.key, n)
			continue
		}

		fmt.Printf("- %-20s : %d (showing up to %d sample ids)\n", ch.key, n, sampleLimit)
		if sampleLimit <= 0 {
			continue
		}

		opts := options.Find().SetProjection(bson.D{
			{Key: "_id", Value: 1},
			{Key: "article.event_content.description", Value: 1},
		}).SetLimit(int64(sampleLimit))

		cur, err := coll.Find(ctx, filter, opts)
		if err != nil {
			fmt.Printf("  sample find error: %v\n", err)
			continue
		}
		defer cur.Close(ctx)

		i := 0
		for cur.Next(ctx) {
			var doc map[string]any
			if err := cur.Decode(&doc); err != nil {
				fmt.Printf("  sample decode error: %v\n", err)
				break
			}

			id := doc["_id"]
			descVal := getDescriptionValue(doc)
			desc := anyToString(descVal)
			if descVal == nil {
				// Minimal debug to understand how mongo-driver decodes projected dotted paths.
				keys := make([]string, 0, len(doc))
				for k := range doc {
					keys = append(keys, k)
				}
				fmt.Printf("  [debug] keys=%v hasFlat=%v articleType=%T\n", keys, doc["article.event_content.description"] != nil, doc["article"])
			}
			desc = strings.ReplaceAll(desc, "\n", " ")
			if len(desc) > 120 {
				desc = desc[:120] + "..."
			}
			fmt.Printf("  [%d] _id=%v desc=%q\n", i, id, desc)
			i++
		}
		_ = cur.Err()
	}
}

func anyToString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(v)
	}
}

func getDescriptionValue(doc map[string]any) any {
	// When projecting a dotted path, mongo-driver may return it as a flattened key.
	if v, ok := doc["article.event_content.description"]; ok {
		return v
	}
	// Or it may decode into nested maps.
	return deepGetAny(doc, "article", "event_content", "description")
}

func deepGetAny(v any, keys ...string) any {
	if len(keys) == 0 {
		return v
	}
	k := keys[0]
	rest := keys[1:]

	switch x := v.(type) {
	case map[string]any:
		nv, ok := x[k]
		if !ok {
			return nil
		}
		return deepGetAny(nv, rest...)
	case bson.M:
		nv, ok := x[k]
		if !ok {
			return nil
		}
		return deepGetAny(nv, rest...)
	case bson.D:
		for _, e := range x {
			if e.Key == k {
				return deepGetAny(e.Value, rest...)
			}
		}
		return nil
	default:
		return nil
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
