// Package public holds the pages an OAuth consent screen links to, which the hub serves
// under /public/ without a credential (ADR 0040).
package public

import "embed"

// Files is every page and its stylesheet. The patterns are explicit so that nothing else in
// the directory, this file included, is ever served.
//
//go:embed *.html *.css
var Files embed.FS
