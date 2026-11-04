package main

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// Internal data codes remain stable; only presentation text is localised.
//
//go:embed traditional_chars.json
var traditionalJSON []byte
var traditionalMap = func() map[string]string { m := map[string]string{}; _ = json.Unmarshal(traditionalJSON, &m); return m }()

func traditional(s string) string {
	s = strings.NewReplacer("采购", "採購", "采購", "採購", "期后", "期後", "后续", "後續", "之后", "之後", "以后", "以後", "后台", "後台", "复核", "覆核", "復核", "覆核", "复制", "複製", "復制", "複製", "合并", "合併", "周期", "週期", "云", "雲").Replace(s)
	return strings.Map(func(r rune) rune {
		if v := traditionalMap[string(r)]; v != "" {
			return []rune(v)[0]
		}
		return r
	}, s)
}
