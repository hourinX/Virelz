// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/hourinX/Virelz/pkg/platform"
)

// viewResponse is the subset of the view API this adapter uses.
type viewResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		BVID     string `json:"bvid"`
		AID      int64  `json:"aid"`
		Title    string `json:"title"`
		Duration int    `json:"duration"`
		Pages    []struct {
			CID  int64  `json:"cid"`
			Page int    `json:"page"`
			Part string `json:"part"`
		} `json:"pages"`
		Owner struct {
			Name string `json:"name"`
		} `json:"owner"`
	} `json:"data"`
}

func (v *viewResponse) pageURL() string {
	if v.Data.BVID == "" {
		return ""
	}
	return "https://www.bilibili.com/video/" + v.Data.BVID
}

// fetchView reads the metadata needed before any stream can be played.
func (c *Client) fetchView(ctx context.Context, id videoRef) (*viewResponse, error) {
	query := url.Values{}
	if id.bvid != "" {
		query.Set("bvid", id.bvid)
	} else {
		query.Set("aid", id.aid)
	}

	var view viewResponse
	if err := c.getJSON(ctx, "/x/web-interface/view?"+query.Encode(), &view); err != nil {
		return nil, err
	}
	if view.Code == -404 {
		return nil, fmt.Errorf("%w: video %s does not exist or has been removed", platform.ErrUnsupportedSource, id.value())
	}
	if view.Code != 0 {
		return nil, fmt.Errorf("%w: Bilibili metadata request failed: %s", platform.ErrUnsupportedSource, safeMessage(view.Code, view.Message))
	}
	if view.Data.BVID == "" || len(view.Data.Pages) == 0 || view.Data.Pages[0].CID == 0 {
		return nil, fmt.Errorf("%w: video %s has no playable parts", platform.ErrUnsupportedSource, id.value())
	}
	return &view, nil
}

// playurlResponse is the subset of the playurl API this adapter uses.
type playurlResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Quality int `json:"quality"` // quality id the server picked for this response
		Dash    *struct {
			Duration int `json:"duration"`
			Video    []dashTrack
			Audio    []dashTrack
		} `json:"dash"`
		Durl []struct {
			URL string `json:"url"`
		} `json:"durl"`
	} `json:"data"`
} // dashQuery asks for the DASH track list, which carries the audio track as well.
func dashQuery(view *viewResponse, cid int64) url.Values {
	query := url.Values{}
	query.Set("bvid", view.Data.BVID)
	query.Set("cid", strconv.FormatInt(cid, 10))
	query.Set("qn", maxQualityID) // ask for the highest tier the server knows
	query.Set("fnval", "4048")    // DASH with every codec and every resolution
	query.Set("fourk", "1")       // let 4K tracks appear when the source has them
	return query
}

// mp4Query asks for the single-file MP4 stream. Bilibili caps the DASH list for
// anonymous callers more aggressively than this endpoint, so the two can differ.
func mp4Query(view *viewResponse, cid int64) url.Values {
	query := url.Values{}
	query.Set("bvid", view.Data.BVID)
	query.Set("cid", strconv.FormatInt(cid, 10))
	query.Set("qn", maxQualityID)
	query.Set("fnval", "1") // MP4 only, no DASH
	query.Set("fourk", "1")
	query.Set("platform", "html5")
	return query
}

// fetchStreams asks both endpoints for addresses and keeps the better one.
func (c *Client) fetchStreams(ctx context.Context, view *viewResponse) ([]platform.Media, string, error) {
	cid := view.Data.Pages[0].CID

	var dash playurlResponse
	if err := c.getJSON(ctx, "/x/player/playurl?"+dashQuery(view, cid).Encode(), &dash); err != nil {
		return nil, "", err
	}
	if dash.Code != 0 {
		return nil, "", fmt.Errorf("%w: Bilibili playurl request failed: %s", platform.ErrUnsupportedSource, safeMessage(dash.Code, dash.Message))
	}

	best, quality, bestID, ok := c.dashStreams(&dash)

	// The MP4 endpoint sometimes returns a higher resolution than DASH does, so
	// ask it as well and keep whichever stream carries the better quality id.
	var mp4 playurlResponse
	if err := c.getJSON(ctx, "/x/player/playurl?"+mp4Query(view, cid).Encode(), &mp4); err == nil && mp4.Code == 0 {
		if streams, label, id, found := c.mp4Streams(&mp4); found {
			if !ok || id > bestID {
				best, quality, ok = streams, label, true
			}
		}
	}

	if ok {
		return best, quality, nil
	}

	return nil, "", fmt.Errorf("%w: video %s is not playable without signing in (member-only, region-restricted, or paid content)", platform.ErrUnsupportedSource, view.Data.BVID)
}

// dashStreams turns a DASH response into media entries when it carries a track.
// The returned id is the quality id of the chosen video track.
func (c *Client) dashStreams(play *playurlResponse) ([]platform.Media, string, int, bool) {
	if play.Data.Dash == nil || len(play.Data.Dash.Video) == 0 {
		return nil, "", 0, false
	}
	return pickDash(play.Data.Dash.Video, play.Data.Dash.Audio, c.mediaHeaders())
}

// mp4Streams turns an MP4 response into one media entry and its quality id.
func (c *Client) mp4Streams(play *playurlResponse) ([]platform.Media, string, int, bool) {
	for _, d := range play.Data.Durl {
		if d.URL == "" {
			continue
		}
		headers := c.mediaHeaders()
		return []platform.Media{{
			Kind:      platform.MediaVideo,
			URL:       d.URL,
			Quality:   qualityName(play.Data.Quality),
			Container: "mp4",
			Headers:   headers,
		}}, qualityName(play.Data.Quality), play.Data.Quality, true
	}
	return nil, "", 0, false
}

// mediaHeaders are the headers every CDN request must repeat.
func (c *Client) mediaHeaders() http.Header {
	headers := http.Header{}
	headers.Set("User-Agent", c.userAgent)
	headers.Set("Referer", "https://www.bilibili.com/")
	if c.sessData != "" {
		headers.Set("Cookie", sessDataField+"="+c.sessData)
	}
	return headers
}

// getJSON performs one API call and decodes its JSON body.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Accept", "application/json")
	if c.sessData != "" {
		req.Header.Set("Cookie", sessDataField+"="+c.sessData)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("requesting Bilibili API: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body := io.LimitReader(resp.Body, 8<<20)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Bilibili API returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("decoding Bilibili API response: %w", err)
	}
	return nil
}
