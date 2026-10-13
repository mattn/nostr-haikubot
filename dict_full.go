//go:build !slimdict

package main

import (
	"github.com/ikawaha/kagome-dict-ipa-neologd"
	"github.com/ikawaha/kagome-dict/dict"
)

// systemDict returns the dictionary embedded in kagome-dict-ipa-neologd.
// Build with -tags slimdict to embed the slimmed-down dictionary instead,
// which needs about 400MB less memory. See cmd/slimdict. The Docker image is
// built that way.
func systemDict() *dict.Dict {
	return ipaneologd.Dict()
}
