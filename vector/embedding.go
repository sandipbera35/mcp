package vector

import (
	"hash/fnv"
	"math"
	"regexp"
	"strings"
)

var tokenRegex = regexp.MustCompile(`[a-zA-Z0-9]+`)

func EmbedText(text string, dimension int) []float32 {
	if dimension <= 0 {
		dimension = 384
	}

	vector := make([]float32, dimension)
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return vector
	}

	for _, token := range tokens {
		index := hashToken(token, dimension)
		vector[index] += 1
	}

	var norm float64
	for _, value := range vector {
		norm += float64(value * value)
	}
	if norm == 0 {
		return vector
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range vector {
		vector[i] *= scale
	}
	return vector
}

func tokenize(value string) []string {
	return tokenRegex.FindAllString(strings.ToLower(value), -1)
}

func hashToken(token string, dimension int) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(token))
	return int(hasher.Sum32() % uint32(dimension))
}
