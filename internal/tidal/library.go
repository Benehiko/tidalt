package tidal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// SearchResults holds the multi-category results of a search query.
type SearchResults struct {
	Tracks    []Track
	Artists   []Artist
	Albums    []Album
	Playlists []Playlist
}

// searchAllResponse decodes the multi-type /search payload.
type searchAllResponse struct {
	Tracks    struct{ Items []Track }    `json:"tracks"`
	Artists   struct{ Items []Artist }   `json:"artists"`
	Albums    struct{ Items []Album }    `json:"albums"`
	Playlists struct{ Items []Playlist } `json:"playlists"`
}

// SearchAll queries every category (tracks, artists, albums, playlists) and
// returns the grouped results. Track artists are normalized so Track.Artist is
// always populated.
func (c *Client) SearchAll(ctx context.Context, query string) (*SearchResults, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", "20")
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("types", "TRACKS,ARTISTS,ALBUMS,PLAYLISTS")

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

	var res searchAllResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	for i := range res.Tracks.Items {
		res.Tracks.Items[i].normalizeArtist()
	}
	return &SearchResults{
		Tracks:    res.Tracks.Items,
		Artists:   res.Artists.Items,
		Albums:    res.Albums.Items,
		Playlists: res.Playlists.Items,
	}, nil
}

// --- Favorite artists & albums ---

// GetFavoriteArtists returns the user's favorited artists.
func (c *Client) GetFavoriteArtists(ctx context.Context, limit int) ([]Artist, error) {
	var res struct {
		Items []struct {
			Item Artist `json:"item"`
		} `json:"items"`
	}
	if err := c.getFavorites(ctx, "artists", limit, &res); err != nil {
		return nil, err
	}
	out := make([]Artist, len(res.Items))
	for i := range res.Items {
		out[i] = res.Items[i].Item
	}
	return out, nil
}

// GetFavoriteAlbums returns the user's favorited albums.
func (c *Client) GetFavoriteAlbums(ctx context.Context, limit int) ([]Album, error) {
	var res struct {
		Items []struct {
			Item Album `json:"item"`
		} `json:"items"`
	}
	if err := c.getFavorites(ctx, "albums", limit, &res); err != nil {
		return nil, err
	}
	out := make([]Album, len(res.Items))
	for i := range res.Items {
		out[i] = res.Items[i].Item
	}
	return out, nil
}

// getFavorites GETs /users/{id}/favorites/{kind} and decodes into target.
func (c *Client) getFavorites(ctx context.Context, kind string, limit int, target any) error {
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("order", "DATE")
	params.Set("orderDirection", "DESC")

	u := fmt.Sprintf("%s/users/%d/favorites/%s?%s", BaseURL, c.Session.UserID, kind, params.Encode())
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return apiErr("get favorite "+kind, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

// AddFavoriteArtist adds an artist to the user's favorites.
func (c *Client) AddFavoriteArtist(ctx context.Context, artistID int) error {
	return c.addFavorite(ctx, "artists", "artistId", artistID)
}

// AddFavoriteAlbum adds an album to the user's favorites.
func (c *Client) AddFavoriteAlbum(ctx context.Context, albumID int) error {
	return c.addFavorite(ctx, "albums", "albumId", albumID)
}

// RemoveFavoriteArtist removes an artist from the user's favorites.
func (c *Client) RemoveFavoriteArtist(ctx context.Context, artistID int) error {
	return c.removeFavorite(ctx, "artists", artistID)
}

// RemoveFavoriteAlbum removes an album from the user's favorites.
func (c *Client) RemoveFavoriteAlbum(ctx context.Context, albumID int) error {
	return c.removeFavorite(ctx, "albums", albumID)
}

func (c *Client) addFavorite(ctx context.Context, kind, idField string, id int) error {
	endpoint := fmt.Sprintf("/users/%d/favorites/%s", c.Session.UserID, kind)
	query := url.Values{}
	query.Set("countryCode", c.Session.CountryCode)

	body := url.Values{}
	body.Set(idField, strconv.Itoa(id))

	resp, err := c.authPostForm(ctx, BaseURL+endpoint+"?"+query.Encode(), body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return apiErr("add favorite "+kind, resp.StatusCode, b)
	}
	return nil
}

func (c *Client) removeFavorite(ctx context.Context, kind string, id int) error {
	endpoint := fmt.Sprintf("/users/%d/favorites/%s/%d", c.Session.UserID, kind, id)
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
		return apiErr("remove favorite "+kind, resp.StatusCode, body)
	}
	return nil
}

// --- Playlists ---

// GetUserPlaylists returns the playlists the user has created or saved.
func (c *Client) GetUserPlaylists(ctx context.Context) ([]Playlist, error) {
	params := url.Values{}
	params.Set("limit", "50")
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("order", "DATE")
	params.Set("orderDirection", "DESC")

	u := fmt.Sprintf("%s/users/%d/playlists?%s", BaseURL, c.Session.UserID, params.Encode())
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("get user playlists", resp.StatusCode, body)
	}

	var res struct {
		Items []Playlist `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Items, nil
}

// GetPlaylistTracks returns the tracks of a playlist by its UUID.
func (c *Client) GetPlaylistTracks(ctx context.Context, uuid string) ([]Track, error) {
	params := url.Values{}
	params.Set("limit", "1000")
	params.Set("countryCode", c.Session.CountryCode)

	u := fmt.Sprintf("%s/playlists/%s/tracks?%s", BaseURL, url.PathEscape(uuid), params.Encode())
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, apiErr("get playlist tracks", resp.StatusCode, body)
	}

	var res struct {
		Items []Track `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	for i := range res.Items {
		res.Items[i].normalizeArtist()
	}
	return res.Items, nil
}

// CreatePlaylist creates a new playlist and returns its UUID.
func (c *Client) CreatePlaylist(ctx context.Context, title, description string) (string, error) {
	endpoint := fmt.Sprintf("/users/%d/playlists", c.Session.UserID)
	query := url.Values{}
	query.Set("countryCode", c.Session.CountryCode)

	body := url.Values{}
	body.Set("title", title)
	body.Set("description", description)

	resp, err := c.authPostForm(ctx, BaseURL+endpoint+"?"+query.Encode(), body)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return "", apiErr("create playlist", resp.StatusCode, b)
	}

	var res struct {
		UUID string `json:"uuid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.UUID == "" {
		return "", errors.New("create playlist: response contained no uuid")
	}
	return res.UUID, nil
}

// AddTracksToPlaylist appends tracks to a playlist. Tidal's v1 playlist
// mutation requires the playlist's current ETag in an If-None-Match header, so
// this first fetches the ETag, then POSTs the comma-separated track IDs.
func (c *Client) AddTracksToPlaylist(ctx context.Context, uuid string, trackIDs []int) error {
	if len(trackIDs) == 0 {
		return nil
	}
	etag, err := c.playlistETag(ctx, uuid)
	if err != nil {
		return err
	}

	ids := make([]string, len(trackIDs))
	for i, id := range trackIDs {
		ids[i] = strconv.Itoa(id)
	}

	query := url.Values{}
	query.Set("countryCode", c.Session.CountryCode)

	body := url.Values{}
	body.Set("trackIds", strings.Join(ids, ","))
	body.Set("onArtifactNotFound", "SKIP")

	u := fmt.Sprintf("%s/playlists/%s/items?%s", BaseURL, url.PathEscape(uuid), query.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(body.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.GetAuthClient(ctx).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated &&
		resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return apiErr("add tracks to playlist", resp.StatusCode, b)
	}
	return nil
}

// playlistETag fetches the playlist's current ETag, required by the mutation
// endpoint's optimistic-concurrency check.
func (c *Client) playlistETag(ctx context.Context, uuid string) (string, error) {
	params := url.Values{}
	params.Set("countryCode", c.Session.CountryCode)

	u := fmt.Sprintf("%s/playlists/%s?%s", BaseURL, url.PathEscape(uuid), params.Encode())
	resp, err := c.authGet(ctx, u)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", apiErr("get playlist etag", resp.StatusCode, body)
	}
	return resp.Header.Get("ETag"), nil
}
