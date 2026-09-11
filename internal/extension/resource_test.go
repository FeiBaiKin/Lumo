package extension_test

import (
	"testing"

	"github.com/FeiBaiKin/lumo/internal/extension"
)

// TestResource 固定 kind 到 URL 复数段的映射。
//
// 这条规则一旦改动，已写入的记录就会换地址，故用例把每个分支都钉住。
func TestResource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind string
		want string
	}{
		{"Post", "posts"},
		{"Page", "pages"},
		{"Category", "categories"},
		{"Key", "keys"},      // 元音 + y 不走 ies 分支
		{"Box", "boxes"},     // x 结尾
		{"Class", "classes"}, // s 结尾
		{"Buzz", "buzzes"},   // z 结尾
		{"Dish", "dishes"},   // sh 结尾
		{"Watch", "watches"}, // ch 结尾
		{"ReplicaSet", "replicasets"},
		{"Y", "ys"}, // 单字母不越界
	}
	for _, c := range cases {
		if got := extension.Resource(c.kind); got != c.want {
			t.Errorf("Resource(%q) = %q，期望 %q", c.kind, got, c.want)
		}
	}
}

func TestValidateGroup(t *testing.T) {
	t.Parallel()

	valid := []string{extension.GroupLumo, "io.github.feibaiikin.blog", "a.b", "x1-y.z9"}
	for _, group := range valid {
		if err := extension.ValidateGroup(group); err != nil {
			t.Errorf("ValidateGroup(%q) = %v，期望通过", group, err)
		}
	}

	// 不带点的单段名会与将来的保留字撞车，必须拒绝。
	invalid := []string{"", "lumo", "Io.Github", "a..b", "-a.b", "a.b-", "a_b.c"}
	for _, group := range invalid {
		if err := extension.ValidateGroup(group); err == nil {
			t.Errorf("ValidateGroup(%q) = nil，期望报错", group)
		}
	}
}

func TestValidateVersion(t *testing.T) {
	t.Parallel()

	valid := []string{"v1alpha1", "v1beta2", "v1", "v2"}
	for _, version := range valid {
		if err := extension.ValidateVersion(version); err != nil {
			t.Errorf("ValidateVersion(%q) = %v，期望通过", version, err)
		}
	}

	invalid := []string{"", "1", "V1", "v0", "v1gamma1", "v1alpha"}
	for _, version := range invalid {
		if err := extension.ValidateVersion(version); err == nil {
			t.Errorf("ValidateVersion(%q) = nil，期望报错", version)
		}
	}
}

func TestValidateKind(t *testing.T) {
	t.Parallel()

	valid := []string{"Post", "ReplicaSet", "A1"}
	for _, kind := range valid {
		if err := extension.ValidateKind(kind); err != nil {
			t.Errorf("ValidateKind(%q) = %v，期望通过", kind, err)
		}
	}

	invalid := []string{"", "post", "1Post", "Post-Type", "Post_Type", "Pöst"}
	for _, kind := range invalid {
		if err := extension.ValidateKind(kind); err == nil {
			t.Errorf("ValidateKind(%q) = nil，期望报错", kind)
		}
	}
}

func TestValidateName(t *testing.T) {
	t.Parallel()

	valid := []string{"a", "hello", "hello-world", "a1-b2"}
	for _, name := range valid {
		if err := extension.ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v，期望通过", name, err)
		}
	}

	invalid := []string{"", "-a", "a-", "Hello", "a_b", "a.b", "你好"}
	for _, name := range invalid {
		if err := extension.ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) = nil，期望报错", name)
		}
	}
}
