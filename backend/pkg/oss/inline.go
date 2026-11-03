package oss

import "strings"

// safeInlineImageTypes 可以在同源免鉴权路由（头像、签名图片）里内联返回的图片类型。
// 只放位图：SVG 能带脚本，不在其中。BMP 一直是支持的上传类型，已有头像和富文本图片里有它。
var safeInlineImageTypes = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/gif":  {},
	"image/webp": {},
	"image/bmp":  {},
}

// IsSafeInlineImage 判断 Content-Type（可带参数）是否是可以内联返回的位图。
func IsSafeInlineImage(contentType string) bool {
	mt, _, _ := strings.Cut(contentType, ";")
	_, ok := safeInlineImageTypes[strings.ToLower(strings.TrimSpace(mt))]
	return ok
}
