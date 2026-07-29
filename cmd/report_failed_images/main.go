package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/tommyliu/huashan_db_tool/internal/config"
)

type record struct {
	Collection     string `json:"collection"`
	ID             any    `json:"_id"`
	LegacyEventID  any    `json:"_legacy_event_id,omitempty"`
	LegacyImages   any    `json:"_legacy_images,omitempty"`
	Title          any    `json:"title,omitempty"`
	Slug           any    `json:"slug,omitempty"`
	HeroImgCount   int    `json:"hero_img_count,omitempty"`
	SquareImgCount int    `json:"square_hero_image_count,omitempty"`
}

func main() {
	var (
		cfgPath   = flag.String("c", "config.yaml", "config path")
		outPath   = flag.String("o", "failed_images.jsonl", "output jsonl path")
		withImages = flag.Bool("with-images", true, "include _legacy_images array in output")
	)
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	must(err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI()))
	must(err)
	defer client.Disconnect(ctx)

	db := client.Database(cfg.MongoDB.Database)

	f, err := os.Create(*outPath)
	must(err)
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	total := 0
	for _, coll := range []string{"events", "umaytheater_events"} {
		n, err := dumpCollection(ctx, db.Collection(coll), coll, w, *withImages)
		must(err)
		total += n
	}

	fmt.Printf("wrote %d records to %s\n", total, *outPath)
}

func dumpCollection(ctx context.Context, c *mongo.Collection, name string, w *bufio.Writer, withImages bool) (int, error) {
	filter := bson.M{"_legacy_images": bson.M{"$exists": true, "$type": "array", "$ne": bson.A{}}}
	proj := bson.D{
		{Key: "_id", Value: 1},
		{Key: "_legacy_event_id", Value: 1},
		{Key: "_legacy_images", Value: 1},
		{Key: "article.default.title", Value: 1},
		{Key: "article.default.slug", Value: 1},
		{Key: "article.event_content.hero_img", Value: 1},
		{Key: "article.event_content.square_hero_image", Value: 1},
	}
	cur, err := c.Find(ctx, filter, options.Find().SetProjection(proj))
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)

	count := 0
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			return count, err
		}
		rec := record{
			Collection:    name,
			ID:            doc["_id"],
			LegacyEventID: doc["_legacy_event_id"],
		}
		if withImages {
			rec.LegacyImages = doc["_legacy_images"]
		}
		rec.Title = deepGet(doc, "article", "default", "title")
		rec.Slug = deepGet(doc, "article", "default", "slug")
		rec.HeroImgCount = arrayLen(deepGet(doc, "article", "event_content", "hero_img"))
		rec.SquareImgCount = arrayLen(deepGet(doc, "article", "event_content", "square_hero_image"))

		b, _ := json.Marshal(rec)
		if _, err := w.Write(append(b, '\n')); err != nil {
			return count, err
		}
		count++
	}
	return count, cur.Err()
}

func deepGet(m bson.M, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(bson.M)
		if !ok {
			// sometimes nested docs decode into map[string]any
			if m2, ok := cur.(map[string]any); ok {
				v, ok := m2[k]
				if !ok {
					return nil
				}
				cur = v
				continue
			}
			return nil
		}
		v, ok := mm[k]
		if !ok {
			return nil
		}
		cur = v
	}
	return cur
}

func arrayLen(v any) int {
	switch x := v.(type) {
	case bson.A:
		return len(x)
	case []any:
		return len(x)
	default:
		return 0
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

