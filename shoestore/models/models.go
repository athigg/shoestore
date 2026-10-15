package models

import (
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"

	// BcryptCost ยิ่งสูงยิ่งช้าต่อการเดารหัสผ่านแบบ brute force
	BcryptCost = 12
)

type User struct {
	gorm.Model
	Username string `gorm:"unique;not null"`
	Email    string `gorm:"unique;not null"`
	Password string `gorm:"not null"`
	Role     string `gorm:"default:'user'"`
}

func (u *User) HashPassword(plainPassword string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(plainPassword), BcryptCost)
	if err != nil {
		return err
	}
	u.Password = string(hashed)
	return nil
}

func (u *User) CheckPassword(plainPassword string) error {
	return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(plainPassword))
}

type Shoe struct {
	gorm.Model
	Name        string
	Brand       string
	Price       float64
	Description string
	ImageURL    string
	Sizes       []ShoeSize `gorm:"foreignKey:ShoeID;constraint:OnDelete:CASCADE;"`
}

type ShoeSize struct {
	gorm.Model
	ShoeID uint
	Size   int
	Stock  int
}

func (s Shoe) TotalStock() int {
	total := 0
	for _, size := range s.Sizes {
		total += size.Stock
	}
	return total
}

// Order: สถานะ pending | shipped | completed | cancelled
type Order struct {
	gorm.Model
	UserID   uint
	User     User
	ShoeID   uint
	Shoe     Shoe
	Size     int
	Quantity int     // เพิ่มฟิลด์นี้: สำหรับเก็บจำนวนคู่
	Price    float64 // เก็บราคารวม (ราคารองเท้า x จำนวน)
	Address  string  `gorm:"size:500"`
	Phone    string  `gorm:"size:20"`
	Status   string  `gorm:"size:20;default:pending"`
}

// Review คือข้อมูลที่มี "เจ้าของ" (UserID) เจ้าของเท่านั้นที่แก้ไข/ลบได้
type Review struct {
	gorm.Model
	UserID  uint `gorm:"index"`
	User    User
	ShoeID  uint `gorm:"index"`
	Rating  int
	Comment string `gorm:"size:500"`
}

// Session เก็บเฉพาะค่า hash ของ token (ถ้าฐานข้อมูลรั่ว ก็เอาไปสวมรอยไม่ได้)
type Session struct {
	TokenHash string `gorm:"primaryKey;size:64"`
	UserID    uint
	CSRF      string    `gorm:"size:64"`
	Flash     string    `gorm:"size:300"`
	ExpiresAt time.Time `gorm:"index"`
	CreatedAt time.Time
}
