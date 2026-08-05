# dpull 开发文档

> 一个支持断点续传、多连接并发、mirror 域名重写的 Docker 镜像下载工具

| 项目 | 内容 |
|---|---|
| 命令名 | `dpull` |
| 语言 | Go 1.22+ |
| 目标平台 | macOS (amd64/arm64)、Linux (amd64/arm64/armv7)、Windows (amd64/arm64) |
| 分发方式 | 单文件静态二进制，GitHub Release 自动构建 |
| License | Apache-2.0 |

---

## 1. 背景与目标

### 1.1 现状问题

现有工具在国内网络环境下拉取大镜像（10GB+ 的 CUDA / 推理框架镜像）时存在明显短板：

| 工具 | 断点续传 | 多连接 | mirror 重写 | 免装 Docker | 进度显示 |
|---|---|---|---|---|---|
| **dpull** | **字节级** | **是** | **模板化** | **是** | **有** |

核心痛点：一个 18GB 的镜像，单层可能有 6GB，网络抖动导致中断后所有工具都要重下整层甚至整个镜像。

### 1.2 设计目标

1. **字节级断点续传** — 对标 `aria2c -c`，中断后从分片粒度恢复
2. **多连接并发** — 对标 `aria2c -x8 -j3`，单层多连接 + 多层并行
3. **镜像身份与下载通道解耦** — 通过 mirror 加速下载，但导入后显示的是原始镜像名
4. **免 Docker 依赖** — 纯 registry HTTP API 实现，产物可 `docker load`
5. **跨平台单文件分发** — 内网机器 scp 过去就能用

### 1.3 非目标

- 不做镜像构建（build）
- 不做镜像运行（run）
- 不替代 registry（不做长期存储）
- 不做镜像内容扫描 / 签名验证（v1 范围外）

---

## 2. 核心概念

### 2.1 Canonical Name 与 Mirror 分离

**这是 dpull 最核心的设计原则。**

用户在命令行永远书写镜像的**真实名称**（canonical name），mirror 仅作为下载通道存在，不参与镜像身份的构成。

```
用户输入:   docker.io/lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux
                          │
                          ├─── 身份 (Identity) ──→ 写入 manifest.json 的 RepoTags
                          │                        docker load 后显示的名字
                          │
                          └─── 通道 (Transport) ─→ 经 mirror 规则改写为实际请求地址
                                                   7c5a...1ms.run/lmsysorg/sglang:...
```

对比其他工具的做法：

```bash

# dpull 的做法 —— 身份与通道分离
dpull -m xxx.1ms.run docker.io/lmsysorg/sglang:v1
docker load -i out.tar
# → 显示 lmsysorg/sglang:v1                ← 正确
```

**收益**：CI 里的镜像引用、k8s 的 `image:` 字段、docker-compose 文件全部保持不变，只在拉取环节切换 mirror。换加速源不需要改任何业务配置。

### 2.2 Mirror 路径模板

不同加速站的路径规则差异很大，硬编码前缀替换无法覆盖，需要模板抽象：

| Mirror 类型 | 实际地址 | 模板 |
|---|---|---|
| 1ms.run 直通型 | `HOST/lmsysorg/sglang` | `{repo}` |
| 华为 SWR ddn-k8s | `HOST/ddn-k8s/docker.io/lmsysorg/sglang` | `ddn-k8s/{registry}/{repo}` |
| 南大镜像站 | `HOST/lmsysorg/sglang` | `{repo}` |
| 阿里云个人版 | `HOST/myns/sglang` | `myns/{name}` |
| Harbor Proxy Cache | `HOST/dockerhub-proxy/lmsysorg/sglang` | `dockerhub-proxy/{repo}` |

模板变量：

| 变量 | 示例值（输入 `docker.io/lmsysorg/sglang:v1`） |
|---|---|
| `{registry}` | `docker.io` |
| `{repo}` | `lmsysorg/sglang` |
| `{namespace}` | `lmsysorg` |
| `{name}` | `sglang` |
| `{tag}` | `v1` |
| `{digest}` | `sha256:...`（若按 digest 引用） |

> **注意**：Docker Hub 官方镜像（如 `nginx`）的 canonical repo 是 `library/nginx`。dpull 内部统一补全为 `library/nginx`，但 `{namespace}` 保持为 `library`，`{name}` 为 `nginx`。部分 mirror 需要 `library/` 前缀，部分不需要，由模板控制。

---

## 3. CLI 设计

### 3.1 基本用法

```bash
dpull [flags] IMAGE
```

最小示例：



```bash
# 使用配置文件中的默认 mirror
dpull docker.io/lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux

# 命令行指定 mirror 和平台
dpull --platform linux/arm64 \
      --mirror docker.1ms.run \
      docker.io/lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux
```

### 3.2 完整 Flag 列表

#### 平台与身份

| Flag | 缩写 | 默认 | 说明 |
|---|---|---|---|
| `--platform` | | 宿主机架构 | `linux/arm64`；多平台逗号分隔，此时强制 `--format oci` |
| `--tag` | | 同 IMAGE | 覆盖导入后的镜像名，如 `--tag myorg/sglang:local` |
| `--all-platforms` | | false | 下载 manifest list 中的全部平台 |

#### Mirror

| Flag | 缩写 | 默认 | 说明 |
|---|---|---|---|
| `--mirror` | `-m` | 配置文件 | mirror host，可重复指定，按序 failover |
| `--mirror-path` | | `{repo}` | 路径模板，与 `-m` 一一对应 |
| `--no-mirror` | | false | 忽略配置文件中的 mirror，直连原始 registry |

#### 下载控制（对标 aria2c）

| Flag | 缩写 | 默认 | aria2c 对应 | 说明 |
|---|---|---|---|---|
| `--connections` | `-x` | 8 | `-x` | 单个 blob 的最大连接数（上限 32） |
| `--jobs` | `-j` | 3 | `-j` | 并发下载的 blob 数 |
| `--min-split-size` | `-k` | `20M` | `-k` | 小于此值的 blob 不分片 |
| `--continue` | `-c` | true | `-c` | 断点续传，`--continue=false` 关闭 |
| `--max-tries` | | 10 | `-m` | 单分片最大重试次数，0 为无限 |
| `--retry-wait` | | `5s` | `--retry-wait` | 重试间隔（指数退避的基数） |
| `--max-rate` | | 无 | `--max-overall-download-limit` | 全局限速，如 `20M` |
| `--timeout` | | `60s` | `--timeout` | 单连接读超时 |
| `--connect-timeout` | | `15s` | | 建连超时 |

#### 输出

| Flag | 缩写 | 默认 | 说明 |
|---|---|---|---|
| `--output` | `-o` | 自动命名 | 输出文件路径；`-` 输出到 stdout |
| `--dir` | `-d` | `~/.dpull/cache` | 缓存与状态目录 |
| `--format` | | `docker` | `docker`（可 load）/ `oci`（OCI Layout 目录） |
| `--load` | | false | 完成后调用 `docker load`（需本机有 docker） |
| `--push` | | 无 | 完成后推送到指定 registry 引用 |
| `--keep-cache` | | false | 完成后保留 blob 缓存 |

自动命名规则：`{name}_{tag}_{arch}.tar`，如 `sglang_v0.5.15.post1-cu130-runtime-linux_arm64.tar`

#### 认证与网络

| Flag | 缩写 | 默认 | 说明 |
|---|---|---|---|
| `--user` | `-u` | 无 | `username:password`，留空则读 `~/.docker/config.json` |
| `--insecure` | | false | 跳过 TLS 证书校验 |
| `--plain-http` | | false | 使用 HTTP 而非 HTTPS |
| `--proxy` | | 环境变量 | HTTP 代理，如 `http://127.0.0.1:7890` |

#### 诊断

| Flag | 缩写 | 默认 | 说明 |
|---|---|---|---|
| `--progress` | | `auto` | `bar` / `plain` / `json` / `none` |
| `--verbose` | `-v` | false | 打印 HTTP 请求详情 |
| `--dry-run` | | false | 只解析并打印计划，不下载 |
| `--check-integrity` | | true | 下载完成后校验每层 sha256 |
| `--log-file` | | 无 | 日志写入文件 |

### 3.3 子命令

```bash
dpull info IMAGE              # 查看镜像详情，不下载
dpull ls                      # 列出所有未完成任务
dpull resume <task-id|--all>  # 恢复任务
dpull rm <task-id>            # 删除任务及其缓存

dpull auth login <host>       # 登录 registry
dpull auth logout <host>
dpull config init             # 生成默认配置文件
dpull config show             # 显示当前生效配置
dpull version
```

#### `dpull info` 输出示例

大镜像下手前必查，避免下到一半发现磁盘不够：

```
$ dpull info --platform linux/arm64 docker.io/lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux

Image:      docker.io/lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux
Digest:     sha256:a3f1c8d2e5b7...
Platform:   linux/arm64
Created:    2026-01-14 08:23:11 UTC

Layers:     28
Download:   18.4 GiB (compressed)
On disk:    ~42 GiB (estimated after extraction)

Largest layers:
  1. sha256:9c2e4f1a...   6.1 GiB   (CUDA runtime)
  2. sha256:3d81b0c7...   2.9 GiB
  3. sha256:f4a92e35...   1.4 GiB

Config:
  Entrypoint: ["python3", "-m", "sglang.launch_server"]
  WorkingDir: /sgl-workspace
  Env:        CUDA_VERSION=13.0, PATH=..., (14 more)

Mirror:     7c5a5eb84ed0a6a64e548ee8d2f90cb1.d.1ms.run  [OK, 12ms, Range: supported]
Local disk: /home/wenjin  (156 GiB available)  ✓
```

### 3.4 进度输出

`--progress bar`（默认，TTY 环境）：

```
sglang:v0.5.15.post1-cu130-runtime-linux (linux/arm64)   28 layers, 18.4 GiB

  [ 8/28] 9c2e4f1a  ████████████████████░░░░░  4.9G/6.1G   81%   38.2MB/s  8 conn
  [ 9/28] 3d81b0c7  ████████░░░░░░░░░░░░░░░░░  1.1G/2.9G   38%   22.7MB/s  8 conn
  [10/28] f4a92e35  ██░░░░░░░░░░░░░░░░░░░░░░░  180M/1.4G   12%   15.1MB/s  8 conn

  ✓ 7 completed (3.2 GiB)   ⏸ 18 queued

  Total  ██████████████░░░░░░░░░░░  9.4G/18.4G  51%  76.0MB/s  ETA 00:19:44
```

`--progress json`（CI 友好，每行一个 JSON 对象）：

```json
{"ts":"2026-08-05T10:23:11Z","event":"layer_progress","digest":"sha256:9c2e...","done":5261334528,"total":6553600000,"rate":40056832}
{"ts":"2026-08-05T10:23:12Z","event":"layer_complete","digest":"sha256:9c2e...","verified":true}
```

---

## 4. 配置文件

路径优先级：`--config` 指定 > `./dpull.yaml` > `~/.dpull/config.yaml`

```yaml
# ~/.dpull/config.yaml

# 默认参数
defaults:
  platform: linux/arm64
  connections: 8
  jobs: 3
  min_split_size: 20M
  max_tries: 10
  retry_wait: 5s
  check_integrity: true

# 缓存
cache:
  dir: ~/.dpull/cache
  max_size: 100G          # 超出后 LRU 清理
  keep_after_success: false

# 认证（也可用 dpull auth login 写入 ~/.dpull/auth.json）
auth:
  harbor.internal:
    username: robot$ci
    password_env: HARBOR_TOKEN    # 从环境变量读，避免明文

# 网络
network:
  proxy: ""
  timeout: 60s
  connect_timeout: 15s
  user_agent: "dpull/1.0"
```

配置好之后，日常使用退化为一行：

```bash
dpull docker.io/lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux
```

---

## 5. 架构设计

### 5.1 模块划分

```
┌─────────────────────────────────────────────────────────────┐
│                        cmd/dpull                            │
│              cobra CLI · flag 解析 · 配置加载                │
└────────────────────────────┬────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────┐
│                      internal/pull                          │
│                     编排器 (Orchestrator)                    │
│    解析 ref → 选 mirror → 拿 manifest → 调度 blob → 组装      │
└──┬────────┬────────┬────────┬────────┬────────┬─────────────┘
   │        │        │        │        │        │
┌──▼───┐ ┌──▼───┐ ┌──▼────┐ ┌─▼────┐ ┌─▼─────┐ ┌▼──────────┐
│ ref  │ │mirror│ │registry│ │downl │ │ store │ │  archive  │
│解析  │ │规则  │ │协议    │ │分片  │ │缓存   │ │  打包     │
│      │ │重写  │ │auth    │ │下载  │ │状态   │ │  docker/  │
│      │ │      │ │manifest│ │续传  │ │bitfield│ │  oci     │
└──────┘ └──────┘ └────────┘ └──────┘ └───────┘ └───────────┘
```

| 包 | 职责 |
|---|---|
| `internal/ref` | 镜像引用解析、canonical 归一化（补 `library/`、补 `:latest`） |
| `internal/mirror` | 模板渲染、mirror 选择、可达性探测、failover |
| `internal/registry` | Registry V2 API：token 认证、manifest 获取、平台选择、blob URL 解析 |
| `internal/downloader` | 分片调度、多连接、Range 请求、限速、重试、重定向刷新 |
| `internal/store` | 内容寻址缓存、`.part` 文件、bitfield 状态持久化 |
| `internal/archive` | docker-archive / OCI Layout 组装 |
| `internal/progress` | 进度聚合与渲染（bar / plain / json） |
| `internal/config` | 配置加载与合并 |
| `internal/auth` | 凭证管理，兼容 `~/.docker/config.json` |

### 5.2 主流程

```
1. 解析 IMAGE
   docker.io/lmsysorg/sglang:v0.5...  →  Reference{registry, repo, tag}
   记录 canonical 用于最终打包

2. 选择 mirror
   查配置 → 渲染模板 → 生成实际请求地址
   并发 HEAD /v2/ 探测可达性与延迟，选最优；失败自动切下一个

3. 认证
   GET /v2/ → 401 → 解析 WWW-Authenticate → 换 Bearer token
   token 有效期通常 5min，需在过期前刷新

4. 获取 manifest
   GET /v2/{repo}/manifests/{tag}
   Accept: application/vnd.oci.image.index.v1+json,
           application/vnd.docker.distribution.manifest.list.v2+json,
           application/vnd.oci.image.manifest.v1+json,
           application/vnd.docker.distribution.manifest.v2+json

   若为 index/list → 按 --platform 选出目标 manifest → 再取一次
   若为单 manifest → 校验 config.architecture 是否匹配 --platform，不匹配则警告

5. 获取 config blob
   GET /v2/{repo}/blobs/{config.digest}

6. 规划下载任务
   layers[] → 检查缓存中是否已有完整 blob（按 digest）
            → 检查是否有 .part 及 bitfield（断点）
            → 生成待下载分片列表

7. 并发下载
   信号量控制 jobs 个 blob 并发
   每个 blob 内部 x 个连接分片并发
   实时刷新 bitfield（fsync）

8. 校验
   逐 blob 计算 sha256 与 digest 比对
   失败的 blob 清除后重下

9. 组装
   docker 格式 → manifest.json + config + layers → tar
   RepoTags 写 canonical name（关键）

10. 收尾
    --load  → docker load
    --push  → 推送到目标 registry
    清理缓存（除非 --keep-cache）
```

---

## 6. 断点续传设计

### 6.1 分片策略

```
blob size < min-split-size (20M)  →  单连接直下，仅支持简单 Range 续传
blob size >= min-split-size       →  分片并发

piece_size = max(20MB, blob_size / connections)
pieces     = ceil(blob_size / piece_size)
```

例：6.1 GiB 的层，`-x 8` → piece_size = 781 MiB，8 片，8 连接各负责一片。

> **权衡**：piece 太小会产生大量 HTTP 请求和 bitfield 写入开销；太大则中断时丢失的进度多。20MB 下限 + 按连接数均分是折中方案。可考虑对超大 blob（>4GB）强制 piece_size 上限 256MB，降低单次丢失量。

### 6.2 状态文件

每个任务一个 `.dpull` 状态文件（对标 aria2 的 `.aria2` 控制文件）：

```
~/.dpull/cache/
├── tasks/
│   └── a3f1c8d2-linux-arm64.json        # 任务状态
└── blobs/
    └── sha256/
        ├── 9c2e4f1a....part             # 下载中
        ├── 9c2e4f1a....bitfield         # 分片完成位图
        └── 3d81b0c7...                  # 已完成（去掉 .part）
```

`tasks/a3f1c8d2-linux-arm64.json`：

```json
{
  "task_id": "a3f1c8d2-linux-arm64",
  "version": 1,
  "canonical": "docker.io/lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux",
  "platform": "linux/arm64",
  "manifest_digest": "sha256:a3f1c8d2e5b7...",
  "config_digest": "sha256:7b3e9f21...",
  "mirror": {
    "endpoint": "7c5a5eb84ed0a6a64e548ee8d2f90cb1.d.1ms.run",
    "path": "lmsysorg/sglang"
  },
  "created_at": "2026-08-05T10:20:00Z",
  "updated_at": "2026-08-05T10:47:33Z",
  "total_size": 19756941312,
  "blobs": [
    {
      "digest": "sha256:9c2e4f1a...",
      "size": 6553600000,
      "piece_size": 819200000,
      "pieces": 8,
      "bitfield": "fc",
      "state": "downloading"
    },
    {
      "digest": "sha256:3d81b0c7...",
      "size": 3113851904,
      "state": "complete",
      "verified": true
    }
  ]
}
```

**bitfield** 为十六进制字符串，每 bit 对应一个 piece，`1` 表示已完成。`"fc"` = `11111100`，即前 6 片已完成。

写入策略：每完成一片 → 更新 bitfield → 写临时文件 → `fsync` → `rename` 原子替换。进程被 kill -9 最多丢一片。

### 6.3 恢复逻辑

```
dpull resume a3f1c8d2-linux-arm64
或
重新执行相同的 dpull 命令（自动匹配 task_id）
```

恢复时：

1. 读取状态文件
2. 校验 `manifest_digest` 是否仍然一致（tag 可能被重新推送）—— 不一致则提示并要求 `--force` 才继续
3. 对每个 blob：
   - `state == complete && verified` → 跳过
   - 有 `.part` + bitfield → 只下载 bitfield 为 0 的片
   - 无记录 → 全新下载
4. **重新获取 blob 的下载 URL**（见 6.4）

### 6.4 预签名 URL 过期（关键坑）

Registry 的 blob 端点通常返回 `307` 重定向到对象存储的**带签名 URL**：

```
GET /v2/lmsysorg/sglang/blobs/sha256:9c2e...
→ 307 Location: https://xxx.oss-cn-hangzhou.aliyuncs.com/docker/registry/v2/blobs/...
                ?Expires=1754387234&Signature=xxx
```

签名有效期通常 **5–15 分钟**。这意味着：

- ❌ **不能**把重定向后的 URL 存进状态文件供下次复用 → 恢复时必然 403
- ❌ **不能**在一次长时间下载中始终使用同一个 URL → 下载 6GB 层耗时超过有效期后中途 403
- ✅ **必须**：每个分片连接建立前，或检测到 403/401 时，重新请求 `/v2/{repo}/blobs/{digest}` 获取新的重定向地址

实现要点：

```go
// 伪代码
type BlobURLResolver struct {
    mu       sync.Mutex
    cached   string
    expireAt time.Time
}

func (r *BlobURLResolver) Get(ctx context.Context) (string, error) {
    r.mu.Lock()
    defer r.mu.Unlock()
    // 保守起见提前 60s 视为过期
    if time.Now().Before(r.expireAt.Add(-60 * time.Second)) {
        return r.cached, nil
    }
    // 重新走 /v2/ 端点拿 307，禁止自动跟随重定向
    loc, exp := r.resolve(ctx)
    r.cached, r.expireAt = loc, exp
    return loc, nil
}
```

有效期从 URL 的 `Expires` 参数解析；解析不到则默认 5 分钟。

**这是自己实现相比直接用 aria2c 拉 blob 的最大优势** —— aria2c 无法感知 registry 语义，中断恢复时用旧 URL 必然失败。

### 6.5 Range 支持探测

并非所有 registry / CDN 都支持 Range 请求：

```
HEAD /v2/{repo}/blobs/{digest}   （跟随重定向）
→ Accept-Ranges: bytes  +  Content-Length: N   → 可分片
→ 否则                                          → 退化为单连接
```

退化模式仍支持简单续传：`Range: bytes={已下载字节数}-`，若服务端返回 `200` 而非 `206`，说明不支持，只能从零开始。

---

## 7. 输出格式

### 7.1 docker-archive（默认）

tar 包结构：

```
manifest.json
repositories                      (可选，兼容旧版)
blobs/sha256/<config-digest-hex>
blobs/sha256/<layer1-digest-hex>
blobs/sha256/<layer2-digest-hex>
...
```

**`manifest.json` 是让 `docker load` 显示正确名称的关键**：

```json
[
  {
    "Config": "blobs/sha256/7b3e9f21...",
    "RepoTags": [
      "lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux"
    ],
    "Layers": [
      "blobs/sha256/9c2e4f1a...",
      "blobs/sha256/3d81b0c7..."
    ]
  }
]
```

**RepoTags 归一化规则**（务必按此实现，否则 `docker images` 显示会很难看）：

| canonical 输入 | RepoTags 写入 |
|---|---|
| `docker.io/lmsysorg/sglang:v1` | `lmsysorg/sglang:v1` （去掉 `docker.io/`） |
| `docker.io/library/nginx:1.27` | `nginx:1.27` （去掉 `docker.io/library/`） |
| `ghcr.io/foo/bar:v1` | `ghcr.io/foo/bar:v1` （保留） |
| `harbor.internal/lib/app:v1` | `harbor.internal/lib/app:v1` （保留） |

即：**只有 Docker Hub 去前缀，其他 registry 保留完整地址**，与 `docker pull` 后的显示习惯一致。

`--tag` 指定时直接使用该值（同样应用上述归一化）。

> **注意 layer 顺序**：`Layers` 数组必须与 manifest 中的顺序严格一致，且与 config 的 `rootfs.diff_ids` 对应。顺序错乱会导致 `docker load` 成功但容器启动异常。

> **压缩层的处理**：manifest 中的 layer digest 是**压缩后**的（`.tar.gz`），而 config 的 `diff_ids` 是**解压后**的。docker-archive 格式接受 gzip 压缩的 layer 文件，无需解压，直接放入即可。

### 7.2 OCI Layout（`--format oci`）

多平台下载时强制使用此格式：

```
<output-dir>/
├── oci-layout                    {"imageLayoutVersion":"1.0.0"}
├── index.json
└── blobs/sha256/...
```

`index.json`：

```json
{
  "schemaVersion": 2,
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:a3f1c8d2...",
      "size": 4721,
      "platform": {"architecture": "arm64", "os": "linux"},
      "annotations": {
        "org.opencontainers.image.ref.name": "lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux"
      }
    }
  ]
}
```

后续可用 `skopeo copy oci:dir:tag docker://...` 或 `ctr images import` 消费。

---

## 8. 项目结构

```
dpull/
├── cmd/
│   └── dpull/
│       ├── main.go
│       ├── root.go               # 根命令与全局 flag
│       ├── pull.go               # 默认命令
│       ├── info.go
│       ├── resume.go
│       ├── ls.go
│       ├── clean.go
│       ├── load.go
│       ├── push.go
│       ├── auth.go
│       └── config.go
├── internal/
│   ├── ref/
│   │   ├── parse.go              # 引用解析与归一化
│   │   └── parse_test.go
│   ├── mirror/
│   │   ├── template.go           # 模板渲染
│   │   ├── selector.go           # 探测与 failover
│   │   └── template_test.go
│   ├── registry/
│   │   ├── client.go
│   │   ├── auth.go               # Bearer token 获取与刷新
│   │   ├── manifest.go           # manifest / index 解析与平台选择
│   │   ├── blob.go               # blob URL 解析（含预签名刷新）
│   │   └── types.go
│   ├── downloader/
│   │   ├── downloader.go         # 调度器
│   │   ├── piece.go              # 分片下载
│   │   ├── ratelimit.go
│   │   └── retry.go
│   ├── store/
│   │   ├── store.go              # 内容寻址缓存
│   │   ├── bitfield.go
│   │   └── task.go               # 状态文件读写
│   ├── archive/
│   │   ├── docker.go             # docker-archive 组装
│   │   ├── oci.go                # OCI Layout 组装
│   │   └── docker_test.go
│   ├── progress/
│   │   ├── bar.go
│   │   ├── json.go
│   │   └── aggregate.go
│   ├── config/
│   │   └── config.go
│   └── auth/
│       └── keychain.go           # 兼容 ~/.docker/config.json
├── pkg/
│   └── version/
│       └── version.go            # ldflags 注入
├── testdata/
├── .github/
│   └── workflows/
│       ├── ci.yml
│       └── release.yml
├── .goreleaser.yaml
├── Makefile
├── go.mod
├── LICENSE
└── README.md
```

## 9. 依赖选型

| 用途 | 库 | 说明 |
|---|---|---|
| CLI | `github.com/spf13/cobra` | 子命令、flag |
| 配置 | `github.com/spf13/viper` | YAML + 环境变量 |
| Registry 协议 | `github.com/google/go-containerregistry` | **只用 `pkg/name`、`pkg/authn`、`pkg/v1/types`**，下载环节自己实现 |
| 进度条 | `github.com/vbauerster/mpb/v8` | 多进度条渲染 |
| 限速 | `golang.org/x/time/rate` | 令牌桶 |
| 重试 | `github.com/cenkalti/backoff/v4` | 指数退避 |
| 日志 | `log/slog` | 标准库，Go 1.21+ |
| 单位解析 | `github.com/docker/go-units` | `20M` → 字节 |

**关于 go-containerregistry**：借用它的引用解析、认证链、类型定义可以省掉大量协议细节（token scope 拼接、WWW-Authenticate 解析、Docker Hub 特殊处理等），但 `remote.Image()` 的下载路径是流式无断点的，必须绕过自己实现。这是 dpull 的核心价值所在。

---

## 10. GitHub Actions 自动构建

### 10.1 `.goreleaser.yaml`

```yaml
version: 2

project_name: dpull

before:
  hooks:
    - go mod tidy

builds:
  - id: dpull
    main: ./cmd/dpull
    binary: dpull
    env:
      - CGO_ENABLED=0
    ldflags:
      - -s -w
      - -X github.com/YOURORG/dpull/pkg/version.Version={{.Version}}
      - -X github.com/YOURORG/dpull/pkg/version.Commit={{.Commit}}
      - -X github.com/YOURORG/dpull/pkg/version.BuildDate={{.Date}}
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
      - arm
    goarm:
      - "7"
    ignore:
      # macOS 无 32 位 ARM
      - goos: darwin
        goarch: arm
      # Windows ARMv7 无意义
      - goos: windows
        goarch: arm

archives:
  - id: default
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    name_template: >-
      {{ .ProjectName }}_{{ .Version }}_
      {{- if eq .Os "darwin" }}Darwin{{ else if eq .Os "linux" }}Linux
      {{- else if eq .Os "windows" }}Windows{{ else }}{{ .Os }}{{ end }}_
      {{- if eq .Arch "amd64" }}x86_64
      {{- else if eq .Arch "arm" }}armv{{ .Arm }}
      {{- else }}{{ .Arch }}{{ end }}
    files:
      - README.md
      - LICENSE

checksum:
  name_template: "{{ .ProjectName }}_{{ .Version }}_checksums.txt"
  algorithm: sha256

snapshot:
  version_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc
  use: github
  groups:
    - title: Features
      regexp: '^.*?feat(\(.+\))??!?:.+$'
      order: 0
    - title: Bug Fixes
      regexp: '^.*?fix(\(.+\))??!?:.+$'
      order: 1
    - title: Performance
      regexp: '^.*?perf(\(.+\))??!?:.+$'
      order: 2
    - title: Others
      order: 999
  filters:
    exclude:
      - '^docs:'
      - '^test:'
      - '^chore:'

release:
  draft: false
  prerelease: auto
  footer: |
    ## 安装

    ### Linux / macOS
    ```bash
    VERSION={{ .Tag }}
    OS=$(uname -s); ARCH=$(uname -m)
    curl -sL "https://github.com/YOURORG/dpull/releases/download/${VERSION}/dpull_${VERSION#v}_${OS}_${ARCH}.tar.gz" \
      | sudo tar -xz -C /usr/local/bin dpull
```

    ### Windows
    下载对应的 zip 包解压即可。
```

### 10.2 `.github/workflows/release.yml`

**打 tag 自动构建并发布**：

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0        # goreleaser 需要完整历史生成 changelog

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache: true

      - name: Run tests
        run: go test ./... -race

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

发布流程：

```bash
git tag -a v0.1.0 -m "first release"
git push origin v0.1.0
# → Actions 自动构建 10+ 平台产物并创建 Release
```

### 10.3 `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache: true
      - run: go build ./...
      - run: go test ./... -race -coverprofile=coverage.out
      - name: Upload coverage
        if: matrix.os == 'ubuntu-latest'
        uses: codecov/codecov-action@v4

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: golangci/golangci-lint-action@v6
        with:
          version: latest

  build-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - name: Cross-compile check
        run: |
          for target in linux/amd64 linux/arm64 linux/arm \
                        darwin/amd64 darwin/arm64 \
                        windows/amd64 windows/arm64; do
            GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 \
              go build -o /dev/null ./cmd/dpull || exit 1
            echo "✓ $target"
          done
```

### 10.4 Makefile

```makefile
VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT  := $(shell git rev-parse --short HEAD)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := github.com/YOURORG/dpull/pkg/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).BuildDate=$(DATE)

.PHONY: build test lint clean snapshot install

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/dpull ./cmd/dpull

install: build
	install -m 0755 bin/dpull /usr/local/bin/dpull

test:
	go test ./... -race -cover

lint:
	golangci-lint run

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin/ dist/
```

---

## 11. 跨平台注意事项

| 问题 | 处理 |
|---|---|
| 路径分隔符 | 一律用 `filepath.Join`，禁止硬编码 `/` |
| 缓存目录 | `os.UserCacheDir()`：Linux `~/.cache`、macOS `~/Library/Caches`、Windows `%LocalAppData%` |
| 文件锁 | Windows 无 `flock`，用 `github.com/gofrs/flock` 抹平 |
| 原子 rename | Windows 下目标文件存在时 `os.Rename` 会失败，需先删除 |
| 大文件 seek | 32 位平台注意 `int64` 溢出，统一用 `int64` |
| 进度条 | Windows 旧终端不支持 ANSI，检测后降级为 `plain` |
| `--load` | Windows 上 docker 可能在 WSL 中，需检测 `docker` 是否在 PATH |
| 文件名 | Windows 不允许 `:`，自动命名时把 tag 中的 `:` 替换为 `_` |
| CGO | 全部 `CGO_ENABLED=0`，保证静态链接 |

---

## 12. 测试计划

### 单元测试

- `ref`：各种引用格式解析（含 `library/` 补全、digest 引用、端口号）
- `mirror`：模板渲染覆盖所有变量组合
- `store`：bitfield 读写、并发安全、崩溃恢复
- `archive`：RepoTags 归一化规则表驱动测试

### 集成测试

用本地 `registry:2` 起测试环境：

```bash
docker run -d -p 5000:5000 --name test-reg registry:2
crane copy alpine:3.19 localhost:5000/alpine:3.19 --insecure
```

场景：

1. 完整下载 → `docker load` → 校验镜像名与 digest
2. 下载中途 `kill -9` → resume → 校验完整性
3. mirror failover：第一个 mirror 不可达时自动切换
4. Range 不支持时的降级路径
5. 预签名 URL 过期后的重新解析
6. 多平台 index 的平台选择
7. 磁盘写满时的错误处理

### 手工验收

用真实大镜像跑一遍，这是最终验收标准：

```bash
dpull --platform linux/arm64 -x 8 -j 3 \
      -m 7c5a5eb84ed0a6a64e548ee8d2f90cb1.d.1ms.run \
      docker.io/lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux

# 中途 Ctrl-C 若干次，重跑同一命令，确认从断点恢复
# 完成后 docker load，确认显示 lmsysorg/sglang:v0.5.15.post1-cu130-runtime-linux
```

---

## 13. 开发里程碑

### v0.1 — 能用（约 1500 行）

- [ ] 引用解析与 canonical 归一化
- [ ] Registry V2 协议：token 认证、manifest 获取、平台选择
- [ ] mirror 模板重写（单 mirror，无 failover）
- [ ] 单连接下载 + 简单 Range 续传
- [ ] docker-archive 输出，RepoTags 正确
- [ ] 基础进度条
- [ ] GoReleaser + GitHub Actions

### v0.2 — 好用

- [ ] 分片并发下载（`-x` / `-j`）
- [ ] bitfield 状态持久化，字节级断点
- [ ] 预签名 URL 自动刷新
- [ ] `dpull info` / `ls` / `resume` / `clean`
- [ ] 配置文件支持
- [ ] sha256 校验

### v0.3 — 完善

- [ ] mirror failover 与延迟探测
- [ ] `--push` / `--load`
- [ ] OCI Layout 与多平台
- [ ] 限速、代理
- [ ] JSON 进度输出（CI 集成）
- [ ] 缓存 LRU 管理

### v1.0

- [ ] 完整测试覆盖
- [ ] Homebrew tap / Scoop manifest
- [ ] 文档与使用示例
- [ ] cosign 签名验证（可选）

---

## 14. 风险与权衡

| 风险 | 影响 | 缓解 |
|---|---|---|
| 加速站不稳定 / 关停 | 下载失败 | 多 mirror failover；配置文件与代码解耦，换源无需升级 |
| 预签名 URL 机制各家不同 | 续传失败 | 统一按"每次连接前重新解析"处理，不依赖 URL 缓存 |
| 高并发被 registry 限流 | 429 / 封 IP | 默认 `-x 8 -j 3` 保守值；识别 429 后自动降并发 |
| 大镜像磁盘占用翻倍 | 磁盘写满 | 下载前检查可用空间；`--push` 模式流式转发不落 tar |
| Docker Hub 匿名拉取限额 | 401/429 | 支持 `--user`；mirror 通常已绕过此限制 |
| manifest 在下载期间被覆盖 | 数据不一致 | 状态文件记录 manifest digest，恢复时比对 |

---

## 15. 附：与现有工具的关系

dpull 不重复造轮子，而是补齐现有工具在**弱网大镜像**场景下的空白：

- **协议层**复用 go-containerregistry，与 crane 生态兼容
- **下载层**自研，这是 crane / skopeo 都缺失的部分
- **产物**为标准 docker-archive / OCI Layout，可被 docker、containerd、skopeo、crane 直接消费

定位：`aria2c` 之于 `wget`，`dpull` 之于 `crane`。
