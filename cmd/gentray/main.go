package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

// 生成三张 32x32 PNG 托盘图标（红/黄/灰），用不同主题色表示三种状态。
// 输出到 cmd/pomodoro-agent/assets/tray_*.png

const size = 32

type palette struct {
	body      [3]uint8 // 番茄主色
	bodyDark  [3]uint8 // 边缘
	leaf      [3]uint8
	highlight [3]uint8
}

var themes = map[string]palette{
	"work":  {body: [3]uint8{232, 70, 70}, bodyDark: [3]uint8{150, 30, 30}, leaf: [3]uint8{72, 168, 96}, highlight: [3]uint8{255, 220, 220}},
	"break": {body: [3]uint8{245, 188, 66}, bodyDark: [3]uint8{180, 130, 30}, leaf: [3]uint8{120, 200, 130}, highlight: [3]uint8{255, 240, 200}},
	"pause": {body: [3]uint8{160, 160, 160}, bodyDark: [3]uint8{90, 90, 90}, leaf: [3]uint8{150, 150, 150}, highlight: [3]uint8{230, 230, 230}},
}

func drawTheme(p palette) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, color.NRGBA{0, 0, 0, 0})
		}
	}
	cx, cy, r := 16.0, 18.0, 11.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - cx
			dy := float64(y) - cy
			d2 := dx*dx + dy*dy
			if d2 <= r*r {
				t := (dx*dx + dy*dy) / (r * r)
				cr := uint8(float64(p.body[0])*(1-t) + float64(p.bodyDark[0])*t)
				cg := uint8(float64(p.body[1])*(1-t) + float64(p.bodyDark[1])*t)
				cb := uint8(float64(p.body[2])*(1-t) + float64(p.bodyDark[2])*t)
				img.Set(x, y, color.NRGBA{cr, cg, cb, 255})
			}
		}
	}
	// 叶子
	for x := 11; x <= 20; x++ {
		for y := 4; y <= 9; y++ {
			if y <= 9-(x-15)*(x-15)/8 {
				img.Set(x, y, color.NRGBA{p.leaf[0], p.leaf[1], p.leaf[2], 255})
			}
		}
	}
	img.Set(15, 6, color.NRGBA{p.leaf[0], p.leaf[1], p.leaf[2], 255})
	img.Set(16, 5, color.NRGBA{p.leaf[0], p.leaf[1], p.leaf[2], 255})
	// 高光
	img.Set(11, 14, color.NRGBA{p.highlight[0], p.highlight[1], p.highlight[2], 200})
	img.Set(10, 15, color.NRGBA{p.highlight[0], p.highlight[1], p.highlight[2], 160})
	return img
}

func main() {
	files := map[string]palette{
		"tray_work.png":  themes["work"],
		"tray_break.png": themes["break"],
		"tray_pause.png": themes["pause"],
	}
	for name, p := range files {
		f, err := os.Create("cmd/pomodoro-agent/assets/" + name)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := png.Encode(f, drawTheme(p)); err != nil {
			panic(err)
		}
	}
	// 兼容旧名 tray.png = work 状态
	f, _ := os.Create("cmd/pomodoro-agent/assets/tray.png")
	defer f.Close()
	_ = png.Encode(f, drawTheme(themes["work"]))
}
