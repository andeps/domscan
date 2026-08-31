// Package domain generates and validates domain-name candidates.
package domain

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const maxCandidateLimit = 1000

type Options struct {
	Keywords  []string `json:"keywords"`
	TLDs      []string `json:"tlds"`
	MinLength int      `json:"minLength"`
	MaxLength int      `json:"maxLength"`
	DigitMode string   `json:"digitMode"`
	Hyphen    bool     `json:"hyphen"`
	FuzzyMode string   `json:"fuzzyMode"` // none, prefix, suffix, both
	Limit     int      `json:"limit"`
	Offset    int      `json:"-"`
}

func Generate(opts Options) ([]string, error) {
	if opts.MinLength < 1 || opts.MaxLength > 63 || opts.MinLength > opts.MaxLength {
		return nil, fmt.Errorf("主体长度必须在 1 到 63 之间，且最小值不能大于最大值")
	}
	if opts.DigitMode != "forbid" && opts.DigitMode != "allow" && opts.DigitMode != "require" {
		return nil, fmt.Errorf("数字规则无效")
	}
	if opts.FuzzyMode == "" {
		opts.FuzzyMode = "none"
	}
	if opts.FuzzyMode != "none" && opts.FuzzyMode != "prefix" && opts.FuzzyMode != "suffix" && opts.FuzzyMode != "both" {
		return nil, fmt.Errorf("模糊组合方向无效")
	}
	if opts.Offset < 0 {
		return nil, fmt.Errorf("候选偏移量不能为负数")
	}
	if opts.Limit <= 0 {
		opts.Limit = 200
	}
	if opts.Limit > maxCandidateLimit {
		opts.Limit = maxCandidateLimit
	}
	keywords, err := normalizeKeywords(opts.Keywords)
	if err != nil {
		return nil, err
	}
	tlds, err := normalizeTLDs(opts.TLDs)
	if err != nil {
		return nil, err
	}
	if len(keywords) == 0 {
		if opts.FuzzyMode != "none" {
			return nil, fmt.Errorf("模糊组合规则需要至少一个关键词")
		}
		return generateAutomaticDomains(opts, tlds), nil
	}
	if opts.FuzzyMode != "none" {
		return generateFuzzyDomains(opts, keywords, tlds), nil
	}

	bases := buildBases(keywords, opts.Hyphen)
	labels := make([]string, 0, len(bases)*5)
	seen := make(map[string]struct{})
	add := func(label string) {
		if len(label) < opts.MinLength || len(label) > opts.MaxLength || !validLabel(label) {
			return
		}
		hasDigit := strings.IndexFunc(label, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
		if (opts.DigitMode == "forbid" && hasDigit) || (opts.DigitMode == "require" && !hasDigit) {
			return
		}
		if _, exists := seen[label]; exists {
			return
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	if opts.DigitMode != "require" {
		for _, base := range bases {
			add(base)
		}
	}
	// Walk numbers first so a low result limit still includes every keyword base.
	if opts.DigitMode != "forbid" {
		for n := 0; n <= 99; n++ {
			num := strconv.Itoa(n)
			for _, base := range bases {
				add(base + num)
				add(num + base)
			}
		}
	}
	result := make([]string, 0, min(opts.Limit, len(labels)*len(tlds)))
	skipped := 0
	for _, label := range labels {
		for _, tld := range tlds {
			if skipped < opts.Offset {
				skipped++
				continue
			}
			result = append(result, label+"."+tld)
			if len(result) == opts.Limit {
				return result, nil
			}
		}
	}
	return result, nil
}

func generateFuzzyDomains(opts Options, keywords, tlds []string) []string {
	alphabet := "abcdefghijklmnopqrstuvwxyz"
	if opts.DigitMode != "forbid" {
		alphabet += "0123456789"
	}
	labels := make([]string, 0, opts.Offset+opts.Limit)
	seen := make(map[string]struct{})
	maxLabels := opts.Offset + opts.Limit
	add := func(label string) bool {
		if len(label) < opts.MinLength || len(label) > opts.MaxLength || !validLabel(label) {
			return true
		}
		hasDigit := strings.IndexFunc(label, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
		if (opts.DigitMode == "forbid" && hasDigit) || (opts.DigitMode == "require" && !hasDigit) {
			return true
		}
		if _, exists := seen[label]; exists {
			return true
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
		return len(labels) < maxLabels
	}
	for targetLength := opts.MinLength; targetLength <= opts.MaxLength && len(labels) < maxLabels; targetLength++ {
		for _, keyword := range keywords {
			fillLength := targetLength - len(keyword)
			if fillLength < 0 {
				continue
			}
			requireDigit := opts.DigitMode == "require" && !containsDigit(keyword)
			continueGeneration := func(filler string) bool {
				if opts.FuzzyMode == "prefix" {
					return add(filler + keyword)
				}
				return add(keyword + filler)
			}
			switch opts.FuzzyMode {
			case "prefix", "suffix":
				walkFillers(fillLength, alphabet, requireDigit, continueGeneration)
			case "both":
				for split := 0; split <= fillLength && len(labels) < maxLabels; split++ {
					leftLength, rightLength := split, fillLength-split
					leftRequiresDigit := requireDigit && leftLength > 0
					walkFillers(leftLength, alphabet, leftRequiresDigit, func(left string) bool {
						return walkFillers(rightLength, alphabet, false, func(right string) bool {
							return add(left + keyword + right)
						})
					})
				}
			}
		}
	}
	return domainsFromLabels(labels, tlds, opts.Offset, opts.Limit)
}

func walkFillers(length int, alphabet string, requireDigit bool, fn func(string) bool) bool {
	indexes := make([]int, length)
	for {
		if requireDigit && !indexesContainDigit(indexes) {
			if length == 0 {
				return true
			}
			indexes[length-1] = 26
		}
		chars := make([]byte, length)
		for i, index := range indexes {
			chars[i] = alphabet[index]
		}
		if !fn(string(chars)) {
			return false
		}
		if !incrementIndexes(indexes, len(alphabet)) {
			return true
		}
	}
}

func containsDigit(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
}

func domainsFromLabels(labels, tlds []string, offset, limit int) []string {
	result := make([]string, 0, limit)
	skipped := 0
	for _, label := range labels {
		for _, tld := range tlds {
			if skipped < offset {
				skipped++
				continue
			}
			result = append(result, label+"."+tld)
			if len(result) == limit {
				return result
			}
		}
	}
	return result
}

func normalizeKeywords(input []string) ([]string, error) {
	seen := make(map[string]struct{})
	var result []string
	for _, raw := range input {
		word := strings.ToLower(strings.TrimSpace(raw))
		if word == "" {
			continue
		}
		if !validLabel(word) {
			return nil, fmt.Errorf("关键词 %q 无效：仅支持英文字母、数字和中划线，且不能以中划线开头或结尾", raw)
		}
		if _, ok := seen[word]; !ok {
			seen[word] = struct{}{}
			result = append(result, word)
		}
	}
	return result, nil
}

func generateAutomaticDomains(opts Options, tlds []string) []string {
	alphabet := "abcdefghijklmnopqrstuvwxyz"
	if opts.DigitMode != "forbid" {
		alphabet += "0123456789"
	}
	result := make([]string, 0, opts.Limit)
	skipped := 0
	for length := opts.MinLength; length <= opts.MaxLength; length++ {
		indexes := make([]int, length)
		for {
			if opts.DigitMode == "require" && !indexesContainDigit(indexes) {
				// Jump directly over the alphabet-only range for this prefix.
				indexes[length-1] = 26
			}
			labelBytes := make([]byte, length)
			for i, index := range indexes {
				labelBytes[i] = alphabet[index]
			}
			label := string(labelBytes)
			for _, tld := range tlds {
				if skipped < opts.Offset {
					skipped++
					continue
				}
				result = append(result, label+"."+tld)
				if len(result) == opts.Limit {
					return result
				}
			}
			if !incrementIndexes(indexes, len(alphabet)) {
				break
			}
		}
	}
	return result
}

func indexesContainDigit(indexes []int) bool {
	for _, index := range indexes {
		if index >= 26 {
			return true
		}
	}
	return false
}

func incrementIndexes(indexes []int, base int) bool {
	for position := len(indexes) - 1; position >= 0; position-- {
		indexes[position]++
		if indexes[position] < base {
			return true
		}
		indexes[position] = 0
	}
	return false
}

func normalizeTLDs(input []string) ([]string, error) {
	seen := make(map[string]struct{})
	var result []string
	for _, raw := range input {
		tld := strings.ToLower(strings.Trim(strings.TrimSpace(raw), "."))
		if tld == "" {
			continue
		}
		if !validLabel(tld) {
			return nil, fmt.Errorf("后缀 %q 无效", raw)
		}
		if _, ok := seen[tld]; !ok {
			seen[tld] = struct{}{}
			result = append(result, tld)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("请至少输入一个域名后缀")
	}
	return result, nil
}

func buildBases(keywords []string, hyphen bool) []string {
	seen := make(map[string]struct{})
	var result []string
	add := func(value string) {
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	for _, word := range keywords {
		add(word)
	}
	for i, left := range keywords {
		for j, right := range keywords {
			if i == j {
				continue
			}
			add(left + right)
			if hyphen {
				add(left + "-" + right)
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if len(result[i]) == len(result[j]) {
			return result[i] < result[j]
		}
		return len(result[i]) < len(result[j])
	})
	return result
}

func validLabel(value string) bool {
	if len(value) < 1 || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, ch := range value {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
			return false
		}
	}
	return true
}
