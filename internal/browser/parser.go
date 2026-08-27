package browser

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// DirEntry represents a single item in a directory index.
type DirEntry struct {
	Name     string `json:"name"`
	Href     string `json:"href"`
	IsDir    bool   `json:"is_dir"`
	IsParent bool   `json:"is_parent"`
	Size     string `json:"size"`
	ModTime  string `json:"mod_time"`
	IconType string `json:"icon_type"`
}

// ParsedListing represents a parsed directory listing.
type ParsedListing struct {
	Title       string     `json:"title"`
	CurrentPath string     `json:"current_path"`
	Entries     []DirEntry `json:"entries"`
}

var (
	reIndexOf = regexp.MustCompile(`(?i)Index of\s+([^\r\n<]+)`)
	rePreLine = regexp.MustCompile(`<a\s+[^>]*href="([^"]+)"[^>]*>([^<]+)</a>(.*)`)
	reNginxDT = regexp.MustCompile(`(\d{2}-[a-zA-Z]{3}-\d{4}\s+\d{2}:\d{2})\s+([0-9\.\-KMGTPkmgtp]+)`)
	reIsoDT   = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}(?::\d{2})?)\s+([0-9\.\-KMGTPkmgtp]+)`)
)

// IsDirectoryIndex checks whether the HTML response appears to be a directory listing.
func IsDirectoryIndex(body []byte) bool {
	lower := bytes.ToLower(body)
	if bytes.Contains(lower, []byte("index of ")) ||
		bytes.Contains(lower, []byte("<table id=\"indexlist\"")) ||
		bytes.Contains(lower, []byte("[parentdir]")) ||
		bytes.Contains(lower, []byte("alt=\"[dir]\"")) ||
		bytes.Contains(lower, []byte("<title>index of")) {
		return true
	}
	// Also check if body has <pre> with multiple <a href="..."> and date-like patterns
	if bytes.Contains(lower, []byte("<pre>")) && bytes.Contains(lower, []byte("<a href=\"../\"")) {
		return true
	}
	return false
}

// ParseDirectoryIndex attempts to parse an upstream HTML directory listing.
func ParseDirectoryIndex(body []byte, requestPath string) (*ParsedListing, bool) {
	if !IsDirectoryIndex(body) {
		return nil, false
	}

	listing := &ParsedListing{
		CurrentPath: requestPath,
		Entries:     make([]DirEntry, 0),
	}

	// Try extracting title
	if m := reIndexOf.FindSubmatch(body); len(m) > 1 {
		listing.Title = strings.TrimSpace(string(m[1]))
	}
	if listing.Title == "" {
		listing.Title = requestPath
	}

	// Try parsing via table structure first
	tableEntries := parseTableListing(body)
	if len(tableEntries) > 0 {
		listing.Entries = tableEntries
		return listing, true
	}

	// Try parsing via <pre> structure (Apache / Nginx autoindex)
	preEntries := parsePreListing(body)
	if len(preEntries) > 0 {
		listing.Entries = preEntries
		return listing, true
	}

	return nil, false
}

func parseTableListing(body []byte) []DirEntry {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil
	}

	var entries []DirEntry
	var findRows func(*html.Node)
	findRows = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			if entry, ok := parseTableRow(n); ok {
				entries = append(entries, entry)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findRows(c)
		}
	}
	findRows(doc)
	return entries
}

func parseTableRow(tr *html.Node) (DirEntry, bool) {
	var cells []string
	var linkHref, linkText string

	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			// Extract link if present
			if linkHref == "" {
				var findA func(*html.Node)
				findA = func(aNode *html.Node) {
					if aNode.Type == html.ElementNode && aNode.Data == "a" {
						for _, attr := range aNode.Attr {
							if attr.Key == "href" {
								linkHref = attr.Val
							}
						}
						linkText = nodeText(aNode)
					}
					for ch := aNode.FirstChild; ch != nil; ch = ch.NextSibling {
						findA(ch)
					}
				}
				findA(c)
			}
			cells = append(cells, strings.TrimSpace(nodeText(c)))
		}
	}

	if linkHref == "" || isSortLink(linkHref) || !safeListingHref(linkHref) {
		return DirEntry{}, false
	}

	isParent := linkHref == "../" || linkHref == "/" || strings.EqualFold(linkText, "Parent Directory") || strings.EqualFold(linkText, "..")
	isDir := isParent || strings.HasSuffix(linkHref, "/") || strings.HasSuffix(linkText, "/")

	name := linkText
	if name == "" {
		name = linkHref
	}

	var modTime, size string
	for _, cell := range cells {
		cellTrimmed := strings.TrimSpace(cell)
		if cellTrimmed == "" || cellTrimmed == name || cellTrimmed == "-" {
			continue
		}
		if looksLikeDate(cellTrimmed) {
			modTime = cellTrimmed
		} else if looksLikeSize(cellTrimmed) {
			size = cellTrimmed
		}
	}

	return DirEntry{
		Name:     name,
		Href:     linkHref,
		IsDir:    isDir,
		IsParent: isParent,
		ModTime:  modTime,
		Size:     size,
		IconType: DetectIconType(name, isDir),
	}, true
}

func parsePreListing(body []byte) []DirEntry {
	text := string(body)
	preStart := strings.Index(text, "<pre>")
	if preStart == -1 {
		preStart = strings.Index(text, "<PRE>")
	}
	if preStart == -1 {
		return nil
	}
	preEnd := strings.Index(text[preStart:], "</pre>")
	if preEnd == -1 {
		preEnd = strings.Index(text[preStart:], "</PRE>")
	}
	if preEnd == -1 {
		preEnd = len(text) - preStart
	}
	preContent := text[preStart+5 : preStart+preEnd]

	lines := strings.Split(preContent, "\n")
	var entries []DirEntry

	for _, line := range lines {
		matches := rePreLine.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}
		href := matches[1]
		name := strings.TrimSpace(matches[2])
		rest := strings.TrimSpace(matches[3])

		if isSortLink(href) || name == "" || !safeListingHref(href) {
			continue
		}

		isParent := href == "../" || href == "/" || strings.EqualFold(name, "Parent Directory") || strings.EqualFold(name, "..") || strings.EqualFold(name, "../")
		isDir := isParent || strings.HasSuffix(href, "/") || strings.HasSuffix(name, "/")

		var modTime, size string
		if dtMatch := reNginxDT.FindStringSubmatch(rest); len(dtMatch) > 2 {
			modTime = strings.TrimSpace(dtMatch[1])
			size = strings.TrimSpace(dtMatch[2])
		} else if isoMatch := reIsoDT.FindStringSubmatch(rest); len(isoMatch) > 2 {
			modTime = strings.TrimSpace(isoMatch[1])
			size = strings.TrimSpace(isoMatch[2])
		} else {
			fields := strings.Fields(rest)
			if len(fields) >= 2 {
				modTime = fields[0] + " " + fields[1]
				if len(fields) >= 3 {
					size = fields[2]
				}
			}
		}

		if size == "-" {
			size = ""
		}

		entries = append(entries, DirEntry{
			Name:     name,
			Href:     href,
			IsDir:    isDir,
			IsParent: isParent,
			ModTime:  modTime,
			Size:     size,
			IconType: DetectIconType(name, isDir),
		})
	}
	return entries
}

func isSortLink(href string) bool {
	return strings.HasPrefix(href, "?") || strings.HasPrefix(href, "#")
}

// safeListingHref accepts only bounded same-origin path references. Directory
// listings are controlled by the upstream, so absolute, scheme-relative and
// executable URLs must never be copied into a MirrorRelay-generated page.
func safeListingHref(href string) bool {
	if href == "" || len(href) > 8192 || strings.TrimSpace(href) != href || strings.ContainsAny(href, "\\\x00\r\n\t") {
		return false
	}
	parsed, err := url.Parse(href)
	return err == nil && !parsed.IsAbs() && parsed.Scheme == "" && parsed.Host == "" && parsed.User == nil && parsed.Opaque == "" && parsed.Fragment == "" && parsed.Path != ""
}

func looksLikeDate(s string) bool {
	return strings.Contains(s, "-") && (strings.Contains(s, ":") || strings.Contains(s, "20"))
}

func looksLikeSize(s string) bool {
	if s == "-" {
		return false
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == 'K' || r == 'M' || r == 'G' || r == 'T' || r == 'B' || r == 'k' || r == 'm' || r == 'g' {
			continue
		}
		return false
	}
	return len(s) > 0
}

func nodeText(n *html.Node) string {
	var buf bytes.Buffer
	var extract func(*html.Node)
	extract = func(node *html.Node) {
		if node.Type == html.TextNode {
			buf.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)
	return buf.String()
}
