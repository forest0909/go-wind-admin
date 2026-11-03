package oss

import (
	"strconv"
	"strings"

	"go-wind-admin/third_party/go-utils/ossutil"
)

// 头像对象放在 images 桶的 avatar/{userId}/ 目录下，user.avatar 存后端相对地址
// /admin/v1/avatars/{userId}/{name}：不过期，和存储实现、download_host 都无关，由后端从存储读出返回。
const (
	AvatarBucket      = ossutil.BucketImages
	AvatarRoutePrefix = "/admin/v1/avatars/"
	avatarDirPrefix   = "avatar/"
)

// AvatarDirectory 用户头像对象所在目录。
func AvatarDirectory(userID uint32) string {
	return avatarDirPrefix + strconv.FormatUint(uint64(userID), 10)
}

// AvatarURL 头像对象 avatar/{userId}/{name} 对应的访问地址。
func AvatarURL(userID uint32, name string) string {
	return AvatarRoutePrefix + strconv.FormatUint(uint64(userID), 10) + "/" + name
}

// validAvatarName 对象名只能是一段：不含 / \ ? #，不以 . 开头（排除 . 和 ..）。
func validAvatarName(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") && !strings.ContainsAny(name, `/\?#`) &&
		!strings.ContainsRune(name, 0)
}

// ParseAvatarURL 解析 /admin/v1/avatars/{userId}/{name}，返回用户 ID 和对象名（含 avatar/ 目录）。
func ParseAvatarURL(u string) (userID uint32, object string, ok bool) {
	rest, found := strings.CutPrefix(u, AvatarRoutePrefix)
	if !found {
		return 0, "", false
	}
	idStr, name, found := strings.Cut(rest, "/")
	if !found || idStr == "" || !validAvatarName(name) {
		return 0, "", false
	}
	for _, c := range idStr {
		if c < '0' || c > '9' {
			return 0, "", false
		}
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return 0, "", false
	}
	userID = uint32(id)
	if AvatarURL(userID, name) != u { // 拒绝前导 0 等非规范写法
		return 0, "", false
	}
	return userID, AvatarDirectory(userID) + "/" + name, true
}

// LegacyAvatarObject 识别旧版存进 user.avatar 的 {download_host}/images/avatar/{userId}/{name}，
// 返回对象名。downloadHost 为空时认不出任何旧地址。
func LegacyAvatarObject(downloadHost, avatarURL string, userID uint32) (object string, ok bool) {
	if downloadHost == "" {
		return "", false
	}
	prefix := ossutil.JoinObjectUrl(downloadHost, AvatarBucket, AvatarDirectory(userID)) + "/"
	name, found := strings.CutPrefix(avatarURL, prefix)
	if !found || !validAvatarName(name) {
		return "", false
	}
	return AvatarDirectory(userID) + "/" + name, true
}
