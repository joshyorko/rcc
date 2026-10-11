package common

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"math"
	"math/rand"
	"time"

	"github.com/dchest/siphash"
)

func Entropy(input []byte) float64 {
	size := len(input)
	if size == 0 {
		return 0.0
	}
	frequencies := make([]int, 256)
	for _, value := range input {
		frequencies[int(value)] += 1
	}
	total := float64(size)
	entropy := 0.0
	for _, count := range frequencies {
		if count == 0 {
			continue
		}
		weight := float64(count) / total
		entropy += (-weight * math.Log2(weight))
	}
	return entropy / 8.0
}

func Hexdigest(raw []byte) string {
	return fmt.Sprintf("%02x", raw)
}

func NewDigester(legacy bool) hash.Hash {
	if legacy {
		return sha256.New()
	} else {
		return siphash.New128([]byte("Very_Random-Seed"))
	}
}

func ShortDigest(content string) string {
	digester := NewDigester(true)
	digester.Write([]byte(content))
	result := Hexdigest(digester.Sum(nil))
	return result[:16]
}

func Digest(content string) string {
	digester := NewDigester(true)
	digester.Write([]byte(content))
	return Hexdigest(digester.Sum(nil))
}

func Siphash(left, right uint64, body []byte) uint64 {
	return siphash.Hash(left, right, body)
}

func DayCountSince(timestamp time.Time) int {
	duration := time.Since(timestamp)
	days := math.Floor(duration.Hours() / 24.0)
	return int(days)
}

func OneOutOf(limit uint8) bool {
	if limit > 1 {
		return rand.Intn(int(limit)) == 0
	}
	return true
}

func BlueprintHash(blueprint []byte) string {
	return Textual(Sipit(blueprint), 0)
}

func Sipit(key []byte) uint64 {
	return Siphash(9007199254740993, 2147483647, key)
}

func Textual(key uint64, size int) string {
	text := fmt.Sprintf("%016x", key)
	if size > 0 {
		return text[:size]
	}
	return text
}

func Gcd(left, right int64) int64 {
	for left != 0 {
		left, right = right%left, left
	}
	if right == 0 {
		return 1
	}
	return right
}
