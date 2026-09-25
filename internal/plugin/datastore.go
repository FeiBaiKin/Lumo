package plugin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/extension"
)

// 插件数据的配额。插件来自第三方，存多少由宿主说了算。
const (
	maxKVKeys     = 10000
	maxKVKeyLen   = 200
	maxKVValue    = 64 << 10
	maxRecords    = 100000
	maxRecordSize = 64 << 10
	// MaxRecordPage 是一次列出的记录数上限。
	MaxRecordPage = 200
)

// ErrQuota 表示插件的数据超出了配额。
var ErrQuota = errors.New("插件数据超出配额")

// ErrRecordNotFound 表示资源记录不存在。
var ErrRecordNotFound = errors.New("记录不存在")

// DataStore 存插件自己的数据：键值与资源记录。所有查询都带着插件名，插件之间互相看不见。
type DataStore struct {
	db *bun.DB
}

// NewDataStore 构造 DataStore。
func NewDataStore(db *bun.DB) *DataStore { return &DataStore{db: db} }

// ---------- 键值 ----------

// KVItem 是一条键值。
type KVItem struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	ExpiresAt *time.Time      `json:"expiresAt,omitempty"`
}

// validKey 校验键：非空、有长度上限、不含控制字符。
func validKey(key string) error {
	switch {
	case key == "":
		return errors.New("键不能为空")
	case len(key) > maxKVKeyLen:
		return fmt.Errorf("键不能超过 %d 个字节", maxKVKeyLen)
	case strings.ContainsFunc(key, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return errors.New("键里不能有控制字符")
	}
	return nil
}

// expiry 把存活时长换成过期时刻；ttl 不大于零表示不过期。
func expiry(ttl time.Duration) *time.Time {
	if ttl <= 0 {
		return nil
	}
	at := time.Now().Add(ttl)
	return &at
}

// KVGet 读一个键；不存在或已过期时 ok 为 false。
func (s *DataStore) KVGet(ctx context.Context, plugin, key string) (value json.RawMessage, ok bool, err error) {
	var raw string
	err = s.db.NewRaw(`SELECT value::text FROM plugin_kv
		WHERE plugin = ? AND key = ? AND (expires_at IS NULL OR expires_at > now())`, plugin, key).Scan(ctx, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("读取插件数据: %w", err)
	}
	return json.RawMessage(raw), true, nil
}

// KVSet 写一个键。新键受数量配额约束，值受大小配额约束。
func (s *DataStore) KVSet(ctx context.Context, plugin, key string, value json.RawMessage, ttl time.Duration) error {
	if err := validKey(key); err != nil {
		return err
	}
	if len(value) > maxKVValue {
		return fmt.Errorf("%w：单个值不能超过 %d KiB", ErrQuota, maxKVValue>>10)
	}
	if !json.Valid(value) {
		return errors.New("值不是合法 JSON")
	}
	if err := s.checkKVQuota(ctx, plugin, key); err != nil {
		return err
	}
	_, err := s.db.NewRaw(`INSERT INTO plugin_kv (plugin, key, value, expires_at) VALUES (?, ?, ?::jsonb, ?)
		ON CONFLICT (plugin, key) DO UPDATE
		SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at, updated_at = now()`,
		plugin, key, string(value), expiry(ttl)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("写入插件数据: %w", err)
	}
	return nil
}

// checkKVQuota 在写入一个新键前核对键数。已存在的键覆盖写不受影响。
func (s *DataStore) checkKVQuota(ctx context.Context, plugin, key string) error {
	var exists bool
	if err := s.db.NewRaw(`SELECT EXISTS (SELECT 1 FROM plugin_kv WHERE plugin = ? AND key = ?)`,
		plugin, key).Scan(ctx, &exists); err != nil {
		return fmt.Errorf("读取插件数据: %w", err)
	}
	if exists {
		return nil
	}
	var count int
	if err := s.db.NewRaw(`SELECT count(*) FROM plugin_kv WHERE plugin = ?`, plugin).Scan(ctx, &count); err != nil {
		return fmt.Errorf("读取插件数据: %w", err)
	}
	if count >= maxKVKeys {
		return fmt.Errorf("%w：键值最多 %d 个", ErrQuota, maxKVKeys)
	}
	return nil
}

// KVIncr 把一个整数键原子地加上 by，返回加完的值。键不存在或已过期时从零开始计，
// ttl 大于零时只在这种「从零开始」的时候设置过期——按天计数的键因此到点自动归零。
func (s *DataStore) KVIncr(ctx context.Context, plugin, key string, by int64, ttl time.Duration) (int64, error) {
	if err := validKey(key); err != nil {
		return 0, err
	}
	if err := s.checkKVQuota(ctx, plugin, key); err != nil {
		return 0, err
	}
	var out int64
	err := s.db.NewRaw(`INSERT INTO plugin_kv (plugin, key, value, expires_at) VALUES (?, ?, to_jsonb(?::bigint), ?)
		ON CONFLICT (plugin, key) DO UPDATE SET
			value = to_jsonb(
				CASE WHEN plugin_kv.expires_at IS NOT NULL AND plugin_kv.expires_at <= now() THEN 0
				     ELSE (plugin_kv.value #>> '{}')::bigint END + ?::bigint),
			expires_at = CASE WHEN plugin_kv.expires_at IS NOT NULL AND plugin_kv.expires_at <= now()
			                  THEN EXCLUDED.expires_at ELSE plugin_kv.expires_at END,
			updated_at = now()
		RETURNING (value #>> '{}')::bigint`, plugin, key, by, expiry(ttl), by).Scan(ctx, &out)
	if err != nil {
		return 0, fmt.Errorf("累加插件数据（原值须为整数）: %w", err)
	}
	return out, nil
}

// KVDelete 删一个键；不存在也不报错。
func (s *DataStore) KVDelete(ctx context.Context, plugin, key string) error {
	if _, err := s.db.NewRaw(`DELETE FROM plugin_kv WHERE plugin = ? AND key = ?`, plugin, key).Exec(ctx); err != nil {
		return fmt.Errorf("删除插件数据: %w", err)
	}
	return nil
}

// KVList 按前缀列出键值，按键排序。
func (s *DataStore) KVList(ctx context.Context, plugin, prefix string, limit int) ([]KVItem, error) {
	if limit <= 0 || limit > MaxRecordPage {
		limit = MaxRecordPage
	}
	var rows []struct {
		Key       string     `bun:"key"`
		Value     string     `bun:"value"`
		ExpiresAt *time.Time `bun:"expires_at"`
	}
	err := s.db.NewRaw(`SELECT key, value::text AS value, expires_at FROM plugin_kv
		WHERE plugin = ? AND key LIKE ? ESCAPE '\' AND (expires_at IS NULL OR expires_at > now())
		ORDER BY key LIMIT ?`, plugin, escapeLike(prefix)+"%", limit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("列出插件数据: %w", err)
	}
	out := make([]KVItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, KVItem{Key: row.Key, Value: json.RawMessage(row.Value), ExpiresAt: row.ExpiresAt})
	}
	return out, nil
}

// SweepKV 删掉全部已过期的键值，返回删了多少。
func (s *DataStore) SweepKV(ctx context.Context) (int64, error) {
	res, err := s.db.NewRaw(`DELETE FROM plugin_kv WHERE expires_at IS NOT NULL AND expires_at <= now()`).Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("清理过期的插件数据: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// escapeLike 转义 LIKE 模式里的通配符。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ---------- 资源记录 ----------

// ResourceRecord 是插件资源的一条记录。
type ResourceRecord struct {
	ID        int64          `json:"id"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

func recordOf(e *extension.Extension) ResourceRecord {
	data := e.Spec
	if data == nil {
		data = map[string]any{}
	}
	return ResourceRecord{ID: e.ID, Data: data, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

// RecordQuery 是资源记录的查询条件。
type RecordQuery struct {
	// Match 是字段相等条件（jsonb 包含匹配，走 GIN 索引）。
	Match map[string]any
	// Search 在标题字段里模糊查找。
	Search string
	// Sort 是排序字段，须是资源的顶层字段；留空按创建先后倒序。
	Sort string
	// Desc 为真时倒序。
	Desc   bool
	Limit  int
	Offset int
}

// scope 把查询限定在某个插件的某种资源上。
func scope(q *bun.SelectQuery, plugin string, res *Resource) *bun.SelectQuery {
	return q.Where("ext.api_group = ? AND ext.version = ? AND ext.kind = ?", ResourceGroup(plugin), ResourceVersion, res.Kind)
}

// Records 按条件列出记录并给出总数。
func (s *DataStore) Records(ctx context.Context, plugin string, res *Resource, q *RecordQuery) ([]ResourceRecord, int, error) {
	limit := q.Limit
	if limit <= 0 || limit > MaxRecordPage {
		limit = MaxRecordPage
	}
	var rows []extension.Extension
	sel := scope(s.db.NewSelect().Model(&rows), plugin, res)
	if len(q.Match) > 0 {
		match, err := json.Marshal(q.Match)
		if err != nil {
			return nil, 0, fmt.Errorf("编码查询条件: %w", err)
		}
		sel = sel.Where("ext.spec @> ?::jsonb", string(match))
	}
	if q.Search != "" && res.Title != "" {
		sel = sel.Where("ext.spec ->> ? ILIKE ? ESCAPE '\\'", res.Title, "%"+escapeLike(q.Search)+"%")
	}
	direction := "ASC"
	if q.Desc {
		direction = "DESC"
	}
	switch {
	case q.Sort != "" && res.numeric[q.Sort]:
		// 先看 JSON 类型再转：一条脏数据不该让整个列表查不出来
		sel = sel.OrderExpr("CASE WHEN jsonb_typeof(ext.spec -> ?) = 'number' THEN (ext.spec ->> ?)::numeric END "+
			direction+" NULLS LAST, ext.id DESC", q.Sort, q.Sort)
	case q.Sort != "" && res.HasField(q.Sort):
		sel = sel.OrderExpr("ext.spec ->> ? "+direction+" NULLS LAST, ext.id DESC", q.Sort)
	default:
		sel = sel.OrderExpr("ext.id DESC")
	}
	total, err := sel.Limit(limit).Offset(max(q.Offset, 0)).ScanAndCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("列出插件记录: %w", err)
	}
	out := make([]ResourceRecord, 0, len(rows))
	for i := range rows {
		out = append(out, recordOf(&rows[i]))
	}
	return out, total, nil
}

// Record 读一条记录。
func (s *DataStore) Record(ctx context.Context, plugin string, res *Resource, id int64) (*ResourceRecord, error) {
	row := new(extension.Extension)
	err := scope(s.db.NewSelect().Model(row), plugin, res).Where("ext.id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("读取插件记录: %w", err)
	}
	out := recordOf(row)
	return &out, nil
}

// checkRecord 核对一条记录的大小。
func checkRecord(data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("记录无法编码: %w", err)
	}
	if len(raw) > maxRecordSize {
		return fmt.Errorf("%w：单条记录不能超过 %d KiB", ErrQuota, maxRecordSize>>10)
	}
	return nil
}

// CreateRecord 新建一条记录。记录名由宿主随机生成：插件与站长都按 ID 寻址。
func (s *DataStore) CreateRecord(ctx context.Context, plugin string, res *Resource, data map[string]any) (*ResourceRecord, error) {
	if err := checkRecord(data); err != nil {
		return nil, err
	}
	var count int
	if err := s.db.NewRaw(`SELECT count(*) FROM extensions WHERE api_group = ?`, ResourceGroup(plugin)).
		Scan(ctx, &count); err != nil {
		return nil, fmt.Errorf("统计插件记录: %w", err)
	}
	if count >= maxRecords {
		return nil, fmt.Errorf("%w：记录最多 %d 条", ErrQuota, maxRecords)
	}
	var id [8]byte
	_, _ = rand.Read(id[:])
	row := &extension.Extension{
		APIGroup: ResourceGroup(plugin), Version: ResourceVersion, Kind: res.Kind, Resource: res.Path,
		Name: "r" + hex.EncodeToString(id[:]), Spec: data,
	}
	if _, err := s.db.NewInsert().Model(row).Returning("id, created_at, updated_at").Exec(ctx); err != nil {
		return nil, fmt.Errorf("新建插件记录: %w", err)
	}
	out := recordOf(row)
	return &out, nil
}

// UpdateRecord 整体替换一条记录的数据。
func (s *DataStore) UpdateRecord(ctx context.Context, plugin string, res *Resource, id int64, data map[string]any) (*ResourceRecord, error) {
	if err := checkRecord(data); err != nil {
		return nil, err
	}
	row := &extension.Extension{Spec: data}
	result, err := s.db.NewUpdate().Model(row).Column("spec").Set("updated_at = now()").
		Where("id = ? AND api_group = ? AND version = ? AND kind = ?", id, ResourceGroup(plugin), ResourceVersion, res.Kind).
		Returning("id, spec, created_at, updated_at").Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("修改插件记录: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil, ErrRecordNotFound
	}
	out := recordOf(row)
	return &out, nil
}

// DeleteRecord 删一条记录。
func (s *DataStore) DeleteRecord(ctx context.Context, plugin string, res *Resource, id int64) error {
	result, err := s.db.NewDelete().Model((*extension.Extension)(nil)).
		Where("id = ? AND api_group = ? AND version = ? AND kind = ?", id, ResourceGroup(plugin), ResourceVersion, res.Kind).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除插件记录: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// ---------- 整体 ----------

// DataCounts 是一个插件名下的数据量，卸载时给站长看。
type DataCounts struct {
	Settings int `json:"settings" doc:"保存过的设置分组数"`
	KV       int `json:"kv" doc:"键值条数"`
	Records  int `json:"records" doc:"资源记录条数"`
}

// Counts 统计一个插件名下的数据。
func (s *DataStore) Counts(ctx context.Context, plugin string) (DataCounts, error) {
	var out DataCounts
	err := s.db.NewRaw(`SELECT
		(SELECT count(*) FROM plugin_settings WHERE plugin = ?) AS settings,
		(SELECT count(*) FROM plugin_kv WHERE plugin = ?) AS kv,
		(SELECT count(*) FROM extensions WHERE api_group = ?) AS records`,
		plugin, plugin, ResourceGroup(plugin)).Scan(ctx, &out.Settings, &out.KV, &out.Records)
	if err != nil {
		return out, fmt.Errorf("统计插件数据: %w", err)
	}
	return out, nil
}

// Purge 删掉一个插件名下的全部数据：设置、键值、资源记录，以及「保留数据」的登记。
func (s *DataStore) Purge(ctx context.Context, plugin string) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM plugin_settings WHERE plugin = ?`, []any{plugin}},
			{`DELETE FROM plugin_kv WHERE plugin = ?`, []any{plugin}},
			{`DELETE FROM extensions WHERE api_group = ?`, []any{ResourceGroup(plugin)}},
			{`DELETE FROM plugin_retained WHERE name = ?`, []any{plugin}},
		} {
			if _, err := tx.NewRaw(q.sql, q.args...).Exec(ctx); err != nil {
				return fmt.Errorf("清除插件数据: %w", err)
			}
		}
		return nil
	})
}

// Retained 是卸载了但保留着数据的插件。
type Retained struct {
	Name        string     `bun:"name" json:"name"`
	DisplayName string     `bun:"display_name" json:"displayName"`
	Version     string     `bun:"version" json:"version"`
	RetainedAt  time.Time  `bun:"retained_at" json:"retainedAt"`
	Counts      DataCounts `bun:"-" json:"counts"`
}

// Retain 登记一个保留了数据的插件。
func (s *DataStore) Retain(ctx context.Context, m *Manifest) error {
	_, err := s.db.NewRaw(`INSERT INTO plugin_retained (name, display_name, version) VALUES (?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET display_name = EXCLUDED.display_name, version = EXCLUDED.version, retained_at = now()`,
		m.Metadata.Name, m.Spec.DisplayName, m.Spec.Version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("登记保留的插件数据: %w", err)
	}
	return nil
}

// Reclaim 同名插件重装时移除登记：数据原样接回去。
func (s *DataStore) Reclaim(ctx context.Context, name string) error {
	if _, err := s.db.NewRaw(`DELETE FROM plugin_retained WHERE name = ?`, name).Exec(ctx); err != nil {
		return fmt.Errorf("接回保留的插件数据: %w", err)
	}
	return nil
}

// RetainedList 列出保留着数据的插件，附上数据量。
func (s *DataStore) RetainedList(ctx context.Context) ([]Retained, error) {
	var rows []Retained
	if err := s.db.NewRaw(`SELECT name, display_name, version, retained_at FROM plugin_retained ORDER BY retained_at DESC`).
		Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("列出保留的插件数据: %w", err)
	}
	for i := range rows {
		counts, err := s.Counts(ctx, rows[i].Name)
		if err != nil {
			return nil, err
		}
		rows[i].Counts = counts
	}
	return rows, nil
}
