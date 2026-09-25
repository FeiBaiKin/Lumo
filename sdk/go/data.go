package lumo

import (
	"encoding/json"
	"time"
)

// 插件自己的数据：键值与资源记录。每个带后端的插件都有，不需要声明能力；
// 插件之间互相看不见。配额：键值 1 万个、单值 64 KiB；资源记录共 10 万条、单条 64 KiB。

// KV 是本插件的键值存储：零碎状态、计数器、缓存。
var KV kvStore

type kvStore struct{}

// KVEntry 是一条键值。
type KVEntry struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	ExpiresAt *time.Time      `json:"expiresAt,omitempty"`
}

// Decode 把值解到 v 里。
func (e *KVEntry) Decode(v any) error { return json.Unmarshal(e.Value, v) }

func seconds(ttl time.Duration) int64 { return int64(ttl / time.Second) }

// kvArgs 是键值调用的参数。
type kvArgs struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value,omitempty"`
	By    *int64          `json:"by,omitempty"`
	TTL   int64           `json:"ttl,omitempty"`
}

// recordArgs 是资源记录调用的参数。
type recordArgs struct {
	Kind  string `json:"kind"`
	ID    int64  `json:"id,omitempty"`
	Data  any    `json:"data,omitempty"`
	Merge bool   `json:"merge,omitempty"`
}

// Get 读一个键，解到 v 里；不存在或已过期时返回 false。
func (kvStore) Get(key string, v any) (bool, error) {
	var out struct {
		Found bool            `json:"found"`
		Value json.RawMessage `json:"value"`
	}
	if err := call("kv.get", kvArgs{Key: key}, &out); err != nil {
		return false, err
	}
	if !out.Found {
		return false, nil
	}
	return true, json.Unmarshal(out.Value, v)
}

// Set 写一个键；ttl 为零表示不过期。
func (kvStore) Set(key string, v any, ttl time.Duration) error {
	value, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return call("kv.set", kvArgs{Key: key, Value: value, TTL: seconds(ttl)}, nil)
}

// Incr 把整数键原子地加上 by，返回加完的值。键不存在或已过期时从零开始计，
// 此时若 ttl 大于零就设上过期——按天计数的键因此到点自动归零。
func (kvStore) Incr(key string, by int64, ttl time.Duration) (int64, error) {
	var out struct {
		Value int64 `json:"value"`
	}
	err := call("kv.incr", kvArgs{Key: key, By: &by, TTL: seconds(ttl)}, &out)
	return out.Value, err
}

// Delete 删一个键。
func (kvStore) Delete(key string) error { return call("kv.delete", kvArgs{Key: key}, nil) }

// List 按前缀列出键值，按键排序，最多 200 条。
func (kvStore) List(prefix string, limit int) ([]KVEntry, error) {
	var out struct {
		Items []KVEntry `json:"items"`
	}
	err := call("kv.list", map[string]any{"prefix": prefix, "limit": limit}, &out)
	return out.Items, err
}

// Record 是一条资源记录。
type Record struct {
	ID        int64          `json:"id"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// Decode 把记录的字段解到 v 里，v 通常是一个带 json 标签的结构体指针。
func (r *Record) Decode(v any) error {
	raw, err := json.Marshal(r.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// Query 是列出资源记录的条件。
type Query struct {
	// Match 是字段相等条件。
	Match map[string]any `json:"match,omitempty"`
	// Search 在标题字段里模糊查找。
	Search string `json:"search,omitempty"`
	// Sort 是排序字段；留空按新建先后倒序。
	Sort string `json:"sort,omitempty"`
	Desc bool   `json:"desc,omitempty"`
	// Limit 最大 200。
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// Page 是一页资源记录。
type Page struct {
	Items []Record `json:"items"`
	Total int      `json:"total"`
}

// Resources 返回本插件某种资源（plugin.yaml 的 spec.resources 里声明的 kind）的读写入口。
func Resources(kind string) Collection { return Collection{kind: kind} }

// Collection 是一种资源的读写入口。写入的数据按清单里的字段声明校验，不合法的写不进去。
type Collection struct{ kind string }

// List 按条件列出记录。
func (c Collection) List(q Query) (*Page, error) {
	args := struct {
		Kind string `json:"kind"`
		Query
	}{c.kind, q}
	out := new(Page)
	return out, call("resources.list", args, out)
}

// Get 按 ID 读一条记录。
func (c Collection) Get(id int64) (*Record, error) {
	out := new(Record)
	return out, call("resources.get", recordArgs{Kind: c.kind, ID: id}, out)
}

// Create 新建一条记录；data 是结构体或 map，缺的字段用声明里的缺省值补上。
func (c Collection) Create(data any) (*Record, error) {
	out := new(Record)
	return out, call("resources.create", recordArgs{Kind: c.kind, Data: data}, out)
}

// Update 整体替换一条记录的数据。
func (c Collection) Update(id int64, data any) (*Record, error) {
	out := new(Record)
	return out, call("resources.update", recordArgs{Kind: c.kind, ID: id, Data: data}, out)
}

// Patch 只改给出的字段，其余保持原样。
func (c Collection) Patch(id int64, data any) (*Record, error) {
	out := new(Record)
	return out, call("resources.update", recordArgs{Kind: c.kind, ID: id, Data: data, Merge: true}, out)
}

// Delete 删一条记录。
func (c Collection) Delete(id int64) error {
	return call("resources.delete", recordArgs{Kind: c.kind, ID: id}, nil)
}
