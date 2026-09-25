package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// 插件自己的数据：键值与资源记录。每个带后端的插件都有，不需要在清单里声明。
func init() {
	for name, op := range map[string]hostOp{
		"kv.get":           {call: hostKVGet},
		"kv.set":           {call: hostKVSet},
		"kv.incr":          {call: hostKVIncr},
		"kv.delete":        {call: hostKVDelete},
		"kv.list":          {call: hostKVList},
		"resources.list":   {call: hostResourcesList},
		"resources.get":    {call: hostResourcesGet},
		"resources.create": {call: hostResourcesCreate},
		"resources.update": {call: hostResourcesUpdate},
		"resources.delete": {call: hostResourcesDelete},
	} {
		hostOps[name] = op
	}
}

// listResult 是列表类宿主调用的结果：这一页，以及（有的话）总数。
type listResult struct {
	Items any `json:"items"`
	Total int `json:"total,omitempty"`
}

// maxTTL 是键值存活时长的上限（一年）：更久的就该是不过期。
const maxTTL = 365 * 24 * time.Hour

func (m *Module) dataStore() (*DataStore, error) {
	if m.data == nil {
		return nil, errors.New("站点没有连接数据库，插件数据不可用")
	}
	return m.data, nil
}

func ttlOf(seconds int64) time.Duration {
	ttl := time.Duration(seconds) * time.Second
	if ttl > maxTTL {
		return maxTTL
	}
	return ttl
}

func hostKVGet(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		Key string `json:"key"`
	}
	if err := decodeArgs("kv.get", args, &in); err != nil {
		return nil, err
	}
	data, err := m.dataStore()
	if err != nil {
		return nil, err
	}
	value, found, err := data.KVGet(ctx, loaded.ID(), in.Key)
	if err != nil {
		return nil, err
	}
	return map[string]any{"found": found, "value": value}, nil
}

func hostKVSet(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
		TTL   int64           `json:"ttl"`
	}
	if err := decodeArgs("kv.set", args, &in); err != nil {
		return nil, err
	}
	data, err := m.dataStore()
	if err != nil {
		return nil, err
	}
	if len(in.Value) == 0 {
		in.Value = json.RawMessage("null")
	}
	return nil, data.KVSet(ctx, loaded.ID(), in.Key, in.Value, ttlOf(in.TTL))
}

func hostKVIncr(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		Key string `json:"key"`
		By  *int64 `json:"by"`
		TTL int64  `json:"ttl"`
	}
	if err := decodeArgs("kv.incr", args, &in); err != nil {
		return nil, err
	}
	by := int64(1)
	if in.By != nil {
		by = *in.By
	}
	data, err := m.dataStore()
	if err != nil {
		return nil, err
	}
	value, err := data.KVIncr(ctx, loaded.ID(), in.Key, by, ttlOf(in.TTL))
	if err != nil {
		return nil, err
	}
	return map[string]int64{"value": value}, nil
}

func hostKVDelete(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		Key string `json:"key"`
	}
	if err := decodeArgs("kv.delete", args, &in); err != nil {
		return nil, err
	}
	data, err := m.dataStore()
	if err != nil {
		return nil, err
	}
	return nil, data.KVDelete(ctx, loaded.ID(), in.Key)
}

func hostKVList(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		Prefix string `json:"prefix"`
		Limit  int    `json:"limit"`
	}
	if err := decodeArgs("kv.list", args, &in); err != nil {
		return nil, err
	}
	data, err := m.dataStore()
	if err != nil {
		return nil, err
	}
	items, err := data.KVList(ctx, loaded.ID(), in.Prefix, in.Limit)
	if err != nil {
		return nil, err
	}
	return listResult{Items: items}, nil
}

// resourceOf 按种类取插件声明的资源。
func resourceOf(loaded *Loaded, kind string) (*Resource, error) {
	res, ok := loaded.ResourceByKind(kind)
	if !ok {
		return nil, fmt.Errorf("插件没有在 spec.resources 里声明资源 %q", kind)
	}
	return res, nil
}

func hostResourcesList(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		Kind   string         `json:"kind"`
		Match  map[string]any `json:"match"`
		Search string         `json:"search"`
		Sort   string         `json:"sort"`
		Desc   bool           `json:"desc"`
		Limit  int            `json:"limit"`
		Offset int            `json:"offset"`
	}
	if err := decodeArgs("resources.list", args, &in); err != nil {
		return nil, err
	}
	res, err := resourceOf(loaded, in.Kind)
	if err != nil {
		return nil, err
	}
	// 写错字段名时查出来是空的，比报错更难发现：直接拒绝
	for field := range in.Match {
		if !res.HasField(field) {
			return nil, fmt.Errorf("资源 %s 没有字段 %q", in.Kind, field)
		}
	}
	if in.Sort != "" && !res.HasField(in.Sort) {
		return nil, fmt.Errorf("资源 %s 没有字段 %q，不能按它排序", in.Kind, in.Sort)
	}
	data, err := m.dataStore()
	if err != nil {
		return nil, err
	}
	items, total, err := data.Records(ctx, loaded.ID(), res, &RecordQuery{
		Match: in.Match, Search: in.Search, Sort: in.Sort, Desc: in.Desc, Limit: in.Limit, Offset: in.Offset,
	})
	if err != nil {
		return nil, err
	}
	return listResult{Items: items, Total: total}, nil
}

// recordArgs 是按 ID 操作一条记录的参数。
type recordArgs struct {
	Kind string         `json:"kind"`
	ID   int64          `json:"id"`
	Data map[string]any `json:"data"`
	// Merge 为真时把 Data 并进原有数据，而不是整体替换。
	Merge bool `json:"merge"`
}

func hostResourcesGet(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in recordArgs
	if err := decodeArgs("resources.get", args, &in); err != nil {
		return nil, err
	}
	res, err := resourceOf(loaded, in.Kind)
	if err != nil {
		return nil, err
	}
	data, err := m.dataStore()
	if err != nil {
		return nil, err
	}
	return data.Record(ctx, loaded.ID(), res, in.ID)
}

func hostResourcesCreate(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in recordArgs
	if err := decodeArgs("resources.create", args, &in); err != nil {
		return nil, err
	}
	res, err := resourceOf(loaded, in.Kind)
	if err != nil {
		return nil, err
	}
	clean, err := res.Normalize(in.Data)
	if err != nil {
		return nil, err
	}
	data, err := m.dataStore()
	if err != nil {
		return nil, err
	}
	return data.CreateRecord(ctx, loaded.ID(), res, clean)
}

func hostResourcesUpdate(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in recordArgs
	if err := decodeArgs("resources.update", args, &in); err != nil {
		return nil, err
	}
	res, err := resourceOf(loaded, in.Kind)
	if err != nil {
		return nil, err
	}
	data, err := m.dataStore()
	if err != nil {
		return nil, err
	}
	next := in.Data
	if in.Merge {
		current, getErr := data.Record(ctx, loaded.ID(), res, in.ID)
		if getErr != nil {
			return nil, getErr
		}
		next = current.Data
		for key, value := range in.Data {
			next[key] = value
		}
	}
	clean, err := res.Normalize(next)
	if err != nil {
		return nil, err
	}
	return data.UpdateRecord(ctx, loaded.ID(), res, in.ID, clean)
}

func hostResourcesDelete(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in recordArgs
	if err := decodeArgs("resources.delete", args, &in); err != nil {
		return nil, err
	}
	res, err := resourceOf(loaded, in.Kind)
	if err != nil {
		return nil, err
	}
	data, err := m.dataStore()
	if err != nil {
		return nil, err
	}
	return nil, data.DeleteRecord(ctx, loaded.ID(), res, in.ID)
}
