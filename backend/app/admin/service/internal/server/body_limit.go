package server

import (
	"bytes"
	"errors"
	"io"
	nethttp "net/http"
	"strings"

	"github.com/go-kratos/kratos/v2/transport/http"

	"go-wind-admin/third_party/go-utils/ossutil"

	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"

	"go-wind-admin/app/admin/service/internal/service"
)

const (
	// maxUploadRequestBytes 文件上传接口的请求体上限：单文件 50 MiB，前端走 JSON + base64（膨胀 4/3），
	// 再留 4 MiB 给 multipart 边界和其他字段。
	maxUploadRequestBytes = ossutil.MaxUploadSize*4/3 + 4<<20

	// defaultRequestBodyBytes 其他所有接口的请求体上限。最大的普通 JSON 是头像上传（base64），
	// 按头像上限的 4/3 再加 1 MiB 余量。
	defaultRequestBodyBytes = service.MaxAvatarBytes*4/3 + 1<<20

	fileUploadPath = "/admin/v1/file/upload"
)

// installRequestBodyLimit 给 REST 服务包一层请求体上限，在 CORS、路由、Bind 和鉴权之前生效。
// Kratos 的 JSON 解码是 io.ReadAll，生成的 handler 又是先 Bind 后鉴权，
// 不在最外层限长的话，未登录请求也能让服务端读入任意大的请求体。
func installRequestBodyLimit(srv *http.Server) {
	srv.Handler = requestBodyLimit(srv.Handler)
}

func requestBodyLimitFor(r *nethttp.Request) int64 {
	if r.URL.Path == fileUploadPath {
		return maxUploadRequestBytes
	}
	return defaultRequestBodyBytes
}

func requestBodyLimit(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		limit := requestBodyLimitFor(r)
		switch {
		case r.ContentLength > limit:
			// 长度已知且超限：一个字节都不读，直接拒绝。
			writeBodyTooLarge(w, r, limit)
			return
		case r.ContentLength < 0 && r.Body != nil && r.Body != nethttp.NoBody:
			// 长度未知（chunked）：最多读 limit+1 字节判断是否超限，没超限就换成内存里的副本往下传，
			// 这样下游拿到的是确定长度的请求体，超限时统一返回 413。
			buf, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
			if err != nil {
				writeBodyReadError(w, r, err)
				return
			}
			if int64(len(buf)) > limit {
				writeBodyTooLarge(w, r, limit)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(buf))
			r.ContentLength = int64(len(buf))
		default:
			r.Body = nethttp.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

func writeBodyTooLarge(w nethttp.ResponseWriter, r *nethttp.Request, limit int64) {
	// 告诉客户端连接不再复用，剩余未读的请求体由 net/http 丢弃。
	w.Header().Set("Connection", "close")
	http.DefaultErrorEncoder(w, r, storageV1.ErrorFileTooLarge("request body exceeds %d bytes", limit))
}

func writeBodyReadError(w nethttp.ResponseWriter, r *nethttp.Request, err error) {
	http.DefaultErrorEncoder(w, r, storageV1.ErrorBadRequest("read request body: %s", err.Error()))
}

func isBodyTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var maxErr *nethttp.MaxBytesError
	// Kratos 的 JSON 解码把原始错误转成了字符串，只能按文案判断。
	return errors.As(err, &maxErr) || strings.Contains(err.Error(), "http: request body too large")
}
