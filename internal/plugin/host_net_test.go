package plugin

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

// 插件连不得本机与内网：只查域名挡不住解析到内网的公网域名，所以建连时还要核对 IP。
func TestPublicAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "1.1.1.1": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "::1": false,
		"fe80::1": false, "fd00::1": false, "::ffff:127.0.0.1": false, "224.0.0.1": false,
	} {
		if got := publicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("publicAddr(%s) = %v，应为 %v", addr, got, want)
		}
	}
}

func TestGuardedDialerRefusesLoopback(t *testing.T) {
	conn, err := guardedDialer.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	if err == nil {
		_ = conn.Close()
		t.Fatal("连本机地址应被拒绝")
	}
	if !strings.Contains(err.Error(), "内网或本机") {
		t.Fatalf("拒绝的原因应说清楚，得到 %v", err)
	}
}

func TestCheckFetchURL(t *testing.T) {
	granted := &Capabilities{HTTP: []string{"api.example.com", "*.cdn.example.org"}}
	for raw, ok := range map[string]bool{
		"https://api.example.com/v1":         true,
		"http://img.cdn.example.org/a.png":   true,
		"https://evil.example.com/":          false,
		"https://api.example.com.evil.net/":  false,
		"ftp://api.example.com/":             false,
		"https://user:pass@api.example.com/": false,
		"/relative":                          false,
	} {
		if _, err := checkFetchURL(granted, raw); (err == nil) != ok {
			t.Errorf("checkFetchURL(%q) 的错误是 %v，应%v", raw, err, map[bool]string{true: "放行", false: "拒绝"}[ok])
		}
	}
}

func TestNormalizeCron(t *testing.T) {
	caps := &Capabilities{Cron: true}
	ok := []CronJob{{Name: "rollup", Every: "1h"}}
	if err := normalizeCron(ok, caps, true); err != nil || ok[0].Interval().Hours() != 1 {
		t.Fatalf("合法的定时任务被拒：%v", err)
	}
	cases := map[string]struct {
		jobs    []CronJob
		caps    *Capabilities
		backend bool
	}{
		"没声明能力":  {[]CronJob{{Name: "a", Every: "1h"}}, &Capabilities{}, true},
		"没有后端":   {[]CronJob{{Name: "a", Every: "1h"}}, caps, false},
		"间隔太短":   {[]CronJob{{Name: "a", Every: "10s"}}, caps, true},
		"间隔写错":   {[]CronJob{{Name: "a", Every: "每小时"}}, caps, true},
		"名字重复":   {[]CronJob{{Name: "a", Every: "1h"}, {Name: "a", Every: "2h"}}, caps, true},
		"名字不合规则": {[]CronJob{{Name: "Roll Up", Every: "1h"}}, caps, true},
	}
	for name, tc := range cases {
		if err := normalizeCron(tc.jobs, tc.caps, tc.backend); err == nil {
			t.Errorf("%s：应被拒绝", name)
		}
	}
}
