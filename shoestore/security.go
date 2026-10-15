package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"shoestore/config"
	"shoestore/models"

	"github.com/gin-gonic/gin"
)

const (
	cookieName   = "sid"
	sessionTTL   = 7 * 24 * time.Hour
	maxBodyBytes = 6 << 20 // 6 MB ต่อ request
)

var (
	secureCookies bool
	dummyHash     []byte // ใช้เทียบรหัสผ่านตอนไม่พบผู้ใช้ เพื่อให้เวลาตอบสนองเท่ากัน

	loginUserLimiter = newLimiter(5, 15*time.Minute)
	loginIPLimiter   = newLimiter(30, 15*time.Minute)
	registerLimiter  = newLimiter(10, time.Hour)
)

// ---------- token ----------

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// ---------- rate limiter (กัน brute force) ----------

type attempt struct {
	count int
	first time.Time
}

type limiter struct {
	mu     sync.Mutex
	m      map[string]*attempt
	limit  int
	window time.Duration
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{m: make(map[string]*attempt), limit: limit, window: window}
}

func (l *limiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[key]
	if !ok {
		return false
	}
	if time.Since(a.first) > l.window {
		delete(l.m, key)
		return false
	}
	return a.count >= l.limit
}

func (l *limiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[key]
	if !ok || time.Since(a.first) > l.window {
		l.m[key] = &attempt{count: 1, first: time.Now()}
		return
	}
	a.count++
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}

func (l *limiter) purge() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, a := range l.m {
		if time.Since(a.first) > l.window {
			delete(l.m, k)
		}
	}
}

// ---------- security headers ----------

func securityHeaders() gin.HandlerFunc {
	csp := "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; " +
		"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if secureCookies {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// ---------- session ----------

func newSession(c *gin.Context, userID uint) *models.Session {
	token := randomToken(32)
	s := &models.Session{
		TokenHash: hashToken(token),
		UserID:    userID,
		CSRF:      randomToken(32),
		ExpiresAt: time.Now().Add(sessionTTL),
	}
	config.DB.Create(s)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	return s
}

func sessionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")

		var sess *models.Session
		if token, err := c.Cookie(cookieName); err == nil && token != "" {
			var s models.Session
			if config.DB.Where("token_hash = ? AND expires_at > ?", hashToken(token), time.Now()).First(&s).Error == nil {
				sess = &s
			}
		}
		if sess == nil {
			sess = newSession(c, 0)
		}
		c.Set("session", sess)

		if sess.UserID != 0 {
			var u models.User
			if config.DB.First(&u, sess.UserID).Error == nil {
				c.Set("user", &u)
			}
		}
		c.Next()
	}
}

func currentSession(c *gin.Context) *models.Session {
	v, _ := c.Get("session")
	s, _ := v.(*models.Session)
	return s
}

func currentUser(c *gin.Context) *models.User {
	v, _ := c.Get("user")
	u, _ := v.(*models.User)
	return u
}

// ---------- CSRF ----------

func csrfMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
		s := currentSession(c)
		token := c.PostForm("csrf_token")
		if s == nil || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.CSRF)) != 1 {
			renderError(c, http.StatusForbidden, "โทเคนความปลอดภัย (CSRF) ไม่ถูกต้องหรือหมดอายุ กรุณากลับไปโหลดหน้าเว็บใหม่แล้วลองอีกครั้ง")
			c.Abort()
			return
		}
		c.Next()
	}
}

// ---------- สิทธิ์การเข้าถึง ----------

func requireLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if currentUser(c) == nil {
			c.Redirect(http.StatusSeeOther, "/login")
			c.Abort()
			return
		}
		c.Next()
	}
}

func requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		if u == nil {
			c.Redirect(http.StatusSeeOther, "/login")
			c.Abort()
			return
		}
		if u.Role != models.RoleAdmin {
			renderError(c, http.StatusForbidden, "คุณไม่มีสิทธิ์เข้าถึงหน้านี้ (เฉพาะผู้ดูแลระบบ)")
			c.Abort()
			return
		}
		c.Next()
	}
}
