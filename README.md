# Virelz

Virelz is a command-line downloader for Bilibili. Give it a page link and it
saves the video to your own computer, at the highest quality your account can
watch.

It handles two very different kinds of content on the same platform:

- **Ordinary videos** — the clips published to a creator's channel, addressed by
  a `BV` or `av` id.
- **Episodic content** — animations, films, and TV series published as a
  numbered season, addressed by an `ep` (episode) or `ss` (season) id. An `ep`
  link downloads that one episode and reports which one it got.

Both are resolved to real media streams and written to disk as one MP4 file
with picture and sound together.

## Why it exists

A Bilibili watch page is not a file. The page carries an id, and the actual
stream addresses have to be requested from a separate API, in a different shape
for videos and for seasons, with a `Referer` that the CDN checks, signed links
that expire, and quality tiers that depend on who is asking. Doing that by hand
means juggling several endpoints and then stitching a video-only track and an
audio-only track back into one file.

Virelz does that work in one step:

1. Recognise what the link points at, without trusting the URL's shape alone.
2. Query the right metadata and playback endpoints for that kind of content.
3. Ask for the highest quality available and keep the best track returned.
4. Download the tracks in parallel, with a real progress bar.
5. Merge split video and audio into a single MP4 without re-encoding.
6. Publish the final file only after the whole transfer succeeded.

The result is one file you can open in any player, and a short report telling
you exactly what you got.

## Supported platforms

Only what is listed here is implemented and verified end to end. Anything else
is reported as unsupported rather than guessed at.

| Platform | Accepted input | Content | Status |
| --- | --- | --- | --- |
| Bilibili | page links with a `BV` or `av` id | ordinary videos | verified |
| Bilibili | page links with an `ep` or `ss` id | animations, films, series | verified |

Some notes on the details:

- Direct media or CDN links are not accepted. Virelz needs a page link so it
  can resolve the streams itself, and it will say so instead of failing
  obscurely.
- An `ep` link downloads exactly that episode.
- An `ss` link downloads the season's first episode. Use a specific `ep` link
  when you want a later one.
- Paid, member-only, and region-restricted content is not downloaded. If the
  server only offers a trial clip, Virelz refuses and explains why instead of
  writing a half episode and calling it done.

## Install

You need Go 1.26 or newer.

```bash
go build -o virelz.exe ./cmd/virelz
```

That produces one self-contained binary with no runtime dependencies.

## Usage

### Interactive prompt

Double-click `virelz.exe` and it opens a prompt. Type a link and press Enter to
download it, `help` for the command list, `exit` to leave:

```
virelz 0.1.0 - resolve and download videos
Type a link to download it, "help" for all commands, "exit" to leave.

virelz> https://www.bilibili.com/bangumi/play/ep2436276
virelz> virelz -d "https://www.bilibili.com/video/BV1GJ411x7h7" -o D:\videos
virelz> exit
```

A leading `virelz` or `virelz.exe` is ignored inside the prompt, so a command
copied from this README or from a shell can be pasted unchanged.

### One-shot commands

The same binary works from a shell:

```bash
virelz -d "https://www.bilibili.com/video/BV1GJ411x7h7"
```

A progress bar runs from 0 to 100% while the file transfers. It is erased when
the file is complete, so the report starts right after the command row with no
blank line in between. Every label starts in the first column and is padded so
all values line up:

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

`MediaDuration` is the length of the video itself; `DownloadDuration` is how
long the transfer took. The platform id is not appended to the file name. When
output is redirected, the bar is omitted and only this block is printed, so logs
stay clean.

### Several links at once

Links are resolved first, then downloaded in parallel:

```bash
virelz -d "link1,link2,link3"
```

`-d` may be repeated, and links can also be passed as plain arguments:

```bash
virelz "link1" "link2" -j 2 -o "D:\videos"
```

### Flags

| Flag | Meaning |
| --- | --- |
| `-d, --download strings` | links to download, comma separated or repeated |
| `-o, --output string` | destination folder; defaults to `<temp>/virelz/<yyyyMMdd>` |
| `-j, --jobs int` | how many links download at the same time (default `3`) |
| `--overwrite` | replace an existing file instead of failing |

The default destination is one folder per day under the system temp directory,
in local time. `-o` is used exactly as given, with no date level appended.

### Commands

| Command | Meaning |
| --- | --- |
| `virelz [flags]` | same as `download` |
| `virelz download [flags]` | download links |
| `virelz resolve <link>` | show what a link resolves to, without downloading |
| `virelz platforms` | list the platforms this build supports |
| `virelz set sessdata = "<value>"` | store your Bilibili session cookie |
| `virelz set sessdata` | show whether a cookie is stored |
| `virelz set sessdata =` | remove the stored cookie |
| `virelz get sessdata` | same as `set sessdata` with no value |

Run with no arguments in a terminal and you get the prompt; run with no
arguments where no terminal is attached (a pipe or a script) and it prints the
help instead.

## Quality

Virelz always asks for the highest quality the platform will serve, and lowers
the request only when the top track is unavailable.

Bilibili answers several playback endpoints differently, so all relevant ones
are queried with the same quality request and the best result wins. Ordinary
videos use the DASH endpoint (`fnval=4048`), which carries the split video and
audio tracks, and the single-file MP4 endpoint (`fnval=1`), which sometimes
returns a higher resolution than DASH does for the same clip. Episodic content
uses its own season and player endpoints, because a season is not a video
archive and the video endpoints cap it much lower.

When several tracks arrive, the choice is made in this order: the highest
quality id first, then the most compatible codec (AVC before HEVC before AV1),
then the highest bitrate. Tracks without a usable address are skipped.

### Logging in for higher quality

Anonymous requests are capped by Bilibili well below the resolution the page
offers. Storing your own session cookie lifts that cap without touching access
control or paid content. Virelz reads `SESSDATA` from your own browser session.
Set it once, straight from the binary:

```bat
virelz set sessdata = "<your SESSDATA value>"
```

That prints `sessdata saved to ...\bilibili.json`. From then on every download
asks for the highest quality the account allows:

```bat
virelz -d "https://www.bilibili.com/video/BV1qqsk65E7t"
```

Other forms do the same job. `virelz set sessdata` reports whether a value is
stored without printing it, and `virelz set sessdata =` removes it. All of them
work in a shell and inside the `virelz>` prompt.

The value is stored in your per-user config directory as `bilibili.json`, with
the file restricted to your account. Outer quotes are stripped, so pasting
either `"abc"` or `abc` stores `abc`. The `VIRELZ_BILI_SESSDATA` environment
variable still overrides the stored file, so a one-off run can use a different
account without changing what is saved.

### What quality you actually get

`Quality:` reports the stream that was really chosen, not the tier that was
requested. Measured here:

| Content | Anonymous | With your own cookie |
| --- | --- | --- |
| Ordinary video | 480p over DASH, 720p over MP4 | 1080p over DASH |
| Episodic content | 480p | 576p |

Two honest limits follow from how the platform works:

- A non-VIP account stops at 1080p. The 4K and HDR tiers belong to paid
  membership, and Virelz reports what it received rather than claiming a tier
  it did not get.
- The reported height is the real track height. A season episode with quality
  id `64` can genuinely be `768x576`, and then it is reported as `576p` because
  that is what the file contains.

## Output

When a source serves video and audio as separate tracks, Virelz merges them
into one MP4 file without re-encoding, so the result is a single file with both
tracks. Each download writes to a temporary file and only publishes the final
name after the transfer completes; interrupted files are removed. A task is
never reported as successful unless the complete output was written and
finalised.

## Notes

- Downloads use the `Referer` the platform requires. No login, DRM, access
  control, or rate limit is bypassed.
- Only download content you are allowed to download.
- Requests are bounded by a connection and response-header timeout. A slow
  transfer is not cut short by a whole-download deadline.

## License

MIT. See [LICENSE](LICENSE).

[中文说明](README.zh-CN.md)