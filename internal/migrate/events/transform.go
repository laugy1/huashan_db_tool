package events

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const mediaBaseURL = "https://media.huashan1914.com/WebUPD"

type siteProfile struct {
	SiteID          int
	SitePath        string
	Collection      string
	SlugPrefix      string
	PostMetaID      string
	PostMetaName    string
	PostMetaPrefix  string
	CategoryRelated string // category.related，對應 events / umaytheater_events
}

var siteProfiles = map[int]siteProfile{
	1: {
		SiteID:          1,
		SitePath:        "huashan1914",
		Collection:      "events",
		SlugPrefix:      "event-",
		PostMetaID:      "events",
		PostMetaName:    "官網活動",
		PostMetaPrefix:  "https://www.huashan1914.com/events/",
		CategoryRelated: "events",
	},
	2: {
		SiteID:          2,
		SitePath:        "umaytheater",
		Collection:      "umaytheater_events",
		SlugPrefix:      "umayevent-",
		PostMetaID:      "umaytheater_events",
		PostMetaName:    "烏梅活動",
		PostMetaPrefix:  "/umaytheater/performance",
		CategoryRelated: "umaytheater_events",
	},
}

type rawRow struct {
	EventID        any
	SiteID         any
	Title          any
	DateStart      any
	DateEnd        any
	TimeStart      any
	TimeEnd        any
	TimeDesc       any
	CreateTime     any
	MenuSN         any
	ParagraphsJSON any
	CategoriesJSON any
	VenuesJSON     any
	OrganizersJSON any
	ObjectsJSON    any
	ImagesJSON     any
}

func buildDocument(row map[string]any, profile siteProfile, catIdx *CategoryIndex) (map[string]any, error) {
	r := rawRow{
		EventID:        row["event_id"],
		SiteID:         row["site_id"],
		Title:          row["title"],
		DateStart:      row["date_start"],
		DateEnd:        row["date_end"],
		TimeStart:      row["time_start"],
		TimeEnd:        row["time_end"],
		TimeDesc:       row["time_desc"],
		CreateTime:     row["create_time"],
		MenuSN:         row["menu_sn"],
		ParagraphsJSON: row["paragraphs_json"],
		CategoriesJSON: row["categories_json"],
		VenuesJSON:     row["venues_json"],
		OrganizersJSON: row["organizers_json"],
		ObjectsJSON:    row["objects_json"],
		ImagesJSON:     row["images_json"],
	}

	title := asString(r.Title)
	categories, err := parseNameList(r.CategoriesJSON)
	if err != nil {
		return nil, fmt.Errorf("categories_json: %w", err)
	}
	venues, err := parseNameList(r.VenuesJSON)
	if err != nil {
		return nil, fmt.Errorf("venues_json: %w", err)
	}
	organizers, err := parseNameList(r.OrganizersJSON)
	if err != nil {
		return nil, fmt.Errorf("organizers_json: %w", err)
	}
	objects, err := parseNameList(r.ObjectsJSON)
	if err != nil {
		return nil, fmt.Errorf("objects_json: %w", err)
	}
	images, err := parseImageList(r.ImagesJSON)
	if err != nil {
		return nil, fmt.Errorf("images_json: %w", err)
	}
	paragraphs, err := parseParagraphs(r.ParagraphsJSON)
	if err != nil {
		return nil, fmt.Errorf("paragraphs_json: %w", err)
	}

	menuSN := asString(r.MenuSN)
	legacyImages := buildLegacyImages(profile.SitePath, menuSN, images)

	categorySelected := catIdx.ResolveEventCategorySelected(profile.CategoryRelated, categories)
	venueSelected := catIdx.ResolveNames(categoryIDEventVenue, profile.CategoryRelated, venues)

	startDate, err := asTime(r.DateStart)
	if err != nil {
		return nil, fmt.Errorf("date_start: %w", err)
	}
	endDate, err := asTime(r.DateEnd)
	if err != nil {
		return nil, fmt.Errorf("date_end: %w", err)
	}
	endDate = resolveEndDate(startDate, endDate)
	startTime := formatTime(r.TimeStart)
	createdAt, err := asTime(r.CreateTime)
	if err != nil {
		return nil, fmt.Errorf("create_time: %w", err)
	}

	// CMS unique index uniq_translation_language requires (translation_id, language)
	// to be unique; generate a UUID per document for monolingual migrated content.
	doc := map[string]any{
		"version":           int64(1),
		"language":          "zh-TW",
		"translation_id":    uuid.New().String(),
		"language_relation": map[string]any{},
		"visible":           true,
		"_legacy_event_id":  r.EventID,
		"article": map[string]any{
			"default": map[string]any{
				"title": title,
				"slug":  slugify(title, profile.SlugPrefix),
			},
			"event_time": map[string]any{
				"start_date":  startDate,
				"end_date":    endDate,
				"start_time":  startTime,
				"end_time":    resolveEndTime(startTime, r.TimeEnd),
				"description": asString(r.TimeDesc),
			},
			"event_content": map[string]any{
				"description":       sanitizeEventHTML(mergeParagraphsHTML(paragraphs)),
				"hero_img":          []any{},
				"square_hero_image": []any{},
			},
			"sidebar_info": map[string]any{
				"suitable_audience": joinNames(objects),
				"ticket_link":       "",
				"organizer":         joinNames(organizers),
				"website_link":      "",
				"social_links_list": nil,
				"description":       "",
			},
		},
		"aside": map[string]any{
			"icon": map[string]any{
				"icon": "i-lucide-calendar",
			},
			"category_event_category": map[string]any{
				"selected": categorySelected,
			},
			"category_event_venue": map[string]any{
				"selected": venueSelected,
			},
			"post_status": map[string]any{
				"date_created": createdAt,
				"author":       "developer",
				"status":       "published",
			},
			"seo": map[string]any{
				"og_title":       "",
				"og_description": "",
				"og_image":       []any{},
			},
			"post_meta": map[string]any{
				"id": profile.PostMetaID,
				"name": map[string]any{
					"zh-TW": profile.PostMetaName,
				},
				"prefix": profile.PostMetaPrefix,
			},
			"language": map[string]any{},
		},
	}

	if len(legacyImages) > 0 {
		doc["_legacy_images"] = legacyImages
	}

	return doc, nil
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, "、")
}

func parseNameList(v any) ([]string, error) {
	if v == nil {
		return []string{}, nil
	}
	raw, err := jsonBytes(v)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return []string{}, nil
	}

	var items []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.Name != "" {
			out = append(out, it.Name)
		}
	}
	return out, nil
}

type paragraph struct {
	ID       string
	Title    string
	Contents string
	Sort     int64
}

type paragraphJSON struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Contents string `json:"contents"`
	Sort     any    `json:"sort"`
}

func parseParagraphs(v any) ([]paragraph, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := jsonBytes(v)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var items []paragraphJSON
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := make([]paragraph, 0, len(items))
	for _, it := range items {
		sortVal, _ := asInt64(it.Sort)
		out = append(out, paragraph{
			ID:       it.ID,
			Title:    it.Title,
			Contents: it.Contents,
			Sort:     sortVal,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func mergeParagraphsHTML(paragraphs []paragraph) string {
	var b strings.Builder
	for _, p := range paragraphs {
		title := strings.TrimSpace(p.Title)
		contents := strings.TrimSpace(p.Contents)
		if title == "" && contents == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		if title != "" {
			b.WriteString("<h2>")
			b.WriteString(html.EscapeString(title))
			b.WriteString("</h2>")
			if contents != "" {
				b.WriteByte('\n')
			}
		}
		if contents != "" {
			b.WriteString(p.Contents)
		}
	}
	return b.String()
}

func parseImageList(v any) ([]string, error) {
	if v == nil {
		return []string{}, nil
	}
	raw, err := jsonBytes(v)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return []string{}, nil
	}

	var items []struct {
		Img string `json:"img"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.Img != "" {
			out = append(out, it.Img)
		}
	}
	return out, nil
}

func buildLegacyImages(sitePath, menuSN string, images []string) []map[string]any {
	if len(images) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(images))
	for _, img := range images {
		entry := map[string]any{
			"img":     img,
			"menu_sn": menuSN,
		}
		if sitePath != "" && menuSN != "" && img != "" {
			entry["url"] = buildImageURL(sitePath, menuSN, img)
		}
		out = append(out, entry)
	}
	return out
}

func buildImageURL(sitePath, menuSN, img string) string {
	return fmt.Sprintf("%s/%s/%s/%s", mediaBaseURL, sitePath, menuSN, url.PathEscape(img))
}

func jsonBytes(v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case []byte:
		return x, nil
	case string:
		return []byte(x), nil
	default:
		return json.Marshal(x)
	}
}

func asString(v any) string {
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

func asTime(v any) (time.Time, error) {
	if v == nil {
		return time.Time{}, nil
	}
	switch x := v.(type) {
	case time.Time:
		return x, nil
	default:
		return time.Time{}, fmt.Errorf("unsupported time type %T", v)
	}
}

func resolveEndDate(start, end time.Time) time.Time {
	if end.IsZero() {
		return start
	}
	return end
}

func resolveEndTime(startTime string, end any) string {
	if isTimeEmpty(end) {
		return startTime
	}
	return formatTime(end)
}

func isTimeEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case time.Time:
		return x.IsZero()
	case string:
		return strings.TrimSpace(x) == ""
	case []byte:
		return strings.TrimSpace(string(x)) == ""
	default:
		return strings.TrimSpace(fmt.Sprint(x)) == ""
	}
}

func formatTime(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case time.Time:
		return x.Format("15:04")
	case string:
		if len(x) >= 5 {
			return x[:5]
		}
		return x
	case []byte:
		s := string(x)
		if len(s) >= 5 {
			return s[:5]
		}
		return s
	default:
		s := fmt.Sprint(x)
		if len(s) >= 5 {
			return s[:5]
		}
		return s
	}
}
