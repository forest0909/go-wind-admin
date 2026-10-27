package data

// derefStrP 把可空的字符串枚举或字符串指针读成普通字符串。
// 空指针返回空串。菜单路径和参数键值都走这里。
func derefStrP[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}
