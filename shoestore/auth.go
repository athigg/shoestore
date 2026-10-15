package main

import (
	"net/http"
	"net/mail"
	"strings"
	"unicode"

	"shoestore/config"
	"shoestore/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func hasLetterAndDigit(s string) bool {
	var letter, digit bool
	for _, r := range s {
		if unicode.IsLetter(r) {
			letter = true
		} else if unicode.IsDigit(r) {
			digit = true
		}
	}
	return letter && digit
}

// validateAccount ตรวจ username / email (ใช้ทั้งตอนสมัครและตอนแอดมินแก้ไขผู้ใช้)
func validateAccount(username, email string) string {
	if !usernameRe.MatchString(username) {
		return "ชื่อผู้ใช้ต้องเป็น a-z, A-Z, 0-9 หรือ _ ความยาว 3-20 ตัวอักษร"
	}
	if len(email) > 100 {
		return "อีเมลยาวเกินไป"
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "รูปแบบอีเมลไม่ถูกต้อง"
	}
	return ""
}

func registerPage(c *gin.Context) {
	if currentUser(c) != nil {
		c.Redirect(http.StatusSeeOther, "/")
		return
	}
	render(c, http.StatusOK, "register.html", gin.H{})
}

func registerSubmit(c *gin.Context) {
	ipKey := c.ClientIP()
	if registerLimiter.blocked(ipKey) {
		renderError(c, http.StatusTooManyRequests, "สมัครสมาชิกบ่อยเกินไป กรุณาลองใหม่ภายหลัง")
		return
	}
	registerLimiter.fail(ipKey)

	username := strings.TrimSpace(c.PostForm("username"))
	email := strings.ToLower(strings.TrimSpace(c.PostForm("email")))
	password := c.PostForm("password")
	confirm := c.PostForm("confirm")

	fail := func(msg string) {
		render(c, http.StatusBadRequest, "register.html", gin.H{"error": msg, "fUsername": username, "fEmail": email})
	}

	if msg := validateAccount(username, email); msg != "" {
		fail(msg)
		return
	}
	if len(password) < 8 || len(password) > 72 {
		fail("รหัสผ่านต้องมีความยาว 8-72 ตัวอักษร")
		return
	}
	if !hasLetterAndDigit(password) {
		fail("รหัสผ่านต้องมีทั้งตัวอักษรและตัวเลข")
		return
	}
	if password != confirm {
		fail("รหัสผ่านและยืนยันรหัสผ่านไม่ตรงกัน")
		return
	}

	var count int64
	config.DB.Unscoped().Model(&models.User{}).Where("username = ? OR email = ?", username, email).Count(&count)
	if count > 0 {
		fail("ชื่อผู้ใช้หรืออีเมลนี้ถูกใช้งานแล้ว")
		return
	}

	// สมัครผ่านหน้าเว็บได้เป็น user เท่านั้น (ห้ามรับ role จากฟอร์ม)
	user := models.User{Username: username, Email: email, Role: models.RoleUser}
	if err := user.HashPassword(password); err != nil {
		fail("เกิดข้อผิดพลาดในการเข้ารหัสรหัสผ่าน")
		return
	}
	if err := config.DB.Create(&user).Error; err != nil {
		fail("ไม่สามารถสมัครสมาชิกได้ (ชื่อผู้ใช้หรืออีเมลอาจซ้ำ)")
		return
	}
	setFlash(c, "ok", "สมัครสมาชิกสำเร็จ กรุณาเข้าสู่ระบบ")
	c.Redirect(http.StatusSeeOther, "/login")
}

func loginPage(c *gin.Context) {
	if currentUser(c) != nil {
		c.Redirect(http.StatusSeeOther, "/")
		return
	}
	render(c, http.StatusOK, "login.html", gin.H{})
}

func loginSubmit(c *gin.Context) {
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	ipKey := c.ClientIP()
	userKey := strings.ToLower(username)

	if loginIPLimiter.blocked(ipKey) || loginUserLimiter.blocked(userKey) {
		render(c, http.StatusTooManyRequests, "login.html", gin.H{
			"error":     "พยายามเข้าสู่ระบบผิดหลายครั้งเกินไป กรุณารอ 15 นาทีแล้วลองใหม่",
			"fUsername": username,
		})
		return
	}

	var user models.User
	err := config.DB.Where("username = ?", username).First(&user).Error
	valid := false
	if err == nil {
		valid = len(password) <= 200 && user.CheckPassword(password) == nil
	} else {
		// ไม่พบผู้ใช้ ก็ยังคำนวณ bcrypt เพื่อกันการเดาชื่อผู้ใช้จากเวลาตอบสนอง
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
	}
	if !valid {
		loginIPLimiter.fail(ipKey)
		loginUserLimiter.fail(userKey)
		render(c, http.StatusUnauthorized, "login.html", gin.H{
			"error":     "ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง",
			"fUsername": username,
		})
		return
	}
	loginUserLimiter.reset(userKey)

	// สร้าง session ใหม่ทุกครั้งที่ล็อกอิน (กัน session fixation)
	if old := currentSession(c); old != nil {
		config.DB.Delete(&models.Session{}, "token_hash = ?", old.TokenHash)
	}
	ns := newSession(c, user.ID)
	c.Set("session", ns)
	c.Set("user", &user)
	setFlash(c, "ok", "เข้าสู่ระบบสำเร็จ")
	c.Redirect(http.StatusSeeOther, "/")
}

func logoutSubmit(c *gin.Context) {
	if old := currentSession(c); old != nil {
		config.DB.Delete(&models.Session{}, "token_hash = ?", old.TokenHash)
	}
	ns := newSession(c, 0)
	c.Set("session", ns)
	c.Set("user", (*models.User)(nil))
	setFlash(c, "ok", "ออกจากระบบแล้ว")
	c.Redirect(http.StatusSeeOther, "/")
}
