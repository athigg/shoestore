package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"shoestore/config"
	"shoestore/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// seedAdmin สร้างบัญชีแอดมินเริ่มต้นเมื่อยังไม่มีแอดมินในระบบ
// (แทนวิธีเดิมที่ "คนแรกที่สมัครได้เป็นแอดมิน" ซึ่งใครมาก่อนก็ยึดระบบได้)
func seedAdmin() {
	var count int64
	config.DB.Model(&models.User{}).Where("role = ?", models.RoleAdmin).Count(&count)
	if count > 0 {
		return
	}
	username := config.Getenv("ADMIN_USERNAME", "admin")
	email := config.Getenv("ADMIN_EMAIL", "admin@example.com")
	password := os.Getenv("ADMIN_PASSWORD")
	generated := false
	if len(password) < 8 {
		password = randomToken(9)
		generated = true
	}
	u := models.User{Username: username, Email: email, Role: models.RoleAdmin}
	if err := u.HashPassword(password); err != nil {
		log.Fatal(err)
	}
	if err := config.DB.Create(&u).Error; err != nil {
		log.Println("ไม่สามารถสร้างบัญชีแอดมินเริ่มต้นได้:", err)
		return
	}
	if generated {
		log.Printf("สร้างบัญชีแอดมินแล้ว  username=%s  password=%s  (แสดงครั้งเดียว กรุณาเปลี่ยน/จดไว้)", username, password)
	} else {
		log.Printf("สร้างบัญชีแอดมินแล้ว  username=%s", username)
	}
}

func startCleanup() {
	go func() {
		t := time.NewTicker(time.Hour)
		for range t.C {
			config.DB.Where("expires_at < ?", time.Now()).Delete(&models.Session{})
			loginUserLimiter.purge()
			loginIPLimiter.purge()
			registerLimiter.purge()
		}
	}()
}

func main() {
	secureCookies = config.Getenv("COOKIE_SECURE", "false") == "true"
	if err := os.MkdirAll("uploads", 0o755); err != nil {
		log.Fatal(err)
	}

	config.ConnectDB()
	if err := config.DB.AutoMigrate(
		&models.User{}, &models.Shoe{}, &models.ShoeSize{},
		&models.Order{}, &models.Review{}, &models.Session{},
	); err != nil {
		log.Fatal("AutoMigrate failed: ", err)
	}
	seedAdmin()
	startCleanup()

	var err error
	dummyHash, err = bcrypt.GenerateFromPassword([]byte(randomToken(12)), models.BcryptCost)
	if err != nil {
		log.Fatal(err)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), securityHeaders())

	// เชื่อถือ X-Forwarded-For เฉพาะ proxy ที่ระบุเท่านั้น (เช่น TRUSTED_PROXIES=127.0.0.1)
	var proxies []string
	if v := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES")); v != "" {
		for _, p := range strings.Split(v, ",") {
			proxies = append(proxies, strings.TrimSpace(p))
		}
	}
	if err := r.SetTrustedProxies(proxies); err != nil {
		log.Fatal(err)
	}

	r.MaxMultipartMemory = 8 << 20
	r.SetFuncMap(template.FuncMap(funcMap))
	r.LoadHTMLGlob("templates/*")

	r.Static("/static", "./static")
	r.Static("/uploads", "./uploads")

	r.Use(sessionMiddleware(), csrfMiddleware())

	// ---- สาธารณะ ----
	r.GET("/", homePage)
	r.GET("/shoe/:id", shoeDetail)
	r.GET("/login", loginPage)
	r.POST("/login", loginSubmit)
	r.GET("/register", registerPage)
	r.POST("/register", registerSubmit)
	r.POST("/logout", logoutSubmit)

	// ---- ต้องล็อกอิน (user และ admin) ----
	auth := r.Group("", requireLogin())
	auth.POST("/order", placeOrder)
	auth.GET("/my-orders", myOrders)
	auth.POST("/my-orders/:id/cancel", cancelOrder)
	auth.POST("/shoe/:id/review", createReview)
	auth.GET("/review/:id/edit", editReviewPage)
	auth.POST("/review/:id/update", updateReview)
	auth.POST("/review/:id/delete", deleteReview)

	// ---- เฉพาะผู้ดูแลระบบ ----
	admin := r.Group("/admin", requireAdmin())
	admin.GET("", func(c *gin.Context) { c.Redirect(http.StatusSeeOther, "/admin/panel") })
	admin.GET("/panel", adminPanel)
	admin.POST("/shoe/add", adminShoeAdd)
	admin.GET("/shoe/:id/edit", adminShoeEditPage)
	admin.POST("/shoe/:id/update", adminShoeUpdate)
	admin.POST("/shoe/:id/delete", adminShoeDelete)
	admin.GET("/orders", adminOrders)
	admin.POST("/order/:id/status", adminOrderStatus)
	admin.POST("/order/:id/delete", adminOrderDelete)
	admin.GET("/users", adminUsers)
	admin.GET("/user/:id/edit", adminUserEditPage)
	admin.POST("/user/:id/update", adminUserUpdate)
	admin.POST("/user/:id/delete", adminUserDelete)

	r.NoRoute(func(c *gin.Context) {
		renderError(c, http.StatusNotFound, "ไม่พบหน้าที่คุณต้องการ")
	})

	port := config.Getenv("PORT", "8080")
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Println("Shoestore กำลังทำงานที่พอร์ต", port)
	log.Fatal(srv.ListenAndServe())
}
