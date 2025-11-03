package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Client communicates with the Jira REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	authHeader string
}

// Issue represents a condensed view of a Jira issue.
type Issue struct {
	Key      string
	Summary  string
	Status   string
	Parent   string
	Resolved string
	URL      string
}

// Filter captures the minimal details needed to execute a Jira filter.
type Filter struct {
	ID        int
	Name      string
	JQL       string
	SearchURL string
}

var errFilterNotFound = errors.New("filter not found")

// NewClient creates a Jira API client configured for the provided credentials.
func NewClient(baseURL, email, apiToken string) (*Client, error) {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		return nil, errors.New("jira base url is required")
	}
	if email == "" {
		return nil, errors.New("jira email is required")
	}
	if apiToken == "" {
		return nil, errors.New("jira api token is required")
	}

	authPayload := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", email, apiToken)))

	return &Client{
		baseURL:    base,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		authHeader: "Basic " + authPayload,
	}, nil
}

// ResolveFilter resolves an identifier (name or numeric id) to a filter definition.
func (c *Client) ResolveFilter(ctx context.Context, identifier string) (*Filter, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, errors.New("empty filter identifier")
	}

	if filter, err := c.filterByName(ctx, identifier); err == nil {
		return filter, nil
	} else if !errors.Is(err, errFilterNotFound) {
		return nil, err
	}

	if id, err := strconv.Atoi(identifier); err == nil {
		return c.filterByID(ctx, id)
	}

	return nil, fmt.Errorf("filter %q not found", identifier)
}

// SearchByFilter fetches issues that belong to the provided Jira filter.
func (c *Client) SearchByFilter(ctx context.Context, filter *Filter) ([]Issue, error) {
	if filter == nil {
		return nil, errors.New("filter is required")
	}
	if filter.ID <= 0 {
		return nil, fmt.Errorf("filter %q is missing a valid id", filter.Name)
	}

	details, err := c.filterByID(ctx, filter.ID)
	if err != nil {
		return nil, fmt.Errorf("fetch filter %d: %w", filter.ID, err)
	}

	searchURL := strings.TrimSpace(details.SearchURL)
	if searchURL == "" {
		return nil, fmt.Errorf("filter %q is missing searchUrl", details.Name)
	}

	issues, err := c.fetchIssuesFromSearchURL(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	if len(issues) == 0 {
		return nil, nil
	}

	return issues, nil
}

// ListFilters fetches a set of filters accessible to the current user.
func (c *Client) ListFilters(ctx context.Context) ([]Filter, error) {
	const pageSize = 100

	filters := make([]Filter, 0)
	startAt := 0

	for {
		endpoint := c.baseURL + "/rest/api/3/filter/search"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("create filter list request: %w", err)
		}

		q := req.URL.Query()
		q.Set("startAt", strconv.Itoa(startAt))
		q.Set("maxResults", strconv.Itoa(pageSize))
		q.Set("expand", "jql")
		req.URL.RawQuery = q.Encode()

		req.Header.Set("Authorization", c.authHeader)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("filter list request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return nil, fmt.Errorf("filter list failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}

		var payload filterSearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode filter list: %w", err)
		}
		resp.Body.Close()

		for _, f := range payload.Values {
			if filter := toFilter(f); filter != nil {
				filters = append(filters, *filter)
			}
		}

		if payload.IsLast || len(payload.Values) == 0 {
			break
		}

		startAt += len(payload.Values)
		if payload.Total > 0 && len(filters) >= payload.Total {
			break
		}
	}

	return filters, nil
}

func (c *Client) filterByName(ctx context.Context, name string) (*Filter, error) {
	endpoint := c.baseURL + "/rest/api/3/filter/search"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create filter search request: %w", err)
	}
	q := req.URL.Query()
	q.Set("filterName", name)
	q.Set("maxResults", "50")
	q.Set("expand", "jql")
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("filter search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errFilterNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("filter search failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload filterSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode filter search: %w", err)
	}

	for _, f := range payload.Values {
		if strings.EqualFold(f.Name, name) {
			return toFilter(f), nil
		}
	}

	if len(payload.Values) > 0 {
		return toFilter(payload.Values[0]), nil
	}

	return nil, errFilterNotFound
}

func (c *Client) filterByID(ctx context.Context, id int) (*Filter, error) {
	if id <= 0 {
		return nil, errFilterNotFound
	}

	endpoint := fmt.Sprintf("%s/rest/api/3/filter/%d", c.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create filter request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("filter request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errFilterNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("filter request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return nil, fmt.Errorf("read filter response: %w", err)
	}

	logFilterResponse(id, bodyBytes)

	var payload filterDetailsResponse
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, fmt.Errorf("decode filter: %w", err)
	}

	return toFilter(filterSummary{
		ID:        payload.ID,
		Name:      payload.Name,
		JQL:       payload.JQL,
		SearchURL: payload.SearchURL,
	}), nil
}

type filterSearchResponse struct {
	Values     []filterSummary `json:"values"`
	StartAt    int             `json:"startAt"`
	MaxResults int             `json:"maxResults"`
	Total      int             `json:"total"`
	IsLast     bool            `json:"isLast"`
}

type filterSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	JQL       string `json:"jql"`
	SearchURL string `json:"searchUrl"`
}

type filterDetailsResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	JQL       string `json:"jql"`
	SearchURL string `json:"searchUrl"`
}

func toFilter(summary filterSummary) *Filter {
	id, err := strconv.Atoi(summary.ID)
	if err != nil {
		id = 0
	}
	return &Filter{
		ID:        id,
		Name:      summary.Name,
		JQL:       summary.JQL,
		SearchURL: summary.SearchURL,
	}
}

func (c *Client) fetchIssuesFromSearchURL(ctx context.Context, searchURL string) ([]Issue, error) {
	searchURL = strings.TrimSpace(searchURL)
	if searchURL == "" {
		return nil, errors.New("search url is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create searchUrl request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute searchUrl request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		trimmed := strings.TrimSpace(string(body))
		return nil, fmt.Errorf("jira api error (searchUrl): %s: %s", resp.Status, trimmed)
	}

	var payload struct {
		Issues []struct {
			ID string `json:"id"`
		} `json:"issues"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode searchUrl response: %w", err)
	}

	if len(payload.Issues) == 0 {
		return nil, nil
	}

	issues := make([]Issue, 0, len(payload.Issues))
	for _, ref := range payload.Issues {
		issueID := strings.TrimSpace(ref.ID)
		if issueID == "" {
			continue
		}
		issue, err := c.fetchIssueDetails(ctx, issueID)
		if err != nil {
			return nil, fmt.Errorf("fetch issue %s: %w", issueID, err)
		}
		issues = append(issues, issue)
	}

	return issues, nil
}

type issueFields struct {
	Summary string `json:"summary"`
	Status  struct {
		Name string `json:"name"`
	} `json:"status"`
	Resolution struct {
		Name string `json:"name"`
	} `json:"resolution"`
	ResolutionDate string `json:"resolutiondate"`
	Parent         struct {
		Key string `json:"key"`
	} `json:"parent"`
}

func (c *Client) fetchIssueDetails(ctx context.Context, issueID string) (Issue, error) {
	endpoint := fmt.Sprintf("%s/rest/api/3/issue/%s", c.baseURL, issueID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return Issue{}, fmt.Errorf("create issue request: %w", err)
	}

	q := req.URL.Query()
	q.Set("fields", "summary,status,resolution,resolutiondate,parent")
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Issue{}, fmt.Errorf("execute issue request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		trimmed := strings.TrimSpace(string(body))
		return Issue{}, fmt.Errorf("jira api error (issue %s): %s: %s", issueID, resp.Status, trimmed)
	}

	var payload struct {
		Key    string      `json:"key"`
		Fields issueFields `json:"fields"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Issue{}, fmt.Errorf("decode issue %s: %w", issueID, err)
	}

	issue := issueFromFields(payload.Key, payload.Fields)
	if issue.Key != "" {
		issue.URL = fmt.Sprintf("%s/browse/%s", c.baseURL, issue.Key)
	}
	return issue, nil
}

func issueFromFields(key string, fields issueFields) Issue {
	return Issue{
		Key:      strings.TrimSpace(key),
		Summary:  strings.TrimSpace(fields.Summary),
		Status:   strings.TrimSpace(fields.Status.Name),
		Parent:   strings.TrimSpace(fields.Parent.Key),
		Resolved: formatResolved(fields.ResolutionDate, fields.Resolution.Name),
	}
}

func debugEnabled() bool {
	return strings.TrimSpace(os.Getenv("JIRA_DEBUG")) != ""
}

func logFilterResponse(filterID int, body []byte) {
	if !debugEnabled() {
		return
	}
	fmt.Fprintf(os.Stderr, "jira filter response (filter=%d):\n%s\n", filterID, string(body))
}

func formatResolved(resolutionDate, resolutionName string) string {
	dateValue := strings.TrimSpace(resolutionDate)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05-0700",
		"2006-01-02 15:04:05-0700",
		"2006-01-02 15:04:05",
	}

	if dateValue != "" {
		for _, layout := range layouts {
			if parsed, err := time.Parse(layout, dateValue); err == nil {
				return parsed.Format("2006-01-02 15:04")
			}
		}
	}

	if name := strings.TrimSpace(resolutionName); name != "" {
		return name
	}

	return dateValue
}
