// ยืนยันก่อนลบ/ยกเลิก (ไม่ใช้ inline script เพื่อให้ CSP เข้มงวดได้)
document.addEventListener('submit', function (e) {
  var msg = e.target && e.target.getAttribute('data-confirm');
  if (msg && !window.confirm(msg)) {
    e.preventDefault();
  }
});
