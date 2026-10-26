package model

type User struct {
	BaseModel
	Username     string `gorm:"size:64;uniqueIndex;not null;comment:登录用户名" json:"username"`
	PasswordHash string `gorm:"size:255;not null;comment:登录密码哈希" json:"-"`
	Nickname     string `gorm:"size:64;comment:用户昵称" json:"nickname"`
	Email        string `gorm:"size:128;comment:邮箱地址" json:"email"`
	Phone        string `gorm:"size:32;comment:手机号码" json:"phone"`
	Role         string `gorm:"size:32;not null;default:operator;comment:用户角色：admin管理员 operator操作员" json:"role"`
	Status       string `gorm:"size:32;not null;default:enabled;comment:账号状态：enabled启用 disabled禁用" json:"status"`
	// TokenVersion 登录凭证版本，修改密码、停用账号或变更角色时递增，使已签发的凭证全部失效
	TokenVersion uint `gorm:"not null;default:0;comment:登录凭证版本，变更后旧凭证失效" json:"-"`
}

func (User) TableName() string {
	return "dasslm_users"
}
