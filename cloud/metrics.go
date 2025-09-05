package cloud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/settings"
	"github.com/robocorp/rcc/xviper"
)

var (
	telemetryBarrier = sync.WaitGroup{}
)

const (
	trackingUrl     = `/metric-v1/%v/%v/%v/%v/%v`
	batchUrl        = `/metric-v1/batch`
	contentType     = `content-type`
	applicationJson = `application/json`
)

type (
	batchStatus struct {
		File    string `json:"file"`
		Host    string `json:"host"`
		Code    int    `json:"code"`
		Warning string `json:"warning"`
		Content string `json:"content"`
	}
)

func sendMetric(metricsHost, kind, name, value string) {
	common.Timeline("%s:%s = %s", kind, name, value)
	defer func() {
		status := recover()
		if status != nil {
			common.Debug("Telemetry panic recovered: %v", status)
		}
		telemetryBarrier.Done()
	}()
	client, err := NewClient(metricsHost)
	if err != nil {
		common.Debug("WARNING: %v (not critical)", err)
		return
	}
	timeout := 5 * time.Second
	client = client.Uncritical().WithTimeout(timeout)
	timestamp := time.Now().UnixNano()
	url := fmt.Sprintf(trackingUrl, url.PathEscape(kind), timestamp, url.PathEscape(xviper.TrackingIdentity()), url.PathEscape(name), url.PathEscape(value))
	common.Debug("Sending metric (timeout %v) as %v%v", timeout, metricsHost, url)
	client.Put(client.NewRequest(url))
}

func BackgroundMetric(kind, name, value string) {
	if common.WarrantyVoided() {
		return
	}
	metricsHost := settings.Global.TelemetryURL()
	if len(metricsHost) == 0 {
		return
	}
	common.Debug("BackgroundMetric kind:%v name:%v value:%v send:%v", kind, name, value, xviper.CanTrack())
	if xviper.CanTrack() {
		telemetryBarrier.Add(1)
		go sendMetric(metricsHost, kind, name, value)
		runtime.Gosched()
	}
}

func InternalBackgroundMetric(kind, name, value string) {
	if common.Product.AllowInternalMetrics() {
		BackgroundMetric(kind, name, value)
	}
}

func stdoutDump(origin error, message any) (err error) {
	defer fail.Around(&err)

	body, failure := json.MarshalIndent(message, "", "  ")
	fail.Fast(failure)

	os.Stdout.Write(append(body, '\n'))

	return origin
}

func BatchMetric(filename string) error {
	status := &batchStatus{
		File: filename,
		Code: 999,
	}

	metricsHost := settings.Global.TelemetryURL()
	if len(metricsHost) < 8 {
		status.Warning = "No metrics host."
		return stdoutDump(nil, status)
	}

	blob, err := os.ReadFile(filename)
	if err != nil {
		status.Code = 998
		status.Warning = err.Error()
		return stdoutDump(err, status)
	}

	status.Host = metricsHost

	client, err := NewClient(metricsHost)
	if err != nil {
		status.Code = 997
		status.Warning = err.Error()
		return stdoutDump(err, status)
	}

	timeout := 10 * time.Second
	client = client.Uncritical().WithTimeout(timeout)
	request := client.NewRequest(batchUrl)
	request.Headers[contentType] = applicationJson
	request.Body = bytes.NewBuffer([]byte(blob))
	response := client.Put(request)
	switch {
	case response == nil:
		status.Code = 996
		status.Warning = "Response was <nil>"
	case response != nil && response.Status == 202 && response.Err == nil:
		status.Code = response.Status
		status.Warning = "ok"
		status.Content = string(response.Body)
	case response != nil && response.Err != nil:
		status.Code = response.Status
		status.Warning = fmt.Sprintf("Failed PUT to %s%s, reason: %v", metricsHost, batchUrl, response.Err)
		status.Content = string(response.Body)
	case response != nil && response.Status != 202:
		status.Code = response.Status
		status.Warning = fmt.Sprintf("Failed PUT to %s%s. See content for details.", metricsHost, batchUrl)
		status.Content = string(response.Body)
	default:
		status.Code = response.Status
		status.Warning = "N/A"
		status.Content = string(response.Body)
	}
	return stdoutDump(nil, status)
}

func WaitTelemetry() {
	defer common.Timeline("wait telemetry done")

	common.Debug("wait telemetry to complete")
	runtime.Gosched()
	telemetryBarrier.Wait()
	common.Debug("telemetry sending completed")
}
