package theme

import (
	"errors"
	"testing"
	"time"
)

func TestTouchesContent(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{`UPDATE "posts" AS "p" SET "title" = 'x' WHERE (id = 1)`, true},
		{`INSERT INTO "comments" ("id", "content") VALUES (DEFAULT, 'x')`, true},
		{`DELETE FROM menu_items WHERE menu_id = 3`, true},
		{`WITH gone AS (DELETE FROM post_tags WHERE post_id = 1) SELECT 1`, true},
		{`SELECT p.id, p.updated_at FROM posts AS p WHERE p.status = 'published'`, false},
		{`UPDATE sessions SET last_seen_at = now() WHERE id = 'x'`, false},
		{`INSERT INTO rate_limits (key, hits) VALUES ('k', 1) ON CONFLICT (key) DO UPDATE SET hits = rate_limits.hits + 1`, false},
		{`SELECT id FROM job_runs WHERE name = 'x' FOR UPDATE SKIP LOCKED`, false},
		{`INSERT INTO plugin_kv (plugin, key, value) VALUES ('p', 'k', '1')`, false},
		{`UPDATE "public"."posts" SET title = 'x'`, true},
	}
	for _, c := range cases {
		if got := touchesContent(c.query); got != c.want {
			t.Errorf("%s：得到 %v，应为 %v", c.query, got, c.want)
		}
	}
}

func TestSharedCacheInvalidation(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newSharedCache()
	c.now = func() time.Time { return now }
	calls := 0
	load := func() (string, error) { calls++; return "v", nil }

	for range 2 {
		if v, err := remember(c, "k", load); err != nil || v != "v" {
			t.Fatalf("取值：%q %v", v, err)
		}
	}
	if calls != 1 {
		t.Fatalf("第二次应命中缓存，查了 %d 次", calls)
	}

	c.invalidate()
	_, _ = remember(c, "k", load)
	_, _ = remember(c, "k", load)
	if calls != 3 {
		t.Fatalf("写入后的静默期内不该存缓存，应查 3 次，查了 %d 次", calls)
	}

	now = now.Add(sharedQuiet)
	_, _ = remember(c, "k", load)
	_, _ = remember(c, "k", load)
	if calls != 4 {
		t.Fatalf("静默期过后应重新缓存，应查 4 次，查了 %d 次", calls)
	}

	now = now.Add(sharedTTL)
	_, _ = remember(c, "k", load)
	if calls != 5 {
		t.Fatalf("过期后应重查，应查 5 次，查了 %d 次", calls)
	}

	// 查询期间发生写入，结果不存
	now = now.Add(sharedQuiet)
	_, _ = remember(c, "slow", func() (string, error) { c.invalidate(); now = now.Add(sharedQuiet); return "old", nil })
	if _, _, ok := c.get("slow"); ok {
		t.Fatal("查询期间有写入，结果不该存下")
	}

	// 出错不存
	if _, err := remember(c, "bad", func() (string, error) { return "", errors.New("坏了") }); err == nil {
		t.Fatal("错误应原样返回")
	}
	if _, _, ok := c.get("bad"); ok {
		t.Fatal("出错的结果不该存下")
	}
}
