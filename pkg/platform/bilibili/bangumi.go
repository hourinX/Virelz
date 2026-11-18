// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package bilibili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/hourinX/Virelz/pkg/platform"
)

// seasonResponse is the pgc season payload, which also resolves an ep link.
type seasonResponse struct {
	Code    int          `json:"code"`    // 0 on success
	Message string       `json:"message"` // human readable failure reason
	Result  seasonResult `json:"result"`  // season metadata
}

type seasonResult struct {
	SeasonID int64      `json:"season_id"` // season the episode belongs to
	Title    string     `json:"title"`     // season title shown to the user
	Episodes []seasonEp `json:"episodes"`  // every episode in the season
}

type seasonEp struct {
	ID        int64  `json:"id"`         // ep id used by the playurl call
	Title     string `json:"title"`      // episode ordinal such as "1"
	LongTitle string `json:"long_title"` // episode name
}

// pgcPlayURLResponse is the bangumi player payload.
type pgcPlayURLResponse struct {
	Code    int              `json:"code"`    // 0 on success
	Message string           `json:"message"` // failure reason
	Result  pgcPlayURLResult `json:"result"`  // stream data
}

type pgcPlayURLResult struct {
	Quality   int `json:"quality"`    // quality actually issued
	IsPreview int `json:"is_preview"` // 1 when only a trial clip is served
	Dash      struct {
		Video []dashTrack `json:"video"` // video renditions
		Audio []dashTrack `json:"audio"` // audio renditions
	} `json:"dash"`
	Durl []struct {
		URL string `json:"url"` // progressive stream
	} `json:"durl"`
}

// resolveBangumi turns an ep or ss link into a playable episode.
func (c *Client) resolveBangumi(ctx context.Context, ref videoRef) (*platform.Video, error) {
	season, err := c.fetchSeason(ctx, ref)
	if err != nil {
		return nil, err
	}

	ep, err := pickEpisode(season, ref)
	if err != nil {
		return nil, err
	}

	play, err := c.fetchPgcPlayURL(ctx, ep.ID)
	if err != nil {
		return nil, err
	}

	if play.Result.IsPreview == 1 {
		return nil, fmt.Errorf("%w: episode %d is served as a trial clip only, a paid account is required", platform.ErrUnsupportedSource, ep.ID)
	}

	streams, quality := c.pgcStreams(play)
	if len(streams) == 0 {
		return nil, fmt.Errorf("%w: episode %d has no playable stream for this account", platform.ErrUnsupportedSource, ep.ID)
	}

	title := season.Result.Title
	if name := strings.TrimSpace(ep.LongTitle); name != "" {
		title = title + " - " + name
	} else if ep.Title != "" {
		title = title + " - " + ep.Title
	}

	video := &platform.Video{
		Site:    siteName,
		ID:      "ep" + strconv.FormatInt(ep.ID, 10),
		PageURL: bangumiPageURL(ep.ID),
		Title:   sanitizeTitle(title),
		Author:  sanitizeTitle(season.Result.Title),
		Media:   streams,
	}
	if play.Result.Quality > 0 && quality == "" {
		quality = qualityName(play.Result.Quality)
	}
	for i := range streams {
		if streams[i].Kind == platform.MediaVideo {
			streams[i].Quality = quality
		}
	}
	if err := video.Validate(); err != nil {
		return nil, fmt.Errorf("resolving %s: %w", ref.value(), err)
	}
	return video, nil
}

// fetchSeason loads the season that owns an ep or ss link.
func (c *Client) fetchSeason(ctx context.Context, ref videoRef) (*seasonResponse, error) {
	query := url.Values{}
	if ref.epID != "" {
		query.Set("ep_id", ref.epID)
	} else {
		query.Set("season_id", ref.ssID)
	}

	var season seasonResponse
	endpoint := "/pgc/view/web/season?" + query.Encode()
	if err := c.getJSON(ctx, endpoint, &season); err != nil {
		return nil, fmt.Errorf("bangumi metadata request failed: %w", err)
	}
	if season.Code != 0 {
		return nil, fmt.Errorf("%w: bangumi metadata request failed: code %d, %s", platform.ErrUnsupportedSource, season.Code, season.Message)
	}
	if len(season.Result.Episodes) == 0 {
		return nil, fmt.Errorf("%w: bangumi season %s lists no episodes", platform.ErrUnsupportedSource, ref.value())
	}
	return &season, nil
}

// pickEpisode returns the requested episode, or the first one for a season link.
func pickEpisode(season *seasonResponse, ref videoRef) (seasonEp, error) {
	if ref.epID == "" {
		return season.Result.Episodes[0], nil
	}
	for _, ep := range season.Result.Episodes {
		if strconv.FormatInt(ep.ID, 10) == ref.epID {
			return ep, nil
		}
	}
	return seasonEp{}, fmt.Errorf("%w: episode %s is not part of season %d", platform.ErrUnsupportedSource, ref.epID, season.Result.SeasonID)
}

// fetchPgcPlayURL asks the bangumi endpoint, which serves the quality a video
// archive cannot.
func (c *Client) fetchPgcPlayURL(ctx context.Context, epID int64) (*pgcPlayURLResponse, error) {
	query := url.Values{}
	query.Set("ep_id", strconv.FormatInt(epID, 10))
	query.Set("qn", maxQualityID)
	query.Set("fnval", "4048")
	query.Set("fnver", "0")
	query.Set("fourk", "1")

	var play pgcPlayURLResponse
	endpoint := "/pgc/player/web/playurl?" + query.Encode()
	if err := c.getJSON(ctx, endpoint, &play); err != nil {
		return nil, fmt.Errorf("bangumi playurl request failed: %w", err)
	}
	if play.Code != 0 {
		return nil, fmt.Errorf("%w: bangumi playurl request failed: code %d, %s", platform.ErrUnsupportedSource, play.Code, play.Message)
	}
	return &play, nil
}

// pgcStreams converts a bangumi payload into media tracks and the chosen quality.
func (c *Client) pgcStreams(play *pgcPlayURLResponse) ([]platform.Media, string) {
	headers := c.mediaHeaders()

	var progressives []platform.Media
	for _, d := range play.Result.Durl {
		if d.URL == "" {
			continue
		}
		progressives = append(progressives, platform.Media{
			Kind:      platform.MediaVideo,
			URL:       d.URL,
			Container: "mp4",
			Headers:   headers,
		})
	}
	if progressives != nil {
		return progressives, qualityName(play.Result.Quality)
	}

	streams, label, _, ok := pickDash(play.Result.Dash.Video, play.Result.Dash.Audio, headers)
	if !ok {
		return nil, ""
	}
	return streams, label
}

// bangumiPageURL rebuilds a stable page link for an episode id.
func bangumiPageURL(epID int64) string {
	return "https://www.bilibili.com/bangumi/play/ep" + strconv.FormatInt(epID, 10)
}
