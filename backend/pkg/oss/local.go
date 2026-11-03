package oss

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"go-wind-admin/third_party/go-utils/ossutil"
	"go-wind-admin/third_party/go-utils/timeutil"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"
)

// 本地存储的目录和文件权限：属主读写，同组只读，其他用户无权限。
const (
	localDirPerm  fs.FileMode = 0o750
	localFilePerm fs.FileMode = 0o640
)

// LocalStorage 本地磁盘存储，对象存放在 root/bucket/object。
type LocalStorage struct {
	root string
	log  *bLogger.Helper
}

var _ Storage = (*LocalStorage)(nil)

// NewLocalStorage 创建本地存储。根目录不存在就创建；不可写时返回错误。
func NewLocalStorage(rootDir string, logger bLogger.Logger) (*LocalStorage, error) {
	if strings.TrimSpace(rootDir) == "" {
		return nil, errors.New("oss.local.root_dir is required when oss.storage is local")
	}
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q: %w", rootDir, err)
	}
	if err = os.MkdirAll(root, localDirPerm); err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q: create directory: %w", root, err)
	}
	probe, err := os.CreateTemp(root, ".write-check-*")
	if err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q is not writable: %w", root, err)
	}
	name := probe.Name()
	if err = probe.Close(); err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q: close probe file: %w", root, err)
	}
	if err = os.Remove(name); err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q: remove probe file: %w", root, err)
	}
	return &LocalStorage{
		root: root,
		log:  bLogger.NewHelper(logger.With("module", "oss/local")),
	}, nil
}

func (s *LocalStorage) Provider() storageV1.OSSProvider { return storageV1.OSSProvider_LOCAL }

// objectPath 把 bucket/object 映射成 root 下的文件路径。
// 拒绝空段、"."、".."、绝对路径和反斜杠，最后再确认结果落在 root 内。
func (s *LocalStorage) objectPath(bucketName, objectName string) (string, error) {
	if bucketName == "" || objectName == "" {
		return "", storageV1.ErrorBadRequest("bucket name and object name are required")
	}
	if strings.ContainsAny(bucketName, `/\`) || bucketName == "." || bucketName == ".." {
		return "", storageV1.ErrorBadRequest("invalid bucket name")
	}
	if strings.HasPrefix(objectName, "/") || strings.Contains(objectName, `\`) ||
		strings.ContainsRune(objectName, 0) || filepath.IsAbs(objectName) {
		return "", storageV1.ErrorBadRequest("invalid object name")
	}
	for _, seg := range strings.Split(objectName, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", storageV1.ErrorBadRequest("invalid object name")
		}
	}
	p := filepath.Join(s.root, bucketName, filepath.FromSlash(objectName))
	rel, err := filepath.Rel(s.root, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", storageV1.ErrorBadRequest("invalid object path")
	}
	return p, nil
}

// UploadFile 先写同目录下的临时文件，再 rename 到目标位置，读的人看不到写了一半的文件。
func (s *LocalStorage) UploadFile(ctx context.Context, bucketName, objectName, mimeType string, fileContent []byte) (UploadInfo, string, string, error) {
	if len(fileContent) == 0 {
		return UploadInfo{}, "", "", storageV1.ErrorUploadFailed("empty fileContent data")
	}
	p, err := s.objectPath(bucketName, objectName)
	if err != nil {
		return UploadInfo{}, "", "", err
	}
	if err = s.writeFile(p, fileContent); err != nil {
		s.log.Errorf(ctx, "local storage: write %s/%s failed: %v", bucketName, objectName, err)
		return UploadInfo{}, "", "", storageV1.ErrorUploadFailed("failed to upload fileContent")
	}
	info := UploadInfo{Bucket: bucketName, Key: objectName, Size: int64(len(fileContent))}
	return info, ossutil.JoinObjectUrl("", bucketName, objectName), "", nil
}

func (s *LocalStorage) writeFile(p string, content []byte) (err error) {
	dir := filepath.Dir(p)
	if err = os.MkdirAll(dir, localDirPerm); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			if rmErr := os.Remove(tmpName); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
				err = errors.Join(err, rmErr)
			}
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		_ = tmp.Close() // 写入已失败，返回写入错误
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close() // 同上
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, localFilePerm); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

func (s *LocalStorage) DeleteFile(ctx context.Context, bucketName, objectName string) error {
	p, err := s.objectPath(bucketName, objectName)
	if err != nil {
		return err
	}
	if err = os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.log.Errorf(ctx, "local storage: delete %s/%s failed: %v", bucketName, objectName, err)
		return storageV1.ErrorDeleteFailed("failed to delete file")
	}
	return nil
}

// open 打开对象文件并确认是普通文件。
func (s *LocalStorage) open(bucketName, objectName string) (*os.File, fs.FileInfo, error) {
	p, err := s.objectPath(bucketName, objectName)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close() // 返回 Stat 的错误
		return nil, nil, err
	}
	if !st.Mode().IsRegular() {
		_ = f.Close() // 不是普通文件，按不存在处理
		return nil, nil, fmt.Errorf("%s/%s: %w", bucketName, objectName, fs.ErrNotExist)
	}
	return f, st, nil
}

// contentTypeOf 本地文件不存 Content-Type：先按扩展名，认不出再嗅探文件头。
func contentTypeOf(f io.ReadSeeker, objectName string) (string, error) {
	if ct := mime.TypeByExtension(path.Ext(objectName)); ct != "" {
		return ct, nil
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if n == 0 {
		return DefaultContentType, nil
	}
	return http.DetectContentType(head[:n]), nil
}

func (s *LocalStorage) GetObjectReader(ctx context.Context, bucketName, objectName string) (io.ReadCloser, string, int64, error) {
	f, st, err := s.open(bucketName, objectName)
	if err != nil {
		s.log.Errorf(ctx, "local storage: open %s/%s failed: %v", bucketName, objectName, err)
		return nil, "", 0, err
	}
	ct, err := contentTypeOf(f, objectName)
	if err != nil {
		_ = f.Close() // 返回检测类型时的错误
		s.log.Errorf(ctx, "local storage: detect content type of %s/%s failed: %v", bucketName, objectName, err)
		return nil, "", 0, err
	}
	return f, ct, st.Size(), nil
}

// DownloadFile 本地存储没有预签名地址，PreferPresignedUrl 也直接返回文件内容。
// Range 语义和 MinIO 一致：闭区间 [start, end]，只给 end 时表示从 0 到 end。
func (s *LocalStorage) DownloadFile(ctx context.Context, req *storageV1.DownloadFileRequest) (*storageV1.DownloadFileResponse, error) {
	if _, ok := req.Selector.(*storageV1.DownloadFileRequest_StorageObject); !ok {
		return nil, storageV1.ErrorBadRequest("invalid selector")
	}
	bucketName := req.GetStorageObject().GetBucketName()
	objectName := req.GetStorageObject().GetObjectName()

	f, st, err := s.open(bucketName, objectName)
	if err != nil {
		s.log.Errorf(ctx, "local storage: open %s/%s failed: %v", bucketName, objectName, err)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, storageV1.ErrorFileNotFound("file not found")
		}
		return nil, storageV1.ErrorDownloadFailed("failed to get object")
	}
	defer func() { _ = f.Close() }() // 只读文件，关闭错误无影响

	start, end := int64(0), st.Size()-1
	if req.RangeStart != nil {
		start = req.GetRangeStart()
	}
	if req.RangeEnd != nil && req.GetRangeEnd() < end {
		end = req.GetRangeEnd()
	}
	if start < 0 || (st.Size() > 0 && start > end) {
		return nil, storageV1.ErrorBadRequest("invalid range")
	}

	buf := new(bytes.Buffer)
	if _, err = buf.ReadFrom(io.NewSectionReader(f, start, end-start+1)); err != nil {
		s.log.Errorf(ctx, "local storage: read %s/%s failed: %v", bucketName, objectName, err)
		return nil, storageV1.ErrorDownloadFailed("failed to read object")
	}

	resp := &storageV1.DownloadFileResponse{
		Content: &storageV1.DownloadFileResponse_File{File: buf.Bytes()},
	}
	if req.GetAcceptMime() != "" {
		resp.Mime = req.GetAcceptMime()
	} else if resp.Mime, err = contentTypeOf(f, objectName); err != nil {
		s.log.Errorf(ctx, "local storage: detect content type of %s/%s failed: %v", bucketName, objectName, err)
		return nil, storageV1.ErrorDownloadFailed("failed to read object")
	}
	modTime := st.ModTime()
	resp.SourceFileName = objectName
	resp.Size = st.Size()
	resp.UpdatedAt = timeutil.TimeToTimestamppb(&modTime)
	return resp, nil
}
