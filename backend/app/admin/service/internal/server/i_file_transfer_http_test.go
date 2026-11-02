package server

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"
)

// zeroReader 产生无限个 'A'，配合 LimitReader 构造超大请求体又不占内存。
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'A'
	}
	return len(p), nil
}

// countingReader 统计服务端实际读了多少字节。
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func newUploadTestServer() *kratosHttp.Server {
	srv := kratosHttp.NewServer()
	// svc 传 nil：超限请求必须在进入业务逻辑之前就被拒绝。
	registerFileTransferServiceHandler(srv, nil)
	installRequestBodyLimit(srv)
	return srv
}

func TestUploadRejectsOversizedContentLength(t *testing.T) {
	srv := newUploadTestServer()
	body := &countingReader{r: io.LimitReader(zeroReader{}, maxUploadRequestBytes+1)}
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/file/upload", body)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = maxUploadRequestBytes + 1
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %s", rec.Code, rec.Body.String())
	}
	if body.n != 0 {
		t.Fatalf("body should not be read when Content-Length exceeds limit, read %d bytes", body.n)
	}
}

// chunked（无 Content-Length）的超大 JSON 请求体：读到上限就停。
func TestUploadRejectsOversizedChunkedJSON(t *testing.T) {
	srv := newUploadTestServer()
	body := &countingReader{r: io.MultiReader(strings.NewReader(`{"file":"`), zeroReader{})}
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/file/upload", body)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %s", rec.Code, rec.Body.String())
	}
	if body.n > maxUploadRequestBytes+1<<20 {
		t.Fatalf("server read %d bytes, limit is %d", body.n, maxUploadRequestBytes)
	}
}

// chunked 的超大 multipart 请求体同样被拒绝。
func TestUploadRejectsOversizedChunkedMultipart(t *testing.T) {
	srv := newUploadTestServer()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		fw, _ := mw.CreateFormFile("file", "big.bin")
		_, err := io.Copy(fw, io.LimitReader(zeroReader{}, maxUploadRequestBytes+1<<20))
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	body := &countingReader{r: pr}
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/file/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.ContentLength = -1
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)
	_ = pr.CloseWithError(io.ErrClosedPipe)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %s", rec.Code, rec.Body.String())
	}
}
