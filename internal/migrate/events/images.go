package events

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/tommyliu/huashan_db_tool/internal/cmsapi"
	"github.com/tommyliu/huashan_db_tool/internal/config"
	"github.com/tommyliu/huashan_db_tool/internal/sink"
	"github.com/tommyliu/huashan_db_tool/internal/source"
)

// ImagesStats describes phase-2 image migration results.
type ImagesStats struct {
	Collection     string
	DocsScanned    int64
	DocsUpdated    int64
	ImagesUploaded int64 // upload 模式：上傳成功；lookup 模式：成功對照 media
	ImagesSkipped  int64
	ImagesFailed   int64
	Duration       time.Duration
}

// RunPhase2 updates event documents with hero_img meta (upload or lookup mode).
func RunPhase2(ctx context.Context, cfg *config.EventsImagesConfig, cms *cmsapi.Client, src *source.Source, dst *sink.Sink, logger *slog.Logger) ([]ImagesStats, error) {
	var mediaIdx *MediaIndex
	if cfg.ModeIsLookup() {
		var err error
		mediaIdx, err = LoadMediaIndex(ctx, dst.Collection(mediaCollection))
		if err != nil {
			return nil, fmt.Errorf("load media index: %w", err)
		}
		logger.Info("loaded cms media index", "entries", mediaIdx.Len())
	}

	collections := []string{"events", "umaytheater_events"}
	results := make([]ImagesStats, 0, len(collections))

	for _, coll := range collections {
		st, err := runImagesCollection(ctx, cfg, cms, src, dst, mediaIdx, coll, logger)
		results = append(results, st)
		if err != nil {
			return results, fmt.Errorf("collection %s: %w", coll, err)
		}
	}
	return results, nil
}

// DryRunPhase2 counts documents pending image migration.
func DryRunPhase2(ctx context.Context, cfg *config.EventsImagesConfig, cms *cmsapi.Client, dst *sink.Sink, logger *slog.Logger) error {
	if cfg.ModeIsLookup() {
		mediaIdx, err := LoadMediaIndex(ctx, dst.Collection(mediaCollection))
		if err != nil {
			return err
		}
		logger.Info("loaded cms media index", "entries", mediaIdx.Len())
	} else {
		if err := cms.Login(ctx); err != nil {
			return fmt.Errorf("cms login: %w", err)
		}
		logger.Info("cms login ok")
	}

	for _, coll := range []string{"events", "umaytheater_events"} {
		n, err := countPendingImages(ctx, dst, coll, !cfg.Reupload)
		if err != nil {
			return err
		}
		logger.Info("pending image migration", "collection", coll, "documents", n)
	}
	return nil
}

func runImagesCollection(ctx context.Context, cfg *config.EventsImagesConfig, cms *cmsapi.Client, src *source.Source, dst *sink.Sink, mediaIdx *MediaIndex, collection string, logger *slog.Logger) (ImagesStats, error) {
	start := time.Now()
	st := ImagesStats{Collection: collection}
	log := logger.With("collection", collection)
	mode := "upload"
	if cfg.ModeIsLookup() {
		mode = "lookup"
	}
	log.Info("starting events phase2 image migration", "mode", mode)

	siteID, err := siteIDForCollection(collection)
	if err != nil {
		return st, err
	}
	sitePath, ok := collectionSite(siteID)
	if !ok {
		return st, fmt.Errorf("no site path for site_id %d", siteID)
	}

	menuSNMap := map[int64]string{}
	if src != nil {
		menuSNMap, err = loadMenuSNMap(ctx, src, siteID)
		if err != nil {
			return st, fmt.Errorf("load menu_sn map: %w", err)
		}
		log.Info("loaded menu_sn map", "entries", len(menuSNMap))
	}

	skipUploaded := !cfg.Reupload
	filter := pendingImagesFilter(skipUploaded)
	opts := options.Find().SetProjection(bson.D{
		{Key: "_id", Value: 1},
		{Key: "_legacy_event_id", Value: 1},
		{Key: "_legacy_images", Value: 1},
	})

	cur, err := dst.Collection(collection).Find(ctx, filter, opts)
	if err != nil {
		return st, err
	}
	defer cur.Close(ctx)

	urlCache := make(map[string]map[string]any)

	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			return st, err
		}
		st.DocsScanned++

		id := doc["_id"]
		docMap := bsonMToMap(doc)
		fallbackSN := ""
		if eid, ok := legacyEventID(docMap); ok {
			fallbackSN = menuSNMap[eid]
		}
		legacy := legacyImageEntries(doc["_legacy_images"], sitePath, fallbackSN)
		if len(legacy) == 0 {
			continue
		}

		var hero, square []any
		var uploaded, skipped, failed int64
		var procErr error
		if cfg.ModeIsLookup() {
			hero, square, uploaded, skipped, failed, procErr = processLegacyImagesLookup(mediaIdx, legacy, urlCache, log)
		} else {
			hero, square, uploaded, skipped, failed, procErr = processLegacyImages(ctx, cms, legacy, urlCache, log)
		}
		st.ImagesUploaded += uploaded
		st.ImagesSkipped += skipped
		st.ImagesFailed += failed

		if procErr != nil {
			log.Warn("document image migration incomplete",
				"doc_id", id,
				"legacy_event_id", doc["_legacy_event_id"],
				"err", procErr,
			)
			if len(hero) == 0 {
				continue
			}
		}

		update := bson.M{
			"$set": bson.M{
				"article.event_content.hero_img":            hero,
				"article.event_content.square_hero_image": square,
			},
		}
		if cfg.RemoveLegacyImages && procErr == nil {
			update["$unset"] = bson.M{"_legacy_images": ""}
		}

		if _, err := dst.Collection(collection).UpdateByID(ctx, id, update); err != nil {
			return st, fmt.Errorf("update doc %v: %w", id, err)
		}
		st.DocsUpdated++
	}
	if err := cur.Err(); err != nil {
		return st, err
	}

	st.Duration = time.Since(start)
	log.Info("events phase2 image migration finished",
		"docs_scanned", st.DocsScanned,
		"docs_updated", st.DocsUpdated,
		"images_uploaded", st.ImagesUploaded,
		"images_skipped", st.ImagesSkipped,
		"images_failed", st.ImagesFailed,
		"duration", st.Duration.String(),
	)
	return st, nil
}

func pendingImagesFilter(skipUploaded bool) bson.M {
	filter := bson.M{
		"_legacy_images": bson.M{"$exists": true, "$type": "array", "$ne": bson.A{}},
	}
	if skipUploaded {
		filter["$or"] = bson.A{
			bson.M{"article.event_content.hero_img": bson.M{"$exists": false}},
			bson.M{"article.event_content.hero_img": bson.A{}},
			bson.M{"article.event_content.hero_img": nil},
		}
	}
	return filter
}

func countPendingImages(ctx context.Context, dst *sink.Sink, collection string, skipUploaded bool) (int64, error) {
	return dst.Collection(collection).CountDocuments(ctx, pendingImagesFilter(skipUploaded))
}

type legacyImage struct {
	URL string
	Img string
}

func bsonMToMap(doc bson.M) map[string]any {
	out := make(map[string]any, len(doc))
	for k, v := range doc {
		out[k] = v
	}
	return out
}

func legacyImageEntries(v any, sitePath, fallbackMenuSN string) []legacyImage {
	arr, ok := v.(bson.A)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]legacyImage, 0, len(arr))
	seen := make(map[string]bool)
	for _, item := range arr {
		m, ok := bsonValueToMap(item)
		if !ok {
			continue
		}
		imgStr := stringField(m, "img")
		menuSN := stringField(m, "menu_sn")
		if menuSN == "" {
			menuSN = fallbackMenuSN
		}
		urlStr := stringField(m, "url")
		if urlStr == "" && imgStr != "" && menuSN != "" && sitePath != "" {
			urlStr = buildImageURL(sitePath, menuSN, imgStr)
		}
		if urlStr == "" {
			continue
		}
		if seen[urlStr] {
			continue
		}
		seen[urlStr] = true
		out = append(out, legacyImage{URL: urlStr, Img: imgStr})
	}
	return out
}

func bsonValueToMap(v any) (map[string]any, bool) {
	switch x := v.(type) {
	case bson.M:
		return x, true
	case map[string]any:
		return x, true
	case bson.D:
		m := make(map[string]any, len(x))
		for _, e := range x {
			m[e.Key] = e.Value
		}
		return m, true
	default:
		return nil, false
	}
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
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

func processLegacyImages(
	ctx context.Context,
	cms *cmsapi.Client,
	legacy []legacyImage,
	cache map[string]map[string]any,
	log *slog.Logger,
) (hero []any, square []any, uploaded, skipped, failed int64, err error) {
	hero = make([]any, 0, len(legacy))
	var firstSquare any
	var lastErr error

	for _, li := range legacy {
		if meta, ok := cache[li.URL]; ok {
			hero = append(hero, meta)
			if firstSquare == nil {
				firstSquare = meta
			}
			skipped++
			continue
		}

		data, filename, dlErr := cmsapi.Download(ctx, li.URL)
		if dlErr != nil {
			failed++
			lastErr = fmt.Errorf("download %s: %w", li.URL, dlErr)
			log.Warn("download failed", "url", li.URL, "err", dlErr)
			continue
		}
		if li.Img != "" {
			filename = li.Img
		}

		metaList, upErr := cms.Upload(ctx, filename, data)
		if upErr != nil {
			failed++
			lastErr = fmt.Errorf("upload %s: %w", li.URL, upErr)
			log.Warn("upload failed", "url", li.URL, "err", upErr)
			continue
		}

		meta := metaList[0]
		cache[li.URL] = meta
		hero = append(hero, meta)
		if firstSquare == nil {
			firstSquare = meta
		}
		uploaded++
	}

	if firstSquare != nil {
		square = []any{firstSquare}
	} else {
		square = []any{}
	}
	return hero, square, uploaded, skipped, failed, lastErr
}

func processLegacyImagesLookup(
	mediaIdx *MediaIndex,
	legacy []legacyImage,
	cache map[string]map[string]any,
	log *slog.Logger,
) (hero []any, square []any, resolved, skipped, failed int64, err error) {
	hero = make([]any, 0, len(legacy))
	var firstSquare any
	var lastErr error

	for _, li := range legacy {
		key := legacyImageLookupKey(li)
		if key == "" {
			failed++
			lastErr = fmt.Errorf("empty image key")
			continue
		}

		if meta, ok := cache[key]; ok {
			hero = append(hero, meta)
			if firstSquare == nil {
				firstSquare = meta
			}
			skipped++
			continue
		}

		meta, ok := mediaIdx.Lookup(key)
		if !ok {
			failed++
			lastErr = fmt.Errorf("media not found for %q", key)
			log.Warn("media lookup failed", "filename", key, "url", li.URL)
			continue
		}

		cache[key] = meta
		hero = append(hero, meta)
		if firstSquare == nil {
			firstSquare = meta
		}
		resolved++
	}

	if firstSquare != nil {
		square = []any{firstSquare}
	} else {
		square = []any{}
	}
	return hero, square, resolved, skipped, failed, lastErr
}
