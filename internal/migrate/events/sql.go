package events

// eventsQuery 以 FOR JSON PATH 子查詢聚合 M:N 關係，避免 JOIN 笛卡爾乘積。
// 欄位名稱依華山 huashan DB 慣例；若實際 schema 不同，請在此調整。
const eventsQuery = `
SELECT
  e.ID          AS event_id,
  e.SiteID      AS site_id,
  e.Title       AS title,
  e.DateStart   AS date_start,
  e.DateEnd     AS date_end,
  e.TimeStart   AS time_start,
  e.TimeEnd     AS time_end,
  e.TimeDesc    AS time_desc,
  e.CreateTime  AS create_time,
  m.SN          AS menu_sn,
  (
    SELECT TOP 1 p.Contents
    FROM Paragraph p
    WHERE p.SourceNo = e.ID
    ORDER BY p.ID
  ) AS contents,
  (
    SELECT et.Name AS name
    FROM EventToType ett
    INNER JOIN EventTypes et ON et.ID = ett.TypeID
    WHERE ett.EventID = e.ID
    FOR JSON PATH
  ) AS categories_json,
  (
    SELECT ep.Name AS name
    FROM EventToPlace etp
    INNER JOIN EventPlace ep ON ep.ID = etp.PlaceID
    WHERE etp.EventID = e.ID
    FOR JSON PATH
  ) AS venues_json,
  (
    SELECT eo.Name AS name
    FROM EventToOrganizer eto
    INNER JOIN EventOrganizer eo ON eo.ID = eto.OrganizerID
    WHERE eto.EventID = e.ID
    FOR JSON PATH
  ) AS organizers_json,
  (
    SELECT eobj.Name AS name
    FROM EventToObject etobj
    INNER JOIN EventObject eobj ON eobj.ID = etobj.ObjectID
    WHERE etobj.EventID = e.ID
    FOR JSON PATH
  ) AS objects_json,
  (
    SELECT ri.Img AS img
    FROM Paragraph p2
    INNER JOIN ResourceImages ri ON ri.SourceNo = p2.ID
    WHERE p2.SourceNo = e.ID
    FOR JSON PATH
  ) AS images_json
FROM Events e
LEFT JOIN Menus m ON m.ID = e.MenuID
WHERE e.SiteID = @p1
ORDER BY e.ID
`
