// Package keyhash 定义 API Key 明文到 apikeys.key_hash 的编码契约（鉴权与持久化须一致）。
package keyhash

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashAPIKey 对明文 API Key 做 SHA-256，并以十六进制字符串返回（与库中 key_hash 列一致）。
func HashAPIKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
