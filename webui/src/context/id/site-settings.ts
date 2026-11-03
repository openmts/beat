// 印尼文目录：与其它语言并行维护，补齐后由 shippedLocales 统一发布。
export const idSiteSettings: Record<string, string> = {
  "settings.site": "Pengaturan situs",
  "settings.site_description":
    "Identitas publik, pencitraan merek, dan tampilan bagi pengunjung.",
  "settings.identity": "Identitas",
  "settings.site_title": "Judul situs",
  "settings.site_title_hint":
    "Ditampilkan di judul browser dan header aplikasi.",
  "settings.description": "Deskripsi situs",
  "settings.description_hint":
    "Dipakai sebagai deskripsi halaman untuk browser dan pratinjau pencarian.",
  "settings.branding": "Pencitraan merek",
  "settings.logo_url": "URL logo",
  "settings.logo_hint": "Gunakan URL HTTPS atau jalur relatif terhadap root.",
  "settings.favicon_url": "URL favicon",
  "settings.favicon_hint": "Ditampilkan di tab browser dan markah.",
  "settings.appearance": "Tampilan",
  "settings.default_theme": "Tema bawaan",
  "settings.theme_system": "Ikuti sistem",
  "settings.theme_light": "Terang",
  "settings.theme_dark": "Gelap",
  "settings.default_theme_hint":
    "Berlaku sampai pengunjung memilih tema lokal.",
  "settings.public_display": "Tampilan publik",
  "settings.show_ip": "Tampilkan alamat IP node",
  "settings.show_ip_hint":
    "Jika dinonaktifkan, respons REST publik dan langsung akan menyamarkan host node.",
  "settings.address_mode": "Alamat node publik",
  "settings.address_hidden": "Tersembunyi",
  "settings.address_masked": "Bertopeng",
  "settings.address_full": "Lengkap",
  "settings.address_hint":
    "Tersembunyi menghapus alamat, bertopeng hanya menyimpan prefiks jaringan (oktet IPv4 pertama atau 32 bit IPv6 pertama), dan lengkap menampilkan nilai tersimpan.",
  "settings.deployment": "Akses deployment",
  "settings.deployment_hint":
    "Nilai ini berasal dari lingkungan server. Nilainya tidak dapat diubah di sini dan daftar origin tidak pernah dikirim ke browser.",
  "settings.deployment_unavailable": "Tidak tersedia",
  "settings.external_url": "URL eksternal",
  "settings.external_url_unset": "Belum dikonfigurasi",
  "settings.api_origins": "Origin REST publik",
  "settings.ws_origins": "Origin WebSocket publik",
  "settings.origins_all": "Semua origin publik",
  "settings.origins_same": "Origin yang sama",
  "settings.origins_configured": "{count} origin terkonfigurasi",
  "settings.address_privacy_note":
    "Identitas dan aktivitas pengunjung tidak pernah dicatat. Telemetri publik hanya berupa agregat dan tidak ada sampel analitik yang ditulis ke SQLite.",
  "settings.show_network": "Tampilkan kualitas jaringan",
  "settings.show_network_hint":
    "Mengontrol bagian kualitas jaringan pada dasbor publik.",
  "settings.show_accelerators": "Tampilkan GPU dan akselerator",
  "settings.show_accelerators_hint":
    "Pengesampingan global. Saat dimatikan, halaman metrik menyembunyikan bagian GPU dan akselerator untuk semua node.",
  "settings.accelerator_node_visibility":
    "Tampilkan node ini di halaman metrik",
  "settings.accelerator_node_visibility_hint":
    "Saat sakelar global aktif, atur apakah bagian GPU dan akselerator node ini ditampilkan.",
  "settings.accelerator_node_global_off":
    "Sakelar global mati. Pengaturan node ini tetap disimpan dan berlaku saat sakelar global diaktifkan kembali.",
  "settings.public_telemetry": "Telemetri agregat anonim",
  "settings.public_telemetry_hint":
    "Menghitung sekumpulan kecil peristiwa halaman yang kasar. Tidak ada identitas, alamat, URL, atau detail bebas yang dikumpulkan.",
  "settings.telemetry_enabled": "Terima jumlah interaksi anonim",
  "settings.telemetry_ceiling": "Batas sampel per sesi",
  "settings.telemetry_ceiling_hint":
    "Berlaku per sesi browser; server juga membatasi laju setiap sumber.",
  "settings.telemetry_retention": "Penyimpanan agregat (hari)",
  "settings.telemetry_retention_hint":
    "Penghitung agregat dihapus setelah jumlah hari ini.",
  "settings.telemetry_proof_hint":
    "Konfirmasi identitas Anda dengan kata sandi saat ini dan kode autentikator sebelum mengubah kebijakan telemetri.",
  "settings.telemetry_password": "Kata sandi saat ini",
  "settings.telemetry_code": "Kode autentikator",
  "settings.telemetry_cancel": "Batal",
  "settings.telemetry_confirm_action": "Konfirmasi",
  "settings.telemetry_save": "Simpan kebijakan telemetri publik",
  "settings.telemetry_saved": "Kebijakan telemetri publik tersimpan",
  "settings.telemetry_unavailable": "Kebijakan telemetri publik tidak tersedia",
  "settings.proxy_title": "Proxy keluar",
  "settings.proxy_description":
    "Rutekan unduhan dan integrasi sisi server melalui satu proxy HTTP, HTTPS, SOCKS5, atau SOCKS5H. Perubahan berlaku pada permintaan baru tanpa memulai ulang server.",
  "settings.proxy_enabled_badge": "Aktif",
  "settings.proxy_disabled_badge": "Nonaktif",
  "settings.proxy_enable": "Gunakan proxy keluar",
  "settings.proxy_enable_hint":
    "Memakai pengaturan tersimpan untuk permintaan latar belakang baru tanpa memulai ulang server.",
  "settings.proxy_url": "URL proxy",
  "settings.proxy_url_hint":
    "Sertakan skema dan port, misalnya http://127.0.0.1:7890 atau socks5h://proxy.example:1080.",
  "settings.proxy_username": "Nama pengguna",
  "settings.proxy_username_hint":
    "Opsional. Kredensial disimpan dengan kunci rahasia aplikasi server.",
  "settings.proxy_password": "Kata sandi",
  "settings.proxy_password_saved":
    "Kata sandi tersimpan. Biarkan kosong agar tidak berubah.",
  "settings.proxy_password_hint":
    "Opsional. Biarkan kosong jika proxy tidak memerlukan autentikasi.",
  "settings.proxy_clear_password": "Hapus kata sandi tersimpan",
  "settings.proxy_clear_password_hint":
    "Nama pengguna tetap disimpan. Gunakan jika autentikasi tidak lagi diperlukan.",
  "settings.proxy_scope":
    "Berlaku untuk unduhan MMDB dan MaxMind, pencarian GeoIP, rilis Agent, pasar tema, webhook, dan OIDC. SMTP dan SSH tetap memakai transport aslinya.",
  "settings.proxy_revision": "Revisi konfigurasi {revision}",
  "settings.proxy_save": "Simpan pengaturan proxy",
  "settings.proxy_saved": "Pengaturan proxy keluar tersimpan",
  "settings.proxy_conflict":
    "Pengaturan berubah di sesi lain. Nilai terbaru telah dimuat ulang; periksa lalu simpan kembali.",
  "settings.proxy_save_failed": "Pengaturan proxy keluar tidak dapat disimpan",
  "settings.proxy_unavailable": "Pengaturan proxy keluar tidak tersedia",
  "settings.proxy_confirm_title": "Simpan proxy keluar",
  "settings.proxy_confirm_description":
    "Perubahan rute jaringan khusus pemilik ini memerlukan konfirmasi identitas.",
  "settings.proxy_test": "Uji proxy tersimpan",
  "settings.proxy_test_hint":
    "Menguji konfigurasi tersimpan. Simpan perubahan yang belum disimpan terlebih dahulu.",
  "settings.proxy_test_title": "Uji proxy keluar",
  "settings.proxy_test_description":
    "Beat akan mengakses halaman publik example.com yang tetap melalui proxy tersimpan. Kredensial proxy tidak pernah dikembalikan.",
  "settings.proxy_test_success": "Koneksi proxy berhasil dalam {latency} ms",
  "settings.proxy_test_failed": "Tidak dapat menguji proxy keluar",
  "settings.saved": "Pengaturan situs tersimpan",
  "settings.preview": "Pratinjau merek",
};
