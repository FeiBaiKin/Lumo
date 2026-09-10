package taxonomy

import (
	"sort"
)

// BuildTree 由平铺列表构造分类森林。
//
// 同级按 position、name、id 排序；父分类不在列表中的节点当作根处理，
// 因此对任意子集都能得到一棵可渲染的树。叶子节点的 Children 为空切片而非 nil，
// 序列化后是 [] 而不是 null，前端不必做空值判断。
func BuildTree(categories []Category) []*CategoryNode {
	nodes := make(map[int64]*CategoryNode, len(categories))
	for i := range categories {
		nodes[categories[i].ID] = &CategoryNode{Category: categories[i], Children: []*CategoryNode{}}
	}

	roots := []*CategoryNode{}
	for i := range categories {
		node := nodes[categories[i].ID]
		if pid := categories[i].ParentID; pid != nil {
			if parent, ok := nodes[*pid]; ok && parent != node {
				parent.Children = append(parent.Children, node)
				continue
			}
		}
		roots = append(roots, node)
	}
	sortNodes(roots)
	return roots
}

// sortNodes 递归地按 position、name、id 排序。
func sortNodes(nodes []*CategoryNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.Position != b.Position {
			return a.Position < b.Position
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	for _, n := range nodes {
		sortNodes(n.Children)
	}
}

// indexByID 把列表转为 ID 索引。
func indexByID(categories []Category) map[int64]*Category {
	byID := make(map[int64]*Category, len(categories))
	for i := range categories {
		byID[categories[i].ID] = &categories[i]
	}
	return byID
}

// wouldCycle 报告把 nodeID 挂到 parentID 之下是否形成环（含挂到自身）。
//
// 从 parentID 沿父指针向上走：遇到 nodeID 即成环。数据本身不应有环，
// 但仍以节点数为步数上限，避免脏数据导致死循环——超限同样按成环处理。
func wouldCycle(byID map[int64]*Category, nodeID, parentID int64) bool {
	current := parentID
	for range len(byID) + 1 {
		if current == nodeID {
			return true
		}
		c, ok := byID[current]
		if !ok || c.ParentID == nil {
			return false
		}
		current = *c.ParentID
	}
	return true
}
