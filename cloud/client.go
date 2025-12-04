package cloud

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
	"github.com/joshyorko/rcc/pretty"
	"github.com/joshyorko/rcc/set"
	"github.com/joshyorko/rcc/settings"
	"github.com/joshyorko/rcc/xviper"
)

// progressWriter wraps an io.Writer to track progress and update progress bar
type progressWriter struct {
	writer      io.Writer
	progressBar pretty.ProgressIndicator
	written     int64
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.writer.Write(p)
	pw.written += int64(n)
	if pw.progressBar != nil {
		pw.progressBar.Update(pw.written, "")
	}
	return n, err
}

type internalClient struct {
	endpoint string
	client   *http.Client
	tracing  bool
	critical bool
}

type Request struct {
	Url              string
	Headers          map[string]string
	TransferEncoding string
	ContentLength    int64
	Body             io.Reader
	Stream           io.Writer
}

type Response struct {
	Status  int
	Err     error
	Body    []byte
	Elapsed common.Duration
}

type Client interface {
	Endpoint() string
	NewRequest(string) *Request
	Head(request *Request) *Response
	Get(request *Request) *Response
	Post(request *Request) *Response
	Put(request *Request) *Response
	Delete(request *Request) *Response
	NewClient(endpoint string) (Client, error)
	WithTimeout(time.Duration) Client
	WithTracing() Client
	Uncritical() Client
}

func EnsureHttps(endpoint string) (string, error) {
	nice := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	parsed, err := url.Parse(nice)
	if err != nil {
		return "", err
	}
	if parsed.Host == "127.0.0.1" || strings.HasPrefix(parsed.Host, "127.0.0.1:") {
		return nice, nil
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("Endpoint '%s' must start with https:// prefix.", nice)
	}
	return nice, nil
}

func NewUnsafeClient(endpoint string) (Client, error) {
	return &internalClient{
		endpoint: endpoint,
		client:   &http.Client{Transport: settings.Global.ConfiguredHttpTransport()},
		tracing:  false,
		critical: true,
	}, nil
}

func NewClient(endpoint string) (Client, error) {
	https, err := EnsureHttps(endpoint)
	if err != nil {
		return nil, err
	}
	return &internalClient{
		endpoint: https,
		client:   &http.Client{Transport: settings.Global.ConfiguredHttpTransport()},
		tracing:  false,
		critical: true,
	}, nil
}

func (it *internalClient) Uncritical() Client {
	it.critical = false
	return it
}

func (it *internalClient) WithTimeout(timeout time.Duration) Client {
	return &internalClient{
		endpoint: it.endpoint,
		client: &http.Client{
			Transport: settings.Global.ConfiguredHttpTransport(),
			Timeout:   timeout,
		},
		tracing:  it.tracing,
		critical: it.critical,
	}
}

func (it *internalClient) WithTracing() Client {
	return &internalClient{
		endpoint: it.endpoint,
		client:   it.client,
		tracing:  true,
		critical: it.critical,
	}
}

func (it *internalClient) NewClient(endpoint string) (Client, error) {
	return NewClient(endpoint)
}

func (it *internalClient) Endpoint() string {
	return it.endpoint
}

func (it *internalClient) does(method string, request *Request) *Response {
	stopwatch := common.Stopwatch("stopwatch")
	response := new(Response)
	url := it.Endpoint() + request.Url
	common.Trace("Doing %s %s", method, url)
	defer func() {
		response.Elapsed = stopwatch.Elapsed()
		common.Trace("%s %s took %s", method, url, response.Elapsed)
	}()
	httpRequest, err := http.NewRequest(method, url, request.Body)
	if err != nil {
		response.Status = 9001
		response.Err = err
		return response
	}
	if request.ContentLength > 0 {
		httpRequest.ContentLength = request.ContentLength
	}
	if len(request.TransferEncoding) > 0 {
		httpRequest.TransferEncoding = []string{request.TransferEncoding}
	}
	// Only send installation identifier if tracking is allowed
	if xviper.CanTrack() {
		httpRequest.Header.Add("robocorp-installation-id", xviper.TrackingIdentity())
	}
	httpRequest.Header.Add("User-Agent", common.UserAgent())
	for name, value := range request.Headers {
		httpRequest.Header.Add(name, value)
	}
	httpResponse, err := it.client.Do(httpRequest)
	if err != nil {
		if it.critical {
			common.Error("http.Do", err)
		} else {
			common.Uncritical("http.Do", err)
		}
		response.Status = 9002
		response.Err = err
		return response
	}
	defer httpResponse.Body.Close()
	if it.tracing {
		common.Trace("Response %d headers:", httpResponse.StatusCode)
		keys := set.Keys(httpResponse.Header)
		for _, key := range keys {
			common.Trace("> %s: %q", key, httpResponse.Header[key])
		}
	}
	response.Status = httpResponse.StatusCode
	if request.Stream != nil {
		io.Copy(request.Stream, httpResponse.Body)
	} else {
		response.Body, response.Err = io.ReadAll(httpResponse.Body)
	}
	if common.DebugFlag() {
		body := "ignore"
		if response.Status > 399 {
			body = string(response.Body)
		}
		common.Debug("%v %v %v => %v (%v)", <-common.Identities, method, url, response.Status, body)
	}
	return response
}

func (it *internalClient) NewRequest(url string) *Request {
	return &Request{
		Url:     url,
		Headers: make(map[string]string),
	}
}

func (it *internalClient) Head(request *Request) *Response {
	return it.does("HEAD", request)
}

func (it *internalClient) Get(request *Request) *Response {
	return it.does("GET", request)
}

func (it *internalClient) Post(request *Request) *Response {
	return it.does("POST", request)
}

func (it *internalClient) Put(request *Request) *Response {
	return it.does("PUT", request)
}

func (it *internalClient) Delete(request *Request) *Response {
	return it.does("DELETE", request)
}

func Download(url, filename string) error {
	common.Timeline("start %s download", filename)
	defer common.Timeline("done %s download", filename)

	if pathlib.Exists(filename) {
		err := os.Remove(filename)
		if err != nil {
			return err
		}
	}

	client := &http.Client{Transport: settings.Global.ConfiguredHttpTransport()}
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	request.Header.Add("Accept", "application/octet-stream")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Downloading %q failed, reason: %q!", url, response.Status)
	}

	out, err := pathlib.Create(filename)
	if err != nil {
		return err
	}
	defer out.Close()

	digest := sha256.New()

	common.Debug("Downloading %s <%s> -> %s", url, response.Status, filename)

	// Use dashboard for large downloads (> 1MB), progress bar for smaller ones
	contentLength := response.ContentLength
	useDashboard := contentLength > 1024*1024 && pretty.Interactive

	var dashboard pretty.Dashboard
	var progressBar pretty.ProgressIndicator

	if useDashboard {
		// Use DownloadDashboard for large files
		dashboard = pretty.NewDownloadDashboard(filename, contentLength)
		dashboard.Start()
		defer dashboard.Stop(true)
	} else if contentLength > 0 && pretty.Interactive {
		// Use simple progress bar for smaller files
		progressBar = pretty.NewProgressBar(fmt.Sprintf("Downloading %s", filename), contentLength)
		progressBar.Start()
		defer progressBar.Stop(true)
	}

	// Create progress-tracking writer
	pw := &progressWriter{
		writer:      io.MultiWriter(out, digest),
		progressBar: progressBar,
		written:     0,
	}

	// For dashboard, we need to update it during the copy
	bytecount := int64(0)
	if useDashboard {
		// Manual copy loop to update dashboard
		buf := make([]byte, 32*1024) // 32KB buffer
		for {
			nr, er := response.Body.Read(buf)
			if nr > 0 {
				nw, ew := pw.writer.Write(buf[0:nr])
				if nw < 0 || nr < nw {
					nw = 0
					if ew == nil {
						ew = fmt.Errorf("invalid write result")
					}
				}
				bytecount += int64(nw)
				if ew != nil {
					err = ew
					break
				}
				if nr != nw {
					err = io.ErrShortWrite
					break
				}

				// Update dashboard with progress
				dashboard.Update(pretty.DashboardState{
					Progress: float64(bytecount) / float64(contentLength),
				})
			}
			if er != nil {
				if er != io.EOF {
					err = er
				}
				break
			}
		}
		if err != nil {
			dashboard.Stop(false)
			return err
		}
	} else {
		// Use standard io.Copy for progress bar or no progress
		bytecount, err = io.Copy(pw, response.Body)
		if err != nil {
			if progressBar != nil {
				progressBar.Stop(false)
			}
			return err
		}
	}

	common.Timeline("downloaded %d bytes to %s", bytecount, filename)

	err = out.Sync()
	if err != nil {
		return err
	}

	return common.Debug("%q SHA256 sum: %02x", filename, digest.Sum(nil))
}
