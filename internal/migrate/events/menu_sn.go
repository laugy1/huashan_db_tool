package events

import (
	"context"
	"fmt"

	"github.com/tommyliu/huashan_db_tool/internal/source"
)

const menuSNQuery = `
SELECT e.ID AS event_id, m.SN AS menu_sn
FROM Events e
LEFT JOIN Menus m ON m.ID = e.MenuID
WHERE e.SiteID = @p1
`

func collectionSite(siteID int) (sitePath string, ok bool) {
	switch siteID {
	case 1:
		return "huashan1914", true
	case 2:
		return "umaytheater", true
	default:
		return "", false
	}
}

// defaultMenuSN is used when SQL MenuID is empty so a legacy image URL can still be built.
func defaultMenuSN(sitePath string) string {
	switch sitePath {
	case "huashan1914":
		return "exhibition"
	case "umaytheater":
		return "performance"
	default:
		return ""
	}
}

func siteIDForCollection(collection string) (int, error) {
	switch collection {
	case "events":
		return 1, nil
	case "umaytheater_events":
		return 2, nil
	default:
		return 0, fmt.Errorf("unknown collection %q", collection)
	}
}

// loadMenuSNMap loads Events.ID -> Menus.SN for a site.
func loadMenuSNMap(ctx context.Context, src *source.Source, siteID int) (map[int64]string, error) {
	out := make(map[int64]string)
	_, err := src.StreamParams(ctx, menuSNQuery, []any{siteID}, func(row source.Row) error {
		id, err := asInt64(row["event_id"])
		if err != nil {
			return err
		}
		sn, _ := row["menu_sn"].(string)
		if sn == "" {
			if b, ok := row["menu_sn"].([]byte); ok {
				sn = string(b)
			}
		}
		if sn != "" {
			out[id] = sn
		}
		return nil
	})
	return out, err
}

func asInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case int32:
		return int64(x), nil
	case int:
		return int64(x), nil
	case float64:
		return int64(x), nil
	default:
		return 0, fmt.Errorf("unsupported int64 type %T", v)
	}
}

func legacyEventID(doc map[string]any) (int64, bool) {
	v, ok := doc["_legacy_event_id"]
	if !ok || v == nil {
		return 0, false
	}
	id, err := asInt64(v)
	if err != nil {
		return 0, false
	}
	return id, true
}
