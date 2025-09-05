package operations

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/settings"
)

const (
	httpsPrefix    = "https:/"
	githubPrefix   = "github.com"
	robocorpPrefix = "robocorp"
	archiveSuffix  = "archive"
	zipFormat      = "%s.zip"
)

var (
	urlPattern = regexp.MustCompile("^https?://")
)

func CommunityLocation(name, branch string) string {
	if urlPattern.MatchString(name) {
		return name
	}
	parts := strings.SplitN(name, "/", -1)
	size := len(parts)
	if size > 3 {
		return name
	}
	result := []string{httpsPrefix}
	if size < 3 {
		result = append(result, githubPrefix)
	}
	if size < 2 {
		result = append(result, robocorpPrefix)
	}
	result = append(result, parts...)
	result = append(result, archiveSuffix, fmt.Sprintf(zipFormat, branch))
	return strings.Join(result, "/")
}

func DownloadCommunityRobot(url, filename string) error {
	client := &http.Client{Transport: settings.Global.ConfiguredHttpTransport()}
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || 299 < response.StatusCode {
		return fmt.Errorf("%s (%s)", response.Status, url)
	}

	out, err := pathlib.Create(filename)
	if err != nil {
		return err
	}
	defer out.Close()

	digest := sha256.New()
	many := io.MultiWriter(out, digest)

	common.Debug("Downloading %s <%s> -> %s", url, response.Status, filename)

	_, err = io.Copy(many, response.Body)
	if err != nil {
		return err
	}

	if common.DebugFlag() {
		sum := fmt.Sprintf("%02x", digest.Sum(nil))
		common.Debug("SHA256 sum: %s", sum)
	}

	return nil
}
