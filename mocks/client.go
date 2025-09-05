package mocks

import (
	"testing"
	"time"

	"github.com/robocorp/rcc/cloud"
)

type MockClient struct {
	Requests  []*cloud.Request
	Responses []*cloud.Response
	Types     []string
}

func NewClient(responses ...*cloud.Response) *MockClient {
	return &MockClient{
		Requests:  make([]*cloud.Request, 0),
		Responses: responses,
		Types:     make([]string, 0),
	}
}

func (it *MockClient) Endpoint() string {
	return "https://this.is/mock"
}

func (it *MockClient) NewClient(endpoint string) (cloud.Client, error) {
	return it, nil
}

func (it *MockClient) Uncritical() cloud.Client {
	return it
}

func (it *MockClient) WithTimeout(time.Duration) cloud.Client {
	return it
}

func (it *MockClient) WithTracing() cloud.Client {
	return it
}

func (it *MockClient) NewRequest(url string) *cloud.Request {
	return &cloud.Request{
		Url:     url,
		Headers: make(map[string]string),
	}
}

func (it *MockClient) Verify(t *testing.T) {
	if len(it.Requests) != len(it.Responses) {
		t.Error("Request and response counts do not match.")
	}
}

func (it *MockClient) does(method string, request *cloud.Request) *cloud.Response {
	index := len(it.Requests)
	it.Requests = append(it.Requests, request)
	it.Types = append(it.Types, method)
	return it.Responses[index]
}

func (it *MockClient) Head(request *cloud.Request) *cloud.Response {
	return it.does("HEAD", request)
}

func (it *MockClient) Get(request *cloud.Request) *cloud.Response {
	return it.does("GET", request)
}

func (it *MockClient) Post(request *cloud.Request) *cloud.Response {
	return it.does("POST", request)
}

func (it *MockClient) Put(request *cloud.Request) *cloud.Response {
	return it.does("PUT", request)
}

func (it *MockClient) Delete(request *cloud.Request) *cloud.Response {
	return it.does("DELETE", request)
}
