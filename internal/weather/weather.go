package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

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
	lat, lon, err := geocode(ctx, city)
	if err != nil {
		return Weather{}, err
	}
	return fetchCurrent(ctx, lat, lon, city)
}

func geocode(ctx context.Context, city string) (float64, float64, error) {
	api := "https://geocoding-api.open-meteo.com/v1/search?count=1&language=zh&format=json&name=" + url.QueryEscape(city)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("geocode status %d", resp.StatusCode)
	}
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return Weather{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Weather{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Weather{}, fmt.Errorf("forecast status %d", resp.StatusCode)
	}
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
