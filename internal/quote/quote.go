package quote

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"strings"
)

// Quote 是一条要展示在弹窗里的语句。
type Quote struct {
	Text   string
	Author string
	Source string
}

type hitokotoResp struct {
	Hitokoto string `json:"hitokoto"`
	From     string `json:"from"`
	FromWho  string `json:"from_who"`
}

// Fetch 从 url 拉一条语句（默认接口：https://v1.hitokoto.cn/?encode=json）。
func Fetch(ctx context.Context, url string) (Quote, error) {
	var q Quote
	if strings.TrimSpace(url) == "" {
		return q, errors.New("empty url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return q, err
	}
	req.Header.Set("User-Agent", "pomodoro-notifier/1.0")

	client := &http.Client{} // 超时由传入的 ctx 控制（config.QuoteTimeout）
	resp, err := client.Do(req)
	if err != nil {
		return q, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return q, errors.New("quote api non-2xx")
	}
	var r hitokotoResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return q, err
	}
	text := strings.TrimSpace(r.Hitokoto)
	if text == "" {
		return q, errors.New("empty quote")
	}
	q.Text = text
	q.Author = strings.TrimSpace(r.FromWho)
	q.Source = strings.TrimSpace(r.From)
	return q, nil
}

// Fallback 返回一句兜底中文诗词/格言。
func Fallback() Quote {
	list := []Quote{
		{Text: "须知少时凌云志，曾许人间第一流。", Author: "", Source: "《上李邕》· 李白"},
		{Text: "千磨万击还坚劲，任尔东西南北风。", Author: "", Source: "《竹石》· 郑燮"},
		{Text: "长风破浪会有时，直挂云帆济沧海。", Author: "", Source: "《行路难》· 李白"},
		{Text: "纸上得来终觉浅，绝知此事要躬行。", Author: "", Source: "《冬夜读书示子聿》· 陆游"},
		{Text: "不畏浮云遮望眼，自缘身在最高层。", Author: "", Source: "《登飞来峰》· 王安石"},
		{Text: "沉舟侧畔千帆过，病树前头万木春。", Author: "", Source: "《酬乐天扬州初逢席上见赠》· 刘禹锡"},
		{Text: "博观而约取，厚积而薄发。", Author: "", Source: "《稼说送张琥》· 苏轼"},
		{Text: "路漫漫其修远兮，吾将上下而求索。", Author: "屈原", Source: "《离骚》"},
		{Text: "莫等闲、白了少年头，空悲切。", Author: "岳飞", Source: "《满江红》"},
		{Text: "盛年不重来，一日难再晨。及时当勉励，岁月不待人。", Author: "陶渊明", Source: "《杂诗》"},
		{Text: "凡是过往，皆为序章。", Author: "莎士比亚", Source: ""},
		{Text: "种一棵树最好的时间是十年前，其次是现在。", Author: "", Source: ""},
		{Text: "Stay hungry, stay foolish.", Author: "Steve Jobs", Source: ""},
		{Text: "We must let go of the life we have planned, so as to accept the one that is waiting for us.", Author: "Joseph Campbell", Source: ""},
	}
	return list[rand.Intn(len(list))]
}
