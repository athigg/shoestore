@echo off
chcp 65001 >nul
REM ===== รันระบบผ่าน cmd: ดับเบิลคลิกไฟล์นี้ หรือพิมพ์ start.bat =====
REM แก้ค่าด้านล่างให้ตรงกับเครื่อง/โฮสต์ของคุณ
set PORT=8080
set DB_HOST=127.0.0.1
set DB_PORT=3306
set DB_USER=root
set DB_PASS=
set DB_NAME=shoestore
set ADMIN_USERNAME=admin
set ADMIN_EMAIL=admin@example.com
set ADMIN_PASSWORD=ChangeMe12345
set COOKIE_SECURE=false
set GIN_MODE=release

if not exist shoestore.exe (
  echo กำลัง build...
  go build -o shoestore.exe .
  if errorlevel 1 exit /b 1
)
shoestore.exe
