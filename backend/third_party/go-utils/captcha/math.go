package captcha

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand/v2"
	"strconv"
	"sync"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/mojocn/base64Captcha"
	"golang.org/x/image/font"
)

// mathFont 算术验证码统一用无衬线的文泉驿微米黑：上游 DriverMath 每个字符随机挑花体字，
// 还会在背景铺满数字噪点，人眼很难分辨，这里只要清楚可读。
// mathFont uses a single plain font for legibility.
var (
	mathFontOnce sync.Once
	mathFont     *truetype.Font
)

func loadMathFont() *truetype.Font {
	mathFontOnce.Do(func() {
		mathFont = base64Captcha.DefaultEmbeddedFonts.LoadFontByName("fonts/wqy-microhei.ttc")
	})
	return mathFont
}

// randomMathQuestion 生成一道加减乘除题，结果均为非负整数。
// randomMathQuestion returns an arithmetic question with a non-negative integer answer.
func randomMathQuestion() (question string, answer int) {
	switch rand.IntN(4) {
	case 0: // 加法：1~20 + 1~20
		a, b := rand.IntN(20)+1, rand.IntN(20)+1
		return fmt.Sprintf("%d + %d = ?", a, b), a + b
	case 1: // 减法：被减数 2~20，结果不为负
		a := rand.IntN(19) + 2
		b := rand.IntN(a) + 1
		return fmt.Sprintf("%d - %d = ?", a, b), a - b
	case 2: // 乘法：2~9 × 2~9
		a, b := rand.IntN(8)+2, rand.IntN(8)+2
		return fmt.Sprintf("%d × %d = ?", a, b), a * b
	default: // 除法：整除，除数 2~9，商 1~9
		b, q := rand.IntN(8)+2, rand.IntN(9)+1
		return fmt.Sprintf("%d ÷ %d = ?", b*q, b), q
	}
}

// generateMath 生成算术验证码：返回 id、PNG data URI、答案。
// generateMath generates an arithmetic captcha.
func (c *Captcha) generateMath() (id string, b64s string, answer string, err error) {
	mathCfg := c.config.MathConfig
	if mathCfg == nil {
		mathCfg = DefaultMathConfig()
	}

	question, result := randomMathQuestion()
	b64s, err = drawMathImage(question, mathCfg.Width, mathCfg.Height)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to draw captcha: %w", err)
	}
	return base64Captcha.RandomId(), b64s, strconv.Itoa(result), nil
}

// drawMathImage 绘制算术题：浅色底、几条淡色干扰线、居中深色文字（每个字符轻微上下抖动）。
// drawMathImage renders the question on a light background with a few faint noise lines.
func drawMathImage(question string, width, height int) (string, error) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 248, G: 249, B: 250, A: 255}}, image.Point{}, draw.Src)

	// 干扰线：颜色浅、数量少，不遮挡文字
	for i := 0; i < 4; i++ {
		drawLine(img,
			rand.IntN(width), rand.IntN(height),
			rand.IntN(width), rand.IntN(height),
			color.RGBA{R: uint8(180 + rand.IntN(50)), G: uint8(180 + rand.IntN(50)), B: uint8(180 + rand.IntN(50)), A: 255},
		)
	}

	f := loadMathFont()
	fontSize := float64(height) * 0.55
	face := truetype.NewFace(f, &truetype.Options{Size: fontSize, DPI: 72})
	defer face.Close()

	textWidth := font.MeasureString(face, question).Round()
	x := (width - textWidth) / 2
	if x < 0 {
		x = 0
	}
	baseline := (height + face.Metrics().Ascent.Round() - face.Metrics().Descent.Round()) / 2

	ctx := freetype.NewContext()
	ctx.SetDPI(72)
	ctx.SetFont(f)
	ctx.SetFontSize(fontSize)
	ctx.SetClip(img.Bounds())
	ctx.SetDst(img)
	ctx.SetHinting(font.HintingFull)

	for _, r := range question {
		ch := string(r)
		ctx.SetSrc(image.NewUniform(base64Captcha.RandDeepColor()))
		y := baseline + rand.IntN(7) - 3
		if _, err := ctx.DrawString(ch, freetype.Pt(x, y)); err != nil {
			return "", err
		}
		x += font.MeasureString(face, ch).Round()
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// drawLine 用 Bresenham 画 1px 直线。
// drawLine draws a 1px line with Bresenham's algorithm.
func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	e := dx + dy
	for {
		img.Set(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
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
