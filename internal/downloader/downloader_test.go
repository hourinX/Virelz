// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package downloader

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hourinX/Virelz/pkg/platform"
)

// --- helpers -------------------------------------------------------------

// box builds one ISO-BMFF box with the given type and payload.
func box(typ string, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(len(out)))
	copy(out[4:8], typ)
	copy(out[8:], payload)
	return out
}

// tkhdPayload builds a version-0 tkhd body declaring trackID.
func tkhdPayload(trackID uint32) []byte {
	// version/flags, creation, modification, track_ID, reserved, duration.
	out := make([]byte, 4+4+4+4+4+4)
	binary.BigEndian.PutUint32(out[12:16], trackID)
	return out
}

// trexPayload builds a trex body declaring trackID.
func trexPayload(trackID uint32) []byte {
	// version/flags, track_ID, default sample description index, duration,
	// size, flags.
	out := make([]byte, 4+4+4+4+4+4)
	binary.BigEndian.PutUint32(out[4:8], trackID)
	return out
}

// mvhdPayload builds a version-0 mvhd body whose next_track_ID is 2.
func mvhdPayload() []byte {
	// version/flags, creation, modification, timescale, duration, rate,
	// volume, reserved, matrix (36), pre_defined (24), next_track_ID.
	out := make([]byte, 4+4+4+4+4+4+2+2+8+36+24+4)
	binary.BigEndian.PutUint32(out[12:16], 1000)
	binary.BigEndian.PutUint32(out[len(out)-4:], 2)
	return out
}

// trackInitSegment builds an ftyp plus a fragmented moov declaring one track,
// as a DASH track header looks. Sources number their only track 1.
func trackInitSegment() []byte {
	trak := box("trak", box("tkhd", tkhdPayload(1)))
	mvex := box("mvex", box("trex", trexPayload(1)))
	body := append(box("mvhd", mvhdPayload()), append(trak, mvex...)...)
	return append(box("ftyp", []byte("iso5avc1iso5dash")), box("moov", body)...)
}

// moofSegment builds one fragment: a moof whose traf/tfhd names trackID,
// followed by the matching mdat.
func moofSegment(trackID uint32, payload string) []byte {
	body := make([]byte, 8)
	// tfhd holds version/flags then track_ID.
	binary.BigEndian.PutUint32(body[4:8], trackID)
	tfhd := box("tfhd", body)
	moof := box("moof", append(box("mfhd", []byte{0, 0, 0, 1}), box("traf", tfhd)...))
	return append(moof, box("mdat", []byte(payload))...)
}

// staticFile serves fixed body bytes with a content length.
func staticFile(t *testing.T, path string, body []byte) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + path
}

// testClient returns a client whose requests fail fast so tests never hang.
func testClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 10 * time.Second
	return &http.Client{Transport: transport}
}

func singleVideo(url string) *platform.Video {
	return &platform.Video{
		Site:    "test",
		ID:      "abc123",
		PageURL: "https://example.com/abc123",
		Title:   "Sample Video",
		Media: []platform.Media{{
			Kind:      platform.MediaVideo,
			URL:       url,
			Quality:   "480p",
			Container: "mp4",
			Headers:   http.Header{"Referer": []string{"https://example.com/"}},
		}},
	}
}

func splitVideo(videoURL, audioURL string) *platform.Video {
	v := singleVideo(videoURL)
	v.Media = append(v.Media, platform.Media{
		Kind:      platform.MediaAudio,
		URL:       audioURL,
		Container: "mp4",
	})
	return v
}

// --- tests ---------------------------------------------------------------

func TestDownloadPublishesFileAndReportsProgress(t *testing.T) {
	body := trackInitSegment()
	body = append(body, moofSegment(1, strings.Repeat("v", 4096))...)
	url := staticFile(t, "/video.m4s", body)

	dir := t.TempDir()
	var mu sync.Mutex
	var updates []Progress
	opts := Options{
		Dir:    dir,
		Client: testClient(),
		Progress: func(p Progress) {
			mu.Lock()
			updates = append(updates, p)
			mu.Unlock()
		},
	}

	video := singleVideo(url)
	res, err := Download(context.Background(), video, opts)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	if len(data) != len(body) {
		t.Fatalf("wrote %d bytes, want %d", len(data), len(body))
	}
	if !bytes.Equal(data, body) {
		t.Fatal("published file differs from the served body")
	}
	if res.Bytes != int64(len(body)) {
		t.Fatalf("result bytes = %d, want %d", res.Bytes, len(body))
	}
	if res.Quality != "480p" {
		t.Fatalf("quality = %q", res.Quality)
	}
	if want := filepath.Join(dir, "Sample Video.mp4"); res.Path != want {
		t.Fatalf("path = %q, want %q", res.Path, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "Sample Video.mp4.part")); err == nil {
		t.Fatal("temporary file survived a successful download")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(updates) == 0 {
		t.Fatal("no progress updates were reported")
	}
	var last int64
	for _, u := range updates {
		if u.Done < last {
			t.Fatalf("progress went backwards: %d after %d", u.Done, last)
		}
		last = u.Done
	}
	if last != int64(len(body)) {
		t.Fatalf("final progress = %d, want %d", last, len(body))
	}
	for _, u := range updates {
		if u.Total != int64(len(body)) {
			t.Fatalf("reported total = %d, want %d", u.Total, len(body))
		}
	}
}

func TestDownloadMergesSplitTracksIntoOneFile(t *testing.T) {
	vbody := append(trackInitSegment(), moofSegment(1, "video-fragment")...)
	abody := append(trackInitSegment(), moofSegment(1, "audio-fragment")...)
	vurl := staticFile(t, "/v.m4s", vbody)
	aurl := staticFile(t, "/a.m4s", abody)

	dir := t.TempDir()
	res, err := Download(context.Background(), splitVideo(vurl, aurl), Options{Dir: dir, Client: testClient()})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	// A playable merge keeps one ftyp, one moov, and one trak per stream.
	top, err := topLevelBoxes(data)
	if err != nil {
		t.Fatalf("parsing merged file: %v", err)
	}
	if top["ftyp"] != 1 || top["moov"] != 1 {
		t.Fatalf("expected one ftyp and one moov, got %v", top)
	}
	if n := trakCount(t, data); n != 2 {
		t.Fatalf("merged moov declares %d tracks, want 2", n)
	}

	// Both fragments must survive.
	if !bytes.Contains(data, []byte("video-fragment")) {
		t.Fatal("merged file is missing the video fragment")
	}
	if !bytes.Contains(data, []byte("audio-fragment")) {
		t.Fatal("merged file is missing the audio fragment")
	}

	// The second stream has to be renumbered, or both fragments would claim
	// the video track and the result would play without sound.
	if ids := fragmentTrackIDs(t, data); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("fragment track IDs = %v, want [1 2]", ids)
	}
	if ids := trexTrackIDs(t, data); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("trex track IDs = %v, want [1 2]", ids)
	}

	if res.Bytes != int64(len(data)) {
		t.Fatalf("result bytes = %d, want %d", res.Bytes, len(data))
	}
	if fi, err := os.Stat(res.Path); err != nil || fi.Size() != res.Bytes {
		t.Fatalf("published size mismatch: %v %d vs %d", err, sizeOf(fi), res.Bytes)
	}
}

func TestDownloadRejectsNonFragmentStreamToAppend(t *testing.T) {
	vbody := append(trackInitSegment(), moofSegment(1, "video-fragment")...)
	abody := moofSegment(1, "audio-fragment") // no init segment
	vurl := staticFile(t, "/v.m4s", vbody)
	aurl := staticFile(t, "/a.m4s", abody)

	dir := t.TempDir()
	_, err := Download(context.Background(), splitVideo(vurl, aurl), Options{Dir: dir, Client: testClient()})
	if !errors.Is(err, errNotFragmentedMP4) {
		t.Fatalf("expected errNotFragmentedMP4, got %v", err)
	}
	assertNoOutput(t, dir)
}

func TestDownloadRefusesExistingFile(t *testing.T) {
	body := append(trackInitSegment(), moofSegment(1, "fragment")...)
	url := staticFile(t, "/v.m4s", body)
	dir := t.TempDir()

	existing := filepath.Join(dir, "Sample Video.mp4")
	if err := os.WriteFile(existing, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Download(context.Background(), singleVideo(url), Options{Dir: dir, Client: testClient()})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected an exists error, got %v", err)
	}
	if data, _ := os.ReadFile(existing); string(data) != "keep me" {
		t.Fatal("existing file was modified")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "Sample Video.mp4" {
		t.Fatalf("unexpected output after a refused download: %v", names(entries))
	}
}

func TestDownloadOverwrite(t *testing.T) {
	body := append(trackInitSegment(), moofSegment(1, "fragment")...)
	url := staticFile(t, "/v.m4s", body)
	dir := t.TempDir()

	existing := filepath.Join(dir, "Sample Video.mp4")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Download(context.Background(), singleVideo(url), Options{Dir: dir, Client: testClient(), Overwrite: true})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if data, _ := os.ReadFile(res.Path); !bytes.Equal(data, body) {
		t.Fatal("overwrite did not publish the new content")
	}
}

func TestDownloadCancellationCleansUp(t *testing.T) {
	release := make(chan struct{})
	mux := http.NewServeMux()
	var mu sync.Mutex
	var received int
	mux.HandleFunc("/v.m4s", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "1000000")
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test server cannot flush")
			return
		}
		w.Header().Set("Content-Length", "1000000")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 10; i++ {
			if _, err := w.Write(append(trackInitSegment(), moofSegment(1, "chunk")...)); err == nil {
				mu.Lock()
				received++
				mu.Unlock()
			}
			flusher.Flush()
			select {
			case <-release:
				return
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Download(ctx, singleVideo(srv.URL+"/v.m4s"), Options{Dir: dir, Client: testClient()})
		done <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := received
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	assertNoOutput(t, dir)
}

func TestDownloadRejectsBadResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v.m4s", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	_, err := Download(context.Background(), singleVideo(srv.URL+"/v.m4s"), Options{Dir: dir, Client: testClient()})
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("expected an HTTP 403 error, got %v", err)
	}
	assertNoOutput(t, dir)
}

func TestDownloadValidatesVideo(t *testing.T) {
	dir := t.TempDir()
	_, err := Download(context.Background(), &platform.Video{Site: "test"}, Options{Dir: dir, Client: testClient()})
	if err == nil {
		t.Fatal("expected validation to fail")
	}
	assertNoOutput(t, dir)
}

func TestDownloadStreamsInsteadOfBuffering(t *testing.T) {
	// A large body must be written incrementally: progress arrives before the
	// transfer ends.
	payload := make([]byte, 1<<20)
	const chunkCount = 8
	mux := http.NewServeMux()
	full := len(trackInitSegment()) + chunkCount*len(box("moof", []byte{0, 0, 0, 2})) + len(payload)*chunkCount
	mux.HandleFunc("/v.m4s", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-0" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", full))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte{0})
			return
		}
		flusher := w.(http.Flusher)
		_, _ = w.Write(trackInitSegment())
		for i := 0; i < chunkCount; i++ {
			_, _ = w.Write(box("moof", []byte{0, 0, 0, 2}))
			_, _ = w.Write(box("mdat", payload))
			flusher.Flush()
			time.Sleep(5 * time.Millisecond)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	var midTransfer int
	res, err := Download(context.Background(), singleVideo(srv.URL+"/v.m4s"), Options{
		Dir:    dir,
		Client: testClient(),
		Progress: func(p Progress) {
			if p.Stage == StageDownloading && p.Done > 0 && p.Done < p.Total {
				midTransfer++
			}
		},
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if midTransfer == 0 {
		t.Fatal("no partial progress was reported; the body may have been buffered")
	}
	if fi, err := os.Stat(res.Path); err != nil || fi.Size() != res.Bytes {
		t.Fatalf("published size mismatch: %v %d vs %d", err, sizeOf(fi), res.Bytes)
	}
}

func sizeOf(fi os.FileInfo) int64 {
	if fi == nil {
		return -1
	}
	return fi.Size()
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func assertNoOutput(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Fatalf("unexpected output %q after a failed download", e.Name())
	}
}

func TestReadStreamHeaderStopsAtFirstFragment(t *testing.T) {
	init := trackInitSegment()
	body := append(init, moofSegment(1, "fragment")...)

	header, err := readStreamHeader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("readStreamHeader: %v", err)
	}
	if ids := header.trackIDs(); len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("track IDs = %v, want [1]", ids)
	}
	out, err := io.ReadAll(header.fragments)
	if err != nil {
		t.Fatal(err)
	}
	if want := moofSegment(1, "fragment"); !bytes.Equal(out, want) {
		t.Fatalf("left %d bytes, want %d", len(out), len(want))
	}

	// A stream starting with a fragment has no header to read.
	if _, err := readStreamHeader(bytes.NewReader(moofSegment(1, "x"))); !errors.Is(err, errNotFragmentedMP4) {
		t.Fatalf("expected errNotFragmentedMP4, got %v", err)
	}

	// A non-fragmented moov cannot be merged either.
	plain := append(box("ftyp", []byte("isom")), box("moov", box("trak", box("tkhd", tkhdPayload(1))))...)
	if _, err := readStreamHeader(bytes.NewReader(plain)); !errors.Is(err, errNotFragmentedMP4) {
		t.Fatalf("expected errNotFragmentedMP4 for a non-fragmented moov, got %v", err)
	}
}

// topLevelBoxes counts the top-level boxes of data by type.
func topLevelBoxes(data []byte) (map[string]int, error) {
	counts := make(map[string]int)
	for off := 0; off < len(data); {
		node, size, ok := parseBoxAt(data, off)
		if !ok {
			return nil, fmt.Errorf("truncated box at offset %d", off)
		}
		counts[node.typ]++
		off += size
	}
	return counts, nil
}

// trakCount reports how many trak boxes the top-level moov declares.
func trakCount(t *testing.T, data []byte) int {
	t.Helper()
	moov, ok := findTopLevel(data, "moov")
	if !ok {
		t.Fatal("no moov box found")
	}
	n := 0
	for _, c := range moov.children {
		if c.typ == "trak" {
			n++
		}
	}
	return n
}

// fragmentTrackIDs returns the tfhd track_ID of every top-level moof, in order.
func fragmentTrackIDs(t *testing.T, data []byte) []uint32 {
	t.Helper()
	var ids []uint32
	for off := 0; off < len(data); {
		node, size, ok := parseBoxAt(data, off)
		if !ok {
			t.Fatalf("truncated box at offset %d", off)
		}
		if node.typ == "moof" {
			for _, traf := range node.children {
				if traf.typ != "traf" {
					continue
				}
				for _, tfhd := range traf.children {
					if tfhd.typ != "tfhd" {
						continue
					}
					body := tfhd.payload()
					if len(body) < 8 {
						t.Fatal("tfhd box is too short")
					}
					ids = append(ids, binary.BigEndian.Uint32(body[4:8]))
				}
			}
		}
		off += size
	}
	return ids
}

// trexTrackIDs returns the track_ID of every trex box inside the moov.
func trexTrackIDs(t *testing.T, data []byte) []uint32 {
	t.Helper()
	moov, ok := findTopLevel(data, "moov")
	if !ok {
		t.Fatal("no moov box found")
	}
	var ids []uint32
	for _, mvex := range moov.children {
		if mvex.typ != "mvex" {
			continue
		}
		for _, trex := range mvex.children {
			if trex.typ != "trex" {
				continue
			}
			body := trex.payload()
			if len(body) < 8 {
				t.Fatal("trex box is too short")
			}
			ids = append(ids, binary.BigEndian.Uint32(body[4:8]))
		}
	}
	return ids
}

// findTopLevel returns the first top-level box of the given type.
func findTopLevel(data []byte, typ string) (boxNode, bool) {
	for off := 0; off < len(data); {
		node, size, ok := parseBoxAt(data, off)
		if !ok {
			return boxNode{}, false
		}
		if node.typ == typ {
			return node, true
		}
		off += size
	}
	return boxNode{}, false
}

func TestSizeFromContentRange(t *testing.T) {
	cases := map[string]int64{
		"bytes 0-0/5578957": 5578957,
		"bytes 0-0/18":      18,
		"bytes 0-0/*":       0,
		"":                  0,
		"garbage":           0,
	}
	for value, want := range cases {
		if got := sizeFromContentRange(value); got != want {
			t.Fatalf("sizeFromContentRange(%q) = %d, want %d", value, got, want)
		}
	}
}

func TestContentLengthUsesRangeProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-0" {
			t.Errorf("probe must ask for a single byte, got %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 0-0/4242")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()

	got, err := contentLength(context.Background(), server.Client(), platform.Media{URL: server.URL})
	if err != nil {
		t.Fatalf("contentLength: %v", err)
	}
	if got != 4242 {
		t.Fatalf("contentLength = %d, want 4242", got)
	}
}

func TestContentLengthFallsBackToFullResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A server that ignores Range answers 200 with the whole body.
		w.Header().Set("Content-Length", "9")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("123456789"))
	}))
	defer server.Close()

	got, err := contentLength(context.Background(), server.Client(), platform.Media{URL: server.URL})
	if err != nil {
		t.Fatalf("contentLength: %v", err)
	}
	if got != 9 {
		t.Fatalf("contentLength = %d, want 9", got)
	}
}

func TestSumSizesReturnsZeroWhenUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	if got := sumSizes(context.Background(), server.Client(), []platform.Media{{URL: server.URL}}); got != 0 {
		t.Fatalf("sumSizes = %d, want 0 when a size is unknown", got)
	}
}
