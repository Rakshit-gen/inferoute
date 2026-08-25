// Package site embeds the documentation page so inferouted can serve it
// directly at /docs — docs/site/index.html stays the single copy, readable
// standalone or served by the running binary.
package site

import _ "embed"

//go:embed index.html
var Index []byte
