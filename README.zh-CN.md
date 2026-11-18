# Virelz

Virelz 是一个 Bilibili 命令行下载器。给它一个页面链接，它会把视频以**你账号能看到的最高画质**保存到你自己的电脑上。

它处理同一平台上的两类完全不同的内容：

- **普通视频** —— UP 主发布的稿件，用 `BV` 号或 `av` 号定位。
- **集序内容** —— 番剧、电影、电视剧，按季和集发布，用 `ep`（单集）号或 `ss`（整季）号定位。给一个 `ep` 链接就下载那一集，并如实告诉你拿到的是第几集。

两类内容最终都会被解析成真实媒体流，并合并成一个画面和声音都在的 MP4 文件写入磁盘。

## 这个项目解决什么问题

Bilibili 的观看页本身不是文件。页面里只有一个 id，真正的流地址要向另外的接口去换，而且普通视频和番剧走的接口不一样、数据形状不一样，还要求带上 CDN 会校验的 `Referer`；签名直链有时效，画质档位又取决于请求方是谁。手工做一遍，意味着要在好几个接口之间来回试，最后还要把一条纯视频轨和一条纯音频轨重新拼成一个文件。

Virelz 把这一串工作合成一步：

1. 判断链接指向的是哪类内容，不只看 URL 的形状。
2. 按内容类型向对应的元数据和播放接口请求。
3. 直接请求最高画质，并在返回的轨道里挑最好的那条。
4. 并发下载各轨道，带真实进度条。
5. 把分离的视频轨和音频轨合并成一个 MP4，不重新编码。
6. 整个传输成功后才发布最终文件。

结果是一个任何播放器都能直接打开的文件，外加一段简短报告，说明你到底拿到了什么。

## 目前支持的平台

只有下表列出的内容实现了并做过端到端验证。其余链接会明确报「不支持」，而不是靠猜去凑。

| 平台 | 接受的输入 | 内容类型 | 状态 |
| --- | --- | --- | --- |
| Bilibili | 含 `BV` 或 `av` 号的页面链接 | 普通视频 | 已验证 |
| Bilibili | 含 `ep` 或 `ss` 号的页面链接 | 番剧、电影、电视剧 | 已验证 |

几点说明：

- 不接受 CDN 直链。Virelz 需要页面链接，才能自己去解析真实流地址；给了直链它会直接说明，而不是抛一个看不懂的错。
- `ep` 链接下载的就是那一集。
- `ss` 链接下载该季的第 1 集。想要后面的集，请用具体的 `ep` 链接。
- 付费内容、大会员专享、地区限制内容不下载。如果服务端只给试看片段，Virelz 会拒绝并说明原因，而不是写一个半截的「一集」出来还告诉你成功了。

## 安装

### 直接下载发布版

从最新发布版下载预编译好的二进制，放到 `PATH` 里的任意目录即可：

1. 打开 [**Releases → latest**](../../releases/latest)。
2. 下载对应平台的压缩包（`virelz_<版本>_windows_amd64.zip`，Linux 和 macOS 为 `.tar.gz`）。
3. 解压后在终端运行 `virelz`（Windows 上是 `virelz.exe`）；也可以直接双击 `virelz.exe` 进入交互模式。

不需要装任何其他东西，发布版二进制是自包含的。

### 从源码构建

需要 Go 1.26 或更高版本。

```bash
go build -o virelz.exe ./cmd/virelz
```

产出同样的自包含二进制，无需其他运行时依赖。其他平台改一下输出名即可，例如
`go build -o virelz ./cmd/virelz`。

## 使用

### 交互模式

双击 `virelz.exe` 会进入提示符。直接粘贴链接回车即下载，`help` 看命令列表，`exit` 退出：

```
virelz 0.1.0 - resolve and download videos
Type a link to download it, "help" for all commands, "exit" to leave.

virelz> https://www.bilibili.com/bangumi/play/ep2436276
virelz> virelz -d "https://www.bilibili.com/video/BV1GJ411x7h7" -o D:\videos
virelz> exit
```

在提示符内，行首的 `virelz` 或 `virelz.exe` 会被自动忽略，所以从本文档或从普通终端复制来的整行命令可以原样粘贴执行。

### 一次性命令

同一个二进制也能直接在终端里用：

```bash
virelz -d "https://www.bilibili.com/video/BV1GJ411x7h7"
```

传输过程中进度条从 0 走到 100%。文件完成后进度条会被擦除，所以报告紧贴在命令那一行后面，中间不留空行。每个标签都从第一列开始，并补齐宽度使所有值对齐：

```
Platform:         bilibili
Title:            他好像真当过老师
Author:           寻洋贱长
Quality:          1080p
MediaDuration:    2m37s
Memory:           39.5 MiB
DownloadDuration: 1.2s
SaveDir:          D:\videos\他好像真当过老师.mp4
```

`MediaDuration` 是视频本身的时长，`DownloadDuration` 是传输耗时。平台 id 不会被拼进文件名。当输出被重定向到文件或管道时，不画进度条，只打印这一块，方便留干净的日志。

### 一次多个链接

先解析，再并发下载：

```bash
virelz -d "link1,link2,link3"
```

`-d` 可以重复写，链接也可以作为普通参数直接跟：

```bash
virelz "link1" "link2" -j 2 -o "D:\videos"
```

### 参数

| 参数 | 含义 |
| --- | --- |
| `-d, --download strings` | 要下载的链接，逗号分隔或重复多次 |
| `-o, --output string` | 目标目录；默认 `<temp>/virelz/<yyyyMMdd>` |
| `-j, --jobs int` | 同时下载几个链接（默认 `3`） |
| `--overwrite` | 覆盖已存在文件，而不是报错 |

默认目录是系统临时目录下按天分的一个文件夹，用本地时区。`-o` 则完全按你给的路径使用，不再追加日期层级。

### 命令

| 命令 | 含义 |
| --- | --- |
| `virelz [flags]` | 等同于 `download` |
| `virelz download [flags]` | 下载链接 |
| `virelz resolve <link>` | 只显示链接解析结果，不下载 |
| `virelz platforms` | 列出当前构建支持的平台 |
| `virelz set sessdata = "<value>"` | 保存你的 B 站登录 Cookie |
| `virelz set sessdata` | 查看是否已保存 |
| `virelz set sessdata =` | 删除已保存的 Cookie |
| `virelz get sessdata` | 等同于不带值的 `set sessdata` |

在终端里不带参数运行会进入提示符；在没有终端的环境（管道或脚本）里不带参数运行，则改为打印帮助。

## 画质

Virelz 总是直接请求平台能给的最高画质，只有在最高档拿不到时才降级。

Bilibili 的几个播放接口返回结果并不一致，所以凡相关的接口都用同样的画质参数请求一次，取最好的那个结果。普通视频用 DASH 接口（`fnval=4048`，返回分离的视频轨和音频轨）和单文件 MP4 接口（`fnval=1`，同一个片子有时反而比 DASH 给得更高）。番剧用自己的季信息接口和播放接口，因为一部番不是普通稿件，走普通视频接口会被压得很低。

拿到多条轨道时，按这个顺序挑：先比画质 ID，再比编码兼容性（AVC 优于 HEVC，HEVC 优于 AV1），最后比带宽。没有可用地址的轨道直接跳过。

### 登录以解锁更高画质

匿名请求会被 Bilibili 限制在远低于网页标称的分辨率。保存你自己的会话 Cookie 就能解除这个限制，全程不碰访问控制，也不碰付费内容。Virelz 读取的是你自己浏览器会话里的 `SESSDATA`。设置一次，直接用二进制完成：

```bat
virelz set sessdata = "<你的 SESSDATA 值>"
```

它会输出 `sessdata saved to ...\bilibili.json`。之后每次下载都会自动请求该账号允许的最高画质：

```bat
virelz -d "https://www.bilibili.com/video/BV1qqsk65E7t"
```

其他写法效果相同。`virelz set sessdata` 只告诉你有没有存过、不会把值打出来；`virelz set sessdata =` 删除。这些写法在终端和 `virelz>` 提示符里都能用。

值保存在你的用户配置目录下的 `bilibili.json`，文件权限限定为你本人。外层引号会被剥掉，所以粘贴 `"abc"` 或 `abc` 存下的都是 `abc`。环境变量 `VIRELZ_BILI_SESSDATA` 优先级仍然更高，方便临时换账号而不改动已保存的内容。

### 实际能拿到什么画质

`Quality:` 报的是真正选中的那条流，不是请求的档位。本机实测：

| 内容 | 匿名 | 带自己的 Cookie |
| --- | --- | --- |
| 普通视频 | DASH 480p，MP4 720p | DASH 1080p |
| 集序内容 | 480p | 576p |

由此有两条需要如实说明的限制：

- 非大会员账号止步 1080p。4K 和 HDR 档位属于付费会员，Virelz 只报实际拿到的东西，不会宣称一个没拿到的档位。
- 报出的高度是轨道真实高度。番剧里画质 ID 为 `64` 的轨道可能实际就是 `768x576`，那就如实报 `576p`，因为文件里装的就是这个。

## 输出

当源站把视频和音频分开给时，Virelz 会把它们合并成一个 MP4，不重新编码，所以结果是画面和声音都在的单个文件。每个下载先写临时文件，整个传输完成后才发布最终文件名；中断的文件会被清理。只要没有完整写出并落盘，任务就不会被报成成功。

## 说明

- 下载会带上平台要求的 `Referer`。不绕过登录、DRM、访问控制或限流。
- 只下载你有权下载的内容。
- 请求受连接和响应头超时约束；但传输慢不会因为一个整体下载超时而被错误中断。

## 许可证

MIT，见 [LICENSE](LICENSE)。

[English](README.md)