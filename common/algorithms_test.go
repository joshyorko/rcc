package common_test

import (
	"testing"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/hamlet"
)

func between(l, m, r float64) bool {
	return l < m && m < r
}

func TestCanCallEntropyFunction(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal(0.0, common.Entropy([]byte("")))
	must_be.Equal(0.0, common.Entropy([]byte(" ")))
	must_be.Equal(0.0, common.Entropy([]byte("a")))
	must_be.Equal(0.0, common.Entropy([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaa")))
	must_be.Equal(0.125, common.Entropy([]byte("ab")))
	must_be.Equal(0.125, common.Entropy([]byte("abababab")))
	must_be.True(between(0.5, common.Entropy([]byte("abcdefghijklmnopqrstuvwxyz")), 0.6))
	must_be.True(between(0.43, common.Entropy([]byte("edf3419283feac3d4f8bb34aa9")), 0.44))
}

func TestCanCalculateGcd(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal(int64(5), common.Gcd(15, 20))
	must_be.Equal(int64(1), common.Gcd(3, 5))
	must_be.Equal(int64(5), common.Gcd(5, 5))
	must_be.Equal(int64(5), common.Gcd(0, 5))
	must_be.Equal(int64(5), common.Gcd(5, 0))
	must_be.Equal(int64(1), common.Gcd(0, 0))
}
