package plugin

import (
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// 插件只能申请内容类写权限：拿到用户、角色、设置、插件这类就能给自己提权。
func TestCapabilitiesRejectPrivilegeEscalation(t *testing.T) {
	for _, p := range []string{"users:manage", "roles:manage", "settings:manage", "plugins:manage",
		"themes:manage", "content:unsafe_html", "site:delete", "posts:everything"} {
		c := Capabilities{Content: ContentAccess{Write: []string{p}}}
		if err := c.normalize(); err == nil {
			t.Errorf("写权限 %s 应被拒绝", p)
		}
	}
	c := Capabilities{Content: ContentAccess{Write: []string{"comments:manage_any", "comments:manage_any", "posts:write"}}}
	if err := c.normalize(); err != nil {
		t.Fatal(err)
	}
	if !c.Content.Read || len(c.Content.Write) != 2 || !c.AllowsWrite(perm.CommentsManageAny) {
		t.Fatalf("写权限应去重并隐含可读，得到 %+v", c.Content)
	}
}

// 外部域名只收公网域名：IP、端口、localhost、内网后缀一律拒绝，防插件借宿主打内网。
func TestCapabilitiesHTTPHosts(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "10.0.0.1", "localhost", "db.internal", "printer.local",
		"example.com:8080", "https://example.com", "exa mple.com", "*.*.example.com", "api.*.com", "com", ""} {
		c := Capabilities{HTTP: []string{host}}
		if err := c.normalize(); err == nil {
			t.Errorf("域名 %q 应被拒绝", host)
		}
	}
	c := Capabilities{HTTP: []string{"API.Akismet.com", "*.example.org", "api.akismet.com"}}
	if err := c.normalize(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.HTTP, ",") != "*.example.org,api.akismet.com" {
		t.Fatalf("域名应小写、去重、排序，得到 %v", c.HTTP)
	}
	for host, want := range map[string]bool{
		"api.akismet.com": true, "API.AKISMET.COM.": true, "evil.akismet.com": false,
		"cdn.example.org": true, "a.b.example.org": true, "example.org": false, "example.org.evil.com": false,
	} {
		if got := c.AllowsHost(host); got != want {
			t.Errorf("AllowsHost(%q) = %v，应为 %v", host, got, want)
		}
	}
}

// 升级后多要的能力没被授予过，就需要重新确认；少要或一样多则不用。
func TestCapabilitiesCovers(t *testing.T) {
	granted := Capabilities{Content: ContentAccess{Read: true}, HTTP: []string{"a.com"}, Mail: true}
	cases := []struct {
		want Capabilities
		ok   bool
	}{
		{Capabilities{Content: ContentAccess{Read: true}}, true},
		{Capabilities{HTTP: []string{"a.com"}, Mail: true}, true},
		{Capabilities{HTTP: []string{"b.com"}}, false},
		{Capabilities{Content: ContentAccess{Write: []string{"posts:write"}}}, false},
		{Capabilities{Cron: true}, false},
		{Capabilities{Frontend: true}, false},
	}
	for i, tc := range cases {
		if got := granted.Covers(&tc.want); got != tc.ok {
			t.Errorf("第 %d 条：Covers = %v，应为 %v", i, got, tc.ok)
		}
	}
}

// 读内容、发请求这类能力只有后端代码用得上：纯声明式插件声明了也只是让站长白白点头。
func TestManifestRequiresBackendForBackendCapabilities(t *testing.T) {
	base := "apiVersion: io.github.feibaikin.lumo/v1alpha1\nkind: Plugin\nmetadata:\n  name: demo\nspec:\n  version: 1.0.0\n"
	if _, err := parseManifest([]byte(base+"  capabilities:\n    mail: true\n"), ""); err == nil {
		t.Fatal("没有 runtime 却声明 mail 应被拒绝")
	}
	if _, err := parseManifest([]byte(base+"  capabilities:\n    frontend: true\n"), ""); err != nil {
		t.Fatalf("纯声明式插件可以只声明 frontend：%v", err)
	}
	if _, err := parseManifest([]byte(base+"  runtime: native\n"), ""); err == nil {
		t.Fatal("未知的 runtime 应被拒绝")
	}
	m, err := parseManifest([]byte(base+"  runtime: wasm\n  capabilities:\n    mail: true\n"), "")
	if err != nil || !m.HasBackend() {
		t.Fatalf("带后端的清单应能解析：%v", err)
	}
}
