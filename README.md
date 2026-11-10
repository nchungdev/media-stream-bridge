# Media Stream Bridge

High-performance, ultra-lightweight **Torznab Indexer & Download Client Hub** written in Go for the `*arr` ecosystem (Prowlarr, Sonarr, Radarr).

Replaces legacy Python bridges with sub-millisecond response times, low memory footprint (~10MB RAM), and native stream grabbing support.

---

## 🌟 Tính Năng Chính

- **Hỗ trợ Đa Nguồn (Multi-Provider Aggregation)**:
  - **KKPhim** (`phimapi.com`): Tự động lấy trực tiếp stream HLS 1080p (`.m3u8`) từ CDN tốc độ cao.
  - **NguonC** (`phim.nguonc.com`): Tích hợp kho phim phong phú với metadata chi tiết.
  - **YanHH3D** (`yanhh3d.ee`): Web scraper chuyên biệt cho Donghua 3D Vietsub độc quyền.
  - **Fshare VIP**: Tương thích ngược với các liên kết Fshare direct.
- **Tương thích chuẩn Torznab XML**:
  - Hỗ trợ đầy đủ các lệnh `caps`, `tvsearch`, `movie`, `search`.
  - Kết nối hoàn hảo với **Prowlarr**, **Sonarr**, **Radarr**.
- **Chuẩn hóa Tiêu đề Thông minh (Smart Title Matching)**:
  - Tự động phát hiện Season, Episode và ánh xạ tên song ngữ (English / Vietnamese) để Sonarr tự động nhận diện series (`Approved: True`) thay vì báo lỗi `Unknown Series`.
- **Tải Luồng Tự Động & Báo Rescan**:
  - Tải trực tiếp luồng stream qua `ffmpeg` mà không cần re-encode (tiết kiệm 100% CPU, tốc độ tối đa băng thông mạng).
- **Siêu Nhẹ & Hiệu Năng Vượt Trội**:
  - Viết bằng Go (Goroutines fan-out concurrency song song với timeout 6s).
  - RAM tiêu thụ chỉ ~10MB (thay vì hàng trăm MB của runtime Python/Docker cũ).

---

## 🚀 Triển Khai Nhanh Với Docker Compose

Docker image được tự động build đa kiến trúc (`linux/amd64`, `linux/arm64`) qua GitHub Actions và lưu trữ trên **GitHub Container Registry (GHCR)**:

Xem `docker-compose.yml` (ví dụ đã được siết bảo mật: `user 1000:100`, `read_only`, `cap_drop ALL`,
`no-new-privileges`, giới hạn pids/RAM, chỉ mount thư mục downloads, không mount thư viện Phim).
Sao chép `.env.example` thành `.env` và điền thông tin đăng nhập thật trước khi chạy.

Chạy container:
```bash
cp .env.example .env   # rồi sửa giá trị
docker compose up -d
```

Dịch vụ **từ chối khởi động** nếu thiếu hoặc yếu thông tin đăng nhập (fail-closed).

---

## ⚙️ Cấu Hình (`config.json`)

```json
{
  "server": {
    "port": 6890
  },
  "arr": {
    "sonarr_url": "http://localhost:8989",
    "sonarr_api_key": "<your-sonarr-api-key>",
    "radarr_url": "http://localhost:7878",
    "radarr_api_key": "<your-radarr-api-key>",
    "prowlarr_url": "http://localhost:9696",
    "prowlarr_api_key": "<your-prowlarr-api-key>"
  },
  "storage": {
    "tv_base_dir": "/srv/mergerfs/MainPool/Phim/TV Shows",
    "movie_base_dir": "/srv/mergerfs/MainPool/Phim/Movies",
    "default_download_dir": "/srv/mergerfs/MainPool/downloads"
  }
}
```

### Biến môi trường

| Biến | Mặc định | Ghi chú |
|---|---|---|
| `MSB_LISTEN_HOST` | `127.0.0.1` | Địa chỉ lắng nghe (chỉ đọc từ env, không đọc `host` trong config.json) |
| `MSB_PORT` | `6890` | Cổng lắng nghe |
| `MSB_QBIT_USERNAME` | (bắt buộc) | Tài khoản giả lập qBittorrent cho Sonarr/Radarr, cũng dùng cho Basic auth của dashboard |
| `MSB_QBIT_PASSWORD` | (bắt buộc) | Tối thiểu 12 ký tự, không được là `admin` |
| `MSB_API_KEY` | (bắt buộc) | API key Torznab, tối thiểu 24 ký tự; có thể lấy từ `server.api_key` nếu đạt cùng điều kiện |
| `MSB_MAX_JOBS` | `200` | Số job tối đa trong hàng đợi |
| `MSB_PUBLIC_URL` | `http://127.0.0.1:6890` | URL gốc dùng để tạo link tải trong Torznab RSS |
| `MSB_EPISODE_TIMEOUT` | `3h` | Thời gian tối đa cho mỗi tập (ffmpeg) |

Nếu đặt sau reverse proxy, hãy giữ nguyên header `Host` (`proxy_set_header Host $host;`) vì các request POST có
`Origin`/`Referer` khác `Host` sẽ bị từ chối (chống CSRF). Bộ đếm đăng nhập sai tính theo IP kết nối trực tiếp.

---

## 🛠️ Tích Hợp Vào Hệ Thống `*arr`

1. **Prowlarr**:
   - Thêm Indexer: `Generic Torznab`
   - URL: `http://localhost:6890/torznab/api`
   - API Key: giá trị `MSB_API_KEY` của bạn
   - Categories: `5000` (TV), `2000` (Movies)
2. **Sonarr & Radarr**: Tự động sync từ Prowlarr hoặc thêm trực tiếp Torznab Indexer với cấu hình tương tự.
3. **Sonarr & Radarr (Download Client)**: thêm client `qBittorrent`, Host/Port của bridge, Username/Password là
   `MSB_QBIT_USERNAME` / `MSB_QBIT_PASSWORD` (không còn chấp nhận `admin/admin`).
4. **Dashboard** (`/dashboard`): trình duyệt sẽ hỏi Basic auth bằng cùng tài khoản qBittorrent.
