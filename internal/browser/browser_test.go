package browser

import (
	"strings"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestParseNginxDirectoryIndex(t *testing.T) {
	nginxHTML := `<html>
<head><title>Index of /debian/</title></head>
<body>
<h1>Index of /debian/</h1><hr><pre><a href="../">../</a>
<a href="dists/">dists/</a>                                             20-Jun-2023 15:42                   -
<a href="doc/">doc/</a>                                               08-Feb-2024 10:15                   -
<a href="pool/">pool/</a>                                              20-Jun-2023 15:42                   -
<a href="README">README</a>                                             23-May-2023 11:21                1234
</pre><hr></body>
</html>`

	listing, ok := ParseDirectoryIndex([]byte(nginxHTML), "/debian/")
	if !ok {
		t.Fatalf("expected ParseDirectoryIndex to succeed")
	}
	if len(listing.Entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(listing.Entries))
	}
	if !listing.Entries[0].IsParent {
		t.Errorf("expected first entry to be parent")
	}
	if listing.Entries[1].Name != "dists/" || !listing.Entries[1].IsDir || listing.Entries[1].IconType != "folder" {
		t.Errorf("unexpected entry 1: %+v", listing.Entries[1])
	}
	if listing.Entries[4].Name != "README" || listing.Entries[4].IsDir || listing.Entries[4].Size != "1234" {
		t.Errorf("unexpected entry 4: %+v", listing.Entries[4])
	}
}

func TestParseApacheTableDirectoryIndex(t *testing.T) {
	apacheHTML := `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 3.2 Final//EN">
<html>
 <head>
  <title>Index of /ubuntu</title>
 </head>
 <body>
<h1>Index of /ubuntu</h1>
  <table>
   <tr><th valign="top"><img src="/icons/blank.gif" alt="[ICO]"></th><th><a href="?C=N;O=D">Name</a></th><th><a href="?C=M;O=A">Last modified</a></th><th><a href="?C=S;O=A">Size</a></th><th><a href="?C=D;O=A">Description</a></th></tr>
   <tr><th colspan="5"><hr></th></tr>
<tr><td valign="top"><img src="/icons/back.gif" alt="[PARENTDIR]"></td><td><a href="/">Parent Directory</a></td><td>&nbsp;</td><td align="right">  - </td><td>&nbsp;</td></tr>
<tr><td valign="top"><img src="/icons/folder.gif" alt="[DIR]"></td><td><a href="dists/">dists/</a></td><td align="right">2024-04-20 18:30  </td><td align="right">  - </td><td>&nbsp;</td></tr>
<tr><td valign="top"><img src="/icons/text.gif" alt="[TXT]"></td><td><a href="ls-lR.gz">ls-lR.gz</a></td><td align="right">2024-04-20 18:35  </td><td align="right"> 24M</td><td>&nbsp;</td></tr>
   <tr><th colspan="5"><hr></th></tr>
</table>
</body></html>`

	listing, ok := ParseDirectoryIndex([]byte(apacheHTML), "/ubuntu/")
	if !ok {
		t.Fatalf("expected ParseDirectoryIndex to succeed")
	}
	if len(listing.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(listing.Entries))
	}
	if !listing.Entries[0].IsParent {
		t.Errorf("expected entry 0 to be parent")
	}
	if listing.Entries[2].Name != "ls-lR.gz" || listing.Entries[2].IconType != "archive" {
		t.Errorf("unexpected entry 2: %+v", listing.Entries[2])
	}
}

func TestRenderHTML(t *testing.T) {
	listing := &ParsedListing{
		Title:       "Index of /debian/",
		CurrentPath: "/debian/",
		Entries: []DirEntry{
			{Name: "dists/", Href: "dists/", IsDir: true, ModTime: "2024-01-01", IconType: "folder"},
			{Name: "package.deb", Href: "package.deb", IsDir: false, Size: "1.5M", IconType: "package"},
		},
	}
	repo := model.Mirror{
		Name: "Debian",
		Slug: "debian",
		Help: model.HelpConfig{Enabled: true},
	}

	htmlOut := RenderHTML(listing, repo, "/debian/", model.BrandingConfig{Title: "Test Mirrors"}, "dark", "#2563eb", false)
	if !strings.Contains(htmlOut, "Index of /debian/") {
		t.Errorf("missing title in rendered HTML")
	}
	if !strings.Contains(htmlOut, "package.deb") {
		t.Errorf("missing file entry in rendered HTML")
	}
	if !strings.Contains(htmlOut, "data-theme=\"dark\"") {
		t.Errorf("missing dark theme in rendered HTML")
	}
	if strings.Contains(htmlOut, "/ui/base.css") {
		t.Error("rendered browser must not load an undefined or origin-controlled base stylesheet")
	}
}

func TestDirectoryBrowserRejectsUnsafeUpstreamLinks(t *testing.T) {
	body := []byte(`<html><head><title>Index of /</title></head><body><table id="indexlist">
<tr><td><a href="package.deb">package.deb</a></td></tr>
<tr><td><a href="javascript:alert(1)">malicious</a></td></tr>
<tr><td><a href="//evil.example/file">external</a></td></tr>
</table></body></html>`)
	listing, ok := ParseDirectoryIndex(body, "/")
	if !ok || len(listing.Entries) != 1 || listing.Entries[0].Href != "package.deb" {
		t.Fatalf("unsafe directory links were retained: ok=%v listing=%+v", ok, listing)
	}

	listing.Entries = append(listing.Entries, DirEntry{Name: "injected", Href: "data:text/html,unsafe"})
	rendered := RenderHTML(listing, model.Mirror{Name: "test"}, "/", model.BrandingConfig{}, "system", "#2563eb", false)
	if strings.Contains(rendered, "data:text/html") || strings.Contains(rendered, "javascript:") || strings.Contains(rendered, "evil.example") {
		t.Fatalf("generated browser page retained an unsafe link: %s", rendered)
	}
}
