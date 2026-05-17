package config

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
)

type secretStorePayload struct {
	Scheme string            `json:"scheme"`
	Values map[string]string `json:"values"`
}

func loadSecretValues(path string) (map[string]string, error) {
	// 读取密钥存储文件，并按当前 scheme 解码每个字段。
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var payload secretStorePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if payload.Scheme == "" {
		payload.Scheme = "base64"
	}
	values := map[string]string{}
	for key, encoded := range payload.Values {
		decoded, err := decodeSecret(encoded, payload.Scheme)
		if err != nil {
			return nil, err
		}
		values[key] = decoded
	}
	return values, nil
}

func storeSecretValues(path string, values map[string]string) error {
	// 把密钥统一编码后写回本地 secrets.json。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	scheme := secretStoreScheme()
	encoded := map[string]string{}
	for key, value := range values {
		next, err := encodeSecret(value, scheme)
		if err != nil {
			return err
		}
		encoded[key] = next
	}
	payload, err := json.MarshalIndent(secretStorePayload{Scheme: scheme, Values: encoded}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}

func encodeBase64Secret(value string) string {
	// base64 只是便于统一存储格式，不提供真正的加密保护。
	return base64.StdEncoding.EncodeToString([]byte(value))
}

func decodeBase64Secret(value string) (string, error) {
	// 对应地把 base64 内容还原回明文字符串。
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
