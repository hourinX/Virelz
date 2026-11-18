// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package downloader

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// TestBoxOffsetsAreRelativeToBuffer checks the offset bookkeeping the merger
// relies on: a child's offset minus its parent's offset must equal the child's
// position inside the parent's raw bytes.
func TestBoxOffsetsAreRelativeToBuffer(t *testing.T) {
	data := moofSegment(1, "payload")

	moof, _, ok := parseBoxAt(data, 0)
	if !ok {
		t.Fatal("parsing moof failed")
	}
	if moof.offset != 0 {
		t.Fatalf("moof offset = %d, want 0", moof.offset)
	}
	if len(moof.children) != 2 {
		t.Fatalf("moof children = %d, want 2", len(moof.children))
	}

	traf := moof.children[1]
	if traf.typ != "traf" {
		t.Fatalf("second child is %q, want traf", traf.typ)
	}
	if got, want := traf.offset-moof.offset, 8+len(moof.children[0].raw); got != want {
		t.Fatalf("traf offset inside moof = %d, want %d", got, want)
	}

	tfhd := traf.children[0]
	if tfhd.typ != "tfhd" {
		t.Fatalf("traf child is %q, want tfhd", tfhd.typ)
	}
	if got, want := tfhd.offset-traf.offset, 8; got != want {
		t.Fatalf("tfhd offset inside traf = %d, want %d", got, want)
	}

	// The track ID the merger rewrites must be the one the test reader reads.
	at := tfhd.offset - moof.offset + tfhd.header + 4
	if at+4 > len(moof.raw) {
		t.Fatalf("computed offset %d is outside the moof (%d bytes)", at, len(moof.raw))
	}
	if got := binary.BigEndian.Uint32(moof.raw[at : at+4]); got != 1 {
		t.Fatalf("track ID at computed offset = %d, want 1", got)
	}
	if got := binary.BigEndian.Uint32(tfhd.payload()[4:8]); got != 1 {
		t.Fatalf("track ID from tfhd payload = %d, want 1", got)
	}
}

// TestBuildMergedHeaderIgnoresBoxOrder covers sources that put mvex before
// trak, which Bilibili's audio stream does. Track numbers must follow the
// stream order, not the box order inside each moov.
func TestBuildMergedHeaderIgnoresBoxOrder(t *testing.T) {
	// mvex comes before trak here, as it does in Bilibili's audio stream.
	moovBody := append([]byte{}, box("mvhd", mvhdPayload())...)
	moovBody = append(moovBody, box("mvex", box("trex", trexPayload(2)))...)
	moovBody = append(moovBody, box("trak", box("tkhd", tkhdPayload(1)))...)

	audioLike := append(box("ftyp", []byte("iso5")), box("moov", moovBody)...)

	first, err := readStreamHeader(bytesReader(trackInitSegment()))
	if err != nil {
		t.Fatalf("reading video header: %v", err)
	}
	second, err := readStreamHeader(bytesReader(audioLike))
	if err != nil {
		t.Fatalf("reading audio header: %v", err)
	}

	merged, err := buildMergedHeader([]*streamHeader{first, second})
	if err != nil {
		t.Fatalf("buildMergedHeader: %v", err)
	}
	if ids := trexTrackIDs(t, merged); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("trex track IDs = %v, want [1 2]", ids)
	}
	if n := trakCount(t, merged); n != 2 {
		t.Fatalf("merged moov declares %d tracks, want 2", n)
	}
}

// bytesReader is a tiny io.Reader used to feed headers straight to
// readStreamHeader.
func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// TestBuildMergedHeaderKeepsOneMehd checks that the movie-level mehd box is not
// duplicated per stream, while each track keeps its own trex.
func TestBuildMergedHeaderKeepsOneMehd(t *testing.T) {
	withMehd := func(trackID uint32) []byte {
		mvex := append([]byte{}, box("mehd", []byte{0, 0, 0, 0, 0, 0, 0, 1})...)
		mvex = append(mvex, box("trex", trexPayload(trackID))...)
		body := append([]byte{}, box("mvhd", mvhdPayload())...)
		body = append(body, box("trak", box("tkhd", tkhdPayload(trackID)))...)
		body = append(body, box("mvex", mvex)...)
		return append(box("ftyp", []byte("iso5")), box("moov", body)...)
	}

	first, err := readStreamHeader(bytesReader(withMehd(1)))
	if err != nil {
		t.Fatalf("reading first header: %v", err)
	}
	second, err := readStreamHeader(bytesReader(withMehd(2)))
	if err != nil {
		t.Fatalf("reading second header: %v", err)
	}

	merged, err := buildMergedHeader([]*streamHeader{first, second})
	if err != nil {
		t.Fatalf("buildMergedHeader: %v", err)
	}

	moov, ok := findTopLevel(merged, "moov")
	if !ok {
		t.Fatal("merged header has no moov")
	}
	counts := map[string]int{}
	for _, c := range moov.children {
		if c.typ != "mvex" {
			continue
		}
		for _, m := range c.children {
			counts[m.typ]++
		}
	}
	if counts["mehd"] != 1 {
		t.Fatalf("merged mvex holds %d mehd boxes, want 1", counts["mehd"])
	}
	if counts["trex"] != 2 {
		t.Fatalf("merged mvex holds %d trex boxes, want 2", counts["trex"])
	}
}
