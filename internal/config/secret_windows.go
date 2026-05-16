//go:build windows

package config

import (
	"encoding/base64"
	"syscall"
	"unsafe"
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32                  = syscall.NewLazyDLL("crypt32.dll")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData     = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData   = crypt32.NewProc("CryptUnprotectData")
	procLocalFree            = kernel32.NewProc("LocalFree")
	feedMeDailyDescription   = syscall.StringToUTF16Ptr("FeedMeDaily")
	cryptProtectUISuppressed = uintptr(0)
)

func secretStoreScheme() string {
	// Windows 上优先使用 DPAPI，让密钥绑定到当前用户环境。
	return "dpapi"
}

func encodeSecret(value string, scheme string) (string, error) {
	// 当 scheme 为 dpapi 时做系统级加密，否则回退到 base64。
	if scheme != "dpapi" {
		return encodeBase64Secret(value), nil
	}
	protected, err := cryptProtect([]byte(value))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(protected), nil
}

func decodeSecret(value string, scheme string) (string, error) {
	// 读取时按 scheme 选择对应的解码方式。
	if scheme != "dpapi" {
		return decodeBase64Secret(value)
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	unprotected, err := cryptUnprotect(raw)
	if err != nil {
		return "", err
	}
	return string(unprotected), nil
}

func cryptProtect(data []byte) ([]byte, error) {
	// 调用 Windows CryptProtectData 把明文加密成二进制 blob。
	var in dataBlob
	if len(data) > 0 {
		in = dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	}
	var out dataBlob
	result, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		uintptr(unsafe.Pointer(feedMeDailyDescription)),
		0,
		0,
		0,
		cryptProtectUISuppressed,
		uintptr(unsafe.Pointer(&out)),
	)
	if result == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return copyBlob(out), nil
}

func cryptUnprotect(data []byte) ([]byte, error) {
	// 调用 Windows CryptUnprotectData 把 DPAPI blob 解密回明文。
	var in dataBlob
	if len(data) > 0 {
		in = dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	}
	var out dataBlob
	result, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		cryptProtectUISuppressed,
		uintptr(unsafe.Pointer(&out)),
	)
	if result == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return copyBlob(out), nil
}

func copyBlob(blob dataBlob) []byte {
	// 把 Windows 返回的内存块复制到 Go 管理的切片中。
	if blob.cbData == 0 || blob.pbData == nil {
		return []byte{}
	}
	return append([]byte(nil), unsafe.Slice(blob.pbData, int(blob.cbData))...)
}
