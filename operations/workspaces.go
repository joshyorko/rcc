package operations

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/robocorp/rcc/cloud"
)

const (
	listWorkspacesApi = `/workspace-v1/workspaces`
	workspaceTreeApi  = `/workspace-v1/workspaces/%s/tree`
	newRobotApi       = `/robot-v1/workspaces/%s/robots`
)

type WorkspaceTreeData struct {
	Id     string       `json:"id"`
	Robots []*RobotData `json:"robots,omitempty"`
}

type RobotData struct {
	Id      string                 `json:"id"`
	Name    string                 `json:"name,omitempty"`
	Package map[string]interface{} `json:"package,omitempty"`
}

func fetchAnyToken(client cloud.Client, account *account, claims *Claims, period *TokenPeriod) (string, error) {
	data, err := AuthorizeCommand(client, account, claims, period)
	if err != nil {
		return "", err
	}
	token, ok := data["token"].(string)
	if ok {
		return token, nil
	}
	return "", errors.New("Could not get authorization token.")
}

func summonEditRobotToken(client cloud.Client, account *account, workspace string) (string, error) {
	period := DefaultTokenPeriod()
	claims := EditRobotClaims(period.RequestSeconds(), workspace)
	token, ok := account.Cached(period, claims.Name, claims.Url)
	if ok {
		return token, nil
	}
	return fetchAnyToken(client, account, claims, period)
}

func summonWorkspaceToken(client cloud.Client, account *account) (string, error) {
	period := DefaultTokenPeriod()
	claims := ViewWorkspacesClaims(period.RequestSeconds())
	token, ok := account.Cached(period, claims.Name, claims.Url)
	if ok {
		return token, nil
	}
	return fetchAnyToken(client, account, claims, period)
}

func WorkspacesCommand(client cloud.Client, account *account) (interface{}, error) {
	credentials, err := summonWorkspaceToken(client, account)
	if err != nil {
		return nil, err
	}
	request := client.NewRequest(listWorkspacesApi)
	request.Headers[authorization] = BearerToken(credentials)
	response := client.Get(request)
	if response.Status != 200 {
		return nil, fmt.Errorf("%d: %s", response.Status, response.Body)
	}
	tokens := make([]Token, 100)
	err = json.Unmarshal(response.Body, &tokens)
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

func WorkspaceTreeCommandRequest(client cloud.Client, account *account, workspace string) (*cloud.Response, error) {
	credentials, err := summonWorkspaceToken(client, account)
	if err != nil {
		return nil, err
	}
	request := client.NewRequest(fmt.Sprintf(workspaceTreeApi, workspace))
	request.Headers[authorization] = BearerToken(credentials)
	response := client.Get(request)
	if response.Status != 200 {
		return nil, fmt.Errorf("%d: %s", response.Status, response.Body)
	}
	return response, nil
}

func WorkspaceTreeCommand(client cloud.Client, account *account, workspace string) (interface{}, error) {
	response, err := WorkspaceTreeCommandRequest(client, account, workspace)
	if err != nil {
		return nil, err
	}
	token := make(Token)
	err = json.Unmarshal(response.Body, &token)
	if err != nil {
		return nil, err
	}
	return token, nil
}

func NewRobotCommand(client cloud.Client, account *account, workspace, robotName string) (Token, error) {
	credentials, err := summonEditRobotToken(client, account, workspace)
	if err != nil {
		return nil, err
	}
	naming := make(Token)
	naming["name"] = robotName
	body, err := naming.AsJson()
	if err != nil {
		return nil, err
	}
	request := client.NewRequest(fmt.Sprintf(newRobotApi, workspace))
	request.Headers[authorization] = BearerToken(credentials)
	request.Headers[contentType] = applicationJson
	request.Body = strings.NewReader(body)
	response := client.Post(request)
	if response.Status != 200 {
		return nil, fmt.Errorf("%d: %s", response.Status, response.Body)
	}
	reply := make(Token)
	err = json.Unmarshal(response.Body, &reply)
	if err != nil {
		return nil, err
	}
	return reply, nil
}
