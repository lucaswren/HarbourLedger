package main

import (
	"regexp"
	"strings"
)

func profileNames(value string) []string {
	out := []string{}
	for _, name := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == '；' || r == '\n' || r == '\r' }) {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func companyNames(cfg Record) []string {
	return append(profileNames(str(cfg, "company")), profileNames(str(cfg, "company_aliases"))...)
}

// Match a configured name with token boundaries; banks may omit spaces.
func descriptionHasName(description, name string) bool {
	words := strings.Fields(strings.ToUpper(name))
	if len(words) == 0 || len(companyIdentity(name)) < 4 {
		return false
	}
	for i := range words {
		words[i] = regexp.QuoteMeta(words[i])
	}
	return rx(`(?:^|[^\p{L}\p{N}])` + strings.Join(words, `\s*`) + `(?:$|[^\p{L}\p{N}])`).MatchString(strings.ToUpper(description))
}

func ownCompanyDescription(description string, cfg Record) bool {
	for _, name := range companyNames(cfg) {
		if descriptionHasName(description, name) {
			return true
		}
	}
	return false
}
