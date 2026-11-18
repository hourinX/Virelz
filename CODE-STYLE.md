# Virelz 代码规范

本文件约束 Virelz 的源码书写方式。规范以可读性优先：读代码的人应当能在一眼之内看清一个函数分成哪几块、每块在做什么。

## 1. 许可证头

每个第一方源文件顶部只保留两行：

```go
// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX
```

其后直接跟 `package` 声明。不再写包级说明注释。`README.md`、`LICENSE`、生成文件、第三方代码不加此头。

## 2. 注释规则

注释总量必须少。注释解释「为什么」和「这里在做什么」，不复述代码本身。

### 2.1 唯一保留的多行注释：函数顶层一行

每个函数只允许在函数上方保留一行注释：

```go
// runURL resolves the link, prints its streams, and downloads them.
func runURL(...) error {
```

函数体内部不写段落式注释。函数内的说明改写成行尾注释（见 2.2）。

### 2.2 字段与变量：行尾一句话

结构体字段、包级变量、常量只用行尾注释，一句话、直白、精简：

```go
type Config struct {
	Name    string            // command name used in help and errors
	Version string            // reported by --version
	Output  string            // destination root, empty means the default
	Jobs    int               // parallel downloads
}
```

禁止在字段上方写多行注释。同一结构体里能自解释的字段不加注释。

### 2.3 关键调用处的意图标识

重要位置允许在代码行尾标一句目的，尤其是跨层调用和平台基座交互：

```go
p, err := cfg.Registry.Lookup(rawURL)  // resolve through the platform registry
files, err := downloader.Download(...) // generic transfer, platform-agnostic
```

只在「不加会看不懂为什么这么调」的地方加。普通赋值、普通返回不加。

### 2.4 语言

注释一律英文。不写中文注释。

## 3. 函数内部分块

一个函数内部按职责切块。每块是一段连续的完整逻辑，块与块之间空一行。块内不留空行。

### 3.1 块的定义

一块通常由三部分组成，顺序固定：

1. 一句话标明这一块在做什么（可以省略，块意图已由行尾注释表达时）
2. 主体调用或计算
3. `if err != nil` 之类的错误处理，紧跟主体

### 3.2 标准形态

```go
func Execute(ctx context.Context, cfg Config, args []string) int {
	// defaults
	if cfg.Name == "" {
		cfg.Name = "virelz"
	}
	if cfg.Output == "" {
		cfg.Output = assets.DefaultRoot()
	}

	// build and run the command
	root := newRootCommand(cfg)
	if len(args) > 0 {
		root.SetArgs(args)
	}

	// report a failure once
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(cfg.Stderr, "%s: %v\n", cfg.Name, err)
		return 1
	}

	return 0
}
```

要点：

- 块与块之间**恰好一个空行**
- 块内不要为了对齐或美观插入空行
- 错误处理属于它所属那一段的末尾，不单独成块

### 3.3 反面写法

不要这样，可读性会被打散：

```go
	// 校验
	if a == "" {
		return err
	}

	b := compute(a)

	// 继续
	if b == nil {
		return err
	}
```

问题在于：一个逻辑单元被切碎成多个「注释 + 一个 if」，读者需要重新拼装上下文。

### 3.4 长函数

函数超过约 60 行时，优先拆成多个小函数，而不是在函数内继续堆块。拆分后的每个小函数遵守第 2.1 节（一行顶层注释）。

## 4. 错误处理

- 用 `fmt.Errorf` 配合 `%w` 包装，保留原因链
- 用标准库 `errors.Is` / `errors.As` 判断
- 不新建自定义错误类型，不建项目级错误框架
- 终端只输出简短、不含凭据的信息

## 5. 目录与依赖方向

```
cmd/virelz        -> internal/app
internal/app      -> internal/cli, internal/downloader, assets, pkg/platform
internal/cli      -> internal/downloader, assets, pkg/platform
internal/downloader -> pkg/platform
pkg/platform      -> 标准库
```

- `pkg/` 下的包不得 import `internal/`
- `assets` 只放静态常量与静态资源，不依赖任何业务包
- 不预先创建 `utils`、`fileutil` 之类通用包，出现真实复用再抽取

## 6. 验证

改动完成后必须全部通过：

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

`gofmt -l .` 无输出才算完成。
