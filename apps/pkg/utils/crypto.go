package utils

import (
	"crypto/md5"
	"encoding/hex"
	"golang.org/x/crypto/bcrypt"
)

// ================== 生产级密码加密 (Bcrypt) ==================

// HashPassword 使用 Bcrypt 对密码进行加密 (生产环境必须用这个存密码)
func HashPassword(password string) (string, error) {
	// DefaultCost 是 10，数值越大越安全，但也越消耗 CPU
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPasswordHash 验证用户输入的明文密码与数据库的密文是否匹配
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ================== 普通数据摘要 (MD5) ==================

// MD5 生成 32 位小写 MD5 字符串 (可用于生成唯一文件ID等非密码敏感场景)
func MD5(str string) string {
	h := md5.New()
	h.Write([]byte(str))
	return hex.EncodeToString(h.Sum(nil))
}
