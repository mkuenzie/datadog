package datadog_client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mythicDatadog/datadog_client/datadogcases"

	mythicConfig "github.com/MythicMeta/MythicContainer/config"
	"github.com/MythicMeta/MythicContainer/logging"
)

const (
	agentCaseVersionPrefix = "agentv"
	caseSearchPageSize     = 100
	maxCaseSearchPages     = 10
	casePollInterval       = 5 * time.Second
	caseTimelinePageSize   = 100
	payloadPostInterval    = 500 * time.Millisecond
	caseTypeID             = "00000000-0000-0000-0000-000000000001"
	CommentMaxLength       = 20000
)

type DatadogClient struct {
	apiClient *datadogcases.Client
	debug     bool
}

var tr = &http.Transport{
	TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
	MaxIdleConns:      1,
	MaxConnsPerHost:   1,
	DisableKeepAlives: true,
}
var client = &http.Client{
	Transport: tr,
}

func Initialize(config config) *DatadogClient {
	if config.Debug {
		logging.UpdateLogToStdout("debug")
	}
	return &DatadogClient{
		apiClient: datadogcases.NewClient(config.Region, config.ApiKey, config.AppKey),
		debug:     config.Debug,
	}

}

func chunkData(data string) ([]string, error) {
	commentCount := 1
	if len(data) > 0 {
		commentCount = (len(data) + CommentMaxLength - 1) / CommentMaxLength
	}

	comments := make([]string, 0, commentCount)
	for index := 0; index < commentCount; index++ {
		start := index * CommentMaxLength
		end := start + CommentMaxLength
		if end > len(data) {
			end = len(data)
		}

		chunkData := ""
		if start < len(data) {
			chunkData = data[start:end]
		}
		if len(chunkData) > CommentMaxLength {
			return nil, fmt.Errorf("put payload comment %d is %d characters, above limit %d", index, len(chunkData), CommentMaxLength)
		}
		comments = append(comments, string(chunkData))
	}
	return comments, nil
}

func (d *DatadogClient) createResponseCase(ctx context.Context, projectId string, parentCaseId string, data string) (string, error) {
	typeID := caseTypeID
	title := fmt.Sprintf("msg_%d", time.Now().Unix())
	caseId, httpResponse, err := d.apiClient.CreateCase(ctx, title, typeID, projectId)
	logging.LogDebug("create case", "httpResponse", httpResponse, "case_id", caseId, "project_id", projectId)
	if err != nil {
		return "", err
	}

	comments, err := chunkData(data)
	for _, comment := range comments {
		response, err := d.apiClient.CommentCase(ctx, caseId, projectId, comment)
		logging.LogDebug("post comment", "httpResponse", response, "case_id", caseId, "project_id", projectId)
		if err != nil {
			logging.LogError(err, "failed to post comment", "case_id", caseId, "project_id", projectId)
			return caseId, err
		}
	}
	d.apiClient.LinkCase(ctx, caseId, parentCaseId)
	d.apiClient.UpdateCasePriority(ctx, caseId, datadogcases.CasePriorityP5)
	return caseId, nil
}

func (d *DatadogClient) searchProjectCases(ctx context.Context, projectId string, pageNumber int) (datadogcases.CasesResponse, *http.Response, error) {
	return d.apiClient.SearchCases(ctx, datadogcases.SearchOptions{
		PageSize:   caseSearchPageSize,
		PageNumber: pageNumber,
		SortField:  datadogcases.SortFieldCreatedAt,
		Filter:     fmt.Sprintf("project:%s status_group:%s", projectId, datadogcases.CaseStatusInProgress),
		SortAsc:    true,
	})
}

func timelineCommentMessages(timeline datadogcases.TimelineResponse) []string {
	messages := []string{}
	for _, cell := range timeline.Data {
		cellAttributes := cell.Attributes
		if cellAttributes.Type != nil && !strings.EqualFold(*cellAttributes.Type, datadogcases.TimelineTypeComment) {
			continue
		}
		if cellAttributes.CellContent == nil || cellAttributes.CellContent.Message == nil {
			continue
		}

		message := strings.TrimSpace(*cellAttributes.CellContent.Message)
		if message != "" {
			messages = append(messages, message)
		}
	}
	return messages
}

func (d *DatadogClient) getCaseCommentMessages(ctx context.Context, caseID string) ([]string, *http.Response, error) {
	messages := []string{}
	var lastHTTPResponse *http.Response
	for pageNumber := 0; ; pageNumber++ {
		timeline, httpResponse, err := d.apiClient.GetCaseTimeline(ctx, caseID, pageNumber, caseTimelinePageSize)
		lastHTTPResponse = httpResponse
		if err != nil {
			return nil, httpResponse, err
		}

		messages = append(messages, timelineCommentMessages(timeline)...)
		if len(timeline.Data) < caseTimelinePageSize {
			return messages, lastHTTPResponse, nil
		}
	}
}

func pushToMythic(ctx context.Context, data string) ([]byte, error) {
	// POST to MYTHIC_ADDRESS with header Mythic: datadog
	requestURL := fmt.Sprintf("http://%s:%d/agent_message", mythicConfig.MythicConfig.MythicServerHost, mythicConfig.MythicConfig.MythicServerPort)
	req, err := http.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewBuffer([]byte(data)))
	if err != nil {
		logging.LogError(err, "failed to create request")
	}
	req.Header.Set("mythic", "datadog")
	resp, err := client.Do(req)
	if err != nil {
		logging.LogError(err, "failed to send request")
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logging.LogError(nil, "bad response from server", "statuscode", resp.StatusCode)
		return nil, err
	}
	finalResponse, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil {
		logging.LogError(err, "failed to decode response", "response", string(body))
		return nil, err
	}
	return finalResponse, nil
}

func (d *DatadogClient) handleCase(ctx context.Context, projectId string, agentCase datadogcases.Case) {
	messages, httpResponse, err := d.getCaseCommentMessages(ctx, agentCase.ID)
	if err != nil {
		logging.LogError(err, "reading case timeline comments failed", "response", httpResponse, "case_id", agentCase.ID, "project_id", projectId)
		return
	}
	payloadData := strings.Join(messages, "")

	mythicResponse, err := pushToMythic(ctx, payloadData)

	if err != nil {
		//Do nothing, case will attempt re-process on next tick
		logging.LogError(err, "pushing payload to mythic failed")
		return
	}
	httpResponse, err = d.apiClient.CloseCase(ctx, agentCase.ID, projectId)
	if err != nil {
		logging.LogError(err, "closing case failed", "response", httpResponse, "case_id", agentCase.ID, "project_id", projectId)
	}

	caseId, err := d.createResponseCase(ctx, projectId, agentCase.ID, string(mythicResponse))
	if err != nil {
		logging.LogError(err, "failed to create case for mythic response", "project_id", projectId)
	}
	logging.LogDebug("created case for mythic response", "case_id", caseId, "project_id", projectId)
}

func (d *DatadogClient) poll(ctx context.Context) {

	projects, err := d.apiClient.GetProjects(ctx)
	if err != nil {
		logging.LogError(err, "no projects retrieved")
	}
	agentCases := []datadogcases.Case{}
	for _, project := range projects {
		for pageNumber := 1; pageNumber <= maxCaseSearchPages; pageNumber++ {
			agentCasesResponse, httpResponse, err := d.searchProjectCases(ctx, project.ID, pageNumber)
			if err != nil {
				logging.LogError(err, "Searching cases failed", "response", httpResponse, "projectId", project.ID, "pageNumber", pageNumber)
			}
			agentCases = append(agentCases, agentCasesResponse.Data...)
			if agentCasesResponse.Meta.Page.Current == agentCasesResponse.Meta.Page.Total {
				break
			}
		}
		for _, agentCase := range agentCases {
			if agentCase.Attributes.Priority == datadogcases.CasePriorityP5 { // P5 indicates write is completed
				go d.handleCase(ctx, project.ID, agentCase)
			}
		}
	}
}

func Start(client *DatadogClient) {

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			client.poll(ctx)
		case <-ctx.Done():
			return
		}
	}
}
