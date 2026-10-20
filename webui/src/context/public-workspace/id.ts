import type { Catalog } from "@/context/locales"

// 公共节点页会渲染只读指标工作区组件，但公共外壳不加载管理端工作区目录（体积预算）；
// 这里只收录公共路径会显示的 workspace.* 文案，值由 metric-workspace 目录生成，
// context/public-workspace/catalog.test.ts 负责两边一致性。
export const publicWorkspaceId: Catalog = {
  "id": {
    "workspace.apply": "Terapkan", "workspace.cancel": "Batal", "workspace.create_alert": "Buat aturan peringatan", "workspace.custom_range": "Rentang kustom",
    "workspace.data": "Data", "workspace.done": "Selesai", "workspace.drag_zoom": "Seret pada grafik untuk memperbesar rentang waktu.", "workspace.duplicate_panel": "Duplikat panel",
    "workspace.duplicate_series": "Referensi kueri harus unik.", "workspace.edit_panel": "Ubah panel", "workspace.export_csv": "Unduh CSV", "workspace.export_failed": "Pengunduhan gagal. Coba lagi.",
    "workspace.export_json": "Unduh JSON", "workspace.export_stats_csv": "Unduh CSV statistik", "workspace.from": "Dari", "workspace.gpu_fixed_node": "Pilih node tertentu sebelum memilih perangkat GPU.", "workspace.heatmap_samples": "Sampel", "workspace.histogram_samples": "Sampel", "workspace.state_timeline_states": "Status",
    "workspace.historical": "Rentang historis tetap", "workspace.inspect": "Periksa data", "workspace.inspector_limit": "Menampilkan 100 baris pertama. Unduhan mencakup semua {rows} baris.", "workspace.invalid_panel": "Periksa judul, kueri, batas sumbu, dan ambang batas.",
    "workspace.invalid_range": "Pilih rentang waktu yang valid hingga 31 hari.", "workspace.large": "Lebar penuh", "workspace.last15m": "15 menit terakhir", "workspace.last1h": "1 jam terakhir",
    "workspace.last24h": "24 jam terakhir", "workspace.last30d": "30 hari terakhir", "workspace.last5m": "5 menit terakhir", "workspace.last6h": "6 jam terakhir",
    "workspace.last7d": "7 hari terakhir", "workspace.last_good_range": "Menampilkan hasil terakhir yang berhasil sementara kueri baru tertunda atau gagal.", "workspace.last_success": "Kueri terakhir yang berhasil: {time}", "workspace.legend.count": "Jumlah sampel",
    "workspace.legend.last": "Terakhir", "workspace.legend.max": "Maks", "workspace.legend.mean": "Rata-rata", "workspace.legend.min": "Min",
    "workspace.stats": "Statistik", "workspace.legend.first": "Pertama", "workspace.legend.gaps": "Celah",
    "workspace.legend.series": "Seri", "workspace.legend.sort": "Urutan legenda", "workspace.legend.sort_asc": "Dari terendah", "workspace.legend.sort_desc": "Dari tertinggi",
    "workspace.legend.sort_none": "Sesuai konfigurasi", "workspace.legend.sum": "Jumlah", "workspace.legend.tooltip": "Tooltip", "workspace.legend.tooltip_all": "Semua seri",
    "workspace.legend.tooltip_off": "Tersembunyi", "workspace.legend.tooltip_single": "Seri yang disorot", "workspace.legend.values": "Nilai legenda", "workspace.live": "Rentang langsung",
    "workspace.load_failed": "Ruang kerja tidak dapat dimuat.", "workspace.medium": "Setengah lebar", "workspace.metric": "Metrik", "workspace.move_down": "Pindah ke belakang",
    "workspace.move_up": "Pindah ke depan", "workspace.no_data": "Tidak ada sampel dalam rentang ini", "workspace.no_data_description": "Coba rentang yang lebih lebar atau periksa status pengumpulan node.", "workspace.node_drilldown": "Fokuskan node ini",
    "workspace.node_refresh_failed": "Detail node tidak dapat disegarkan. Menampilkan ringkasan terakhir yang diterima.", "workspace.node_unavailable": "Node ini tidak lagi publik atau tidak tersedia.", "workspace.off": "Mati", "workspace.pause": "Jeda penyegaran",
    "workspace.pie_legend": "{title} · legenda potongan", "workspace.query": "Kueri", "workspace.query_failed": "Kueri metrik gagal.", "workspace.query_options": "Opsi kueri",
    "workspace.refresh": "Segarkan sekarang", "workspace.refresh_interval": "Penyegaran otomatis", "workspace.remove_panel": "Hapus panel", "workspace.repeat_truncated": "Hanya {count} panel pengulangan pertama yang ditampilkan.",
    "workspace.response_summary": "{series} seri · {points} titik · bucket {interval}d", "workspace.retry": "Coba lagi", "workspace.row": "Baris", "workspace.row_panels": "{count} panel", "workspace.toggle_row": "Bentangkan atau lipat baris ini", "workspace.sample_time": "Sampel diambil: {time}", "workspace.seconds": "Setiap {seconds} detik",
    "workspace.geomap_layer_markers": "Penanda", "workspace.geomap_layer_heatmap": "Peta panas", "workspace.geomap_no_match": "Tidak ada seri yang cocok dengan lokasi di daftar.", "workspace.geomap_unmatched": "{count} seri tanpa koordinat",
    "workspace.select_gpu_device": "Pilih perangkat GPU yang stabil.", "workspace.select_node": "Pilih node atau grup yang memiliki node.", "workspace.series_limit": "Satu panel dapat mengueri hingga delapan seri yang diperluas. Pilih lebih sedikit node atau kueri.", "workspace.small": "Sepertiga",
    "workspace.time_range": "Rentang waktu", "workspace.timestamp": "Stempel waktu (UTC)", "workspace.to": "Sampai", "workspace.updating": "Memperbarui…",
    "workspace.utc_hint": "Waktu dalam UTC. Rentang maksimum: 31 hari.", "workspace.expression_from": "Ekspresi waktu mulai", "workspace.expression_to": "Ekspresi waktu selesai", "workspace.expression_placeholder": "now, now-6h, now-7d, now/d, 2026-10-17 09:00", "workspace.expression_invalid": "Gunakan now, now-6h, now-7d, now/d, atau 2026-10-17 09:00 (UTC).", "workspace.value": "Nilai", "workspace.variable_picker_hint": "Hanya opsi yang dapat dikueri dasbor ini yang ditampilkan.", "workspace.variable_selected": "+{count} lagi",
    "workspace.view_panel": "Lihat panel",
  },
}
