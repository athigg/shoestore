package main

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"shoestore/config"
	"shoestore/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	errOutOfStock  = errors.New("สินค้าไซส์นี้ไม่พอต่อจำนวนที่ต้องการ")
	errNotPending  = errors.New("ออเดอร์นี้ไม่อยู่ในสถานะรอดำเนินการ")
	errCancelledSt = errors.New("ออเดอร์ที่ยกเลิกแล้วไม่สามารถแก้ไขได้")
)

// ---------- หน้าแรก / รายละเอียดสินค้า ----------

func homePage(c *gin.Context) {
	search := truncate(strings.TrimSpace(c.Query("search")), 100)
	brand := truncate(strings.TrimSpace(c.Query("brand")), 100)

	var shoes []models.Shoe
	q := config.DB.Model(&models.Shoe{}).Preload("Sizes", bySize).Order("id DESC")
	if search != "" {
		q = q.Where("name LIKE ?", "%"+search+"%")
	}
	if brand != "" {
		q = q.Where("brand = ?", brand)
	}
	q.Find(&shoes)

	var brands []string
	config.DB.Model(&models.Shoe{}).Distinct().Pluck("brand", &brands)

	render(c, http.StatusOK, "index.html", gin.H{
		"shoes":         shoes,
		"brands":        brands,
		"search":        search,
		"selectedBrand": brand,
	})
}

func shoeDetail(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		renderError(c, http.StatusNotFound, "ไม่พบสินค้า")
		return
	}
	showShoe(c, http.StatusOK, id, nil)
}

// showShoe สร้างหน้ารายละเอียดสินค้า (ใช้ซ้ำตอนฟอร์มสั่งซื้อ/รีวิวมีข้อผิดพลาด)
func showShoe(c *gin.Context, status int, id uint, extra gin.H) {
	var shoe models.Shoe
	if err := config.DB.Preload("Sizes", bySize).First(&shoe, id).Error; err != nil {
		renderError(c, http.StatusNotFound, "ไม่พบสินค้า")
		return
	}

	var reviews []models.Review
	config.DB.Preload("User", unscoped).Where("shoe_id = ?", id).Order("id DESC").Find(&reviews)

	total := 0
	for _, r := range reviews {
		total += r.Rating
	}
	avg := 0.0
	if len(reviews) > 0 {
		avg = float64(total) / float64(len(reviews))
	}

	hasReviewed := false
	if u := currentUser(c); u != nil {
		for _, r := range reviews {
			if r.UserID == u.ID {
				hasReviewed = true
			}
		}
	}

	data := gin.H{
		"shoe":        shoe,
		"reviews":     reviews,
		"avg":         avg,
		"avgInt":      int(math.Round(avg)),
		"reviewCount": len(reviews),
		"hasReviewed": hasReviewed,
		"selSize":     0,
		"qty":         1, // ค่าเริ่มต้นของจำนวน
		"addr":        "",
		"phone":       "",
		"rating":      5,
		"comment":     "",
		"title":       shoe.Name,
	}
	for k, v := range extra {
		data[k] = v
	}
	render(c, status, "shoe.html", data)
}

// ---------- ฟอร์มที่ 1: สั่งซื้อ (ที่อยู่จัดส่ง + เบอร์โทร) ----------

func placeOrder(c *gin.Context) {
	u := currentUser(c)
	if u.Role == models.RoleAdmin {
		renderError(c, http.StatusForbidden, "บัญชีผู้ดูแลระบบไม่สามารถสั่งซื้อสินค้าได้")
		return
	}

	shoeID64, err := strconv.ParseUint(c.PostForm("shoe_id"), 10, 32)
	if err != nil || shoeID64 == 0 {
		renderError(c, http.StatusBadRequest, "ข้อมูลสินค้าไม่ถูกต้อง")
		return
	}
	shoeID := uint(shoeID64)
	size, _ := strconv.Atoi(strings.TrimSpace(c.PostForm("size")))

	// รับค่าจำนวนและตั้งค่าป้องกันการกรอกผิด
	quantity, _ := strconv.Atoi(strings.TrimSpace(c.PostForm("quantity")))
	if quantity < 1 {
		quantity = 1
	}

	addr := strings.TrimSpace(c.PostForm("address"))
	phone := strings.TrimSpace(c.PostForm("phone"))

	fail := func(msg string) {
		showShoe(c, http.StatusBadRequest, shoeID, gin.H{"orderError": msg, "selSize": size, "qty": quantity, "addr": addr, "phone": phone})
	}

	if size < 30 || size > 60 {
		fail("กรุณาเลือกไซส์รองเท้า")
		return
	}
	if quantity > 10 { // ป้องกันคนสั่งเล่นทีละมากๆ สามารถแก้เป็นตัวเลขอื่นได้
		fail("สั่งซื้อได้สูงสุด 10 คู่ต่อ 1 คำสั่งซื้อ")
		return
	}
	if n := runeLen(addr); n < 10 || n > 300 {
		fail("ที่อยู่จัดส่งต้องมีความยาว 10-300 ตัวอักษร")
		return
	}
	if !phoneRe.MatchString(phone) {
		fail("เบอร์โทรไม่ถูกต้อง (ตัวเลข 8-15 หลัก)")
		return
	}

	var shoe models.Shoe
	if err := config.DB.First(&shoe, shoeID).Error; err != nil {
		renderError(c, http.StatusNotFound, "ไม่พบสินค้า")
		return
	}

	// คำนวณราคารวม (ราคารองเท้าต่อคู่ * จำนวน)
	totalPrice := shoe.Price * float64(quantity)

	// หักสต็อกแบบ atomic ตามจำนวน quantity ที่สั่ง
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.ShoeSize{}).
			Where("shoe_id = ? AND size = ? AND stock >= ?", shoe.ID, size, quantity).
			Update("stock", gorm.Expr("stock - ?", quantity))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errOutOfStock
		}
		order := models.Order{
			UserID: u.ID, ShoeID: shoe.ID, Size: size, Quantity: quantity, // บันทึกจำนวน
			Price:   totalPrice, // บันทึกราคารวม
			Address: addr, Phone: phone, Status: "pending",
		}
		return tx.Create(&order).Error
	})
	if err != nil {
		if errors.Is(err, errOutOfStock) {
			fail(err.Error())
		} else {
			renderError(c, http.StatusInternalServerError, "ไม่สามารถบันทึกคำสั่งซื้อได้")
		}
		return
	}
	setFlash(c, "ok", "สั่งซื้อสำเร็จ! ขอบคุณที่ใช้บริการ")
	c.Redirect(http.StatusSeeOther, "/my-orders")
}

func myOrders(c *gin.Context) {
	u := currentUser(c)
	var orders []models.Order
	config.DB.Preload("Shoe", unscoped).Where("user_id = ?", u.ID).Order("id DESC").Find(&orders)
	render(c, http.StatusOK, "my_orders.html", gin.H{"orders": orders})
}

func cancelOrder(c *gin.Context) {
	u := currentUser(c)
	id, ok := paramID(c)
	var order models.Order
	// ตรวจความเป็นเจ้าของ: ต้องเป็นออเดอร์ของผู้ใช้คนนี้เท่านั้น
	if !ok || config.DB.Where("id = ? AND user_id = ?", id, u.ID).First(&order).Error != nil {
		renderError(c, http.StatusNotFound, "ไม่พบคำสั่งซื้อ")
		return
	}
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.Order{}).Where("id = ? AND status = ?", order.ID, "pending").Update("status", "cancelled")
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errNotPending
		}

		// คืนสต็อกตามจำนวน quantity ของออเดอร์นั้น
		return tx.Model(&models.ShoeSize{}).
			Where("shoe_id = ? AND size = ?", order.ShoeID, order.Size).
			Update("stock", gorm.Expr("stock + ?", order.Quantity)).Error
	})
	if err != nil {
		setFlash(c, "err", err.Error())
	} else {
		setFlash(c, "ok", "ยกเลิกคำสั่งซื้อเรียบร้อยแล้ว")
	}
	c.Redirect(http.StatusSeeOther, "/my-orders")
}

// ---------- ฟอร์มที่ 2: รีวิวสินค้า (CRUD ที่มีเจ้าของ) ----------

func parseReview(c *gin.Context) (int, string, string) {
	rating, _ := strconv.Atoi(c.PostForm("rating"))
	comment := strings.TrimSpace(c.PostForm("comment"))
	if rating < 1 || rating > 5 {
		return rating, comment, "กรุณาเลือกคะแนน 1-5 ดาว"
	}
	if n := runeLen(comment); n < 1 || n > 500 {
		return rating, comment, "ความคิดเห็นต้องมีความยาว 1-500 ตัวอักษร"
	}
	return rating, comment, ""
}

func createReview(c *gin.Context) {
	u := currentUser(c)
	shoeID, ok := paramID(c)
	if !ok {
		renderError(c, http.StatusNotFound, "ไม่พบสินค้า")
		return
	}
	var shoe models.Shoe
	if err := config.DB.First(&shoe, shoeID).Error; err != nil {
		renderError(c, http.StatusNotFound, "ไม่พบสินค้า")
		return
	}
	rating, comment, msg := parseReview(c)
	if msg != "" {
		showShoe(c, http.StatusBadRequest, shoeID, gin.H{"reviewError": msg, "rating": rating, "comment": comment})
		return
	}
	var count int64
	config.DB.Model(&models.Review{}).Where("user_id = ? AND shoe_id = ?", u.ID, shoeID).Count(&count)
	if count > 0 {
		showShoe(c, http.StatusBadRequest, shoeID, gin.H{"reviewError": "คุณรีวิวสินค้านี้ไปแล้ว (สามารถแก้ไขรีวิวเดิมได้)", "rating": rating, "comment": comment})
		return
	}
	review := models.Review{UserID: u.ID, ShoeID: shoeID, Rating: rating, Comment: comment}
	if err := config.DB.Create(&review).Error; err != nil {
		renderError(c, http.StatusInternalServerError, "ไม่สามารถบันทึกรีวิวได้")
		return
	}
	setFlash(c, "ok", "บันทึกรีวิวเรียบร้อยแล้ว")
	c.Redirect(http.StatusSeeOther, "/shoe/"+strconv.FormatUint(uint64(shoeID), 10))
}

// findOwnReview หารีวิวที่เป็นของผู้ใช้ปัจจุบันเท่านั้น (กัน IDOR)
func findOwnReview(c *gin.Context) (*models.Review, bool) {
	u := currentUser(c)
	id, ok := paramID(c)
	var rv models.Review
	if !ok || config.DB.Where("id = ? AND user_id = ?", id, u.ID).First(&rv).Error != nil {
		renderError(c, http.StatusNotFound, "ไม่พบรีวิว หรือคุณไม่มีสิทธิ์แก้ไขรีวิวนี้")
		return nil, false
	}
	return &rv, true
}

func editReviewPage(c *gin.Context) {
	rv, ok := findOwnReview(c)
	if !ok {
		return
	}
	render(c, http.StatusOK, "edit_review.html", gin.H{"review": rv})
}

func updateReview(c *gin.Context) {
	rv, ok := findOwnReview(c)
	if !ok {
		return
	}
	rating, comment, msg := parseReview(c)
	if msg != "" {
		rv.Rating, rv.Comment = rating, comment
		render(c, http.StatusBadRequest, "edit_review.html", gin.H{"review": rv, "error": msg})
		return
	}
	config.DB.Model(rv).Updates(map[string]interface{}{"rating": rating, "comment": comment})
	setFlash(c, "ok", "แก้ไขรีวิวเรียบร้อยแล้ว")
	c.Redirect(http.StatusSeeOther, "/shoe/"+strconv.FormatUint(uint64(rv.ShoeID), 10))
}

func deleteReview(c *gin.Context) {
	u := currentUser(c)
	id, ok := paramID(c)
	var rv models.Review
	q := config.DB.Where("id = ?", id)
	if u.Role != models.RoleAdmin { // ผู้ใช้ทั่วไปลบได้เฉพาะของตัวเอง, แอดมินลบได้ทุกรีวิว (ดูแลเนื้อหา)
		q = q.Where("user_id = ?", u.ID)
	}
	if !ok || q.First(&rv).Error != nil {
		renderError(c, http.StatusNotFound, "ไม่พบรีวิว หรือคุณไม่มีสิทธิ์ลบรีวิวนี้")
		return
	}
	config.DB.Delete(&rv)
	setFlash(c, "ok", "ลบรีวิวเรียบร้อยแล้ว")
	c.Redirect(http.StatusSeeOther, "/shoe/"+strconv.FormatUint(uint64(rv.ShoeID), 10))
}
