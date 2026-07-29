// Package config defines the YAML configuration schema and loader for huashan_db_tool.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration loaded from a YAML file.
type Config struct {
	MSSQL        MSSQLConfig        `yaml:"mssql"`
	MongoDB      MongoConfig        `yaml:"mongodb"`
	Migrations   []Migration        `yaml:"migrations,omitempty"`
	Defaults     *Defaults          `yaml:"defaults,omitempty"`
	Events       *EventsConfig      `yaml:"events,omitempty"`
	EventsImages *EventsImagesConfig `yaml:"events_images,omitempty"`
	CMS          *CMSConfig         `yaml:"cms,omitempty"`
}

// EventsConfig controls the events phase-1 migration task.
type EventsConfig struct {
	Truncate  bool `yaml:"truncate,omitempty"`
	BatchSize int  `yaml:"batch_size,omitempty"`
}

// EventsImagesConfig controls phase-2 image migration for migrated events.
type EventsImagesConfig struct {
	// Mode: upload（預設）從舊站 URL 下載並上傳 CMS；lookup 從 MongoDB media 對照既有檔案 meta。
	Mode string `yaml:"mode,omitempty"`
	// Reupload 為 true 時會重處理已有 hero_img 的文件；預設 false（略過已處理）。
	Reupload bool `yaml:"reupload,omitempty"`
	// RemoveLegacyImages 全部圖片成功處理後移除 _legacy_images。
	RemoveLegacyImages bool `yaml:"remove_legacy_images,omitempty"`
}

// ModeIsLookup reports whether phase-2 should link existing CMS media instead of uploading.
func (c *EventsImagesConfig) ModeIsLookup() bool {
	return c != nil && c.Mode == "lookup"
}

// CMSConfig is the DCSN backend used for media upload.
type CMSConfig struct {
	BaseURL  string `yaml:"base_url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// MSSQLConfig holds connection parameters for the source SQL Server.
type MSSQLConfig struct {
	Server    string `yaml:"server"`
	Port      int    `yaml:"port"`
	Database  string `yaml:"database"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
	// Encrypt 可為 "true" / "false" / "disable" / "strict"，預設 "false"（本地開發常用）。
	Encrypt           string `yaml:"encrypt,omitempty"`
	TrustServerCert   bool   `yaml:"trust_server_certificate,omitempty"`
	ConnectionTimeout int    `yaml:"connection_timeout_seconds,omitempty"`
}

// MongoConfig holds connection parameters for the destination MongoDB.
type MongoConfig struct {
	// 完整 URI 優先；若未提供則用 Host/Port/Username/Password 組裝。
	URI      string `yaml:"uri,omitempty"`
	Host     string `yaml:"host,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Database string `yaml:"database"`
	AuthDB   string `yaml:"auth_database,omitempty"`
}

// Defaults provides fallback values for per-migration options.
type Defaults struct {
	BatchSize int  `yaml:"batch_size,omitempty"`
	Truncate  bool `yaml:"truncate,omitempty"`
}

// Migration describes a single source-to-destination dataflow.
type Migration struct {
	Name             string            `yaml:"name"`
	Query            string            `yaml:"query"`
	TargetCollection string            `yaml:"target_collection"`
	// GroupBy 列出的欄位會成為父文件的鍵，其餘欄位則以 EmbedAs 為名彙整成子陣列。
	// 若未設定 EmbedAs，則 GroupBy 之外的欄位仍以平坦結構寫入（取每組第一筆）。
	// 若 GroupBy 也未設定，則為 1:1 直接寫入。
	GroupBy []string `yaml:"group_by,omitempty"`
	EmbedAs string   `yaml:"embed_as,omitempty"`
	// Rename 用於把 SQL 欄位名改為 MongoDB 文件中的鍵名（含 "_id"）。
	Rename map[string]string `yaml:"rename,omitempty"`
	// UpsertKey 為 MongoDB 文件中的唯一鍵（rename 後的名稱），預設 "_id"。
	UpsertKey string `yaml:"upsert_key,omitempty"`
	BatchSize int    `yaml:"batch_size,omitempty"`
	Truncate  bool   `yaml:"truncate,omitempty"`
}

// Load reads a YAML config file from path, expands ${ENV_VAR} references,
// applies defaults and validates the result.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	expanded := os.ExpandEnv(string(raw))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.MSSQL.Port == 0 {
		c.MSSQL.Port = 1433
	}
	if c.MSSQL.Encrypt == "" {
		c.MSSQL.Encrypt = "false"
	}
	if c.MSSQL.ConnectionTimeout == 0 {
		c.MSSQL.ConnectionTimeout = 30
	}
	if c.MongoDB.Port == 0 {
		c.MongoDB.Port = 27017
	}

	def := c.Defaults
	if def == nil {
		def = &Defaults{BatchSize: 1000}
		c.Defaults = def
	}
	if def.BatchSize <= 0 {
		def.BatchSize = 1000
	}

	if c.Events == nil {
		c.Events = &EventsConfig{}
	}
	if c.Events.BatchSize <= 0 {
		c.Events.BatchSize = 500
	}

	if c.EventsImages == nil {
		c.EventsImages = &EventsImagesConfig{RemoveLegacyImages: true}
	}
	if c.CMS == nil {
		c.CMS = &CMSConfig{}
	}

	for i := range c.Migrations {
		m := &c.Migrations[i]
		if m.BatchSize <= 0 {
			m.BatchSize = def.BatchSize
		}
		if m.UpsertKey == "" {
			m.UpsertKey = "_id"
		}
	}
}

func (c *Config) validate() error {
	if c.MSSQL.Server == "" {
		return fmt.Errorf("mssql.server is required")
	}
	if c.MSSQL.Database == "" {
		return fmt.Errorf("mssql.database is required")
	}
	if c.MongoDB.URI == "" && c.MongoDB.Host == "" {
		return fmt.Errorf("mongodb.uri or mongodb.host is required")
	}
	if c.MongoDB.Database == "" {
		return fmt.Errorf("mongodb.database is required")
	}
	seen := make(map[string]bool, len(c.Migrations))
	for i, m := range c.Migrations {
		if m.Name == "" {
			return fmt.Errorf("migrations[%d].name is required", i)
		}
		if seen[m.Name] {
			return fmt.Errorf("duplicate migration name %q", m.Name)
		}
		seen[m.Name] = true
		if m.Query == "" {
			return fmt.Errorf("migrations[%s].query is required", m.Name)
		}
		if m.TargetCollection == "" {
			return fmt.Errorf("migrations[%s].target_collection is required", m.Name)
		}
		if m.EmbedAs != "" && len(m.GroupBy) == 0 {
			return fmt.Errorf("migrations[%s].embed_as requires at least one group_by column", m.Name)
		}
	}
	return nil
}

// ValidateCMS 檢查階段二 upload 模式所需的 CMS 設定。
func (c *Config) ValidateCMS() error {
	if c.EventsImages != nil && c.EventsImages.ModeIsLookup() {
		return nil
	}
	if c.CMS == nil || c.CMS.BaseURL == "" {
		return fmt.Errorf("cms.base_url is required for events-images task")
	}
	if c.CMS.Username == "" {
		return fmt.Errorf("cms.username is required for events-images task")
	}
	if c.CMS.Password == "" {
		return fmt.Errorf("cms.password is required for events-images task")
	}
	return nil
}

// MSSQLDSN builds a sqlserver:// connection string suitable for go-mssqldb.
func (c *Config) MSSQLDSN() string {
	q := url.Values{}
	q.Set("database", c.MSSQL.Database)
	q.Set("encrypt", c.MSSQL.Encrypt)
	if c.MSSQL.TrustServerCert {
		q.Set("TrustServerCertificate", "true")
	}
	q.Set("connection timeout", strconv.Itoa(c.MSSQL.ConnectionTimeout))

	u := &url.URL{
		Scheme:   "sqlserver",
		Host:     fmt.Sprintf("%s:%d", c.MSSQL.Server, c.MSSQL.Port),
		RawQuery: q.Encode(),
	}
	if c.MSSQL.Username != "" {
		u.User = url.UserPassword(c.MSSQL.Username, c.MSSQL.Password)
	}
	return u.String()
}

// MongoURI returns the connection URI, assembled from parts when not provided directly.
func (c *Config) MongoURI() string {
	if c.MongoDB.URI != "" {
		return c.MongoDB.URI
	}
	u := &url.URL{
		Scheme: "mongodb",
		Host:   fmt.Sprintf("%s:%d", c.MongoDB.Host, c.MongoDB.Port),
		// mongo driver 要求若有 query 參數，host 後需有 "/"；統一補上不影響功能。
		Path: "/",
	}
	if c.MongoDB.Username != "" {
		u.User = url.UserPassword(c.MongoDB.Username, c.MongoDB.Password)
	}
	if c.MongoDB.AuthDB != "" {
		q := url.Values{}
		q.Set("authSource", c.MongoDB.AuthDB)
		u.RawQuery = q.Encode()
	}
	return u.String()
}
