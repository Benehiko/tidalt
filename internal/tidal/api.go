package tidal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// ErrNotFound is returned by GetTrack when the API responds with 404.
var ErrNotFound = errors.New("not found")

// authGet issues a context-aware GET to u through the authenticated client.
// It centralises request construction so every call carries the request
// context (satisfying noctx) and reuses the oauth2 token source.
func (c *Client) authGet(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	return c.GetAuthClient(ctx).Do(req)
}

// authPostForm issues a context-aware form POST to u through the authenticated
// client.
func (c *Client) authPostForm(ctx context.Context, u string, body url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.GetAuthClient(ctx).Do(req)
}

// CoverURL returns the HTTPS URL for the album cover image at the given size.
// cover is the UUID string returned in Track.Album.Cover.
// Typical sizes: "80x80", "160x160", "320x320", "640x640", "1280x1280".
// Returns "" when cover is empty.
func CoverURL(cover, size string) string {
	if cover == "" {
		return ""
	}
	// Tidal stores the UUID with dashes replaced by underscores in the JSON;
	// the image CDN path uses forward slashes between groups.
	dashed := strings.ReplaceAll(cover, "-", "/")
	return "https://resources.tidal.com/images/" + dashed + "/" + size + ".jpg"
}

// apiErr returns a formatted error from a non-2xx response. It tries to extract
// the human-readable "userMessage" field from the Tidal JSON error body; if
// that is not present it falls back to the raw body text.
func apiErr(op string, status int, body []byte) error {
	var e struct {
		UserMessage string `json:"userMessage"`
	}
	if json.Unmarshal(body, &e) == nil && e.UserMessage != "" {
		return fmt.Errorf("%s: %s", op, e.UserMessage)
	}
	return fmt.Errorf("%s (status %d): %s", op, status, strings.TrimSpace(string(body)))
}

type Artist struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Track struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Artist Artist `json:"artist"`
	// Artists is the plural form returned by some endpoints (notably /search),
	// where the singular "artist" field is absent. normalizeArtist backfills
	// Artist from Artists[0] so callers can always rely on Track.Artist.
	Artists []Artist `json:"artists"`
	Album   struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
		Cover string `json:"cover"` // UUID, e.g. "a3f1d2e4-1234-5678-abcd-ef0123456789"
	} `json:"album"`
	Duration int    `json:"duration"`
	URL      string `json:"url"`
}

type Mix struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	SubTitle    string `json:"subTitle"`
	Description string `json:"description"`
}

type SearchResponse struct {
	Tracks struct {
		Items []Track `json:"items"`
	} `json:"tracks"`
}

type StreamResponse struct {
	URLs []string `json:"urls"`
}

type UserResponse struct {
	ID           int    `json:"id"`
	CountryCode  string `json:"countryCode"`
	Email        string `json:"email"`
	FullName     string `json:"fullName"`
	ProfileImage string `json:"picture"`
}

type FavoritesResponse struct {
	Items []struct {
		Item Track `json:"item"`
	} `json:"items"`
}

type radioResponse struct {
	Items []Track `json:"items"`
}

type albumTracksResponse struct {
	Items []Track `json:"items"`
}

// Album is a standalone album as returned by the artist albums endpoint.
// (Track has its own inline album struct; this is the richer top-level shape.)
type Album struct {
	ID             int    `json:"id"`
	Title          string `json:"title"`
	Cover          string `json:"cover"` // UUID, same format as Track.Album.Cover
	ReleaseDate    string `json:"releaseDate"`
	NumberOfTracks int    `json:"numberOfTracks"`
}

type artistAlbumsResponse struct {
	Items              []Album `json:"items"`
	TotalNumberOfItems int     `json:"totalNumberOfItems"`
}

type artistTopTracksResponse struct {
	Items []Track `json:"items"`
}

// v2 JSON:API types for mixes and playlist items.

type v2ResourceIdentifier struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type v2PlaylistAttributes struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type v2jsonAPIResponse struct {
	Data     []v2ResourceIdentifier `json:"data"`
	Included []json.RawMessage      `json:"included"`
}

func (c *Client) GetUser(ctx context.Context) (*UserResponse, error) {
	resp, err := c.authGet(ctx, fmt.Sprintf("%s/users/%d", BaseURL, c.Session.UserID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("get user", resp.StatusCode, body)
	}

	var u UserResponse
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Client) GetTrack(ctx context.Context, trackID string) (*Track, error) {
	params := url.Values{}
	params.Set("countryCode", c.Session.CountryCode)
	resp, err := c.authGet(ctx, BaseURL+"/tracks/"+trackID+"?"+params.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("get track", resp.StatusCode, body)
	}

	var t Track
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) Search(ctx context.Context, query string) ([]Track, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", "20")
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("types", "TRACKS")

	u := BaseURL + "/search?" + params.Encode()
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("search", resp.StatusCode, body)
	}

	var res SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	for i := range res.Tracks.Items {
		res.Tracks.Items[i].normalizeArtist()
	}
	return res.Tracks.Items, nil
}

// normalizeArtist fills the singular Artist field from Artists[0] when the
// endpoint only returned the plural "artists" array (e.g. /search). This keeps
// Track.Artist usable everywhere — playback, favoriting, the artist view.
func (t *Track) normalizeArtist() {
	if t.Artist.ID == 0 && t.Artist.Name == "" && len(t.Artists) > 0 {
		t.Artist = t.Artists[0]
	}
}

// StreamInfo carries the resolved stream URL and its detected format extension.
type StreamInfo struct {
	URL string
	Ext string // e.g. "flac", "mp4", "m4a"
}

func (c *Client) GetStreamURL(ctx context.Context, trackID int) (StreamInfo, error) {
	qualities := []string{"HI_RES_LOSSLESS", "LOSSLESS", "HIGH", "LOW"}
	var lastErr error

	for _, q := range qualities {
		info, err := c.streamURLForQuality(ctx, trackID, q)
		if err != nil {
			lastErr = err
			continue
		}
		return info, nil
	}

	if lastErr != nil {
		return StreamInfo{}, fmt.Errorf("no stream available for track %d: %w", trackID, lastErr)
	}
	return StreamInfo{}, fmt.Errorf("no stream available for track %d", trackID)
}

// streamURLForQuality fetches the stream URL for a single audio-quality tier.
// Pulled out of GetStreamURL so the response body is closed per attempt rather
// than via a defer accumulated inside the quality-ladder loop.
func (c *Client) streamURLForQuality(ctx context.Context, trackID int, q string) (StreamInfo, error) {
	endpoint := fmt.Sprintf("/tracks/%d/urlpostpaywall", trackID)
	params := url.Values{}
	params.Set("urlusagemode", "STREAM")
	params.Set("audioquality", q)
	params.Set("assetpresentation", "FULL")
	params.Set("countryCode", c.Session.CountryCode)

	u := BaseURL + endpoint + "?" + params.Encode()
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return StreamInfo{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return StreamInfo{}, apiErr("get stream ("+q+")", resp.StatusCode, body)
	}

	var s StreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return StreamInfo{}, err
	}
	if len(s.URLs) == 0 {
		return StreamInfo{}, fmt.Errorf("get stream (%s): response contained no URLs", q)
	}

	streamURL := s.URLs[0]
	base := strings.SplitN(streamURL, "?", 2)[0]
	ext := ""
	if i := strings.LastIndex(base, "."); i >= 0 {
		ext = strings.ToLower(base[i+1:])
	}
	return StreamInfo{URL: streamURL, Ext: ext}, nil
}

func (c *Client) GetFavorites(ctx context.Context, limit int) ([]Track, error) {
	endpoint := fmt.Sprintf("/users/%d/favorites/tracks", c.Session.UserID)
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("order", "DATE")
	params.Set("orderDirection", "DESC")

	u := BaseURL + endpoint + "?" + params.Encode()
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("get favorites", resp.StatusCode, body)
	}

	var res FavoritesResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	tracks := make([]Track, len(res.Items))
	for i := range res.Items {
		tracks[i] = res.Items[i].Item
	}
	return tracks, nil
}

func (c *Client) GetTrackRadio(ctx context.Context, trackID int) ([]Track, error) {
	params := url.Values{}
	params.Set("limit", "100")
	params.Set("countryCode", c.Session.CountryCode)

	u := fmt.Sprintf("%s/tracks/%d/radio?%s", BaseURL, trackID, params.Encode())
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("get track radio", resp.StatusCode, body)
	}

	var res radioResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (c *Client) GetAlbumTracks(ctx context.Context, albumID string) ([]Track, error) {
	params := url.Values{}
	params.Set("countryCode", c.Session.CountryCode)

	u := fmt.Sprintf("%s/albums/%s/tracks?%s", BaseURL, albumID, params.Encode())
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("get album tracks", resp.StatusCode, body)
	}

	var res albumTracksResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Items, nil
}

// artistAlbumsPageLimit is the per-page album count requested when paginating
// an artist's discography. artistAlbumsMaxPages caps the loop so a malformed
// totalNumberOfItems can never spin forever.
const (
	artistAlbumsPageLimit = 50
	artistAlbumsMaxPages  = 20 // up to 1000 albums
)

// GetArtistAlbums returns the artist's full discography, paginating through the
// v1 /artists/{id}/albums endpoint until exhausted.
func (c *Client) GetArtistAlbums(ctx context.Context, artistID int) ([]Album, error) {
	var albums []Album
	for page := range artistAlbumsMaxPages {
		params := url.Values{}
		params.Set("countryCode", c.Session.CountryCode)
		params.Set("limit", strconv.Itoa(artistAlbumsPageLimit))
		params.Set("offset", strconv.Itoa(page*artistAlbumsPageLimit))

		u := fmt.Sprintf("%s/artists/%d/albums?%s", BaseURL, artistID, params.Encode())
		resp, err := c.authGet(ctx, u)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, apiErr("get artist albums", resp.StatusCode, body)
		}

		var res artistAlbumsResponse
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

		albums = append(albums, res.Items...)

		// Last page reached: a short page, or we've collected the reported total.
		if len(res.Items) < artistAlbumsPageLimit ||
			(res.TotalNumberOfItems > 0 && len(albums) >= res.TotalNumberOfItems) {
			break
		}
	}
	return albums, nil
}

// GetArtistTopTracks returns the artist's most popular tracks (up to limit).
func (c *Client) GetArtistTopTracks(ctx context.Context, artistID, limit int) ([]Track, error) {
	params := url.Values{}
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("limit", strconv.Itoa(limit))

	u := fmt.Sprintf("%s/artists/%d/toptracks?%s", BaseURL, artistID, params.Encode())
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("get artist top tracks", resp.StatusCode, body)
	}

	var res artistTopTracksResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	for i := range res.Items {
		res.Items[i].normalizeArtist()
	}
	return res.Items, nil
}

// artistAllTracksConcurrency bounds how many album track-list requests run at
// once in GetArtistAllTracks, so a deep discography doesn't open hundreds of
// connections at the same time.
const artistAllTracksConcurrency = 8

// GetArtistAllTracks collects every track across all of the artist's albums.
// Albums are fetched concurrently (bounded by artistAllTracksConcurrency); an
// album that fails to load (e.g. region-locked) is skipped rather than aborting
// the whole list. The result is flattened in album order and deduped by track ID
// so songs that appear on both an album and a single/compilation are not repeated.
func (c *Client) GetArtistAllTracks(ctx context.Context, artistID int) ([]Track, error) {
	albums, err := c.GetArtistAlbums(ctx, artistID)
	if err != nil {
		return nil, err
	}
	if len(albums) == 0 {
		return nil, nil
	}

	perAlbum := make([][]Track, len(albums))
	sem := make(chan struct{}, artistAllTracksConcurrency)
	var wg sync.WaitGroup
	for i, alb := range albums {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx, albumID int) {
			defer wg.Done()
			defer func() { <-sem }()
			ts, err := c.GetAlbumTracks(ctx, strconv.Itoa(albumID))
			if err != nil {
				return // skip this album
			}
			perAlbum[idx] = ts
		}(i, alb.ID)
	}
	wg.Wait()

	var all []Track
	seen := make(map[int]bool)
	for _, ts := range perAlbum {
		for i := range ts {
			if seen[ts[i].ID] {
				continue
			}
			seen[ts[i].ID] = true
			all = append(all, ts[i])
		}
	}
	return all, nil
}

func (c *Client) AddFavorite(ctx context.Context, trackID int) error {
	endpoint := fmt.Sprintf("/users/%d/favorites/tracks", c.Session.UserID)
	query := url.Values{}
	query.Set("countryCode", c.Session.CountryCode)

	body := url.Values{}
	body.Set("trackId", strconv.Itoa(trackID))

	resp, err := c.authPostForm(ctx, BaseURL+endpoint+"?"+query.Encode(), body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return apiErr("add favorite", resp.StatusCode, b)
	}
	return nil
}

func (c *Client) RemoveFavorite(ctx context.Context, trackID int) error {
	endpoint := fmt.Sprintf("/users/%d/favorites/tracks/%d", c.Session.UserID, trackID)
	params := url.Values{}
	params.Set("countryCode", c.Session.CountryCode)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		BaseURL+endpoint+"?"+params.Encode(), http.NoBody)
	if err != nil {
		return err
	}
	resp, err := c.GetAuthClient(ctx).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return apiErr("remove favorite", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) GetMixes(ctx context.Context) ([]Mix, error) {
	params := url.Values{}
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("include", "myMixes")

	u := BaseURLV2 + "/userRecommendations/me/relationships/myMixes?" + params.Encode()
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("get mixes", resp.StatusCode, body)
	}

	var res v2jsonAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	// Build a lookup of playlist attributes from included resources.
	playlistAttrs := make(map[string]v2PlaylistAttributes)
	for _, raw := range res.Included {
		var obj struct {
			ID         string               `json:"id"`
			Type       string               `json:"type"`
			Attributes v2PlaylistAttributes `json:"attributes"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		if obj.Type == "playlists" {
			playlistAttrs[obj.ID] = obj.Attributes
		}
	}

	mixes := make([]Mix, 0, len(res.Data))
	for _, ref := range res.Data {
		mix := Mix{ID: ref.ID}
		if attrs, ok := playlistAttrs[ref.ID]; ok {
			mix.Title = attrs.Name
			mix.SubTitle = attrs.Description
		} else {
			mix.Title = ref.ID
		}
		mixes = append(mixes, mix)
	}
	return mixes, nil
}

func (c *Client) GetMixTracks(ctx context.Context, mixID string) ([]Track, error) {
	// Step 1: fetch the ordered list of track IDs from the v2 playlist endpoint.
	// The v2 API only returns IDs here — artist/album sideloading is not supported
	// by this endpoint despite the include parameter existing in the spec.
	params := url.Values{}
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("include", "items")

	u := BaseURLV2 + "/playlists/" + mixID + "/relationships/items?" + params.Encode()
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("get mix tracks", resp.StatusCode, body)
	}

	var res v2jsonAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	// Collect track IDs in order, skipping non-track refs.
	ids := make([]string, 0, len(res.Data))
	for _, ref := range res.Data {
		if ref.Type == "tracks" {
			ids = append(ids, ref.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}

	// Step 2: fetch full track details (with artist + album) from the v1 API
	// concurrently, one request per track.
	type result struct {
		idx   int
		track Track
		err   error
	}
	ch := make(chan result, len(ids))
	for i, id := range ids {
		go func(idx int, trackID string) {
			t, err := c.GetTrack(ctx, trackID)
			if err != nil {
				ch <- result{idx: idx, err: err}
				return
			}
			ch <- result{idx: idx, track: *t}
		}(i, id)
	}

	type indexedTrack struct {
		idx   int
		track Track
	}
	var available []indexedTrack
	for range ids {
		r := <-ch
		if errors.Is(r.err, ErrNotFound) {
			continue
		}
		if r.err != nil {
			return nil, fmt.Errorf("failed to get mix track details: %w", r.err)
		}
		available = append(available, indexedTrack{r.idx, r.track})
	}
	// Sort by original playlist position.
	slices.SortFunc(available, func(a, b indexedTrack) int { return a.idx - b.idx })
	ordered := make([]Track, len(available))
	for i := range available {
		ordered[i] = available[i].track
	}
	return ordered, nil
}
