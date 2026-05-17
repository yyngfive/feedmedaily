//go:build !windows

package config

func secretStoreScheme() string {
	// 非 Windows 平台暂时不接系统密钥链，统一回退到 base64 存储。
	return "base64"
}

func encodeSecret(value string, scheme string) (string, error) {
	// 非 Windows 平台直接使用 base64 编码。
	return encodeBase64Secret(value), nil
}

func decodeSecret(value string, scheme string) (string, error) {
	// 非 Windows 平台直接做 base64 解码。
	return decodeBase64Secret(value)
}
