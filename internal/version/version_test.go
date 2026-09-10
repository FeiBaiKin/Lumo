package version

import (
	"strings"
	"testing"
)

func TestGetFillsRuntimeFields(t *testing.T) {
	t.Parallel()

	info := Get()
	if info.Version == "" {
		t.Error("Version 不应为空")
	}
	if info.GoVersion == "" {
		t.Error("GoVersion 不应为空")
	}
	if !strings.Contains(info.Platform, "/") {
		t.Errorf("Platform 应形如 os/arch，实际 = %q", info.Platform)
	}
}

func TestInfoString(t *testing.T) {
	t.Parallel()

	info := Info{
		Version:   "1.2.3",
		Commit:    "abc1234",
		Date:      "2026-01-01T00:00:00Z",
		GoVersion: "go1.26.4",
		Platform:  "linux/amd64",
	}

	got := info.String()
	for _, want := range []string{"lumo", "1.2.3", "abc1234", "linux/amd64"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q，缺少 %q", got, want)
		}
	}
}
