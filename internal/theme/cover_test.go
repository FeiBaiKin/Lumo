package theme

import "testing"

// 特色图来自第三方主题包：格式只看文件头，改个扩展名骗不过去。
func TestCoverTypeTrustsSignatureOnly(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"PNG", append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "rest"...), "image/png"},
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10}, "image/jpeg"},
		{"WebP", []byte("RIFF\x10\x00\x00\x00WEBPVP8 "), "image/webp"},
		{"改了扩展名的 HTML", []byte("<script>alert(1)</script>"), ""},
		{"其他 RIFF 容器", []byte("RIFF\x10\x00\x00\x00WAVEfmt "), ""},
		{"半截 WebP 头", []byte("RIFF\x10\x00"), ""},
		{"空文件", nil, ""},
	}
	for _, tc := range cases {
		if got := coverType(tc.data); got != tc.want {
			t.Errorf("%s：coverType = %q，应为 %q", tc.name, got, tc.want)
		}
	}
}
