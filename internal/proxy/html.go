package proxy

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/browser"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"golang.org/x/net/html"
)

const auxiliaryUpstreamPrefix = "/_mirrorrelay/upstream/"

const auxiliaryURLVersion = "v1"

var browsableURLAttributes = map[string]bool{
	"action": true, "background": true, "cite": true, "formaction": true,
	"href": true, "poster": true, "src": true,
}

type auxiliaryUpstreamRoute struct {
	repositoryID int64
	upstreamID   int64
	signature    []byte
	target       *url.URL
}

func parseAuxiliaryUpstreamRoute(value, rawQuery string) (auxiliaryUpstreamRoute, error) {
	remaining := strings.TrimPrefix(value, auxiliaryUpstreamPrefix)
	parts := strings.Split(remaining, "/")
	if len(parts) != 4 {
		return auxiliaryUpstreamRoute{}, errors.New("invalid upstream auxiliary resource route")
	}
	repositoryID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || repositoryID <= 0 {
		return auxiliaryUpstreamRoute{}, errors.New("invalid upstream auxiliary resource repository id")
	}
	upstreamID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || upstreamID <= 0 {
		return auxiliaryUpstreamRoute{}, errors.New("invalid upstream auxiliary resource upstream id")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != sha256.Size {
		return auxiliaryUpstreamRoute{}, errors.New("invalid upstream auxiliary resource signature")
	}
	escapedPath, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(escapedPath) == 0 || escapedPath[0] != '/' {
		return auxiliaryUpstreamRoute{}, errors.New("invalid upstream auxiliary resource target")
	}
	target, err := url.ParseRequestURI(string(escapedPath))
	if err != nil || target.IsAbs() || target.Host != "" || target.User != nil || target.Fragment != "" ||
		containsEncodedPathSeparator(target.EscapedPath()) || unsafeRepositoryPath(target.Path) {
		return auxiliaryUpstreamRoute{}, errors.New("invalid upstream auxiliary resource target")
	}
	target.RawQuery = rawQuery
	return auxiliaryUpstreamRoute{repositoryID: repositoryID, upstreamID: upstreamID, signature: signature, target: target}, nil
}

func auxiliaryURLSignature(key []byte, repository model.Mirror, upstream model.Upstream, target *url.URL) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(auxiliaryURLVersion + "\x00" + strconv.FormatInt(repository.ID, 10) + "\x00" +
		strconv.FormatInt(upstream.ID, 10) + "\x00" + strings.TrimSpace(upstream.URL) + "\x00" + strings.TrimSpace(upstream.Host) + "\x00" +
		strings.TrimSpace(repository.HostRewrite) + "\x00" + target.EscapedPath() + "\x00" + target.RawQuery))
	return mac.Sum(nil)
}

func verifyAuxiliaryURLSignature(key []byte, repository model.Mirror, upstream model.Upstream, route auxiliaryUpstreamRoute) bool {
	return len(key) >= 32 && hmac.Equal(route.signature, auxiliaryURLSignature(key, repository, upstream, route.target))
}

func (e *Engine) auxiliaryRouteAllowed(repository model.Mirror, authority string) bool {
	if hostRepository, _, found := e.registry.Route(authority, "/"); found && hostRepository.PublicMode == "host" {
		return hostRepository.ID == repository.ID
	}
	if repository.PublicMode == "host" {
		return false
	}
	if e.cfg.HTTP.PublicBaseURL == "" {
		return true
	}
	publicURL, err := url.Parse(e.cfg.HTTP.PublicBaseURL)
	return err == nil && strings.EqualFold(strings.TrimSuffix(publicURL.Hostname(), "."), requestHostname(authority))
}

func requestHostname(authority string) string {
	if host, _, err := net.SplitHostPort(authority); err == nil {
		authority = host
	}
	return strings.ToLower(strings.TrimSuffix(strings.Trim(authority, "[]"), "."))
}

func shouldRewriteHTMLBody(response *http.Response) bool {
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusPartialContent {
		return false
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(contentType, "text/html")
}

func rewriteHTMLResponseBody(response *http.Response, repository model.Mirror, upstream model.Upstream, pageURL *url.URL,
	cfg config.MetadataConfig, uiEnhancement model.UIEnhancementConfig, acceptsGzip bool, compressors *gzipPool,
	auxiliarySigningKey []byte) (metadataValidator, bool, error) {
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return metadataValidator{}, false, &unexpectedHTMLEncodingError{encoding: encoding}
	}
	limit := cfg.RewriteBufferLimit
	if response.ContentLength > limit {
		return metadataValidator{}, false, &htmlRewriteTooLargeError{limit: limit}
	}
	source, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return metadataValidator{}, false, err
	}
	if int64(len(source)) > limit {
		return metadataValidator{}, false, &htmlRewriteTooLargeError{limit: limit}
	}
	_ = response.Body.Close()

	var rewritten []byte
	var changed bool
	safeUI := pageURL != nil && pageURL.Query().Get("safe-ui") == "1"

	if uiEnhancement.Enabled && uiEnhancement.RepositoryBrowser.Enabled && !safeUI && browser.IsDirectoryIndex(source) {
		reqPath := ""
		if pageURL != nil {
			reqPath = pageURL.Path
		}
		if listing, ok := browser.ParseDirectoryIndex(source, reqPath); ok {
			rendered := browser.RenderHTML(listing, repository, reqPath, uiEnhancement.Branding, uiEnhancement.Theme, safeUI)
			rewritten = []byte(rendered)
			changed = true
		}
	}

	if !changed && repository.HTMLRewriteEnabled {
		rewritten, changed, err = rewriteHTMLDocument(source, repository, upstream, pageURL, auxiliarySigningKey)
		if err != nil {
			return metadataValidator{}, false, err
		}
	}

	if !changed {
		response.Body = io.NopCloser(bytes.NewReader(source))
		response.ContentLength = int64(len(source))
		response.Header.Set("Content-Length", strconv.FormatInt(response.ContentLength, 10))
		return metadataValidator{}, false, nil
	}

	output := rewritten
	encoding := "identity"
	compressionMayVary := cfg.OutputCompression == "auto" || cfg.OutputCompression == "gzip"
	if compressionMayVary && acceptsGzip && len(rewritten) >= cfg.GzipMinLength {
		output, err = compressors.compress(rewritten)
		if err != nil {
			return metadataValidator{}, false, err
		}
		encoding = "gzip"
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("mirrorrelay-browsable-html-v1\x00" + strconv.FormatInt(repository.ID, 10) + "\x00" +
		repository.PublicMode + "\x00" + repository.PublicPath + "\x00" + repository.StripPrefix + "\x00" + upstream.URL + "\x00" + encoding + "\x00"))
	_, _ = hash.Write(output)
	etag := `"mirrorrelay-html-v1-` + hex.EncodeToString(hash.Sum(nil)) + `"`

	response.Body = io.NopCloser(bytes.NewReader(output))
	response.ContentLength = int64(len(output))
	response.Header.Set("Content-Length", strconv.FormatInt(response.ContentLength, 10))
	response.Header.Set("ETag", etag)
	response.Header.Del("Content-Encoding")
	for _, name := range []string{"Content-Range", "Accept-Ranges", "Last-Modified", "Content-MD5", "Digest", "Content-Digest", "Repr-Digest"} {
		response.Header.Del(name)
	}
	if encoding == "gzip" {
		response.Header.Set("Content-Encoding", "gzip")
	}
	if compressionMayVary {
		addVary(response.Header, "Accept-Encoding")
	}
	return metadataValidator{ETag: etag}, true, nil
}

func rewriteHTMLDocument(source []byte, repository model.Mirror, upstream model.Upstream, pageURL *url.URL, auxiliarySigningKey []byte) ([]byte, bool, error) {
	repositoryBase, err := effectiveRepositoryBaseURL(repository, upstream)
	if err != nil || pageURL == nil || !sameOrigin(repositoryBase, pageURL) {
		return source, false, nil
	}

	resolverBase := cloneURL(pageURL)
	baseSeen := false
	changed := false
	var output bytes.Buffer
	tokenizer := html.NewTokenizer(bytes.NewReader(source))
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if err := tokenizer.Err(); err != nil && err != io.EOF {
				return nil, false, err
			}
			break
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			_, _ = output.Write(tokenizer.Raw())
			continue
		}

		token := tokenizer.Token()
		tokenChanged := false
		for index := range token.Attr {
			attribute := &token.Attr[index]
			name := strings.ToLower(attribute.Key)
			if name == "srcset" {
				if rewritten, ok := rewriteSrcset(attribute.Val, resolverBase, repository, upstream, repositoryBase, auxiliarySigningKey); ok {
					attribute.Val = rewritten
					tokenChanged = true
				}
				continue
			}
			if !browsableURLAttributes[name] {
				continue
			}
			resolved := resolveBrowsableURL(attribute.Val, resolverBase)
			if strings.EqualFold(token.Data, "base") && name == "href" && !baseSeen {
				baseSeen = true
				if resolved != nil {
					resolverBase = cloneURL(resolved)
				}
			}
			if resolved == nil {
				continue
			}
			if rewritten, ok := mapBrowsableURL(repository, upstream, repositoryBase, resolved, auxiliarySigningKey); ok {
				attribute.Val = rewritten
				tokenChanged = true
			}
		}
		if tokenChanged {
			_, _ = output.WriteString(token.String())
			changed = true
		} else {
			_, _ = output.Write(tokenizer.Raw())
		}
	}
	if !changed {
		return source, false, nil
	}
	return output.Bytes(), true, nil
}

func effectiveRepositoryBaseURL(repository model.Mirror, upstream model.Upstream) (*url.URL, error) {
	base, err := url.Parse(upstream.URL)
	if err != nil || base.Scheme == "" || base.Hostname() == "" {
		return nil, errors.New("invalid repository upstream URL")
	}
	if authority := repositoryLogicalAuthority(repository, upstream); authority != "" {
		base.Host = authority
	}
	if repository.AddPrefix != "" {
		base.Path = joinPath(base.Path, repository.AddPrefix)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/"
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base, nil
}

func resolveBrowsableURL(raw string, base *url.URL) *url.URL {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "#") || base == nil {
		return nil
	}
	reference, err := url.Parse(value)
	if err != nil || reference.Opaque != "" || reference.User != nil {
		return nil
	}
	target := base.ResolveReference(reference)
	if target.User != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" {
		return nil
	}
	return target
}

func mapBrowsableURL(repository model.Mirror, upstream model.Upstream, repositoryBase, target *url.URL, auxiliarySigningKey []byte) (string, bool) {
	if repository.ID <= 0 || !sameOrigin(repositoryBase, target) || containsEncodedPathSeparator(target.EscapedPath()) {
		return "", false
	}
	relative, inside := pathWithinRepositoryBase(repositoryBase.Path, target.Path)
	var mappedPath, mappedRawPath string
	if inside {
		mappedPath = publicRepositoryResourcePath(repository, relative)
		if rawRelative, rawInside := pathWithinRepositoryBase(repositoryBase.EscapedPath(), target.EscapedPath()); rawInside {
			mappedRawPath = publicRepositoryResourceRawPath(repository, rawRelative)
		}
	} else {
		if upstream.ID <= 0 || len(auxiliarySigningKey) < 32 {
			return "", false
		}
		signature := base64.RawURLEncoding.EncodeToString(auxiliaryURLSignature(auxiliarySigningKey, repository, upstream, target))
		encodedPath := base64.RawURLEncoding.EncodeToString([]byte(target.EscapedPath()))
		mappedPath = strings.TrimRight(auxiliaryUpstreamPrefix, "/") + "/" + strconv.FormatInt(repository.ID, 10) + "/" +
			strconv.FormatInt(upstream.ID, 10) + "/" + signature + "/" + encodedPath
		mappedRawPath = ""
	}
	mapped := &url.URL{Path: mappedPath, RawPath: mappedRawPath, RawQuery: target.RawQuery, ForceQuery: target.ForceQuery, Fragment: target.Fragment}
	return mapped.String(), true
}

func containsEncodedPathSeparator(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c")
}

func pathWithinRepositoryBase(basePath, targetPath string) (string, bool) {
	if basePath == "" {
		basePath = "/"
	}
	if targetPath == "" {
		targetPath = "/"
	}
	root := strings.TrimSuffix(basePath, "/")
	if targetPath == root || targetPath == basePath {
		return "/", true
	}
	prefix := root + "/"
	if !strings.HasPrefix(targetPath, prefix) {
		return "", false
	}
	return ensureLeadingSlash(strings.TrimPrefix(targetPath, prefix)), true
}

func publicRepositoryResourcePath(repository model.Mirror, relative string) string {
	prefix := "/"
	if repository.PublicMode != "host" {
		prefix = repository.PublicPath
		if prefix == "" {
			prefix = "/" + repository.Slug + "/"
		}
	}
	if stripped := strings.Trim(repository.StripPrefix, "/"); stripped != "" {
		prefix = strings.TrimRight(prefix, "/") + "/" + stripped + "/"
	}
	root := strings.TrimRight(prefix, "/")
	if relative == "" || relative == "/" {
		if root == "" {
			return "/"
		}
		return root + "/"
	}
	return root + ensureLeadingSlash(relative)
}

func publicRepositoryResourceRawPath(repository model.Mirror, relative string) string {
	root := (&url.URL{Path: publicRepositoryResourcePath(repository, "/")}).EscapedPath()
	if relative == "" || relative == "/" {
		return root
	}
	return strings.TrimRight(root, "/") + ensureLeadingSlash(relative)
}

func rewriteSrcset(value string, base *url.URL, repository model.Mirror, upstream model.Upstream, repositoryBase *url.URL,
	auxiliarySigningKey []byte) (string, bool) {
	parts := parseSrcsetCandidates(value)
	changed := false
	for index := range parts {
		if strings.HasPrefix(strings.ToLower(parts[index].url), "data:") {
			continue
		}
		resolved := resolveBrowsableURL(parts[index].url, base)
		if resolved == nil {
			continue
		}
		if rewritten, ok := mapBrowsableURL(repository, upstream, repositoryBase, resolved, auxiliarySigningKey); ok {
			parts[index].url = rewritten
			changed = true
		}
	}
	if !changed {
		return value, false
	}
	rendered := make([]string, 0, len(parts))
	for _, part := range parts {
		candidate := part.url
		if part.descriptor != "" {
			candidate += " " + part.descriptor
		}
		rendered = append(rendered, candidate)
	}
	return strings.Join(rendered, ", "), true
}

type srcsetCandidate struct {
	url        string
	descriptor string
}

func parseSrcsetCandidates(value string) []srcsetCandidate {
	isSpace := func(value byte) bool {
		return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
	}
	var candidates []srcsetCandidate
	for index := 0; index < len(value); {
		for index < len(value) && (isSpace(value[index]) || value[index] == ',') {
			index++
		}
		if index >= len(value) {
			break
		}
		start := index
		// The URL token is delimited by ASCII whitespace, not by every comma:
		// data URLs contain an internal comma. Trailing commas terminate a
		// descriptor-less candidate and are removed below, matching the HTML
		// srcset parsing algorithm.
		for index < len(value) && !isSpace(value[index]) {
			index++
		}
		candidateURL := value[start:index]
		trailingCommas := 0
		for strings.HasSuffix(candidateURL, ",") {
			candidateURL = strings.TrimSuffix(candidateURL, ",")
			trailingCommas++
		}
		candidateURL = strings.TrimSpace(candidateURL)
		if candidateURL == "" {
			continue
		}
		if trailingCommas > 0 {
			candidates = append(candidates, srcsetCandidate{url: candidateURL})
			continue
		}
		for index < len(value) && isSpace(value[index]) {
			index++
		}
		descriptorStart := index
		parentheses := 0
		for index < len(value) {
			if value[index] == '(' {
				parentheses++
			} else if value[index] == ')' && parentheses > 0 {
				parentheses--
			} else if value[index] == ',' && parentheses == 0 {
				break
			}
			index++
		}
		descriptor := strings.TrimSpace(value[descriptorStart:index])
		if candidateURL != "" {
			candidates = append(candidates, srcsetCandidate{url: candidateURL, descriptor: descriptor})
		}
		if index < len(value) {
			index++
		}
	}
	return candidates
}

type htmlRewriteTooLargeError struct{ limit int64 }

func (e *htmlRewriteTooLargeError) Error() string {
	return "browsable HTML rewrite exceeds " + strconv.FormatInt(e.limit, 10) + " bytes"
}

type unexpectedHTMLEncodingError struct{ encoding string }

func (e *unexpectedHTMLEncodingError) Error() string {
	return "browsable HTML upstream ignored identity encoding request and returned " + e.encoding
}
