package datadogcases

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MythicMeta/MythicContainer/logging"
)

const (
	CaseStatusClosed       = "CLOSED"
	CaseStatusOpen         = "OPEN"
	CaseStatusInProgress   = "IN_PROGRESS"
	CaseStatusGroupOpen    = "SG_OPEN"
	CasePriorityNotDefined = "NOT_DEFINED"
	CasePriorityP5         = "P5"
	CaseTypeCase           = "case"
	ProjectResourceType    = "project"
	SortFieldCreatedAt     = "created_at"
	TimelineTypeComment    = "COMMENT"
	ProjectsRoute          = "/api/v2/cases/projects"
	CasesRoute             = "/api/v2/cases"
	ApiSuffix              = "datadoghq.com"
	defaultRequestTimeout  = 30 * time.Second
	maxRateLimitRetries    = 8
	maxErrorBodyCharacters = 2000
	maxRateLimitDelay      = 30 * time.Second
)

type Client struct {
	Site          string
	APIKey        string
	AppKey        string
	HTTPClient    *http.Client
	RateLimitWait func(context.Context, time.Duration) error
}

type SearchOptions struct {
	PageSize   int
	PageNumber int
	SortField  string
	Filter     string
	SortAsc    bool
}

type Meta struct {
	Page struct {
		Current int `json:"current,omitempty"`
		Size    int `json:"size,omitempty"`
		Total   int `json:"total,omitempty"`
	} `json:"page:omitempty"`
}

type CasesResponse struct {
	Data []Case `json:"data,omitempty"`
	Meta Meta   `json:"meta,omitempty"`
}

type CaseResponse struct {
	Data *Case `json:"data,omitempty"`
}

type Case struct {
	ID            string             `json:"id,omitempty"`
	Type          string             `json:"type,omitempty"`
	Attributes    CaseAttributes     `json:"attributes,omitempty"`
	Relationships *CaseRelationships `json:"relationships,omitempty"`
}

type Project struct {
	ID         string `json:"id,omitempty"`
	Attributes struct {
		Name string `json:"name,omitempty"`
		Key  string `json:"key,omitempty"`
	} `json:"attributes,omitempty"`
	Type string `json:"type,omitempty"`
}

type CaseAttributes struct {
	CreatedAt   time.Time `json:"created_at,omitempty"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status,omitempty"`
	StatusGroup string    `json:"status_group,omitempty"`
	StatusName  string    `json:"status_name,omitempty"`
	Title       string    `json:"title,omitempty"`
	Priority    string    `json:"priority,omitempty"`
	TypeID      string    `json:"type_id,omitempty"`
}

type CaseRelationships struct {
	Project *ProjectRelationship `json:"project,omitempty"`
}

type ProjectRelationship struct {
	Data ProjectRelationshipData `json:"data"`
}

type ProjectRelationshipData struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func projectRelationship(projectID string) ProjectRelationship {
	return ProjectRelationship{
		Data: ProjectRelationshipData{
			ID:   projectID,
			Type: ProjectResourceType,
		},
	}
}

type TimelineResponse struct {
	Data []TimelineCellResource `json:"data,omitempty"`
}

type TimelineCellResource struct {
	ID         string       `json:"id,omitempty"`
	Type       string       `json:"type,omitempty"`
	Attributes TimelineCell `json:"attributes,omitempty"`
}

type TimelineCell struct {
	Type        *string              `json:"type,omitempty"`
	CellContent *TimelineCellContent `json:"cell_content,omitempty"`
}

type TimelineCellContent struct {
	Message *string `json:"message,omitempty"`
}

type APIError struct {
	Status string
	Body   string
}

func NewClient(site string, apiKey string, appKey string) *Client {
	return &Client{
		Site:   site,
		APIKey: apiKey,
		AppKey: appKey,
		HTTPClient: &http.Client{
			Timeout: defaultRequestTimeout,
		},
	}
}

func (client *Client) GetProjects(ctx context.Context) ([]Project, error) {
	var projectsResponse = struct {
		Data []Project `json:"data,omitempty"`
	}{}
	_, err := client.do(ctx, http.MethodGet, ProjectsRoute, nil, nil, &projectsResponse)
	if err != nil {
		return nil, err
	}
	return projectsResponse.Data, nil
}

func (client *Client) SearchCases(ctx context.Context, options SearchOptions) (CasesResponse, *http.Response, error) {
	query := searchCasesQuery(options)

	var cases CasesResponse
	response, err := client.do(ctx, http.MethodGet, CasesRoute, query, nil, &cases)
	return cases, response, err
}

func (client *Client) UpdateCasePriority(ctx context.Context, caseId string, priority string) (*http.Response, error) {
	body := struct {
		Data struct {
			Attributes struct {
				Priority string `json:"priority"`
			} `json:"attributes"`
			Type string `json:"type"`
		} `json:"data"`
	}{}
	body.Data.Attributes.Priority = priority
	body.Data.Type = CaseTypeCase

	return client.do(ctx, http.MethodPost, "/api/v2/cases/"+url.PathEscape(caseId)+"/priority", nil, body, nil)
}

func (err APIError) Error() string {
	body := strings.TrimSpace(err.Body)
	if body == "" {
		return err.Status
	}
	if len(body) > maxErrorBodyCharacters {
		body = body[:maxErrorBodyCharacters] + "..."
	}
	return fmt.Sprintf("%s: %s", err.Status, body)
}

func searchCasesQuery(options SearchOptions) url.Values {
	query := url.Values{}
	if options.PageNumber > 0 {
		query.Set("page[number]", fmt.Sprint(options.PageNumber))
	}
	if options.PageSize > 0 {
		query.Set("page[size]", fmt.Sprint(options.PageSize))
	}
	if options.SortField != "" {
		query.Set("sort[field]", options.SortField)
	}
	if options.Filter != "" {
		query.Set("filter", options.Filter)
	}
	query.Set("sort[asc]", fmt.Sprint(options.SortAsc))
	return query
}

func (client *Client) LinkCase(ctx context.Context, childCaseId string, parentCaseId string) error {
	body := struct {
		Data struct {
			Attributes struct {
				ChildEntityId    string `json:"child_entity_id"`
				ChildEntityType  string `json:"child_entity_type"`
				ParentEntityId   string `json:"parent_entity_id"`
				ParentEntityType string `json:"parent_entity_type"`
				Relationship     string `json:"relationship"`
			} `json:"attributes"`
			Type string `json:"type"`
		} `json:"data"`
	}{}
	body.Data.Attributes.ChildEntityId = childCaseId
	body.Data.Attributes.ChildEntityType = CaseTypeCase
	body.Data.Attributes.ParentEntityId = parentCaseId
	body.Data.Attributes.ParentEntityType = CaseTypeCase
	body.Data.Attributes.Relationship = "RELATES_TO"
	body.Data.Type = "link"

	_, err := client.do(ctx, http.MethodPost, "/api/v2/cases/link", nil, body, nil)
	if err != nil {
		return err
	}
	return nil
}

func (client *Client) CreateCase(ctx context.Context, title string, typeID string, projectID string) (string, *http.Response, error) {
	body := struct {
		Data struct {
			Attributes struct {
				Description string `json:"description"`
				Priority    string `json:"priority"`
				Title       string `json:"title"`
				TypeID      string `json:"type_id"`
			} `json:"attributes"`
			Relationships struct {
				Project ProjectRelationship `json:"project"`
			} `json:"relationships"`
			Type string `json:"type"`
		} `json:"data"`
	}{}
	body.Data.Attributes.Priority = CasePriorityNotDefined
	body.Data.Attributes.Title = title
	body.Data.Attributes.TypeID = typeID
	body.Data.Relationships.Project = projectRelationship(projectID)
	body.Data.Type = CaseTypeCase

	var caseResponse CaseResponse
	fmt.Printf("%+v\n", body)
	response, err := client.do(ctx, http.MethodPost, "/api/v2/cases", nil, body, &caseResponse)
	if err != nil {
		return "", response, err
	}
	if caseResponse.Data == nil || caseResponse.Data.ID == "" {
		return "", response, fmt.Errorf("create case response did not include a case id")
	}
	return caseResponse.Data.ID, response, nil
}

func (client *Client) CommentCase(ctx context.Context, caseID string, projectID string, message string) (*http.Response, error) {
	body := struct {
		Data struct {
			Attributes struct {
				Comment string `json:"comment"`
			} `json:"attributes"`
			Relationships struct {
				Project ProjectRelationship `json:"project"`
			} `json:"relationships"`
			Type string `json:"type"`
		} `json:"data"`
	}{}
	body.Data.Attributes.Comment = message
	body.Data.Relationships.Project = projectRelationship(projectID)
	body.Data.Type = CaseTypeCase

	return client.do(ctx, http.MethodPost, "/api/v2/cases/"+url.PathEscape(caseID)+"/comment", nil, body, nil)
}

func (client *Client) CloseCase(ctx context.Context, caseID string, projectID string) (*http.Response, error) {
	body := struct {
		Data struct {
			Attributes struct {
				Status string `json:"status"`
			} `json:"attributes"`
			Relationships struct {
				Project ProjectRelationship `json:"project"`
			} `json:"relationships"`
			Type string `json:"type"`
		} `json:"data"`
	}{}
	body.Data.Attributes.Status = CaseStatusClosed
	body.Data.Relationships.Project = projectRelationship(projectID)
	body.Data.Type = CaseTypeCase

	return client.do(ctx, http.MethodPost, "/api/v2/cases/"+url.PathEscape(caseID)+"/status", nil, body, nil)
}

// Get comments in chronological order (oldest) first.  created_at serves as chunk index.
func (client *Client) GetCaseTimeline(ctx context.Context, caseID string, pageNumber int, pageSize int) (TimelineResponse, *http.Response, error) {
	query := url.Values{}
	query.Set("page[size]", fmt.Sprint(pageSize))
	query.Set("page[number]", fmt.Sprint(pageNumber))
	query.Set("sort[ascending]", "true")

	var timeline TimelineResponse
	response, err := client.do(ctx, http.MethodGet, "/api/v2/cases/"+url.PathEscape(caseID)+"/timelines", query, nil, &timeline)
	return timeline, response, err
}

func (client *Client) do(ctx context.Context, method string, path string, query url.Values, body interface{}, output interface{}) (*http.Response, error) {
	requestURL := client.baseURL() + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}

	var encodedBody []byte
	var err error
	if body != nil {
		encodedBody, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	for attempt := 0; ; attempt++ {
		var requestBody io.Reader
		if encodedBody != nil {
			requestBody = bytes.NewReader(encodedBody)
		}

		request, err := http.NewRequestWithContext(ctx, method, requestURL, requestBody)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("DD-API-KEY", client.APIKey)
		request.Header.Set("DD-APPLICATION-KEY", client.AppKey)
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		logging.LogDebug("sending request", "method", method, "url", requestURL, "body", string(encodedBody))
		response, err := client.httpClient().Do(request)
		if err != nil {
			return response, err
		}

		responseBody, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			return response, readErr
		}
		logging.LogDebug("received response", "statusCode", response.Status, "body", string(responseBody))
		if response.StatusCode == http.StatusTooManyRequests && attempt < maxRateLimitRetries {
			if err := client.waitRateLimit(ctx, rateLimitDelay(response, attempt)); err != nil {
				return response, err
			}
			continue
		}

		if response.StatusCode >= 300 {
			return response, APIError{Status: response.Status, Body: string(responseBody)}
		}
		if output == nil || len(responseBody) == 0 {
			return response, nil
		}
		if err := json.Unmarshal(responseBody, output); err != nil {
			return response, err
		}
		return response, nil
	}
}

func (client *Client) baseURL() string {
	site := strings.TrimSpace(client.Site)
	baseUrl := "https://"
	switch site {
	case "us1":
		baseUrl = baseUrl + "api." + ApiSuffix
	case "eu1":
		baseUrl = baseUrl + "api.datadoghq.eu"
	default:
		baseUrl = baseUrl + "api." + site + "." + ApiSuffix
	}
	return baseUrl
}

func (client *Client) httpClient() *http.Client {
	if client.HTTPClient != nil {
		return client.HTTPClient
	}
	return &http.Client{Timeout: defaultRequestTimeout}
}

func (client *Client) waitRateLimit(ctx context.Context, delay time.Duration) error {
	if client.RateLimitWait != nil {
		return client.RateLimitWait(ctx, delay)
	}
	return sleepContext(ctx, delay)
}

func rateLimitDelay(response *http.Response, attempt int) time.Duration {
	if delay := secondsHeaderDelay(response.Header.Get("Retry-After")); delay > 0 {
		return delay
	}
	if delay := secondsHeaderDelay(response.Header.Get("X-RateLimit-Reset")); delay > 0 {
		return delay
	}

	delay := time.Duration(1<<attempt) * time.Second
	if delay > maxRateLimitDelay {
		return maxRateLimitDelay
	}
	return delay
}

func secondsHeaderDelay(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
