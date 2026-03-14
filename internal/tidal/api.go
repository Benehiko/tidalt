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
)

// ErrNotFound is returned by GetTrack when the API responds with 404.
var ErrNotFound = errors.New("not found")

type Track struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Artist struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
	Album struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
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
	client := c.GetAuthClient(ctx)
	resp, err := client.Get(fmt.Sprintf("%s/users/%d", BaseURL, c.Session.UserID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get user (status %d): %s", resp.StatusCode, string(body))
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
	client := c.GetAuthClient(ctx)
	resp, err := client.Get(BaseURL + "/tracks/" + trackID + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 404 {
		return nil, ErrNotFound
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get track (status %d): %s", resp.StatusCode, string(body))
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

	client := c.GetAuthClient(ctx)
	u := BaseURL + "/search?" + params.Encode()
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed (status %d): %s", resp.StatusCode, string(body))
	}

	var res SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Tracks.Items, nil
}

func (c *Client) GetStreamURL(ctx context.Context, trackID int) (string, error) {
	qualities := []string{"HI_RES_LOSSLESS", "LOSSLESS", "HIGH", "LOW"}
	var lastErr error

	for _, q := range qualities {
		endpoint := fmt.Sprintf("/tracks/%d/urlpostpaywall", trackID)
		params := url.Values{}
		params.Set("urlusagemode", "STREAM")
		params.Set("audioquality", q)
		params.Set("assetpresentation", "FULL")
		params.Set("countryCode", c.Session.CountryCode)

		client := c.GetAuthClient(ctx)
		u := BaseURL + endpoint + "?" + params.Encode()
		resp, err := client.Get(u)
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == 200 {
			var s StreamResponse
			if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
				return "", err
			}
			if len(s.URLs) == 0 {
				return "", fmt.Errorf("stream response contained no URLs")
			}
			return s.URLs[0], nil
		}

		body, _ := io.ReadAll(resp.Body)
		lastErr = fmt.Errorf("failed to get stream for %s (status %d): %s", q, resp.StatusCode, string(body))
	}

	return "", lastErr
}

func (c *Client) GetFavorites(ctx context.Context, limit int) ([]Track, error) {
	endpoint := fmt.Sprintf("/users/%d/favorites/tracks", c.Session.UserID)
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("order", "DATE")
	params.Set("orderDirection", "DESC")

	client := c.GetAuthClient(ctx)
	u := BaseURL + endpoint + "?" + params.Encode()
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get favorites (status %d): %s", resp.StatusCode, string(body))
	}

	var res FavoritesResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	tracks := make([]Track, len(res.Items))
	for i, item := range res.Items {
		tracks[i] = item.Item
	}
	return tracks, nil
}

func (c *Client) GetTrackRadio(ctx context.Context, trackID int) ([]Track, error) {
	params := url.Values{}
	params.Set("limit", "100")
	params.Set("countryCode", c.Session.CountryCode)

	client := c.GetAuthClient(ctx)
	u := fmt.Sprintf("%s/tracks/%d/radio?%s", BaseURL, trackID, params.Encode())
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get track radio (status %d): %s", resp.StatusCode, string(body))
	}

	var res radioResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (c *Client) AddFavorite(ctx context.Context, trackID int) error {
	endpoint := fmt.Sprintf("/users/%d/favorites/tracks", c.Session.UserID)
	query := url.Values{}
	query.Set("countryCode", c.Session.CountryCode)

	body := url.Values{}
	body.Set("trackId", strconv.Itoa(trackID))

	client := c.GetAuthClient(ctx)
	resp, err := client.Post(
		BaseURL+endpoint+"?"+query.Encode(),
		"application/x-www-form-urlencoded",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add favorite (status %d): %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *Client) RemoveFavorite(ctx context.Context, trackID int) error {
	endpoint := fmt.Sprintf("/users/%d/favorites/tracks/%d", c.Session.UserID, trackID)
	params := url.Values{}
	params.Set("countryCode", c.Session.CountryCode)

	client := c.GetAuthClient(ctx)
	req, err := newDeleteRequest(BaseURL + endpoint + "?" + params.Encode())
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to remove favorite (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func newDeleteRequest(u string) (*http.Request, error) {
	return http.NewRequest(http.MethodDelete, u, nil)
}

func (c *Client) GetMixes(ctx context.Context) ([]Mix, error) {
	params := url.Values{}
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("include", "myMixes")

	client := c.GetAuthClient(ctx)
	u := BaseURLV2 + "/userRecommendations/me/relationships/myMixes?" + params.Encode()
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get mixes (status %d): %s", resp.StatusCode, string(body))
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

	client := c.GetAuthClient(ctx)
	u := BaseURLV2 + "/playlists/" + mixID + "/relationships/items?" + params.Encode()
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get mix tracks (status %d): %s", resp.StatusCode, string(body))
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
	for i, it := range available {
		ordered[i] = it.track
	}
	return ordered, nil
}
