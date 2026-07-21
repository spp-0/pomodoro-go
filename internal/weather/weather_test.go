package weather

import "testing"

func TestDescribe(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{0, "晴"},
		{3, "阴"},
		{61, "雨"},
		{95, "雷阵雨"},
		{999, "未知"},
	}
	for _, c := range cases {
		emoji, text := describe(c.code)
		if text != c.want {
			t.Fatalf("code %d 期望文案 %q，实际 %q", c.code, c.want, text)
		}
		if emoji == "" {
			t.Fatalf("code %d emoji 不应为空", c.code)
		}
	}
}

func TestTempString(t *testing.T) {
	if TempString(25.4) != "25°C" {
		t.Fatalf("25.4 应格式化为 25°C，实际 %s", TempString(25.4))
	}
	if TempString(-2.6) != "-3°C" {
		t.Fatalf("-2.6 应格式化为 -3°C，实际 %s", TempString(-2.6))
	}
}
