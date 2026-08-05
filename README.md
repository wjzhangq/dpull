# dpull

Docker 镜像下载工具，支持多连接下载、字节级断点续传、镜像源重写和代理配置。

## 特性

- **字节级断点续传**：支持任意时刻中断和恢复，不会重复下载已完成的部分
- **多连接下载**：每个 blob 可使用多个并发连接，充分利用带宽
- **并发下载层**：支持同时下载多个镜像层
- **镜像源重写**：支持国内镜像源，加速下载
- **代理支持**：支持 HTTP/HTTPS 代理，优先级：`--proxy` > 配置文件 > 环境变量
- **完整性校验**：下载后自动验证 sha256 摘要
- **Docker 兼容**：生成标准 docker-archive tar 文件，可直接 `docker load`
- **身份验证**：兼容 `~/.docker/config.json` 凭据

## 安装

### 从源码构建

```bash
git clone https://github.com/wjzhangq/dpull.git
cd dpull
go build -o bin/dpull ./cmd/dpull
```

### 使用 go install

```bash
go install github.com/wjzhangq/dpull/cmd/dpull@latest
```

### 下载预编译二进制

从 [Releases](https://github.com/wjzhangq/dpull/releases) 页面下载对应平台的二进制文件。

## 快速开始

### 基本用法

```bash
# 下载镜像
dpull nginx:1.27

# 使用代理
dpull --proxy http://127.0.0.1:7890 nginx:1.27

# 使用镜像源
dpull -m mirror.example.com nginx:1.27

# 多连接下载（每个 blob 16 个连接，同时下载 5 个 blob）
dpull -x 16 -j 5 docker.io/lmsysorg/sglang:v0.5.15

# 指定平台
dpull --platform linux/arm64 nginx:1.27

# 加载到 Docker
docker load -i nginx_1.27.tar
```

### 断点续传

```bash
# 启动下载
dpull docker.io/lmsysorg/sglang:v0.5.15

# 中断后（Ctrl-C 或网络故障），继续下载
dpull docker.io/lmsysorg/sglang:v0.5.15

# 或者使用 --continue 标志
dpull --continue docker.io/lmsysorg/sglang:v0.5.15
```

### 任务管理

```bash
# 列出所有任务
dpull ls

# 恢复指定任务
dpull resume <task-id>

# 删除任务和缓存
dpull rm <task-id>

# 删除所有任务
dpull rm --all
```

### 查看镜像信息

```bash
# 查看镜像详情（不下载）
dpull info nginx:1.27
```

## 配置文件

### 生成默认配置

```bash
dpull config init
```

这会在 `~/.dpull/config.yaml` 创建配置文件：

```yaml
# dpull 配置文件

# 默认设置
defaults:
  platform: ""              # 目标平台（如 linux/amd64, linux/arm64）
  connections: 8            # 每个 blob 的连接数
  jobs: 3                   # 最大并发下载 blob 数
  min_split_size: "20M"     # 启用分片的最小大小
  max_tries: 10             # 每个分片的最大重试次数
  check_integrity: true     # 下载后验证 sha256

# 缓存设置
cache:
  dir: ~/.dpull/cache       # 缓存目录
  max_size: "100G"          # 最大缓存大小（暂未实现）
  keep_after_success: false # 成功创建 tar 后保留 blobs

# 网络设置
network:
  proxy: ""                 # HTTP 代理（如 http://127.0.0.1:7890）
  timeout: "60s"            # 请求超时
  connect_timeout: "15s"    # 连接超时
  user_agent: "dpull/1.0"   # User Agent

# 认证（可选）
# 此处的凭据仅在 ~/.docker/config.json 中未找到时使用
auth:
  registries: {}
    # docker.io:
    #   username: "myuser"
    #   password: "mypass"
    # ghcr.io:
    #   username: "myuser"
    #   password_env: "GITHUB_TOKEN"

# 镜像配置（可选）
# mirror:
#   endpoint: "mirror.example.com"
#   path_template: "{registry}/{repo}"
```

### 查看当前配置

```bash
dpull config show
```

## 命令行选项

### 全局选项

```
--cache-dir string       缓存目录（默认 ~/.dpull/cache）
--config string          配置文件路径
--proxy string           HTTP 代理 URL（覆盖环境变量）
-j, --jobs int           最大并发下载 blob 数（默认 3）
-x, --connections int    每个 blob 的连接数（默认 8）
--min-split-size int     启用分片的最小大小（字节，默认 20MB）
--max-retries int        每个分片的最大重试次数（默认 10）
--progress string        进度显示模式：bar, plain, json, none（默认 bar）
--platform string        目标平台（如 linux/amd64, linux/arm64）
-m, --mirror string      镜像源端点
--mirror-path string     镜像路径模板（默认 {registry}/{repo}）
--plain-http             使用 HTTP 而非 HTTPS（不安全，仅用于测试）
```

### 下载选项

```
-o, --output string      输出 tar 文件路径（默认自动生成）
-c, --continue           继续中断的下载
--force                  强制重新下载（即使任务存在）
--check-integrity        下载后验证 sha256（默认 true）
--keep-cache             成功创建 tar 后保留缓存的 blobs
```

## 代理配置

代理优先级（从高到低）：

1. `--proxy` 命令行标志
2. 配置文件中的 `network.proxy`
3. 环境变量 `HTTP_PROXY` / `HTTPS_PROXY`

示例：

```bash
# 使用命令行标志
dpull --proxy http://127.0.0.1:7890 nginx:1.27

# 使用环境变量
export HTTP_PROXY=http://127.0.0.1:7890
dpull nginx:1.27

# 使用配置文件
# 编辑 ~/.dpull/config.yaml，设置 network.proxy
dpull nginx:1.27
```

## 镜像源配置

### 模板变量

镜像路径模板支持以下变量：

- `{registry}`: 注册表地址（如 docker.io）
- `{repo}`: 完整仓库路径（如 lmsysorg/sglang）
- `{namespace}`: 命名空间（如 lmsysorg）
- `{name}`: 镜像名（如 sglang）
- `{tag}`: 标签（如 v1）

### 示例

```bash
# 默认模板：{registry}/{repo}
dpull -m mirror.example.com --mirror-path "{registry}/{repo}" nginx:1.27
# 实际请求：https://mirror.example.com/docker.io/library/nginx

# 自定义模板
dpull -m mirror.example.com --mirror-path "docker/{namespace}/{name}" nginx:1.27
# 实际请求：https://mirror.example.com/docker/library/nginx
```

**注意**：无论使用哪个镜像源下载，`docker load` 后显示的镜像名始终是原始名称（如 `nginx:1.27`），不会显示镜像源地址。

## 进度显示模式

```bash
# 进度条模式（默认）
dpull nginx:1.27

# 纯文本模式
dpull --progress plain nginx:1.27

# JSON 模式（适合 CI/CD）
dpull --progress json nginx:1.27

# 静默模式
dpull --progress none nginx:1.27
```

## 常见问题

### 1. 下载速度慢

使用代理或镜像源：

```bash
dpull --proxy http://127.0.0.1:7890 nginx:1.27
# 或
dpull -m mirror.example.com nginx:1.27
```

增加连接数和并发数：

```bash
dpull -x 16 -j 5 nginx:1.27
```

### 2. 网络不稳定，频繁中断

dpull 支持字节级断点续传，直接重新运行相同命令即可：

```bash
dpull nginx:1.27
# 中断后再次运行
dpull nginx:1.27  # 自动从上次中断处继续
```

### 3. 磁盘空间不足

下载前查看镜像大小：

```bash
dpull info nginx:1.27
```

清理未完成的任务：

```bash
dpull rm --all
```

### 4. 认证失败

确保 `~/.docker/config.json` 包含正确的凭据，或在配置文件中配置：

```yaml
auth:
  registries:
    docker.io:
      username: "your-username"
      password: "your-password"
```

## 架构设计

### 核心概念

1. **身份与传输分离**：镜像的规范名称（canonical name）与下载传输地址分离，确保 `docker load` 后显示原始镜像名
2. **字节级断点续传**：使用 bitfield 跟踪每个分片的下载状态，支持任意时刻中断和恢复
3. **预签名 URL 处理**：自动处理对象存储的预签名 URL 过期（5-15 分钟），下载时自动刷新
4. **原子状态持久化**：任务状态使用 tmp → fsync → rename 模式，保证断电安全

### 目录结构

```
~/.dpull/
├── cache/
│   ├── blobs/
│   │   └── sha256/
│   │       ├── abc123...  # 完整的 blob
│   │       └── def456.part  # 下载中的 blob
│   └── tasks/
│       └── <task-id>.json  # 任务状态（包含 bitfield）
└── config.yaml  # 配置文件
```

## 开发

### 构建

```bash
# 开发构建
go build -o bin/dpull ./cmd/dpull

# 生产构建（注入版本信息）
make build
```

### 测试

```bash
# 运行所有测试
go test ./...

# 运行特定包的测试
go test ./internal/store -v

# 集成测试（需要本地 registry）
make test-integration
```

### 代码结构

```
dpull/
├── cmd/dpull/          # CLI 入口
├── internal/
│   ├── archive/        # docker-archive tar 组装
│   ├── auth/           # Docker 凭据兼容
│   ├── config/         # 配置文件加载
│   ├── downloader/     # 多连接下载引擎
│   ├── mirror/         # 镜像源模板渲染
│   ├── progress/       # 进度显示
│   ├── ref/            # 镜像引用解析
│   ├── registry/       # Registry V2 API
│   └── store/          # 内容寻址缓存和任务状态
└── pkg/version/        # 版本信息
```

## 许可证

Apache-2.0

## 致谢

- [go-containerregistry](https://github.com/google/go-containerregistry) - 镜像引用解析
- [cobra](https://github.com/spf13/cobra) - CLI 框架
- [viper](https://github.com/spf13/viper) - 配置管理
- [mpb](https://github.com/vbauerster/mpb) - 进度条显示
