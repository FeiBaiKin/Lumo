package taxonomy

import (
	"encoding/json"
	"strings"
	"testing"
)

func ptr(v int64) *int64 { return &v }

func TestBuildTree(t *testing.T) {
	t.Parallel()

	// 故意乱序、含 position 相同的兄弟，以及父节点缺失的孤儿。
	categories := []Category{
		{ID: 3, ParentID: ptr(1), Name: "Rust", Position: 1},
		{ID: 1, Name: "技术", Position: 0},
		{ID: 2, ParentID: ptr(1), Name: "Go", Position: 0},
		{ID: 4, Name: "生活", Position: 1},
		{ID: 5, ParentID: ptr(2), Name: "泛型", Position: 0},
		{ID: 6, ParentID: ptr(99), Name: "孤儿", Position: 5},
		{ID: 7, Name: "Alpha", Position: 1},
	}

	roots := BuildTree(categories)
	if len(roots) != 4 {
		t.Fatalf("根节点数 = %d，期望 4", len(roots))
	}
	// 按 position 升序；position 相同（Alpha 与 生活 均为 1）时按名称字节序，孤儿的 position 为 5 排最后。
	wantRoots := []string{"技术", "Alpha", "生活", "孤儿"}
	for i, want := range wantRoots {
		if roots[i].Name != want {
			t.Errorf("roots[%d] = %s，期望 %s", i, roots[i].Name, want)
		}
	}

	tech := roots[0]
	if len(tech.Children) != 2 || tech.Children[0].Name != "Go" || tech.Children[1].Name != "Rust" {
		t.Errorf("技术 的子节点 = %v", names(tech.Children))
	}
	if got := tech.Children[0].Children; len(got) != 1 || got[0].Name != "泛型" {
		t.Errorf("Go 的子节点 = %v", names(got))
	}

	// 叶子的 children 必须序列化为 []，而非 null。
	raw, err := json.Marshal(tech.Children[1])
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(string(raw), `"children":[]`) {
		t.Errorf("叶子节点 children 应为 []: %s", raw)
	}
	if strings.Contains(string(raw), "BaseModel") {
		t.Errorf("序列化结果不应暴露 bun 内部字段: %s", raw)
	}

	if got := BuildTree(nil); got == nil || len(got) != 0 {
		t.Errorf("空输入应返回空切片而非 nil: %v", got)
	}
}

func names(nodes []*CategoryNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

func TestWouldCycle(t *testing.T) {
	t.Parallel()

	// 1 → 2 → 3（3 的父是 2，2 的父是 1）
	byID := indexByID([]Category{
		{ID: 1},
		{ID: 2, ParentID: ptr(1)},
		{ID: 3, ParentID: ptr(2)},
		{ID: 4},
	})

	tests := []struct {
		name         string
		node, parent int64
		want         bool
	}{
		{"挂到自身", 2, 2, true},
		{"挂到直接子节点", 1, 2, true},
		{"挂到孙节点", 1, 3, true},
		{"挂到祖先", 3, 1, false},
		{"挂到无关节点", 3, 4, false},
		{"父节点不存在按无环处理", 3, 999, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wouldCycle(byID, tt.node, tt.parent); got != tt.want {
				t.Errorf("wouldCycle(%d → %d) = %v，期望 %v", tt.node, tt.parent, got, tt.want)
			}
		})
	}

	// 脏数据本身成环时不得死循环。
	cyclic := indexByID([]Category{{ID: 1, ParentID: ptr(2)}, {ID: 2, ParentID: ptr(1)}})
	if !wouldCycle(cyclic, 3, 1) {
		t.Error("已有环的数据应按成环处理")
	}
}
