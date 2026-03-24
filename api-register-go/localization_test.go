package main

import "testing"

func TestNormalizeLanguage(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{input: "", want: langEN},
		{input: "en", want: langEN},
		{input: "zh-TW", want: langZHTW},
		{input: "zh-Hant", want: langZHTW},
	}

	for _, tc := range cases {
		if got := normalizeLanguage(tc.input); got != tc.want {
			t.Fatalf("normalizeLanguage(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestLocalizeRuntimeTextEnglish(t *testing.T) {
	got := localizeRuntimeText(langEN, "  🚫 临时邮箱域名已写入本地数据库并标记为不可用: sharebot.net")
	if got != "  🚫 Temp-mail domain saved to the local database as unavailable: sharebot.net" {
		t.Fatalf("unexpected english localization: %q", got)
	}
}

func TestLocalizeRuntimeTextTraditionalChinese(t *testing.T) {
	got := localizeRuntimeText(langZHTW, "检查 IP 地理位置失败")
	if got != "檢查 IP 地理位置失敗" {
		t.Fatalf("unexpected zh-TW localization: %q", got)
	}
}
