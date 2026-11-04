//go:build windows

package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os/exec"

	"github.com/gogpu/systray"
)

// Run shows the tray icon and blocks on the platform message loop until the
// user chooses Quit (or the tray is removed).
func Run(opts Options) error {
	t := systray.New()

	menu := systray.NewMenu()
	menu.Add("Open status page", func() { openURL(opts.StatusURL) })
	menu.Add("Open data folder", func() { openPath(opts.DataDir) })
	menu.AddSeparator()
	menu.Add("Quit SML SignX", func() {
		if opts.Quit != nil {
			opts.Quit()
		}
		t.Remove()
	})

	t.SetIcon(defaultIcon()).
		SetTooltip(opts.Tooltip).
		SetMenu(menu)
	t.OnClick(func() { openURL(opts.StatusURL) })
	t.Show()

	return t.Run()
}

func openURL(url string) {
	if url == "" {
		return
	}
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func openPath(path string) {
	if path == "" {
		return
	}
	_ = exec.Command("explorer", path).Start()
}

func defaultIcon() []byte {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	bg := color.RGBA{R: 0x1D, G: 0x4E, B: 0xD8, A: 0xFF}
	fg := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if insideRoundedRect(x, y, size, size, 7) {
				img.Set(x, y, bg)
			}
		}
	}

	drawLine(img, 8, 17, 14, 23, fg)
	drawLine(img, 14, 23, 24, 9, fg)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

func insideRoundedRect(x, y, w, h, r int) bool {
	if x >= r && x < w-r {
		return true
	}
	if y >= r && y < h-r {
		return true
	}
	cx := r
	if x >= w-r {
		cx = w - r - 1
	}
	cy := r
	if y >= h-r {
		cy = h - r - 1
	}
	dx := x - cx
	dy := y - cy
	return dx*dx+dy*dy <= r*r
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		for oy := 0; oy < 2; oy++ {
			for ox := 0; ox < 2; ox++ {
				img.Set(x0+ox, y0+oy, c)
			}
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
