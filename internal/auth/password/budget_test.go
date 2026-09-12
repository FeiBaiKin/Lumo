package password

import (
	"errors"
	"testing"
)

// TestHashConcurrencyBudget 验证进程级 argon2 并发额度：
// 额度用满时立即返回 ErrBusy，而不是把请求堆在队列里等内存。
//
// 放在包内是为了直接操作额度闸门；导出「占位/释放」这类测试专用接口
// 会把一个安全机制变成任何人都能拆掉的零件。
func TestHashConcurrencyBudget(t *testing.T) {
	// 不并行：这条用例要独占额度，其他用例也会走 Hash/Verify。
	slots := cap(hashSlots)
	if slots < 1 {
		t.Fatalf("并发额度 = %d，期望至少 1", slots)
	}

	held := 0
	for range slots {
		if err := acquire(); err != nil {
			t.Fatalf("第 %d 次占位失败: %v", held+1, err)
		}
		held++
	}
	defer func() {
		for range held {
			release()
		}
	}()

	if _, err := Hash("test-password-123"); !errors.Is(err, ErrBusy) {
		t.Errorf("额度用满时 Hash 应返回 ErrBusy，实际 %v", err)
	}
	// 哈希串本身要能被解析，才会走到额度检查那一步。
	const encoded = "$argon2id$v=19$m=65536,t=3,p=1$c2FsdHNhbHRzYWx0c2FsdA$" +
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := Verify("test-password-123", encoded); !errors.Is(err, ErrBusy) {
		t.Errorf("额度用满时 Verify 应返回 ErrBusy，实际 %v", err)
	}

	// 归还一个额度后应立刻恢复。
	release()
	held--
	if _, err := Hash("test-password-123"); err != nil {
		t.Errorf("归还额度后应能继续哈希，实际 %v", err)
	}
}
