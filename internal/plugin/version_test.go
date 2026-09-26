package plugin

import "testing"

func TestParseAndCompareVersions(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1", "1.0.0"},
		{"1.2", "1.2.0"},
		{"1.2.3", "1.2.3"},
		{"0.1.9", "0.1.9"},
		{"2.0.0-rc.1", "2.0.0-rc.1"},
		{" 1.2.3 ", "1.2.3"},
	}
	for _, c := range cases {
		v, err := parseVersion(c.in)
		if err != nil {
			t.Errorf("parseVersion(%q) 出错：%v", c.in, err)
			continue
		}
		if got := v.String(); got != c.want {
			t.Errorf("parseVersion(%q) = %s，想要 %s", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "v1.0.0", "1.2.3.4", "1.x", "01.2", "-beta", "1.2.3-"} {
		if _, err := parseVersion(bad); err == nil {
			t.Errorf("parseVersion(%q) 本该报错", bad)
		}
	}
}

func TestVersionOrder(t *testing.T) {
	// 每对都是「小 < 大」
	pairs := [][2]string{
		{"0.9.9", "1.0.0"},
		{"1.0.0", "1.0.1"},
		{"1.0.1", "1.1.0"},
		{"1.9.0", "2.0.0"},
		{"1.0.0-alpha", "1.0.0"},
		{"1.0.0-alpha", "1.0.0-beta"},
		{"1.0.0-alpha.1", "1.0.0-alpha.2"},
		{"1.0.0-alpha.9", "1.0.0-alpha.10"},
		{"1.0.0-1", "1.0.0-alpha"},
		{"1.0.0-beta.2", "1.0.0-beta.11"},
	}
	for _, p := range pairs {
		lo, err := parseVersion(p[0])
		if err != nil {
			t.Fatalf("parseVersion(%q)：%v", p[0], err)
		}
		hi, err := parseVersion(p[1])
		if err != nil {
			t.Fatalf("parseVersion(%q)：%v", p[1], err)
		}
		if lo.compare(hi) != -1 {
			t.Errorf("%s 本该小于 %s", p[0], p[1])
		}
		if hi.compare(lo) != 1 {
			t.Errorf("%s 本该大于 %s", p[1], p[0])
		}
		if lo.compare(lo) != 0 {
			t.Errorf("%s 与自身比较本该相等", p[0])
		}
	}
}

func TestRangeMatches(t *testing.T) {
	cases := []struct {
		rang string
		ver  string
		want bool
	}{
		{"", "1.0.0", true},
		{"*", "9.9.9", true},
		{">=1.0.0", "1.0.0", true},
		{">=1.0.0", "0.9.9", false},
		{">1.0.0", "1.0.0", false},
		{">1.0.0", "1.0.1", true},
		{"<2.0.0", "1.9.9", true},
		{"<=2.0.0", "2.0.0", true},
		{"=1.2.3", "1.2.3", true},
		{"1.2.3", "1.2.3", true},
		{"1.2.3", "1.2.4", false},
		{"1.2", "1.2.9", true},
		{"1.2", "1.3.0", false},
		{"1.x", "1.9.9", true},
		{"1.x", "2.0.0", false},
		{"1.2.*", "1.2.7", true},
		{"1.2.*", "1.3.0", false},
		{"^1.2.3", "1.9.9", true},
		{"^1.2.3", "2.0.0", false},
		{"^1.2.3", "1.2.2", false},
		{"^0.2.3", "0.2.9", true},
		{"^0.2.3", "0.3.0", false},
		{"^0.0.3", "0.0.3", true},
		{"^0.0.3", "0.0.4", false},
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{"~1.2", "1.2.9", true},
		{"~1.2", "1.3.0", false},
		{"~1", "1.9.9", true},
		{"~1", "2.0.0", false},
		{">=1.0.0 <2.0.0", "1.5.0", true},
		{">=1.0.0 <2.0.0", "2.0.0", false},
		{">=1.0.0, <2.0.0", "1.5.0", true},
		{">=1.0.0 <2.0.0 || >=3.0.0", "3.1.0", true},
		{">=1.0.0 <2.0.0 || >=3.0.0", "2.5.0", false},
		// 预发布按普通大小比较：^1.0.0 不含 1.0.0-beta
		{"^1.0.0", "1.0.0-beta", false},
		{">=1.0.0", "1.0.0-beta", false},
		{"^2.0.0-rc.1", "2.0.0-rc.2", true},
		{"^2.0.0-rc.1", "2.0.0", true},
		// * 混在比较符里不约束任何东西，不该变成「永不匹配」
		{">=1.0.0 *", "1.5.0", true},
		{"* <2.0.0", "1.5.0", true},
		{"* <2.0.0", "2.0.0", false},
	}
	for _, c := range cases {
		r, err := parseRange(c.rang)
		if err != nil {
			t.Errorf("parseRange(%q) 出错：%v", c.rang, err)
			continue
		}
		v, err := parseVersion(c.ver)
		if err != nil {
			t.Fatalf("parseVersion(%q)：%v", c.ver, err)
		}
		if got := r.matches(v); got != c.want {
			t.Errorf("%q 匹配 %s = %v，想要 %v", c.rang, c.ver, got, c.want)
		}
	}
}

func TestRangeRejectsBadSyntax(t *testing.T) {
	for _, bad := range []string{
		"1.2.3.4", ">=", "1.*.3", "^*", "abc", "1.2.3-", ">=1.0.0 <", "*.*.3",
	} {
		if _, err := parseRange(bad); err == nil {
			t.Errorf("parseRange(%q) 本该报错", bad)
		}
	}
}
