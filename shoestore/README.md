# Shoestore – ร้านรองเท้าออนไลน์ (Go + Gin + GORM + MySQL)

## 1) ตรวจตามเงื่อนไขขั้นต่ำ

| เงื่อนไข | ทำที่ไหน |
|---|---|
| สมัคร/เข้าสู่ระบบ + hashing | `auth.go`, `models/models.go` – **bcrypt cost 12** (มี salt ในตัว), ตรวจความยาว/ความซับซ้อนรหัสผ่าน |
| 2 บทบาท | `user` (ดู/สั่งซื้อ/รีวิว/ยกเลิกออเดอร์ตัวเอง) และ `admin` (จัดการสินค้า ออเดอร์ ผู้ใช้) – บังคับด้วย middleware `requireLogin` / `requireAdmin` ฝั่งเซิร์ฟเวอร์ |
| CRUD ที่มีเจ้าของ | **รีวิวสินค้า** (`shop.go`) – สร้าง/อ่าน/แก้ไข/ลบ โดยค้นด้วย `id AND user_id = ผู้ใช้ปัจจุบัน` ผู้ใช้ A จึงแก้/ลบรีวิวของ B ไม่ได้ (แอดมินลบได้เพื่อดูแลเนื้อหา) และ **ยกเลิกออเดอร์** ได้เฉพาะของตัวเอง |
| ฟอร์ม ≥ 2 ฟอร์ม บันทึก DB และแสดงผลกลับ | ฟอร์มสั่งซื้อ (ที่อยู่/เบอร์โทร → หน้า "คำสั่งซื้อของฉัน"), ฟอร์มรีวิว (→ หน้ารายละเอียดสินค้า), ฟอร์มสมัครสมาชิก, ฟอร์มเพิ่ม/แก้ไขสินค้า |
| หน้าผู้ดูแลระบบ | `/admin/panel`, `/admin/orders`, `/admin/users` |

## 2) การป้องกันการโจมตีที่ทำไว้

| ภัยคุกคาม | วิธีป้องกัน |
|---|---|
| เก็บรหัสผ่านรั่ว | bcrypt cost 12, ไม่เก็บ plaintext |
| Session ปนกันระหว่างผู้ใช้ (บั๊กเดิม) | session ต่อผู้ใช้ด้วย token สุ่ม 256 บิต เก็บเฉพาะ SHA-256 ใน DB, cookie `HttpOnly` + `SameSite=Lax` + `Secure` (เมื่อเปิด HTTPS), หมดอายุ 7 วัน |
| Session fixation | สร้าง session ใหม่ทุกครั้งที่ล็อกอิน/ออกจากระบบ |
| CSRF | CSRF token ต่อ session ในทุกฟอร์ม POST (ตรวจแบบ constant-time), การลบ/ออกจากระบบเปลี่ยนจาก GET เป็น POST |
| SQL Injection | GORM + parameterized query ทุกจุด |
| XSS | `html/template` escape อัตโนมัติ + Content-Security-Policy (`script-src 'self'`, ไม่มี inline script) |
| IDOR / ยกระดับสิทธิ์ | ตรวจความเป็นเจ้าของใน query, สมัครเองได้เป็น `user` เท่านั้น, แอดมินลดสิทธิ์/ลบบัญชีตัวเองไม่ได้, แอดมินเริ่มต้นสร้างจาก env (เลิก "คนแรกที่สมัครเป็นแอดมิน") |
| Brute force | จำกัดการล็อกอินผิด 5 ครั้ง/15 นาที ต่อชื่อผู้ใช้ (และ 30 ครั้งต่อ IP), จำกัดการสมัคร 10 ครั้ง/ชม./IP |
| User enumeration | ข้อความล็อกอินผิดแบบเดียวกันเสมอ + คำนวณ bcrypt แม้ไม่พบผู้ใช้ |
| อัปโหลดไฟล์อันตราย | ตรวจชนิดจากเนื้อไฟล์จริง (JPG/PNG/GIF/WEBP), ≤ 3 MB, ตั้งชื่อไฟล์สุ่มเอง, `X-Content-Type-Options: nosniff` |
| ขายเกินสต็อก (race condition) | หักสต็อกแบบ atomic `stock = stock - 1 WHERE stock > 0` ใน transaction |
| แก้ราคาผ่านฟอร์ม | ราคาอ่านจาก DB เสมอ |
| Input ผิดปกติ | validate ความยาว/รูปแบบ/ช่วงตัวเลขทุกฟิลด์, จำกัดขนาด request 6 MB |
| Clickjacking / อื่น ๆ | `X-Frame-Options: DENY`, `frame-ancestors 'none'`, `Referrer-Policy`, HSTS (เมื่อเปิด HTTPS), server timeouts |
| ความลับรั่วในโค้ด | ค่า DB/แอดมินอ่านจาก environment variable |

## 3) วิธีรันผ่าน cmd (Windows)

ต้องมี Go และ MySQL (เช่น XAMPP) เปิดอยู่

```cmd
cd shoestore
go mod tidy
set DB_USER=root
set DB_PASS=
set ADMIN_PASSWORD=ChangeMe12345
go run .
```

หรือ build เป็น exe แล้วรัน: `go build -o shoestore.exe .` แล้วพิมพ์ `start.bat`
เปิดเบราว์เซอร์ที่ http://localhost:8080 ล็อกอินแอดมินด้วย `admin` / ค่า `ADMIN_PASSWORD`
(ถ้าไม่ตั้ง `ADMIN_PASSWORD` ระบบจะสุ่มรหัสให้และพิมพ์ในหน้าต่าง cmd ครั้งเดียว)
ระบบสร้างฐานข้อมูล/ตารางให้อัตโนมัติ และใช้ข้อมูลเดิมที่มีอยู่ได้

## 4) ขึ้นโฮสต์

**ทางเลือก A – VPS / Windows Server ที่เปิดพอร์ตได้**
1. ติดตั้ง Go และ MySQL บนเซิร์ฟเวอร์ แล้ว copy โฟลเดอร์โปรเจ็กต์ไปวาง
2. แก้ค่าใน `start.bat` (รหัสผ่าน DB, `ADMIN_PASSWORD`) แล้วรัน `start.bat` ผ่าน cmd
3. เปิดพอร์ต 8080 ใน firewall → เข้าด้วย `http://IP-เซิร์ฟเวอร์:8080`
4. ถ้ามีโดเมน ให้ใส่ reverse proxy (nginx/Caddy) ทำ HTTPS แล้วตั้ง `COOKIE_SECURE=true` และ `TRUSTED_PROXIES=127.0.0.1`

**ทางเลือก B – Docker (Render / Railway / Fly.io ฯลฯ)**
1. push โปรเจ็กต์ขึ้น GitHub (มี `Dockerfile` ให้แล้ว) → สร้าง Web Service แบบ Docker
2. สร้างฐานข้อมูล MySQL ของผู้ให้บริการ แล้วตั้ง env: `DB_HOST DB_PORT DB_USER DB_PASS DB_NAME` (หรือ `DATABASE_DSN`), `DB_TLS=true` ถ้าบังคับ TLS, `ADMIN_PASSWORD`, `COOKIE_SECURE=true`
3. ผูก Persistent Disk ที่ `/app/uploads` ไม่เช่นนั้นรูปที่อัปโหลดจะหายเมื่อรีสตาร์ต

> ควรตั้ง `ADMIN_PASSWORD` ใหม่ที่เดายากก่อนขึ้นโฮสต์จริง และอย่าใช้ MySQL `root` แบบไม่มีรหัสผ่านนอกเครื่องตัวเอง

## 5) คำถามที่มักถูกถาม (เตรียมตอบ)

- **ทำไมใช้ bcrypt ไม่ใช่ SHA-256/MD5?** bcrypt ช้าโดยออกแบบและมี salt สุ่มในตัว จึงทน brute force/rainbow table ได้ ส่วน SHA/MD5 เร็วเกินไป
- **CSRF คืออะไร ป้องกันอย่างไร?** การหลอกให้เบราว์เซอร์ที่ล็อกอินอยู่ส่งคำขอโดยผู้ใช้ไม่รู้ตัว เราใส่ token สุ่มต่อ session ในทุกฟอร์ม POST และตรวจก่อนทำงาน + ใช้ SameSite cookie + เปลี่ยนการลบเป็น POST
- **SQL Injection ป้องกันอย่างไร?** ใช้ parameterized query ผ่าน GORM ไม่ต่อสตริง SQL เอง
- **XSS ป้องกันอย่างไร?** `html/template` escape ข้อมูลผู้ใช้ทุกจุด และ CSP ห้ามรันสคริปต์ที่ไม่ใช่ของเรา
- **IDOR คืออะไร?** การแก้ ID ใน URL เพื่อเข้าถึงข้อมูลคนอื่น แก้โดย query ด้วย `user_id` ของผู้ใช้ปัจจุบันเสมอ
- **ทำไมต้องเก็บ session ฝั่งเซิร์ฟเวอร์?** ต่างจากบั๊กเดิมที่เก็บผู้ใช้ในตัวแปรส่วนกลาง (ทุกคนใช้ร่วมกัน) ตอนนี้แต่ละคนมี token ของตัวเอง
- **แยกสิทธิ์ user/admin อย่างไร?** ตรวจ role ฝั่งเซิร์ฟเวอร์ด้วย middleware ทุกเส้นทาง `/admin/*` ไม่พึ่งการซ่อนปุ่มในหน้าเว็บ
