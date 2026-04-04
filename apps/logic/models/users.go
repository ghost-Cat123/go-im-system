package models

import (
	"time"
)

// User 数据库映射模型
type User struct {
	// 【核心身份】
	// ID 可以是 varchar 的 UUID，也可以是自增 int64。
	// 大厂 IM 为了防止泄露每日注册量，通常用雪花算法生成的 int64 或 UUID。这里演示 varchar。
	UserId int64 `gorm:"primaryKey;autoIncrement;column:user_id"`

	// 【登录凭证】
	Username string `gorm:"type:varchar(32);uniqueIndex;not null;column:username"` // 登录账号，必须唯一
	Password string `gorm:"type:varchar(128);not null;column:password"`            // 存放 Bcrypt 加密后的密文，绝不存明文！

	// 【业务展示】
	Nickname string `gorm:"type:varchar(64);default:'用户';column:nickname"` // 聊天时显示的昵称
	Avatar   string `gorm:"type:varchar(255);column:avatar"`               // 头像的 URL

	// 【状态与时间】
	CreateTime time.Time `gorm:"autoCreateTime;column:create_time"` // 注册时间
	UpdateTime time.Time `gorm:"autoUpdateTime;column:update_time"` // 资料最后修改时间
}

func NewUser(userId int64, username string, password string, nickname string, avatar string) *User {
	return &User{UserId: userId, Username: username, Password: password, Nickname: nickname, Avatar: avatar}
}
