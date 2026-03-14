package tidal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
)

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

// v2 JSON:API types for mixes and playlist items.

type v2ResourceIdentifier struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type v2PlaylistAttributes struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type v2TrackAttributes struct {
	Title string `json:"title"`
}

type v2ArtistAttributes struct {
	Name string `json:"name"`
}

type v2TrackRelationships struct {
	Artists struct {
		Data []v2ResourceIdentifier `json:"data"`
	} `json:"artists"`
}

type v2TrackResource struct {
	ID            string               `json:"id"`
	Type          string               `json:"type"`
	Attributes    v2TrackAttributes    `json:"attributes"`
	Relationships v2TrackRelationships `json:"relationships"`
}

type v2ArtistResource struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Attributes v2ArtistAttributes `json:"attributes"`
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
	client := c.GetAuthClient(ctx)
	resp, err := client.Get(BaseURL + "/tracks/" + trackID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

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

	// Parse included resources into typed maps.
	trackMap := make(map[string]v2TrackResource)
	artistMap := make(map[string]v2ArtistResource)
	for _, raw := range res.Included {
		var base struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &base); err != nil {
			continue
		}
		switch base.Type {
		case "tracks":
			var t v2TrackResource
			if err := json.Unmarshal(raw, &t); err == nil {
				trackMap[t.ID] = t
			}
		case "artists":
			var a v2ArtistResource
			if err := json.Unmarshal(raw, &a); err == nil {
				artistMap[a.ID] = a
			}
		}
	}

	tracks := make([]Track, 0, len(res.Data))
	for _, ref := range res.Data {
		if ref.Type != "tracks" {
			continue
		}
		t, ok := trackMap[ref.ID]
		if !ok {
			continue
		}
		id, err := strconv.Atoi(ref.ID)
		if err != nil {
			continue
		}
		track := Track{
			ID:    id,
			Title: t.Attributes.Title,
		}
		// Use the first artist from the track's relationships.
		if len(t.Relationships.Artists.Data) > 0 {
			artistID := t.Relationships.Artists.Data[0].ID
			if a, ok := artistMap[artistID]; ok {
				track.Artist.Name = a.Attributes.Name
			}
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}
