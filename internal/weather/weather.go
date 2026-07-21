package weather

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// httpClient 强制使用 HTTP/1.1。部分网络环境下 HTTP/2 握手会挂起，
// 导致 open-meteo 请求长时间不返回；实测 HTTP/1.1 耗时约为 HTTP/2 的一半且更稳定。
// 每个请求单独 5s 超时，避免单请求无限挂起。
var httpClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		TLSNextProto: make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
	},
}

// getURL 发起 GET 请求，对网络/超时类错误重试一次（open-meteo 在部分网络下偶发抖动）。
// 返回成功（2xx）的 *http.Response，调用方负责关闭 Body。
func getURL(ctx context.Context, urlStr string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if err != nil {
			return nil, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, urlStr)
		}
		return resp, nil
	}
	return nil, lastErr
}

// Weather 表示某城市当前天气。
type Weather struct {
	City        string
	Temperature float64
	Code        int    // WMO weather code
	Text        string // 中文天气文案
	Emoji       string // 对应 emoji
	IsDay       int
}

// Fetch 通过 Open-Meteo（无需 API key）获取指定城市的当前天气。
// 流程：先用城市名地理编码得到经纬度，再拉取当前天气。
// 任何一步失败都返回 error，由调用方决定兜底（通常静默忽略天气）。
func Fetch(ctx context.Context, city string) (Weather, error) {
	city = strings.TrimSpace(city)
	if city == "" {
		return Weather{}, fmt.Errorf("city is empty")
	}
	// 地理编码 + 当前天气是两次串行网络请求，且各自内部还有一次重试
	// （每次请求 5s 客户端超时）。给整次抓取一个充足的总超时（12s），
	// 同时尊重调用方 ctx 的取消。注意：不要把调用方那个偏短的 context
	// 直接作为硬上限，否则前一步请求消耗掉预算后，后一步必然超时。
	fetchCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	lat, lon, err := geocode(fetchCtx, city)
	if err != nil {
		return Weather{}, err
	}
	return fetchCurrent(fetchCtx, lat, lon, city)
}

func geocode(ctx context.Context, city string) (float64, float64, error) {
	api := "https://geocoding-api.open-meteo.com/v1/search?count=1&language=zh&format=json&name=" + url.QueryEscape(city)
	resp, err := getURL(ctx, api)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	var g struct {
		Results []struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		return 0, 0, err
	}
	if len(g.Results) == 0 {
		return 0, 0, fmt.Errorf("city not found: %s", city)
	}
	return g.Results[0].Latitude, g.Results[0].Longitude, nil
}

func fetchCurrent(ctx context.Context, lat, lon float64, city string) (Weather, error) {
	api := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current_weather=true", lat, lon)
	resp, err := getURL(ctx, api)
	if err != nil {
		return Weather{}, err
	}
	defer resp.Body.Close()
	var f struct {
		CurrentWeather struct {
			Temperature  float64 `json:"temperature"`
			WeatherCode  int     `json:"weathercode"`
			WindSpeed    float64 `json:"windspeed"`
			WindDirection int    `json:"winddirection"`
			IsDay        int     `json:"is_day"`
		} `json:"current_weather"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return Weather{}, err
	}
	emoji, text := describe(f.CurrentWeather.WeatherCode)
	return Weather{
		City:        city,
		Temperature: f.CurrentWeather.Temperature,
		Code:        f.CurrentWeather.WeatherCode,
		Text:        text,
		Emoji:       emoji,
		IsDay:       f.CurrentWeather.IsDay,
	}, nil
}

// describe 把 WMO weather code 映射为 emoji + 中文文案。
func describe(code int) (string, string) {
	switch code {
	case 0:
		return "☀️", "晴"
	case 1:
		return "🌤️", "晴间多云"
	case 2:
		return "⛅", "局部多云"
	case 3:
		return "☁️", "阴"
	case 45, 48:
		return "🌫️", "雾"
	case 51, 53, 55:
		return "🌦️", "毛毛雨"
	case 56, 57:
		return "🌧️", "冻毛毛雨"
	case 61, 63, 65:
		return "🌧️", "雨"
	case 66, 67:
		return "🌧️", "冻雨"
	case 71, 73, 75:
		return "🌨️", "雪"
	case 77:
		return "🌨️", "雪粒"
	case 80, 81, 82:
		return "🌦️", "阵雨"
	case 85, 86:
		return "🌨️", "阵雪"
	case 95:
		return "⛈️", "雷阵雨"
	case 96, 99:
		return "⛈️", "雷阵雨伴冰雹"
	default:
		return "🌡️", "未知"
	}
}

// TempString 把温度格式化为整数度的字符串（如 "26°C"）。
func TempString(t float64) string {
	return strconv.Itoa(int(math.Round(t))) + "°C"
}
