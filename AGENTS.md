# Virelz Architecture and Development Guidelines

Virelz is a Go-based video link resolver and downloader intended to support multiple platforms. The current interface is a Cobra CLI, with reusable backend capabilities for a future Electron application. Add platform support incrementally and only describe a platform as supported after its implementation and permitted download path have been verified.

## Architecture

- `cmd/virelz/main.go` is the executable entry point. Keep it small and delegate startup to `internal/app`.
- `pkg/platform/` is the reusable platform foundation. Put the shared platform interface, shared media/result types, registry, and platform-specific adapters such as `x`, `tk`, `ins`, and `fb` here. Add a platform package only when implementing that platform; do not create empty placeholders.
- `internal/` contains Virelz-specific application logic. Put application wiring in `internal/app/`, Cobra commands and terminal presentation in `internal/cli/`, and task orchestration, concurrency, generic transfer, and file handling in `internal/downloader/`.
- Dependency direction is `internal -> pkg/platform`. Packages under `pkg/` must not import `internal/`.
- Keep code in the package that owns the behavior. Do not create generic `utils` or `fileutil` packages preemptively; extract a focused shared package only when real reuse exists.
- Platform adapters own source-specific URL matching and media resolution. The downloader owns scheduling and generic download behavior. If a platform later requires a specialized transfer flow, extend the platform contract deliberately rather than putting platform-specific branches in the generic downloader.

## Download Behavior

- Keep the initial implementation narrow: one verified source and a correct end-to-end flow before adding more platforms or parallelism. Prefer serial downloads until concurrency is needed and tested.
- Use `context.Context` for cancellation and request deadlines. Bound platform resolution separately from file transfer; do not apply a short whole-download timeout that can incorrectly fail large or slow downloads.
- Configure connection and response-header timeouts for HTTP requests. Make timeout values configurable or keep defaults in the package that owns them; do not collect unrelated constants in a global constants package.
- Stream response bodies to disk instead of buffering entire videos in memory. Each concurrent task must write to its own temporary file and publish the final filename only after a successful transfer. Remove incomplete temporary files on failure or cancellation.
- Use one writer per task. Do not have concurrent tasks write to the same ordinary `io.Writer`. Parallel range downloads of one file require explicit range validation and offset-based writes; do not add them unless there is a measured need.
- Report transfer progress from bytes actually written. Keep terminal rendering in the CLI and serialize concurrent progress updates through a single renderer or equivalent mechanism.
- Do not show a download progress bar before resolution succeeds. On failure, stop that task's progress display and show a clear failure result; do not leave a stale or frozen bar.

## Errors and Diagnostics

- Do not introduce custom error types or a project-wide error framework by default. Use `fmt.Errorf` with `%w` to add context and standard-library `errors.Is`/`errors.As` where needed.
- Show concise, user-safe errors in the CLI. Keep useful wrapped causes in diagnostics, but do not expose credentials, tokens, or sensitive URL parameters in terminal output or logs.
- Never report a task as successful unless the complete output has been written and finalized.

## License Headers

- This project is licensed under the MIT License. Add an SPDX identifier and the copyright attribution from `LICENSE` to the top of each first-party source file, using that language's comment syntax. For Go files, use `// SPDX-License-Identifier: MIT` and `// Copyright (c) 2026 hourinX`.
- Do not add these headers to generated files, vendored or third-party code, lockfiles, binary assets, or `LICENSE` itself. Do not alter files that cannot represent comments without breaking their format.

## Scope and Validation

- Only support content the user is authorized to access and download. Do not bypass login requirements, access controls, DRM, or service-side rate limits. If a source has no confirmed permitted download path, report it as unsupported rather than claiming support.
- Add focused tests for URL matching, parsing outcomes, cancellation/timeouts, file cleanup, and progress accounting as those behaviors are implemented.
- Keep the README aligned with implemented sources, actual flags, and tested behavior. Do not document planned platform support as available functionality.
