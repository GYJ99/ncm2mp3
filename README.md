# ncm-tool

网易云音乐 `.ncm` 加密文件在线解密工具。一个 Go 单文件二进制即可启动 Web 服务，支持 PC 与移动端浏览器，可作为 PWA 安装到手机桌面。

## 特性

- 🎵 **一键解密**：上传 `.ncm` 文件，本地解密为 MP3 / FLAC
- 📱 **PWA 支持**：可添加到手机桌面，离线访问主页面
- 🚀 **零依赖部署**：单二进制，5-6 MB，无外部依赖
- 🖥️ **响应式 UI**：自动适配 PC / 平板 / 手机
- 🔒 **本地处理**：文件不上传第三方，浏览器 → Go 服务全程内网
- 🧹 **自动清理**：临时文件 30 分钟无访问自动删除
- ⚡ **批量转换**：支持多文件并发处理

## 快速开始

### 下载预编译二进制

从 [Releases](../../releases) 下载对应平台的二进制：

- `ncm-tool-darwin-arm64` — macOS (Apple Silicon)
- `ncm-tool-linux-amd64` — Linux x86_64

### 运行

```bash
# 默认监听 :8932
./ncm-tool

# 自定义端口
PORT=8080 ./ncm-tool
```

浏览器访问 `http://localhost:8932` 即可。

## 从源码构建

需要 Go 1.24+。

```bash
# 本地平台构建
go build -trimpath -ldflags="-s -w" -o ncm-tool .

# 跨平台构建（macOS / Linux）
./build.sh
# 产物输出到 dist/
```

`build.sh` 支持的环境变量：

```bash
VERSION=v1.0.0 ./build.sh   # 自定义版本号（默认从 git tag 推断）
```

## API

| 路径 | 方法 | 说明 |
|---|---|---|
| `/` | GET | Web 前端页面 |
| `/manifest.json` | GET | PWA manifest |
| `/sw.js` | GET | Service Worker |
| `/icon.svg` | GET | 应用图标 |
| `/api/convert` | POST | 上传 `.ncm` 文件，返回解密结果信息 |
| `/api/download` | GET | 下载解密后的文件（需带 `id` 和 `name` 参数） |
| `/api/openapi.json` | GET | OpenAPI 3.0 规范（智能体发现和调用接口） |
| `/api/docs.md` | GET | Markdown 格式完整 API 文档 |

### 智能体集成

访问 `/api/openapi.json` 获取 OpenAPI 3.0 规范，LLM/智能体可自动发现和调用接口。完整流程示例：

```bash
# 1. 解密
RESP=$(curl -s -X POST -F "file=@song.ncm" http://localhost:8932/api/convert)
# {"original_name":"song.ncm","output_name":"song.mp3","format":"mp3","size":5242880,"id":"l9x2kf7z"}

# 2. 提取 id 和 output_name
ID=$(echo "$RESP" | jq -r .id)
NAME=$(echo "$RESP" | jq -r .output_name)

# 3. 下载
curl "http://localhost:8932/api/download?id=$ID&name=$NAME" -o "$NAME"
```

## 部署为 Web 服务

### 单机部署（systemd）

最简单的方式：把二进制扔到服务器上跑。示例 `/etc/systemd/system/ncm-tool.service`：

```ini
[Unit]
Description=ncm-tool
After=network.target

[Service]
Type=simple
User=nobody
WorkingDirectory=/opt/ncm-tool
ExecStart=/opt/ncm-tool/ncm-tool
Restart=on-failure
Environment=PORT=8932

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ncm-tool
```

### 反向代理（Nginx）

让 Nginx 监听 80/443 端口，转发到 ncm-tool：

```nginx
server {
    listen 80;
    server_name ncm.example.com;

    client_max_body_size 60M;   # 略大于服务端 50MB 限制

    location / {
        proxy_pass http://127.0.0.1:8932;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_read_timeout 300s;
        proxy_request_buffering off;   # 大文件上传需要
        proxy_buffering off;
    }
}
```

### HTTPS

强烈建议在公网部署时启用 HTTPS（PWA 的 Service Worker 必须在 HTTPS 下才能注册）。可用 Caddy：

```caddy
ncm.example.com {
    reverse_proxy 127.0.0.1:8932
}
```

## 移动端 / PWA

### 添加到主屏幕

- **iOS Safari**：分享按钮 → 添加到主屏幕
- **Android Chrome**：菜单 → 安装应用 / 添加到主屏幕

添加后会以全屏模式运行，体验接近原生 App。

### 离线行为

Service Worker 仅缓存主页 HTML。解密功能需要联网（与服务端通信）。离线时仍可打开主页查看说明，但无法转换。

## 技术栈

- **后端**：Go 标准库（`crypto/aes`、`net/http`、`embed`），零外部依赖
- **前端**：单 HTML 文件，Vanilla JS，玻璃态 UI
- **解密算法**：AES-ECB + RC4（兼容 ncmdump）

## 文件结构

```
ncm-tool-wangyiyun/
├── main.go              # Go 后端（解密 + HTTP 服务）
├── index.html           # 前端单页应用（嵌入 main.go）
├── build.sh             # 跨平台构建脚本
├── go.mod
├── .gitignore
└── README.md
```

## 常见问题

**Q: 解密后的文件元数据（歌名、艺术家、封面）丢了？**

A: 当前实现仅解出原始音频流，不写 ID3 / Vorbis tag。计划在未来版本加入元数据嵌入。

**Q: 文件大小限制？**

A: 服务端默认 50MB（`http.MaxBytesReader`）。可通过修改 `main.go` 中的 `50 << 20` 调整。

**Q: 可以商用吗？**

A: 本项目仅用于个人学习与备份已购买的音乐，请勿用于商业用途或传播受版权保护的内容。

## License

MIT
