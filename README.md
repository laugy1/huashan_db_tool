# huashan_db_tool

把 MS SQL Server 的資料一次性遷移到 MongoDB 的 Go CLI 工具，特色：

- YAML 設定檔驅動，可定義多個遷移任務。
- 直接寫 SQL（支援 JOIN），用 `group_by` + `embed_as` 把子表 rows 聚合為 MongoDB 嵌入陣列。
- 串流讀取 + 批次 upsert（`ReplaceOne` with `upsert: true`），記憶體用量穩定。
- 支援欄位重命名（例如把 SQL 的 `id` 映射為 MongoDB 的 `_id`）。
- 支援寫入前 truncate 目標 collection、`--dry-run` 連線驗證。
- 結構化日誌（`slog`，可切 JSON）。

實務上華山活動資料分兩階段：`--task events` 搬活動文件，`--task events-images` 處理圖片。

## 需求

- Go 1.25+
- 可連線的 MS SQL Server 與 MongoDB
- `--task events-images` 且 `mode: upload` 時，還需要可登入的 CMS（DCSN）

## 編譯

```bash
go build -o huashan_db_tool .
```

## 指令

```bash
./huashan_db_tool [選項]
```

| 選項 | 預設值 | 說明 |
|------|--------|------|
| `-c` | `config.yaml` | YAML 設定檔路徑 |
| `--task` | `events` | 要執行的任務：`events`、`events-images`、`generic` |
| `--dry-run` | false | 驗證設定與連線後結束，不寫入資料 |
| `--json` | false | 以 JSON 輸出日誌 |
| `--log-level` | `info` | 日誌等級：`debug`、`info`、`warn`、`error` |

未知的 `--task` 會直接失敗。`generic` 需要設定檔裡有至少一筆 `migrations`。

### 常用指令

```bash
# 連線健檢，不寫入任何資料（預設 task=events）
./huashan_db_tool -c config.yaml --dry-run

# 階段一：從 MS SQL 遷移活動到 MongoDB（華山 events、烏梅 umaytheater_events）
./huashan_db_tool -c config.yaml
./huashan_db_tool -c config.yaml --task events

# 階段二：處理活動圖片（先確認 pending 筆數）
./huashan_db_tool -c config.yaml --task events-images --dry-run

# 階段二：實際處理圖片
./huashan_db_tool -c config.yaml --task events-images

# 通用 YAML migrations（需在設定檔寫 migrations:）
./huashan_db_tool -c config.yaml --task generic --dry-run
./huashan_db_tool -c config.yaml --task generic

# JSON 格式日誌、開 debug 等級
./huashan_db_tool -c config.yaml --json --log-level debug
```

建議順序：先 `--task events`，確認 MongoDB 文件寫入後，再跑 `--task events-images`。

## 設定

複製 `config.example.yaml` 為 `config.yaml`，依環境調整：

```bash
cp config.example.yaml config.yaml
export MSSQL_PASSWORD='your-password'
export MONGO_PASSWORD='your-password'
export CMS_PASSWORD='your-password'   # 僅 events-images 的 upload 模式需要
```

YAML 內可使用 `${ENV_VAR}` 引用環境變數，避免把密碼寫死在檔案裡。`config.yaml` 已加入 `.gitignore`，不會進版控。

### 活動遷移（`--task events`）

對應設定檔的 `events:`。會把舊站 SiteID 1（華山）與 2（烏梅）寫入 `events`、`umaytheater_events`。

| 欄位 | 預設 | 說明 |
|------|------|------|
| `truncate` | false | 寫入前是否清空目標 collection |
| `batch_size` | 500 | 單次 BulkWrite 大小 |

```yaml
events:
  truncate: true
  batch_size: 500
```

### 活動圖片（`--task events-images`）

對應設定檔的 `events_images:` 與（upload 模式需要的）`cms:`。會掃描 `events`、`umaytheater_events`。

| 欄位 | 預設 | 說明 |
|------|------|------|
| `mode` | `upload` | `lookup`：對照 MongoDB `media` 既有檔案；`upload`：下載舊站圖並上傳 CMS |
| `reupload` | false | false 時略過已有 `hero_img` 的文件；true 時重處理 |
| `remove_legacy_images` | true | 該筆圖片全部成功後，移除 `_legacy_images` |

```yaml
events_images:
  mode: lookup
  reupload: false
  remove_legacy_images: true

cms:
  base_url: "http://localhost:9453/dcsn"
  username: "developer"
  password: "${CMS_PASSWORD}"
```

`mode: lookup` 不連 MS SQL、不登入 CMS，只讀 MongoDB 的 `media` 做對照。`mode: upload` 必須設定 `cms.base_url`、`cms.username`、`cms.password`。

### Migration schema（`--task generic`）

| 欄位                   | 必填 | 說明 |
|------------------------|------|------|
| `name`                 | ✓    | 名稱（log 顯示用，必須唯一） |
| `query`                | ✓    | 來源 SQL；可任意 join |
| `target_collection`    | ✓    | 目標 MongoDB collection |
| `group_by`             |      | 父文件鍵欄位，連續同鍵的 row 會合併 |
| `embed_as`             |      | 子文件陣列名稱；需搭配 `group_by` 使用 |
| `rename`               |      | SQL 欄位 → MongoDB 鍵的映射（例如 `id: _id`） |
| `upsert_key`           |      | 父文件唯一鍵，預設 `_id` |
| `batch_size`           |      | 單次 BulkWrite 大小，預設 1000 |
| `truncate`             |      | 寫入前是否清空目標 collection |

### group_by + embed_as 行為

- 工具依 SQL 回傳的列順序「逐列掃描」，當 `group_by` 值跟前一列相同時，把目前列的非 group_by 欄位塞進 `embed_as` 陣列；不同時則 flush 上一份父文件，再開始新組。
- 因此 **SQL 必須以 `group_by` 欄位排序**（通常加 `ORDER BY` 即可），否則同一鍵會被切成多份文件。
- LEFT JOIN 沒命中時，子文件欄位全為 NULL 會被視為「無子文件」（不會在陣列裡塞一筆空物件）。

## 專案結構

```
.
├── main.go                       # CLI 入口
├── config.example.yaml           # 範例設定
└── internal/
    ├── config/   # YAML 載入、驗證、DSN 組裝
    ├── source/   # MS SQL Server streaming reader
    ├── sink/     # MongoDB 批次 upsert
    ├── cmsapi/   # CMS（DCSN）登入與媒體上傳
    ├── migrate/  # events / events-images 專用遷移
    └── pipeline/ # generic：group_by + embed_as 聚合與執行流程
```

## 注意事項

- 遇到 `Ctrl+C` / `SIGTERM` 會中止當前 query，但已寫進 MongoDB 的批次不會回滾（這是 upsert 的特性）。重跑時若 `upsert_key` 設定正確，會以原文件覆蓋，不會產生重複。
- SQL Server 的 `varchar/nvarchar` 可能以 `[]byte` 回傳；工具預設轉成 `string`。若你需要原始 bytes，請改用 `CAST(... AS varbinary(MAX))` 或在 pipeline 加擴充點。
- `decimal`、`uniqueidentifier` 等型別的處理交由 driver 預設行為，寫進 Mongo 時會以對應 BSON 型別保存。
