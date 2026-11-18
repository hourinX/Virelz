// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package downloader

import (
	"encoding/binary"
	"fmt"
	"io"
)

var errNotFragmentedMP4 = fmt.Errorf("not a fragmented MP4 stream") // stream cannot be merged

var defaultFtyp = encodeBox("ftyp", []byte("iso5\x00\x00\x00\x01"))

// buildMergedHeader returns the ftyp and moov placed before the fragments.
func buildMergedHeader(headers []*streamHeader) ([]byte, error) {
	if len(headers) == 0 {
		return nil, fmt.Errorf("%w: nothing to merge", errNotFragmentedMP4)
	}

	var (
		ftyp      = headers[0].seg.ftyp
		mvhd      []byte
		traks     [][]byte
		mvexParts [][]byte
		mehd      [][]byte
	)

	for i, h := range headers {
		// Number tracks by output position, not by box order inside moov.
		trackID := uint32(i + 1)
		ids := h.trackIDs()
		if len(ids) != 1 {
			return nil, fmt.Errorf("%w: a merged stream must hold exactly one track, found %d", errNotFragmentedMP4, len(ids))
		}
		for _, c := range h.seg.moov.children {
			switch c.typ {
			case "mvhd":
				if mvhd == nil {
					mvhd = c.raw
				}
			case "trak":
				traks = append(traks, renumberTrack(c, trackID))
			case "mvex":
				if mehd == nil {
					for _, m := range c.children {
						if m.typ == "mehd" {
							mehd = append([][]byte{}, m.raw)
							break
						}
					}
				}
				parts, err := renumberMvex(c, trackID)
				if err != nil {
					return nil, err
				}
				mvexParts = append(mvexParts, parts...)
			}
		}
	}

	if len(traks) == 0 {
		return nil, fmt.Errorf("%w: no track was found to merge", errNotFragmentedMP4)
	}
	if mvhd == nil {
		return nil, fmt.Errorf("%w: moov box has no mvhd", errNotFragmentedMP4)
	}
	if ftyp == nil {
		ftyp = defaultFtyp
	}

	children := [][]byte{patchMVHDTrackCount(mvhd, uint32(len(traks)))}
	children = append(children, traks...)
	if len(mvexParts) > 0 {
		// mehd describes the movie, so it is carried over once.
		parts := append([][]byte{}, mehd...)
		parts = append(parts, mvexParts...)
		children = append(children, encodeBox("mvex", joinBytes(parts...)))
	}
	return joinBytes(ftyp, encodeBox("moov", joinBytes(children...))), nil
}

// renumberTrack returns trak with its tkhd track_ID set to id.
func renumberTrack(trak boxNode, id uint32) []byte {
	raw := make([]byte, len(trak.raw))
	copy(raw, trak.raw)

	tkhd, ok := child(trak, "tkhd")
	if !ok {
		return raw
	}
	at := tkhd.offset - trak.offset + tkhd.header + 4 + 4 + 4
	if tkhd.payload()[0] == 1 {
		at = tkhd.offset - trak.offset + tkhd.header + 4 + 8 + 8
	}
	if at+4 > len(raw) {
		return raw
	}
	binary.BigEndian.PutUint32(raw[at:at+4], id)
	return raw
}

// renumberMvex returns the mvex children owned by one track, renumbered to id.
func renumberMvex(mvex boxNode, id uint32) ([][]byte, error) {
	var out [][]byte
	for _, c := range mvex.children {
		switch c.typ {
		case "mehd":
		case "trex", "trep":
			raw, err := renumberBoxTrack(c, id)
			if err != nil {
				return nil, err
			}
			out = append(out, raw)
		default:
			out = append(out, c.raw)
		}
	}
	return out, nil
}

// renumberBoxTrack copies box with the track_ID in its payload set to id.
func renumberBoxTrack(b boxNode, id uint32) ([]byte, error) {
	raw := make([]byte, len(b.raw))
	copy(raw, b.raw)

	at := b.header + 4 // version/flags then track_ID
	if at+4 > len(raw) {
		return nil, fmt.Errorf("%w: %s box is too short", errNotFragmentedMP4, b.typ)
	}
	binary.BigEndian.PutUint32(raw[at:at+4], id)
	return raw, nil
}

// patchMVHDTrackCount sets next_track_ID to one past the last track.
func patchMVHDTrackCount(mvhd []byte, lastTrack uint32) []byte {
	const header = 8
	if len(mvhd) < header+4 {
		return mvhd
	}
	version := mvhd[header]

	var at int
	if version == 1 {
		at = header + 4 + 8 + 8 + 4 + 8
	} else {
		at = header + 4 + 4 + 4 + 4 + 4
	}
	at += 4 + 2 + 2 + 8 + 36 + 24
	if at+4 > len(mvhd) {
		return mvhd
	}
	raw := make([]byte, len(mvhd))
	copy(raw, mvhd)
	binary.BigEndian.PutUint32(raw[at:at+4], lastTrack+1)
	return raw
}

// copyFragments copies media fragments to w, rewriting the tfhd track_ID.
func copyFragments(w io.Writer, r io.Reader, src, dst uint32) error {
	var (
		buf   []byte
		chunk = make([]byte, 64<<10)
		done  bool
	)

	for {
		// Write every complete box buffered so far.
		consumed, err := writeCompleteBoxes(w, buf, src, dst)
		if err != nil {
			return err
		}
		buf = buf[consumed:]

		if done {
			if len(buf) > 0 { // keep a trailing partial box
				if _, err := w.Write(buf); err != nil {
					return err
				}
			}
			return nil
		}

		n, rerr := r.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if rerr != nil {
			if rerr != io.EOF {
				return rerr
			}
			done = true
		}
	}
}

// writeCompleteBoxes writes each complete top-level box in buf and reports the
// bytes consumed.
func writeCompleteBoxes(w io.Writer, buf []byte, src, dst uint32) (int, error) {
	off := 0
	for off < len(buf) {
		node, size, ok := parseBoxAt(buf, off)
		if !ok {
			return off, nil // wait for the rest of this box
		}
		switch node.typ {
		case "moof":
			if err := writeMoof(w, node, src, dst); err != nil {
				return 0, err
			}
		case "ftyp", "moov":
		default:
			if _, err := w.Write(node.raw); err != nil {
				return 0, err
			}
		}
		off += size
	}
	return off, nil
}

// writeMoof writes moof, renumbering the tfhd track_ID inside each traf.
func writeMoof(w io.Writer, moof boxNode, src, dst uint32) error {
	if src == dst {
		_, err := w.Write(moof.raw)
		return err
	}
	raw := make([]byte, len(moof.raw))
	copy(raw, moof.raw)

	for _, traf := range moof.children {
		if traf.typ != "traf" {
			continue
		}
		for _, tfhd := range traf.children {
			if tfhd.typ != "tfhd" {
				continue
			}
			at := tfhd.offset - moof.offset + tfhd.header + 4
			if at+4 > len(raw) {
				return fmt.Errorf("%w: tfhd box is too short", errNotFragmentedMP4)
			}
			if binary.BigEndian.Uint32(raw[at:at+4]) == src {
				binary.BigEndian.PutUint32(raw[at:at+4], dst)
			}
		}
	}
	_, err := w.Write(raw)
	return err
}
