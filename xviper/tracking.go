package xviper

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/robocorp/rcc/common"
)

const (
	trackingIdentityKey = `tracking.identity`
	trackingConsentKey  = `tracking.consent`
)

var (
	guidSteps = []int{4, 2, 2, 2, 6}
)

func init() {
	rand.Seed(common.When)
}

func AsGuid(content []byte) string {
	result := make([]string, 0, len(guidSteps))
	for _, step := range guidSteps {
		result = append(result, fmt.Sprintf("%02x", content[:step]))
		content = content[step:]
	}
	return strings.Join(result, "-")
}

func generateRandomIdentity() string {
	now := time.Now()
	digester := sha256.New()
	content := fmt.Sprintf("ID: %v/%v/%v", now.Format(time.RFC3339Nano), rand.Uint64(), rand.Uint64())
	digester.Write([]byte(content))
	return AsGuid(digester.Sum(nil))
}

func TrackingIdentity() string {
	identity := GetString(trackingIdentityKey)
	if len(identity) == 0 {
		identity = generateRandomIdentity()
		Set(trackingIdentityKey, identity)
		ConsentTracking(true)
	}
	return identity
}

func ConsentTracking(state bool) {
	Set(trackingConsentKey, state)
}

func CanTrack() bool {
	return GetBool(trackingConsentKey) && !common.WarrantyVoided()
}
