package model

// Material 记录经 /v1/sd/upload 落到对象存储的素材。
// 用途是风控：同一 sha256 被哪些账号重复引用、单账号短时间上传量。
// 故意不给 sha256 加唯一约束——同内容多次上传各记一行。
type Material struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId    int    `json:"user_id" gorm:"index"`
	TokenId   int    `json:"token_id" gorm:"index"`
	Sha256    string `json:"sha256" gorm:"type:char(64);index"`
	Size      int64  `json:"size" gorm:"bigint"`
	Mime      string `json:"mime" gorm:"type:varchar(64)"`
	Kind      string `json:"kind" gorm:"type:varchar(16)"` // image / video / audio
	ObjectKey string `json:"object_key" gorm:"type:varchar(255)"`
	Url       string `json:"url" gorm:"type:varchar(512)"`
	FileName  string `json:"file_name" gorm:"type:varchar(255)"`
	Source    string `json:"source" gorm:"type:varchar(16)"` // upload / import
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

func (Material) TableName() string {
	return "materials"
}

func (m *Material) Insert() error {
	return DB.Create(m).Error
}
