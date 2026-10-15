package main

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"shoestore/config"
	"shoestore/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	usernameRe = regexp.MustCompile(`^[A-Za-z0-9_]{3,20}$`)
	phoneRe    = regexp.MustCompile(`^[0-9+\- ]{8,15}$`)

	shoeSizes = []int{39, 40, 41, 42, 43, 44, 45}

	allowedImages = map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/gif":  ".gif",
		"image/webp": ".webp",
	}
)

const maxImageBytes = 3 << 20 // 3 MB

var pageTitles = map[string]string{
	"index.html":        "ร้านรองเท้า",
	"shoe.html":         "รายละเอียดสินค้า",
	"login.html":        "เข้าสู่ระบบ",
	"register.html":     "สมัครสมาชิก",
	"my_orders.html":    "คำสั่งซื้อของฉัน",
	"edit_review.html":  "แก้ไขรีวิว",
	"admin_panel.html":  "จัดการสินค้า",
	"edit_shoe.html":    "แก้ไขสินค้า",
	"admin_orders.html": "จัดการออเดอร์",
	"admin_users.html":  "จัดการผู้ใช้งาน",
	"edit_user.html":    "แก้ไขผู้ใช้งาน",
	"error.html":        "เกิดข้อผิดพลาด",
}

var funcMap = template.FuncMap{
	"stars": func(n int) string {
		if n < 0 {
			n = 0
		}
		if n > 5 {
			n = 5
		}
		return strings.Repeat("★", n) + strings.Repeat("☆", 5-n)
	},
	"statusText": func(s string) string {
		switch s {
		case "pending":
			return "รอดำเนินการ"
		case "shipped":
			return "จัดส่งแล้ว"
		case "completed":
			return "สำเร็จ"
		case "cancelled":
			return "ยกเลิก"
		}
		return s
	},
	"statusClass": func(s string) string {
		switch s {
		case "shipped":
			return "st-shipped"
		case "completed":
			return "st-completed"
		case "cancelled":
			return "st-cancelled"
		}
		return "st-pending"
	},
}

// ---------- render ----------

func render(c *gin.Context, status int, name string, data gin.H) {
	if data == nil {
		data = gin.H{}
	}
	if _, ok := data["title"]; !ok {
		t := pageTitles[name]
		if t == "" {
			t = "Shoestore"
		}
		data["title"] = t
	}

	var meID uint
	me := ""
	u := currentUser(c)
	if u != nil {
		meID = u.ID
		me = u.Username
	}
	data["meID"] = meID
	data["me"] = me
	data["isLoggedIn"] = u != nil
	data["isAdmin"] = u != nil && u.Role == models.RoleAdmin
	data["csrf"] = ""
	data["ratingOptions"] = []int{5, 4, 3, 2, 1}

	if s := currentSession(c); s != nil {
		data["csrf"] = s.CSRF
		if s.Flash != "" {
			parts := strings.SplitN(s.Flash, "|", 2)
			if len(parts) == 2 {
				data["flash"] = parts[1]
				if parts[0] == "err" {
					data["flashClass"] = "alert-error"
				} else {
					data["flashClass"] = "alert-ok"
				}
			}
			s.Flash = ""
			config.DB.Model(&models.Session{}).Where("token_hash = ?", s.TokenHash).Update("flash", "")
		}
	}
	c.HTML(status, name, data)
}

func renderError(c *gin.Context, status int, msg string) {
	render(c, status, "error.html", gin.H{"code": status, "message": msg})
}

// setFlash เก็บข้อความแจ้งเตือนไว้แสดงในหน้าถัดไป (kind = "ok" หรือ "err")
func setFlash(c *gin.Context, kind, msg string) {
	s := currentSession(c)
	if s == nil {
		return
	}
	s.Flash = kind + "|" + msg
	config.DB.Model(&models.Session{}).Where("token_hash = ?", s.TokenHash).Update("flash", s.Flash)
}

// ---------- helpers ----------

func paramID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func bySize(db *gorm.DB) *gorm.DB   { return db.Order("size ASC") }
func unscoped(db *gorm.DB) *gorm.DB { return db.Unscoped() }

// restoreStock คืนสต็อกตามจำนวนคู่ที่สั่ง (ใช้ตอนยกเลิก/ลบออเดอร์)
func restoreStock(tx *gorm.DB, shoeID uint, size int, qty int) error {
	if qty < 1 {
		qty = 1
	}
	return tx.Model(&models.ShoeSize{}).
		Where("shoe_id = ? AND size = ?", shoeID, size).
		Update("stock", gorm.Expr("stock + ?", qty)).Error
}

// saveImage เก็บรูปลงฐานข้อมูล (ไม่หายเมื่อ deploy ใหม่บน Render)
// ตรวจชนิดไฟล์จากเนื้อหาจริง ไม่เชื่อนามสกุลที่ผู้ใช้ส่งมา คืนค่า "" ถ้าไม่มีไฟล์แนบ
func saveImage(c *gin.Context, field string) (string, error) {
	fh, err := c.FormFile(field)
	if err != nil {
		return "", nil
	}
	if fh.Size > maxImageBytes {
		return "", errors.New("ไฟล์รูปต้องมีขนาดไม่เกิน 3 MB")
	}
	f, err := fh.Open()
	if err != nil {
		return "", errors.New("เปิดไฟล์ไม่สำเร็จ")
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil || len(data) > maxImageBytes {
		return "", errors.New("อ่านไฟล์รูปไม่สำเร็จ หรือไฟล์ใหญ่เกิน 3 MB")
	}
	ctype := http.DetectContentType(data)
	if _, ok := allowedImages[ctype]; !ok {
		return "", errors.New("รองรับเฉพาะไฟล์รูป JPG, PNG, GIF และ WEBP")
	}
	img := models.Image{ContentType: ctype, Data: data}
	if err := config.DB.Create(&img).Error; err != nil {
		return "", errors.New("บันทึกไฟล์รูปไม่สำเร็จ")
	}
	return "/img/" + strconv.FormatUint(uint64(img.ID), 10), nil
}

func removeImage(url string) {
	if strings.HasPrefix(url, "/img/") {
		if id, err := strconv.ParseUint(strings.TrimPrefix(url, "/img/"), 10, 32); err == nil {
			config.DB.Delete(&models.Image{}, uint(id))
		}
		return
	}
	if strings.HasPrefix(url, "/uploads/") {
		_ = os.Remove(filepath.Join("uploads", filepath.Base(url)))
	}
}

type shoeForm struct {
	Name, Brand, Description string
	Price                    float64
	Stocks                   []int
}

func parseShoeForm(c *gin.Context) (shoeForm, string) {
	f := shoeForm{
		Name:        strings.TrimSpace(c.PostForm("name")),
		Brand:       strings.TrimSpace(c.PostForm("brand")),
		Description: strings.TrimSpace(c.PostForm("description")),
	}
	if n := runeLen(f.Name); n < 1 || n > 100 {
		return f, "ชื่อสินค้าต้องมีความยาว 1-100 ตัวอักษร"
	}
	if n := runeLen(f.Brand); n < 1 || n > 50 {
		return f, "แบรนด์ต้องมีความยาว 1-50 ตัวอักษร"
	}
	if runeLen(f.Description) > 1000 {
		return f, "รายละเอียดต้องไม่เกิน 1000 ตัวอักษร"
	}
	price, err := strconv.ParseFloat(strings.TrimSpace(c.PostForm("price")), 64)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 || price > 1000000 {
		return f, "ราคาไม่ถูกต้อง (ต้องมากกว่า 0 และไม่เกิน 1,000,000)"
	}
	f.Price = math.Round(price*100) / 100
	for _, sz := range shoeSizes {
		raw := strings.TrimSpace(c.PostForm(fmt.Sprintf("stock_%d", sz)))
		if raw == "" {
			raw = "0"
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 100000 {
			return f, fmt.Sprintf("สต็อกของไซส์ %d ไม่ถูกต้อง", sz)
		}
		f.Stocks = append(f.Stocks, n)
	}
	return f, ""
}
