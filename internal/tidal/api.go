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

type MixesResponse struct {
	Items []Mix `json:"items"`
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
	endpoint := "/pages/my_collection_my_mixes"
	params := url.Values{}
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("deviceType", "BROWSER")
	params.Set("locale", "en_US")

	client := c.GetAuthClient(ctx)
	u := BaseURL + endpoint + "?" + params.Encode()
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get mixes (status %d): %s", resp.StatusCode, string(body))
	}

	// Mixes response is complex (paged), simplified here
	var res struct {
		Rows []struct {
			Modules []struct {
				PagedList struct {
					Items []Mix `json:"items"`
				} `json:"pagedList"`
			} `json:"modules"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	var mixes []Mix
	for _, row := range res.Rows {
		for _, mod := range row.Modules {
			mixes = append(mixes, mod.PagedList.Items...)
		}
	}
	return mixes, nil
}

func (c *Client) GetMixTracks(ctx context.Context, mixID string) ([]Track, error) {
	endpoint := "/pages/mix"
	params := url.Values{}
	params.Set("mixId", mixID)
	params.Set("countryCode", c.Session.CountryCode)
	params.Set("deviceType", "BROWSER")

	client := c.GetAuthClient(ctx)
	u := BaseURL + endpoint + "?" + params.Encode()
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var res struct {
		Rows []struct {
			Modules []struct {
				PagedList struct {
					Items []struct {
						Item Track `json:"item"`
					} `json:"items"`
				} `json:"pagedList"`
			} `json:"modules"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	var tracks []Track
	for _, row := range res.Rows {
		for _, mod := range row.Modules {
			for _, item := range mod.PagedList.Items {
				tracks = append(tracks, item.Item)
			}
		}
	}
	return tracks, nil
}
