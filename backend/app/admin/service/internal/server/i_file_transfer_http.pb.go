package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	nethttp "net/http"
	"strconv"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport/http"
	"go-wind-admin/third_party/go-utils/ossutil"
	"go-wind-admin/third_party/go-utils/trans"

	"github.com/go-kratos/kratos/v2/encoding"
	_ "github.com/go-kratos/kratos/v2/encoding/json"

	"go-wind-admin/app/admin/service/internal/service"

	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"
)

var codec = encoding.GetCodec("json")

// maxUploadRequestBytes 上传请求体上限：单文件上限 50 MiB，前端走 JSON + base64（膨胀 4/3），
// 再留 4 MiB 给 multipart 边界和其他字段。
const maxUploadRequestBytes = ossutil.MaxUploadSize*4/3 + 4<<20

// limitUploadBody 在解析请求体之前限制大小：Content-Length 超限直接拒绝，
// 其余情况（含 chunked）用 MaxBytesReader 兜住，读到上限就报错，不会先把整个请求体读进内存或临时文件。
func limitUploadBody(ctx http.Context) error {
	r := ctx.Request()
	if r.ContentLength > maxUploadRequestBytes {
		return uploadTooLargeError()
	}
	r.Body = nethttp.MaxBytesReader(ctx.Response(), r.Body, maxUploadRequestBytes)
	return nil
}

func isBodyTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var maxErr *nethttp.MaxBytesError
	// Kratos 的 JSON 解码把原始错误转成了字符串，只能按文案判断。
	return errors.As(err, &maxErr) || strings.Contains(err.Error(), "http: request body too large")
}

func uploadTooLargeError() error {
	return storageV1.ErrorFileTooLarge("request body exceeds %d bytes", maxUploadRequestBytes)
}

func registerFileTransferServiceHandler(srv *http.Server, svc *service.FileTransferService) {
	r := srv.Route("/")

	r.POST("admin/v1/file/upload", _FileTransferService_PostUploadFile_HTTP_Handler(svc))
	r.PUT("admin/v1/file/upload", _FileTransferService_PutUploadFile_HTTP_Handler(svc))

	r.GET("admin/v1/file/download", _FileTransferService_DownloadFile_HTTP_Handler(svc))

	// 签名图片代理（免鉴权：签名即凭证）。手动注册的路由未设置 Operation，
	// selector 按 Operation 匹配的 auth 中间件天然不应用；安全性由
	// HMAC 签名 + 过期时间在 handler 内校验保证。
	r.GET("admin/v1/file/image", func(ctx http.Context) error {
		return svc.ServeImageHandler(ctx.Response(), ctx.Request())
	})
}

const OperationFileTransferServicePostUploadFile = "/admin.service.v1.FileTransferService/PostUploadFile"
const OperationFileTransferServicePutUploadFile = "/admin.service.v1.FileTransferService/PutUploadFile"

const OperationFileTransferServiceDownloadFile = "/admin.service.v1.FileTransferService/DownloadFile"

func _FileTransferService_PostUploadFile_HTTP_Handler(svc *service.FileTransferService) func(ctx http.Context) error {
	return func(ctx http.Context) error {
		http.SetOperation(ctx, OperationFileTransferServicePostUploadFile)

		var in storageV1.UploadFileRequest
		var err error

		if err = limitUploadBody(ctx); err != nil {
			return err
		}

		file, header, err := ctx.Request().FormFile("file")
		if isBodyTooLarge(err) {
			return uploadTooLargeError()
		}
		if err == nil {
			defer file.Close()

			b := new(strings.Builder)
			if _, err = io.Copy(b, file); err != nil {
				log.Errorf("Read upload file error: %v", err)
				if isBodyTooLarge(err) {
					return uploadTooLargeError()
				}
				return err
			}

			in.Source = &storageV1.UploadFileRequest_File{File: []byte(b.String())}

			sourceFileName := ctx.Request().FormValue("sourceFileName")
			if sourceFileName != "" {
				in.SourceFileName = trans.Ptr(sourceFileName)
			} else {
				in.SourceFileName = trans.Ptr(header.Filename)
			}
			//log.Debugf("Upload file sourceFileName: %s", sourceFileName)

			mime := ctx.Request().FormValue("mime")
			if mime != "" {
				in.Mime = trans.Ptr(mime)
			} else {
				in.Mime = trans.Ptr(header.Header.Get("Content-Type"))
			}
			//log.Debugf("Upload file mime: %s", mime)

			size := ctx.Request().FormValue("size")
			if size != "" {
				var n int64
				n, err = strconv.ParseInt(size, 10, 64)
				if err == nil {
					in.Size = trans.Ptr(n)
				}
			} else {
				in.Size = trans.Ptr(header.Size)
			}
			//log.Debugf("Upload file size: %s", size)

			storageObject := ctx.Request().FormValue("storageObject")
			if storageObject != "" {
				in.StorageObject = &storageV1.StorageObject{}
				if err = codec.Unmarshal([]byte(storageObject), in.StorageObject); err != nil {
					log.Errorf("Unmarshal upload file storageObject error: %v", err)
					return err
				}
			}
			//log.Debugf("Upload file storageObject: %s", storageObject)
		} else {
			if err = ctx.Bind(&in); err != nil {
				log.Errorf("Bind upload file request error: %v", err)
				if isBodyTooLarge(err) {
					return uploadTooLargeError()
				}
				return err
			}
		}

		h := ctx.Middleware(func(ctx context.Context, req interface{}) (interface{}, error) {
			aReq := req.(*storageV1.UploadFileRequest)

			var resp *storageV1.UploadFileResponse
			resp, err = svc.UploadFile(ctx, aReq)
			in.Source = nil

			return resp, err
		})

		// 逻辑处理，取数据
		out, err := h(ctx, &in)
		if err != nil {
			return err
		}

		reply := out.(*storageV1.UploadFileResponse)

		return ctx.Result(200, reply)
	}
}

func _FileTransferService_PutUploadFile_HTTP_Handler(svc *service.FileTransferService) func(ctx http.Context) error {
	return func(ctx http.Context) error {
		http.SetOperation(ctx, OperationFileTransferServicePutUploadFile)

		var in storageV1.UploadFileRequest
		var err error

		if err = limitUploadBody(ctx); err != nil {
			return err
		}

		file, header, err := ctx.Request().FormFile("file")
		if isBodyTooLarge(err) {
			return uploadTooLargeError()
		}
		if err == nil {
			defer file.Close()

			b := new(strings.Builder)
			if _, err = io.Copy(b, file); err != nil {
				log.Errorf("Read upload file error: %v", err)
				if isBodyTooLarge(err) {
					return uploadTooLargeError()
				}
				return err
			}

			in.Source = &storageV1.UploadFileRequest_File{File: []byte(b.String())}

			sourceFileName := ctx.Request().FormValue("sourceFileName")
			if sourceFileName != "" {
				in.SourceFileName = trans.Ptr(sourceFileName)
			} else {
				in.SourceFileName = trans.Ptr(header.Filename)
			}
			//log.Debugf("Upload file sourceFileName: %s", sourceFileName)

			mime := ctx.Request().FormValue("mime")
			if mime != "" {
				in.Mime = trans.Ptr(mime)
			} else {
				in.Mime = trans.Ptr(header.Header.Get("Content-Type"))
			}
			//log.Debugf("Upload file mime: %s", mime)

			size := ctx.Request().FormValue("size")
			if size != "" {
				var n int64
				n, err = strconv.ParseInt(size, 10, 64)
				if err == nil {
					in.Size = trans.Ptr(n)
				}
			} else {
				in.Size = trans.Ptr(header.Size)
			}
			//log.Debugf("Upload file size: %s", size)

			storageObject := ctx.Request().FormValue("storageObject")
			if storageObject != "" {
				in.StorageObject = &storageV1.StorageObject{}
				if err = codec.Unmarshal([]byte(storageObject), in.StorageObject); err != nil {
					log.Errorf("Unmarshal upload file storageObject error: %v", err)
					return err
				}
			}
			//log.Debugf("Upload file storageObject: %s", storageObject)
		} else {
			if err = ctx.Bind(&in); err != nil {
				log.Errorf("Bind upload file request error: %v", err)
				if isBodyTooLarge(err) {
					return uploadTooLargeError()
				}
				return err
			}
		}

		h := ctx.Middleware(func(ctx context.Context, req interface{}) (interface{}, error) {
			aReq := req.(*storageV1.UploadFileRequest)

			var resp *storageV1.UploadFileResponse
			resp, err = svc.UploadFile(ctx, aReq)
			in.Source = nil

			return resp, err
		})

		// 逻辑处理，取数据
		out, err := h(ctx, &in)
		if err != nil {
			return err
		}

		reply := out.(*storageV1.UploadFileResponse)

		return ctx.Result(200, reply)
	}
}

func _FileTransferService_DownloadFile_HTTP_Handler(svc *service.FileTransferService) func(ctx http.Context) error {
	return func(ctx http.Context) error {
		http.SetOperation(ctx, OperationFileTransferServiceDownloadFile)

		var in storageV1.DownloadFileRequest
		var err error

		if err = ctx.BindQuery(&in); err != nil {
			return err
		}

		h := ctx.Middleware(func(ctx context.Context, req interface{}) (interface{}, error) {
			aReq := req.(*storageV1.DownloadFileRequest)
			var resp *storageV1.DownloadFileResponse
			resp, err = svc.DownloadFile(ctx, aReq)
			return resp, err
		})

		// 逻辑处理，取数据
		out, err := h(ctx, &in)
		if err != nil {
			return err
		}

		reply := out.(*storageV1.DownloadFileResponse)
		rw := ctx.Response()
		if rw == nil {
			return ctx.Result(500, "response writer not available")
		}

		data := reply.GetFile()
		if len(data) == 0 {
			// 若没有文件字节，交由框架默认处理（保持原行为）
			return ctx.Result(200, reply)
		}

		// 基本头部
		mime := reply.GetMime()
		if mime == "" {
			mime = "application/octet-stream"
		}
		rw.Header().Set("Content-Type", mime)

		filename := reply.GetSourceFileName()
		if filename == "" {
			filename = "file"
		}

		var disposition string
		if in.GetDisposition() != "" {
			disposition = in.GetDisposition()
		} else {
			disposition = "attachment; filename=\"" + filename + "\""
		}
		rw.Header().Set("Content-Disposition", disposition)
		rw.Header().Set("Accept-Ranges", "bytes")

		// 使用 bytes.Reader 以便支持高效的部分读取/流式写入
		reader := bytes.NewReader(data)
		total := int64(len(data))

		// 检查请求是否带 Range 头（仅支持单区间，格式 bytes=start-end 或 bytes=start-）
		rangeHeader := ctx.Request().Header.Get("Range")
		if strings.HasPrefix(rangeHeader, "bytes=") {
			r := strings.TrimPrefix(rangeHeader, "bytes=")
			parts := strings.SplitN(r, "-", 2)
			if len(parts) != 2 {
				return ctx.Result(400, "invalid Range header")
			}

			start, err1 := strconv.ParseInt(parts[0], 10, 64)
			if err1 != nil || start < 0 {
				return ctx.Result(400, "invalid Range start")
			}

			var end int64
			if parts[1] == "" {
				end = total - 1
			} else {
				end, err1 = strconv.ParseInt(parts[1], 10, 64)
				if err1 != nil || end < start {
					return ctx.Result(400, "invalid Range end")
				}
			}

			if start >= total {
				return ctx.Result(416, "requested range not satisfiable")
			}
			if end >= total {
				end = total - 1
			}

			length := end - start + 1
			// 设置部分响应头
			rw.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(total, 10))
			rw.Header().Set("Content-Length", strconv.FormatInt(length, 10))

			// 返回 206 并写入指定区间
			rw.WriteHeader(206)
			if _, err = reader.Seek(start, io.SeekStart); err != nil {
				return ctx.Result(500, err.Error())
			}
			if _, err = io.CopyN(rw, reader, length); err != nil {
				return ctx.Result(500, err.Error())
			}
			return nil
		}

		// 无 Range，返回完整内容（200）
		rw.Header().Set("Content-Length", strconv.FormatInt(total, 10))
		rw.WriteHeader(200)
		if _, err = io.Copy(rw, reader); err != nil {
			return ctx.Result(500, err.Error())
		}
		return nil
	}
}
