package main

import (
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"embed"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed index.html
var staticFiles embed.FS

// PWA 资源（通过变量注入，避免额外文件）
var manifestJSON = []byte(`{
  "name": "网易云ncm解密",
  "short_name": "ncm解密",
  "description": "在线解密网易云音乐 .ncm 加密文件",
  "start_url": "/",
  "display": "standalone",
  "background_color": "#1a1a2e",
  "theme_color": "#c0392b",
  "icons": [
    {
      "src": "/icon.svg",
      "sizes": "any",
      "type": "image/svg+xml",
      "purpose": "any maskable"
    }
  ]
}`)

var swJS = []byte(`const CACHE = "ncm-tool-v2";
self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(["/"])));
  self.skipWaiting();
});
self.addEventListener("activate", (e) => e.waitUntil(clients.claim()));
self.addEventListener("fetch", (e) => {
  e.respondWith(
    fetch(e.request).catch(() => caches.match(e.request).then((r) => r || new Response("", { status: 503 })))
  );
});`)

var iconSVG = []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">
  <defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#e74c3c"/><stop offset="1" stop-color="#c0392b"/></linearGradient></defs>
  <rect width="512" height="512" rx="96" fill="url(#g)"/>
  <text x="256" y="340" text-anchor="middle" fill="#fff" font-size="280" font-family="serif">♪</text>
</svg>`)

var openapiJSON = []byte(`{
  "openapi": "3.0.3",
  "info": {
    "title": "ncm-tool API",
    "version": "1.0.0",
    "description": "网易云音乐 .ncm 加密文件在线解密服务"
  },
  "servers": [{"url": "http://localhost:8932", "description": "Default"}],
  "paths": {
    "/api/convert": {
      "post": {
        "summary": "解密 .ncm 文件",
        "description": "上传一个 .ncm 加密文件，服务端解密后返回元信息和下载 ID。最大 50MB。",
        "requestBody": {
          "required": true,
          "content": {
            "multipart/form-data": {
              "schema": {
                "type": "object",
                "properties": {
                  "file": {"type": "string", "format": "binary", "description": ".ncm 加密文件"}
                },
                "required": ["file"]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "成功",
            "content": {"application/json": {"schema": {"$ref": "#/components/schemas/ConvertResult"}}}
          }
        }
      }
    },
    "/api/download": {
      "get": {
        "summary": "下载解密后的文件",
        "description": "通过 convert 返回的 id 和 output_name 下载文件。文件 30 分钟后自动清理。",
        "parameters": [
          {"name": "id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "convert 返回的 id"},
          {"name": "name", "in": "query", "required": true, "schema": {"type": "string"}, "description": "convert 返回的 output_name"}
        ],
        "responses": {
          "200": {"description": "文件二进制流", "content": {"application/octet-stream": {}}},
          "400": {"description": "缺少参数"},
          "404": {"description": "文件不存在或已过期"}
        }
      }
    }
  },
  "components": {
    "schemas": {
      "ConvertResult": {
        "type": "object",
        "properties": {
          "original_name": {"type": "string", "description": "原始文件名"},
          "output_name": {"type": "string", "description": "解密后的文件名"},
          "format": {"type": "string", "enum": ["mp3", "flac"], "description": "输出格式"},
          "size": {"type": "integer", "description": "文件大小（字节）"},
          "id": {"type": "string", "description": "下载 ID，配合 output_name 拼出下载链接"},
          "error": {"type": "string", "description": "错误信息（成功时省略）"}
        }
      }
    }
  }
}`)

var docsMD = []byte(`# ncm-tool API 文档

网易云音乐 .ncm 加密文件在线解密服务。

- 基础 URL: <code>http://&lt;host&gt;:&lt;port&gt;</code>（默认端口 8932）
- 最大上传: **50MB**
- 文件保留: **30 分钟**（之后自动清理）

---

## POST /api/convert

解密一个 .ncm 文件。

**请求**: <code>multipart/form-data</code>，字段名 <code>file</code>。

**响应**:

` + "```json\n{\n  \"original_name\": \"song.ncm\",\n  \"output_name\": \"song.mp3\",\n  \"format\": \"mp3\",\n  \"size\": 5242880,\n  \"id\": \"l9x2kf7z\"\n}\n```" + `

**错误响应**（HTTP 200，<code>error</code> 字段非空）:

| error 值 | 含义 |
|---|---|
| <code>文件过大或上传失败</code> | 超过 50MB 或 multipart 解析失败 |
| <code>未找到文件</code> | 请求中没有 <code>file</code> 字段 |
| <code>请上传 .ncm 文件</code> | 文件扩展名不是 .ncm |
| <code>解密失败: &lt;detail&gt;</code> | 解密过程出错 |
| <code>写入文件失败: &lt;detail&gt;</code> | 服务端文件系统错误 |

**cURL 示例**:

` + "```bash\ncurl -X POST -F \"file=@/path/to/song.ncm\" http://localhost:8932/api/convert\n```" + `

---

## GET /api/download

下载解密后的文件。

**Query 参数**:

| 参数 | 必填 | 说明 |
|---|---|---|
| <code>id</code> | 是 | <code>/api/convert</code> 返回的 <code>id</code> |
| <code>name</code> | 是 | <code>/api/convert</code> 返回的 <code>output_name</code> |

**cURL 示例**:

` + "```bash\ncurl \"http://localhost:8932/api/download?id=l9x2kf7z&name=song.mp3\" -o song.mp3\n```" + `

---

## 完整流程示例

` + "```bash\n# 1. 解密\nRESP=$(curl -s -X POST -F \"file=@song.ncm\" http://localhost:8932/api/convert)\necho \"$RESP\"\n# {\"original_name\":\"song.ncm\",\"output_name\":\"song.mp3\",\"format\":\"mp3\",\"size\":5242880,\"id\":\"l9x2kf7z\"}\n\n# 2. 提取 id 和 output_name\nID=$(echo \"$RESP\" | python3 -c \"import sys,json;print(json.load(sys.stdin)['id'])\")\nNAME=$(echo \"$RESP\" | python3 -c \"import sys,json;print(json.load(sys.stdin)['output_name'])\")\n\n# 3. 下载\ncurl \"http://localhost:8932/api/download?id=$ID&name=$NAME\" -o \"$NAME\"\n```" + `

---

## 智能体集成

访问 <code>/api/openapi.json</code> 获取 OpenAPI 3.0 规范，LLM/智能体可自动发现和调用接口。

---

## CORS

所有响应均带:

- <code>Access-Control-Allow-Origin: *</code>
- <code>Access-Control-Allow-Methods: POST, GET, OPTIONS</code>
- <code>Access-Control-Allow-Headers: Content-Type</code>
`)

// NCM 解密密钥
var (
	coreKey = []byte{0x68, 0x7A, 0x48, 0x52, 0x41, 0x6D, 0x73, 0x6F, 0x35, 0x6B, 0x49, 0x6E, 0x62, 0x61, 0x78, 0x57}
	metaKey = []byte{0x23, 0x31, 0x34, 0x6C, 0x6A, 0x6B, 0x5F, 0x21, 0x5C, 0x5D, 0x26, 0x30, 0x55, 0x3C, 0x27, 0x28}
)

// ---------- AES-ECB 模式 ----------

func aesEcbDecrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("data not aligned")
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += block.BlockSize() {
		block.Decrypt(out[i:], data[i:i+block.BlockSize()])
	}
	return out, nil
}

// ---------- PKCS7 Unpad ----------

func pkcs7Unpad(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	padLen := int(data[len(data)-1])
	if padLen > len(data) || padLen > 16 || padLen == 0 {
		return data
	}
	return data[:len(data)-padLen]
}

// ---------- NCM 解密核心 ----------

func decryptNCM(r io.Reader) (audioData []byte, meta map[string]interface{}, coverData []byte, err error) {
	// 读取 magic header
	magic := make([]byte, 8)
	if _, err = io.ReadFull(r, magic); err != nil {
		return nil, nil, nil, fmt.Errorf("读取magic失败: %w", err)
	}
	if string(magic) != "CTENFDAM" {
		return nil, nil, nil, fmt.Errorf("非法的NCM文件")
	}

	// 跳过 2 字节
	skip2 := make([]byte, 2)
	io.ReadFull(r, skip2)

	// 读取 key length
	var keyLen uint32
	binary.Read(r, binary.LittleEndian, &keyLen)

	// 读取并解密 key data
	keyData := make([]byte, keyLen)
	io.ReadFull(r, keyData)

	// XOR 0x64
	for i := range keyData {
		keyData[i] ^= 0x64
	}

	// AES-ECB 解密
	keyData, err = aesEcbDecrypt(coreKey, keyData)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("密钥解密失败: %w", err)
	}
	keyData = pkcs7Unpad(keyData)

	// 跳过 "neteasecloudmusic" 前缀 (17 bytes)
	if len(keyData) < 17 {
		return nil, nil, nil, fmt.Errorf("密钥数据格式错误")
	}
	musicKey := keyData[17:]

	// 构建 RC4 S-box
	S := make([]byte, 256)
	for i := range S {
		S[i] = byte(i)
	}
	j := 0
	for i := 0; i < 256; i++ {
		j = (j + int(S[i]) + int(musicKey[i%len(musicKey)])) & 0xFF
		S[i], S[j] = S[j], S[i]
	}

	// 读取 meta length
	var metaLen uint32
	binary.Read(r, binary.LittleEndian, &metaLen)

	// 读取并解密 meta data
	if metaLen > 0 {
		metaData := make([]byte, metaLen)
		io.ReadFull(r, metaData)

		// XOR 0x63
		for i := range metaData {
			metaData[i] ^= 0x63
		}

		metaStr := string(metaData)
		// 跳过前 22 字节标识符
		if len(metaStr) > 22 {
			b64Part := metaStr[22:]
			decoded, decErr := base64.StdEncoding.DecodeString(b64Part)
			if decErr == nil {
				decoded, decErr = aesEcbDecrypt(metaKey, decoded)
				if decErr == nil {
					decoded = pkcs7Unpad(decoded)
					if len(decoded) > 6 {
						json.Unmarshal(decoded[6:], &meta)
					}
				}
			}
		}
	}

	// 跳过 5 字节
	skip5 := make([]byte, 5)
	io.ReadFull(r, skip5)

	// 读取封面
	var imageSpace uint32
	binary.Read(r, binary.LittleEndian, &imageSpace)
	var imageSize uint32
	binary.Read(r, binary.LittleEndian, &imageSize)

	if imageSize > 0 {
		coverData = make([]byte, imageSize)
		io.ReadFull(r, coverData)
	}
	skipImg := int64(imageSpace) - int64(imageSize)
	if skipImg > 0 {
		io.CopyN(io.Discard, r, skipImg)
	}

	// 读取加密的音频数据
	encAudio, readErr := io.ReadAll(r)
	if readErr != nil {
		return nil, nil, nil, fmt.Errorf("读取音频数据失败: %w", readErr)
	}

	// 生成 RC4 流密钥 (修改版)
	// 每个 256 字节块生成一次 keystream
	stream := make([]byte, 256)
	for i := 0; i < 256; i++ {
		stream[i] = S[(int(S[i])+int(S[(i+int(S[i]))&0xFF]))&0xFF]
	}

	// 解密音频 (跳过 stream[0])
	audioData = make([]byte, len(encAudio))
	streamIdx := 1
	for i := range encAudio {
		audioData[i] = encAudio[i] ^ stream[streamIdx]
		streamIdx++
		if streamIdx >= 256 {
			streamIdx = 0
		}
	}

	return audioData, meta, coverData, nil
}

// ---------- 判断输出格式 ----------

func getOutputFormat(audioData []byte, meta map[string]interface{}) string {
	// 检查文件头
	if len(audioData) >= 4 && string(audioData[:4]) == "fLaC" {
		return "flac"
	}
	if meta != nil {
		if format, ok := meta["format"].(string); ok {
			return strings.ToLower(format)
		}
	}
	return "mp3"
}

// ---------- HTTP 处理 ----------

var (
	outputDir string
)

type ConvertResult struct {
	OriginalName string `json:"original_name"`
	OutputName   string `json:"output_name"`
	Format       string `json:"format"`
	Size         int64  `json:"size"`
	Error        string `json:"error,omitempty"`
	ID           string `json:"id"`
}

func handleConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 限制上传大小 50MB
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)

	err := r.ParseMultipartForm(50 << 20)
	if err != nil {
		writeJSON(w, ConvertResult{Error: "文件过大或上传失败"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, ConvertResult{Error: "未找到文件"})
		return
	}
	defer file.Close()

	originalName := header.Filename

	// 检查扩展名
	if !strings.HasSuffix(strings.ToLower(originalName), ".ncm") {
		writeJSON(w, ConvertResult{Error: "请上传 .ncm 文件"})
		return
	}

	// 解密
	audioData, meta, coverData, err := decryptNCM(file)
	if err != nil {
		writeJSON(w, ConvertResult{Error: fmt.Sprintf("解密失败: %v", err)})
		return
	}

	format := getOutputFormat(audioData, meta)

	// 构建输出文件名
	baseName := strings.TrimSuffix(originalName, ".ncm")
	baseName = strings.TrimSuffix(baseName, ".NCM")
	safeName := sanitizeFilename(baseName) + "." + format

	// 生成唯一 ID
	id := strconv.FormatInt(time.Now().UnixNano(), 36)

	// 写入输出文件
	outputPath := filepath.Join(outputDir, id+"_"+safeName)
	if err := os.WriteFile(outputPath, audioData, 0644); err != nil {
		writeJSON(w, ConvertResult{Error: fmt.Sprintf("写入文件失败: %v", err)})
		return
	}

	// 嵌入封面和元数据（基本支持）
	info, _ := os.Stat(outputPath)
	_ = coverData // 封面嵌入比较复杂，暂跳过
	_ = meta       // 元数据嵌入比较复杂，暂跳过

	writeJSON(w, ConvertResult{
		OriginalName: originalName,
		OutputName:   safeName,
		Format:       format,
		Size:         info.Size(),
		ID:           id,
	})
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	name := r.URL.Query().Get("name")
	if id == "" || name == "" {
		http.Error(w, "缺少参数", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(outputDir, id+"_"+name)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "文件不存在或已过期", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, filePath)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", "\n", "", "\r", "",
	)
	return replacer.Replace(name)
}

// ---------- 清理 goroutine ----------

func startCleaner(interval time.Duration, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			entries, err := os.ReadDir(outputDir)
			if err != nil {
				continue
			}
			now := time.Now()
			for _, entry := range entries {
				info, err := entry.Info()
				if err != nil {
					continue
				}
				if now.Sub(info.ModTime()) > maxAge {
					os.Remove(filepath.Join(outputDir, entry.Name()))
				}
			}
		}
	}()
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	data, _ := staticFiles.ReadFile("index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func main() {
	// 创建临时目录
	baseDir, err := os.MkdirTemp("", "ncm-tool-*")
	if err != nil {
		log.Fatal("创建临时目录失败:", err)
	}
	outputDir = filepath.Join(baseDir, "outputs")
	os.MkdirAll(outputDir, 0755)

	// 清理旧文件（每5分钟清理超过30分钟的文件）
	startCleaner(5*time.Minute, 30*time.Minute)

	port := "8932"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	// 启动服务器
	mux := http.NewServeMux()
	mux.HandleFunc("/api/convert", handleConvert)
	mux.HandleFunc("/api/download", handleDownload)
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(manifestJSON)
	})
	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(swJS)
	})
	mux.HandleFunc("/icon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(iconSVG)
	})
	mux.HandleFunc("/api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(openapiJSON)
	})
	mux.HandleFunc("/api/docs.md", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write(docsMD)
	})
	mux.HandleFunc("/", handleIndex)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// 包装 CORS（直接复用 mux，所有路由都生效）
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		mux.ServeHTTP(w, r)
	})

	fmt.Printf("\n╔══════════════════════════════════════════╗\n")
	fmt.Printf("║   🎵 网易云ncm → mp3工具                 ║\n")
	fmt.Printf("║   地址: http://localhost:%s              ║\n", port)
	fmt.Printf("║   按 Ctrl+C 停止                         ║\n")
	fmt.Printf("╚══════════════════════════════════════════╝\n\n")

	if err := server.ListenAndServe(); err != nil {
		log.Fatal("启动失败:", err)
	}
}
