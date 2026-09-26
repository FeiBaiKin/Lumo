package plugin

import (
	"testing"

	"github.com/FeiBaiKin/lumo/internal/extension"
)

// 0.2.0 写的 apiVersion 照样认，并改写成现行写法；别的写法拒绝。
func TestManifestLegacyAPIVersion(t *testing.T) {
	body := "kind: Plugin\nmetadata:\n  name: demo\nspec:\n  version: 1.0.0\n"
	m, err := parseManifest([]byte("apiVersion: "+legacyAPIVersion+"\n"+body), "")
	if err != nil {
		t.Fatalf("旧写法应能解析：%v", err)
	}
	if m.APIVersion != APIVersion {
		t.Fatalf("旧写法应改写成 %s，得到 %s", APIVersion, m.APIVersion)
	}
	if _, err := parseManifest([]byte("apiVersion: example.com/v1\n"+body), ""); err == nil {
		t.Fatal("未知的 apiVersion 应被拒绝")
	}
}

// 插件资源的分组要能走 Extension 平面的命名校验，名字最长时也一样。
func TestResourceGroupIsValid(t *testing.T) {
	for _, name := range []string{"a", "visit-stats", "p123456789-123456789-123456789-123456789-123456789-123456789-123"} {
		if err := extension.ValidateGroup(ResourceGroup(name)); err != nil {
			t.Errorf("%s：%v", name, err)
		}
	}
}
