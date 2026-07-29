# huashan_db_tool

把 MS SQL Server 的資料一次性遷移到 MongoDB 的 Go CLI 工具，特色：

- YAML 設定檔驅動，可定義多個遷移任務。
- 直接寫 SQL（支援 JOIN），用 `group_by` + `embed_as` 把子表 rows 聚合為 MongoDB 嵌入陣列。
- 串流讀取 + 批次 upsert（`ReplaceOne` with `upsert: true`），記憶體用量穩定。
- 支援欄位重命名（例如把 SQL 的 `id` 映射為 MongoDB 的 `_id`）。
- 支援寫入前 truncate 目標 collection、`--dry-run` 連線驗證。
- 結構化日誌（`slog`，可切 JSON）。

## 需求

- Go 1.25+
- 可連線的 MS SQL Server 與 MongoDB

## 編譯

```bash
go build -o huashan_db_tool .
```

## 設定

複製 `config.example.yaml` 為 `config.yaml`，依環境調整：

```bash
cp config.example.yaml config.yaml
export MSSQL_PASSWORD='your-password'
```

YAML 內可使用 `${ENV_VAR}` 引用環境變數，避免把密碼寫死在檔案裡。

### Migration schema

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

## 執行

```bash
# 連線健檢，不寫入任何資料
./huashan_db_tool -c config.yaml --dry-run

# 實際執行遷移
./huashan_db_tool -c config.yaml

# JSON 格式日誌、開 debug 等級
./huashan_db_tool -c config.yaml --json --log-level debug
```

## 專案結構

```
.
├── main.go                       # CLI 入口
├── config.example.yaml           # 範例設定
└── internal/
    ├── config/   # YAML 載入、驗證、DSN 組裝
    ├── source/   # MS SQL Server streaming reader
    ├── sink/     # MongoDB 批次 upsert
    └── pipeline/ # group_by + embed_as 聚合與執行流程
```

## 注意事項

- 遇到 `Ctrl+C` / `SIGTERM` 會中止當前 query，但已寫進 MongoDB 的批次不會回滾（這是 upsert 的特性）。重跑時若 `upsert_key` 設定正確，會以原文件覆蓋，不會產生重複。
- SQL Server 的 `varchar/nvarchar` 可能以 `[]byte` 回傳；工具預設轉成 `string`。若你需要原始 bytes，請改用 `CAST(... AS varbinary(MAX))` 或在 pipeline 加擴充點。
- `decimal`、`uniqueidentifier` 等型別的處理交由 driver 預設行為，寫進 Mongo 時會以對應 BSON 型別保存。
