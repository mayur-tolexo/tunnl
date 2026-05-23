package relay

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

var adjectives = []string{
	"happy", "brave", "calm", "clever", "eager", "fancy", "gentle", "jolly",
	"keen", "lively", "merry", "nimble", "proud", "quiet", "rapid", "shiny",
	"swift", "tidy", "witty", "zesty",
}

var nouns = []string{
	"fox", "otter", "panda", "hawk", "lynx", "moose", "newt", "owl",
	"quail", "raven", "seal", "tiger", "viper", "wolf", "yak", "zebra",
	"bison", "crane", "dingo", "egret",
}

// GenerateSubdomain returns a random, URL-safe slug like "happy-fox-0042".
func GenerateSubdomain() (string, error) {
	adj, err := pick(adjectives)
	if err != nil {
		return "", err
	}
	noun, err := pick(nouns)
	if err != nil {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%04d", adj, noun, n.Int64()), nil
}

func pick(list []string) (string, error) {
	idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return "", err
	}
	return list[idx.Int64()], nil
}
