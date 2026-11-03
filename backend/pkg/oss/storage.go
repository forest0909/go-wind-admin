package oss

import (
	"context"
	"fmt"
	"io"
	"strings"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"
)

// 配置项 oss.storage 的取值。
const (
	StorageMinIO = "minio"
	StorageLocal = "local"
)

// UploadInfo 上传结果：对象实际落在哪个桶、哪个 key，以及字节数。
type UploadInfo struct {
	Bucket string
	Key    string
	Size   int64
}

// Storage 业务用到的对象存储操作。每个部署只用一种实现（MinIO 或本地磁盘）。
type Storage interface {
	// Provider 写进 files.provider 的值，也用来判断已有记录是不是存在当前存储里。
	Provider() storageV1.OSSProvider

	// UploadFile 上传对象。storagePath 形如 /bucket/object；downloadURL 只有 MinIO 有（拼了 download_host），本地为空。
	UploadFile(ctx context.Context, bucketName, objectName, mimeType string, fileContent []byte) (info UploadInfo, storagePath, downloadURL string, err error)

	// DeleteFile 删除对象。对象不存在不算错。
	DeleteFile(ctx context.Context, bucketName, objectName string) error

	// GetObjectReader 流式读取对象，返回 reader、Content-Type、字节数。调用方负责关闭 reader。
	GetObjectReader(ctx context.Context, bucketName, objectName string) (io.ReadCloser, string, int64, error)

	// DownloadFile 按 StorageObject 下载（整块读入内存）。
	DownloadFile(ctx context.Context, req *storageV1.DownloadFileRequest) (*storageV1.DownloadFileResponse, error)
}

// NewStorage 按 oss.storage 选择存储实现。留空等于 minio，保持原来的行为。
// 选 local 时不创建 MinIO 客户端；根目录不可用时返回错误，启动失败。
func NewStorage(cfg *conf.Bootstrap, logger bLogger.Logger) (Storage, error) {
	switch kind := strings.ToLower(strings.TrimSpace(cfg.GetOss().GetStorage())); kind {
	case "", StorageMinIO:
		return NewMinIoClient(cfg, logger), nil
	case StorageLocal:
		return NewLocalStorage(cfg.GetOss().GetLocal().GetRootDir(), logger)
	default:
		return nil, fmt.Errorf("oss.storage: unknown storage %q, want %q or %q", kind, StorageMinIO, StorageLocal)
	}
}

// FileProvider 文件记录的存储方式。没有写 provider 的旧记录按 MinIO 算（ent 默认值）。
func FileProvider(f *storageV1.File) storageV1.OSSProvider {
	if f == nil || f.Provider == nil {
		return storageV1.OSSProvider_MINIO
	}
	return f.GetProvider()
}

// CheckFileProvider 文件记录存放的存储和当前配置不一致时（中途换了存储）返回 STORAGE_PROVIDER_MISMATCH。
func CheckFileProvider(f *storageV1.File, st Storage) error {
	if got, want := FileProvider(f), st.Provider(); got != want {
		return storageV1.ErrorStorageProviderMismatch("file %d is stored in %s, but the configured storage is %s",
			f.GetId(), got.String(), want.String())
	}
	return nil
}
