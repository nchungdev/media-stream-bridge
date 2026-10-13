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
  - Tải trực tiếp luồng stream qua `ffmpeg` hoặc `yt-dlp` mà không cần re-encode (tiết kiệm 100% CPU, tốc độ tối đa băng thông mạng).
  - Tự động gọi API Sonarr/Radarr rescan series sau khi hoàn tất tải.
- **Siêu Nhẹ & Hiệu Năng Vượt Trội**:
  - Viết bằng Go (Goroutines fan-out concurrency song song với timeout 6s).
  - RAM tiêu thụ chỉ ~10MB (thay vì hàng trăm MB của runtime Python/Docker cũ).

---

## 🚀 Triển Khai Nhanh Với Docker Compose

Docker image được tự động build đa kiến trúc (`linux/amd64`, `linux/arm64`) qua GitHub Actions và lưu trữ trên **GitHub Container Registry (GHCR)**:

```yaml
services:
  media-stream-bridge:
    image: ghcr.io/nchungdev/media-stream-bridge:latest
    container_name: media-stream-bridge
    restart: unless-stopped
    network_mode: host
    environment:
      - TZ=Asia/Ho_Chi_Minh
      - MEDIA_STREAM_CONFIG=/config/config.json
    volumes:
      - /etc/fshare-bridge/config.json:/config/config.json:ro
      - /srv/mergerfs/MainPool/Phim:/srv/mergerfs/MainPool/Phim
      - /srv/mergerfs/MainPool/downloads:/srv/mergerfs/MainPool/downloads
```

Chạy container:
```bash
docker compose up -d
```

---

## ⚙️ Cấu Hình (`config.json`)

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 6890,
    "api_key": "fshare-bridge-secret"
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

---

## 🛠️ Tích Hợp Vào Hệ Thống `*arr`

1. **Prowlarr**:
   - Thêm Indexer: `Generic Torznab`
   - URL: `http://localhost:6890/torznab/api`
   - API Key: `fshare-bridge-secret`
   - Categories: `5000` (TV), `2000` (Movies)
2. **Sonarr & Radarr**: Tự động sync từ Prowlarr hoặc thêm trực tiếp Torznab Indexer với cấu hình tương tự.
