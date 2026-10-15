package main

import (
	"errors"
	"net/http"
	"strings"

	"shoestore/config"
	"shoestore/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ---------- จัดการสินค้า ----------

func renderAdminPanel(c *gin.Context, status int, extra gin.H) {
	var shoes []models.Shoe
	config.DB.Preload("Sizes", bySize).Order("id DESC").Find(&shoes)

	var users, orders, shoeCount int64
	config.DB.Model(&models.User{}).Count(&users)
	config.DB.Model(&models.Order{}).Count(&orders)
	config.DB.Model(&models.Shoe{}).Count(&shoeCount)

	data := gin.H{
		"shoes": shoes, "sizes": shoeSizes,
		"nUsers": users, "nOrders": orders, "nShoes": shoeCount,
		"fName": "", "fBrand": "", "fPrice": "", "fDesc": "", "fImageURL": "",
	}
	for k, v := range extra {
		data[k] = v
	}
	render(c, status, "admin_panel.html", data)
}

func adminPanel(c *gin.Context) {
	renderAdminPanel(c, http.StatusOK, nil)
}

func adminShoeAdd(c *gin.Context) {
	form, msg := parseShoeForm(c)
	var imageURL string
	if msg == "" {
		var err error
		imageURL, err = saveImage(c, "image")
		if err != nil {
			msg = err.Error()
		}
	}
	if msg != "" {
		renderAdminPanel(c, http.StatusBadRequest, gin.H{
			"error": msg, "fName": form.Name, "fBrand": form.Brand,
			"fPrice": c.PostForm("price"), "fDesc": form.Description,
		})
		return
	}

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		shoe := models.Shoe{Name: form.Name, Brand: form.Brand, Price: form.Price, Description: form.Description, ImageURL: imageURL}
		if err := tx.Create(&shoe).Error; err != nil {
			return err
		}
		for i, sz := range shoeSizes {
			if err := tx.Create(&models.ShoeSize{ShoeID: shoe.ID, Size: sz, Stock: form.Stocks[i]}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		renderError(c, http.StatusInternalServerError, "ไม่สามารถเพิ่มสินค้าได้")
		return
	}
	setFlash(c, "ok", "เพิ่มสินค้าเรียบร้อยแล้ว")
	c.Redirect(http.StatusSeeOther, "/admin/panel")
}

func renderEditShoe(c *gin.Context, status int, shoe models.Shoe, msg string) {
	sizeMap := make(map[int]int)
	for _, s := range shoe.Sizes {
		sizeMap[s.Size] = s.Stock
	}
	render(c, status, "edit_shoe.html", gin.H{"shoe": shoe, "sizeMap": sizeMap, "sizes": shoeSizes, "error": msg})
}

func adminShoeEditPage(c *gin.Context) {
	id, ok := paramID(c)
	var shoe models.Shoe
	if !ok || config.DB.Preload("Sizes", bySize).First(&shoe, id).Error != nil {
		renderError(c, http.StatusNotFound, "ไม่พบสินค้า")
		return
	}
	renderEditShoe(c, http.StatusOK, shoe, "")
}

func adminShoeUpdate(c *gin.Context) {
	id, ok := paramID(c)
	var shoe models.Shoe
	if !ok || config.DB.Preload("Sizes", bySize).First(&shoe, id).Error != nil {
		renderError(c, http.StatusNotFound, "ไม่พบสินค้า")
		return
	}

	form, msg := parseShoeForm(c)
	var newImage string
	if msg == "" {
		var err error
		newImage, err = saveImage(c, "image")
		if err != nil {
			msg = err.Error()
		}
	}
	if msg != "" {
		renderEditShoe(c, http.StatusBadRequest, shoe, msg)
		return
	}

	oldImage := shoe.ImageURL
	shoe.Name, shoe.Brand, shoe.Price, shoe.Description = form.Name, form.Brand, form.Price, form.Description
	if newImage != "" {
		shoe.ImageURL = newImage
	}

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Shoe{}).Where("id = ?", shoe.ID).Updates(map[string]interface{}{
			"name": shoe.Name, "brand": shoe.Brand, "price": shoe.Price,
			"description": shoe.Description, "image_url": shoe.ImageURL,
		}).Error; err != nil {
			return err
		}
		for i, sz := range shoeSizes {
			var ss models.ShoeSize
			e := tx.Where("shoe_id = ? AND size = ?", shoe.ID, sz).First(&ss).Error
			switch {
			case e == nil:
				if err := tx.Model(&ss).Update("stock", form.Stocks[i]).Error; err != nil {
					return err
				}
			case errors.Is(e, gorm.ErrRecordNotFound):
				if err := tx.Create(&models.ShoeSize{ShoeID: shoe.ID, Size: sz, Stock: form.Stocks[i]}).Error; err != nil {
					return err
				}
			default:
				return e
			}
		}
		return nil
	})
	if err != nil {
		renderError(c, http.StatusInternalServerError, "ไม่สามารถแก้ไขสินค้าได้")
		return
	}
	if newImage != "" {
		removeImage(oldImage)
	}
	setFlash(c, "ok", "แก้ไขสินค้าเรียบร้อยแล้ว")
	c.Redirect(http.StatusSeeOther, "/admin/panel")
}

func adminShoeDelete(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		renderError(c, http.StatusNotFound, "ไม่พบสินค้า")
		return
	}
	config.DB.Delete(&models.Shoe{}, id)
	setFlash(c, "ok", "ลบสินค้าเรียบร้อยแล้ว")
	c.Redirect(http.StatusSeeOther, "/admin/panel")
}

// ---------- จัดการออเดอร์ ----------

func adminOrders(c *gin.Context) {
	var orders []models.Order
	config.DB.Preload("User", unscoped).Preload("Shoe", unscoped).Order("id DESC").Find(&orders)
	render(c, http.StatusOK, "admin_orders.html", gin.H{"orders": orders})
}

func adminOrderStatus(c *gin.Context) {
	id, ok := paramID(c)
	status := c.PostForm("status")
	if !ok || (status != "pending" && status != "shipped" && status != "completed" && status != "cancelled") {
		renderError(c, http.StatusBadRequest, "ข้อมูลไม่ถูกต้อง")
		return
	}
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var o models.Order
		if err := tx.First(&o, id).Error; err != nil {
			return err
		}
		if o.Status == "cancelled" {
			return errCancelledSt
		}
		if status == "cancelled" {
			if err := restoreStock(tx, o.ShoeID, o.Size); err != nil {
				return err
			}
		}
		return tx.Model(&o).Update("status", status).Error
	})
	if err != nil {
		if errors.Is(err, errCancelledSt) {
			setFlash(c, "err", err.Error())
		} else {
			setFlash(c, "err", "ไม่สามารถอัปเดตสถานะได้")
		}
	} else {
		setFlash(c, "ok", "อัปเดตสถานะออเดอร์เรียบร้อยแล้ว")
	}
	c.Redirect(http.StatusSeeOther, "/admin/orders")
}

func adminOrderDelete(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		renderError(c, http.StatusNotFound, "ไม่พบออเดอร์")
		return
	}
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var o models.Order
		if err := tx.First(&o, id).Error; err != nil {
			return err
		}
		if o.Status == "pending" || o.Status == "shipped" {
			if err := restoreStock(tx, o.ShoeID, o.Size); err != nil {
				return err
			}
		}
		return tx.Delete(&o).Error
	})
	if err != nil {
		setFlash(c, "err", "ไม่สามารถลบออเดอร์ได้")
	} else {
		setFlash(c, "ok", "ลบออเดอร์เรียบร้อยแล้ว")
	}
	c.Redirect(http.StatusSeeOther, "/admin/orders")
}

// ---------- จัดการผู้ใช้ ----------

func adminUsers(c *gin.Context) {
	var users []models.User
	config.DB.Order("id ASC").Find(&users)
	render(c, http.StatusOK, "admin_users.html", gin.H{"users": users})
}

func adminUserEditPage(c *gin.Context) {
	id, ok := paramID(c)
	var target models.User
	if !ok || config.DB.First(&target, id).Error != nil {
		renderError(c, http.StatusNotFound, "ไม่พบผู้ใช้")
		return
	}
	render(c, http.StatusOK, "edit_user.html", gin.H{"target": target})
}

func adminUserUpdate(c *gin.Context) {
	me := currentUser(c)
	id, ok := paramID(c)
	var target models.User
	if !ok || config.DB.First(&target, id).Error != nil {
		renderError(c, http.StatusNotFound, "ไม่พบผู้ใช้")
		return
	}

	username := strings.TrimSpace(c.PostForm("username"))
	email := strings.ToLower(strings.TrimSpace(c.PostForm("email")))
	role := c.PostForm("role")

	fail := func(msg string) {
		render(c, http.StatusBadRequest, "edit_user.html", gin.H{"target": target, "error": msg})
	}
	if msg := validateAccount(username, email); msg != "" {
		fail(msg)
		return
	}
	if role != models.RoleAdmin && role != models.RoleUser {
		fail("บทบาทไม่ถูกต้อง")
		return
	}
	if target.ID == me.ID && role != models.RoleAdmin {
		fail("ไม่สามารถลดสิทธิ์บัญชีของตัวเองได้")
		return
	}
	var dup int64
	config.DB.Unscoped().Model(&models.User{}).
		Where("(username = ? OR email = ?) AND id <> ?", username, email, target.ID).Count(&dup)
	if dup > 0 {
		fail("ชื่อผู้ใช้หรืออีเมลนี้ถูกใช้งานแล้ว")
		return
	}
	if err := config.DB.Model(&target).Updates(map[string]interface{}{
		"username": username, "email": email, "role": role,
	}).Error; err != nil {
		fail("ไม่สามารถบันทึกข้อมูลได้")
		return
	}
	setFlash(c, "ok", "แก้ไขข้อมูลผู้ใช้เรียบร้อยแล้ว")
	c.Redirect(http.StatusSeeOther, "/admin/users")
}

func adminUserDelete(c *gin.Context) {
	me := currentUser(c)
	id, ok := paramID(c)
	if !ok {
		renderError(c, http.StatusNotFound, "ไม่พบผู้ใช้")
		return
	}
	if id == me.ID {
		setFlash(c, "err", "ไม่สามารถลบบัญชีของตัวเองได้")
		c.Redirect(http.StatusSeeOther, "/admin/users")
		return
	}
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", id).Delete(&models.Session{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.User{}, id).Error
	})
	if err != nil {
		setFlash(c, "err", "ไม่สามารถลบผู้ใช้ได้")
	} else {
		setFlash(c, "ok", "ลบผู้ใช้เรียบร้อยแล้ว")
	}
	c.Redirect(http.StatusSeeOther, "/admin/users")
}
